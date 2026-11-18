package viewgen

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strings"
)

// binder resolves view names through Go's lexical scopes.
type binder struct {
	pkg           *types.Package
	scope         *types.Scope
	fs            *token.FileSet
	pos           token.Pos
	members       map[string]types.Object
	replacements  map[types.Object]string
	fieldBindings map[types.Object]string
	useImport     func(string, string) string
}

func newBinder(recv *types.Named, pkg *types.Package, fs *token.FileSet, pos token.Pos, scope *types.Scope, aliases map[string]*types.Package, receiver, context string, useImport func(string, string) string) (*binder, error) {
	b := &binder{pkg: pkg, scope: scope, fs: fs, pos: pos, members: map[string]types.Object{}, replacements: map[types.Object]string{}, fieldBindings: map[types.Object]string{}, useImport: useImport}
	b.replacements[scope.Lookup("s")] = receiver
	b.replacements[scope.Lookup("c")] = context
	for name, obj := range memberNames(recv, pkg) {
		b.members[name] = obj
		if name == "s" || name == "c" {
			continue
		}
		proxy := types.NewVar(token.NoPos, pkg, name, obj.Type())
		scope.Insert(proxy)
		b.replacements[proxy] = receiver + "." + name
		if _, field := obj.(*types.Var); field {
			b.fieldBindings[proxy] = receiver + "." + name
		}
	}
	for name, p := range aliases {
		if name == "_" || name == "." || scope.Lookup(name) != nil {
			continue
		}
		scope.Insert(types.NewPkgName(token.NoPos, pkg, name, p))
	}
	// Renamed parameters must also be available during expression checks.
	if receiver != "s" {
		if alt := scope.Insert(types.NewVar(token.NoPos, pkg, receiver, types.NewPointer(recv))); alt != nil {
			return nil, fmt.Errorf("the receiver's name %s is a member's (%s)", receiver, alt.Name())
		}
	}
	if context != "c" {
		if alt := scope.Insert(types.NewVar(token.NoPos, pkg, context, scope.Lookup("c").Type())); alt != nil {
			return nil, fmt.Errorf("the context's name %s is a member's (%s)", context, alt.Name())
		}
	}
	return b, nil
}

// memberNames includes direct and promoted fields and methods.
func memberNames(recv *types.Named, pkg *types.Package) map[string]types.Object {
	names := map[string]bool{}
	var fields func(types.Type)
	seen := map[types.Type]bool{}
	fields = func(t types.Type) {
		if p, ok := t.(*types.Pointer); ok {
			t = p.Elem()
		}
		if seen[t] {
			return
		}
		seen[t] = true
		if st, ok := t.Underlying().(*types.Struct); ok {
			for i := 0; i < st.NumFields(); i++ {
				f := st.Field(i)
				names[f.Name()] = true
				if f.Embedded() {
					fields(f.Type())
				}
			}
		}
	}
	fields(recv)
	methods := types.NewMethodSet(types.NewPointer(recv))
	for i := 0; i < methods.Len(); i++ {
		names[methods.At(i).Obj().Name()] = true
	}
	out := map[string]types.Object{}
	for name := range names {
		obj, _, _ := types.LookupFieldOrMethod(types.NewPointer(recv), true, pkg, name)
		if obj != nil {
			out[name] = obj
		}
	}
	return out
}

func (b *binder) expr(src string, pos Position, path string) (string, ast.Expr, error) {
	return b.rewrite(src, pos, path, false)
}

func (b *binder) body(src string, pos Position, path string) (string, []ast.Stmt, error) {
	_, e, err := b.rewrite("func(){"+src+"\n}", pos, path, true)
	if err != nil {
		return "", nil, err
	}
	fn := e.(*ast.FuncLit)
	var out strings.Builder
	for _, stmt := range fn.Body.List {
		formatNode(b.fs, stmt, &out)
		out.WriteByte('\n')
	}
	return strings.TrimSuffix(out.String(), "\n"), fn.Body.List, nil
}

func (b *binder) rewrite(src string, pos Position, path string, body bool) (string, ast.Expr, error) {
	e, err := parser.ParseExprFrom(b.fs, "view-expression", src, 0)
	kind := "expression"
	if body {
		kind = "statements"
	}
	if err != nil {
		return "", nil, Diagnostic{Pos: pos, Path: path, Msg: fmt.Sprintf("not Go %s: %v", kind, err)}
	}
	info := &types.Info{Uses: map[*ast.Ident]types.Object{}, Types: map[ast.Expr]types.TypeAndValue{}, Scopes: map[ast.Node]*types.Scope{}}
	if checkErr := types.CheckExpr(b.fs, b.pkg, b.pos, e, info); checkErr != nil {
		return "", nil, Diagnostic{Pos: pos, Path: path, Msg: checkErr.Error()}
	}
	type edit struct {
		start, end int
		text       string
	}
	var edits []edit
	file := b.fs.File(e.Pos())
	ids := make([]*ast.Ident, 0, len(info.Uses))
	for id := range info.Uses {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].Pos() < ids[j].Pos() })
	for _, id := range ids {
		obj := info.Uses[id]
		replacement, ok := b.replacements[obj]
		if !ok {
			if p, yes := obj.(*types.PkgName); yes {
				replacement = b.useImport(p.Imported().Path(), p.Name())
				ok = true
			}
		}
		if ok && replacement != id.Name {
			edits = append(edits, edit{file.Offset(id.Pos()), file.Offset(id.End()), replacement})
		}
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, edit := range edits {
		src = src[:edit.start] + edit.text + src[edit.end:]
	}
	e, err = parser.ParseExprFrom(b.fs, "bound-view-expression", src, 0)
	if err != nil {
		return "", nil, Diagnostic{Pos: pos, Path: path, Msg: err.Error()}
	}
	var out strings.Builder
	formatNode(b.fs, e, &out)
	return out.String(), e, nil
}

func (b *binder) eval(src string) (types.TypeAndValue, error) {
	return types.Eval(b.fs, b.pkg, b.pos, src)
}

// declaredNames reserves snippet declarations to avoid generated-name collisions.
func declaredNames(f *File) map[string]bool {
	names := map[string]bool{}
	add := func(ids []*ast.Ident) {
		for _, id := range ids {
			names[id.Name] = true
		}
	}
	var visit func(*Node)
	visit = func(n *Node) {
		parts := append([]TextPart(nil), n.Text...)
		for _, a := range n.Attrs {
			parts = append(parts, a.Value...)
		}
		for _, p := range parts {
			if !p.IsExpr {
				continue
			}
			src := "package p\nfunc _(){" + p.Expr + "\n}"
			tree, err := parser.ParseFile(token.NewFileSet(), "view-names", src, 0)
			if err != nil {
				tree, err = parser.ParseFile(token.NewFileSet(), "view-names", "package p\nvar _ = "+p.Expr, 0)
			}
			if err != nil {
				continue
			}
			ast.Inspect(tree, func(node ast.Node) bool {
				switch v := node.(type) {
				case *ast.Field:
					add(v.Names)
				case *ast.ValueSpec:
					add(v.Names)
				case *ast.TypeSpec:
					names[v.Name.Name] = true
				case *ast.AssignStmt:
					if v.Tok == token.DEFINE {
						for _, lhs := range v.Lhs {
							if id, ok := lhs.(*ast.Ident); ok {
								names[id.Name] = true
							}
						}
					}
				case *ast.RangeStmt:
					if v.Tok == token.DEFINE {
						for _, lhs := range []ast.Expr{v.Key, v.Value} {
							if id, ok := lhs.(*ast.Ident); ok {
								names[id.Name] = true
							}
						}
					}
				}
				return true
			})
		}
		for _, child := range n.Children {
			visit(child)
		}
	}
	for _, root := range f.Roots {
		visit(root)
	}
	return names
}
