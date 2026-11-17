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

// binder gives the type checker a view's namespace. Receiver members are
// represented by local objects in the stub View's scope. Uses of those
// objects are qualified after checking; Go itself resolves all declarations,
// shadowing, composite literal keys and control-flow scopes.
type binder struct {
	pkg          *types.Package
	scope        *types.Scope
	fs           *token.FileSet
	pos          token.Pos
	members      map[string]types.Object
	replacements map[types.Object]string
	useImport    func(string, string) string
}

func newBinder(recv *types.Named, pkg *types.Package, fs *token.FileSet, pos token.Pos, scope *types.Scope, aliases map[string]*types.Package, receiver, context string, useImport func(string, string) string) (*binder, error) {
	b := &binder{pkg: pkg, scope: scope, fs: fs, pos: pos, members: map[string]types.Object{}, replacements: map[types.Object]string{}, useImport: useImport}
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
	}
	for name, p := range aliases {
		if name == "_" || name == "." || scope.Lookup(name) != nil {
			continue
		}
		scope.Insert(types.NewPkgName(token.NoPos, pkg, name, p))
	}
	// The emitted names can differ from the public expression names s/c
	// when a user closure declares a local with one of those names. A
	// name a member already took must not be picked: nothing may quietly
	// fail to enter the scope.
	// Scope.Insert returns the object that kept the name when it had
	// one already, and nil when the new object entered: a name a member
	// already took must not be picked, quietly failing to enter.
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

// memberNames are the receiver's members as the binder binds them: its
// own fields and methods, and those its embedded structs promote.
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
	// unsafe.Offsetof asks for a selector by its form: a bare member
	// name, which the checker reads as a plain variable, is written as
	// the receiver's field in the source text itself, so that what is
	// checked and what is emitted are one tree (#1457).
	if qualified, changed := b.qualifyOffsetof(src, e); changed {
		src = qualified
		if e, err = parser.ParseExprFrom(b.fs, "view-expression", src, 0); err != nil {
			return "", nil, Diagnostic{Pos: pos, Path: path, Msg: fmt.Sprintf("not Go %s: %v", kind, err)}
		}
	}
	info := &types.Info{Uses: map[*ast.Ident]types.Object{}, Types: map[ast.Expr]types.TypeAndValue{}}
	if err := types.CheckExpr(b.fs, b.pkg, b.pos, e, info); err != nil {
		return "", nil, Diagnostic{Pos: pos, Path: path, Msg: err.Error()}
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
	// Reparse rather than putting selectors into Ident.Name: the returned
	// tree is ordinary Go AST, usable by later type checking as well.
	e, err = parser.ParseExprFrom(b.fs, "bound-view-expression", src, 0)
	if err != nil {
		return "", nil, Diagnostic{Pos: pos, Path: path, Msg: err.Error()}
	}
	var out strings.Builder
	formatNode(b.fs, e, &out)
	return out.String(), e, nil
}

// eval checks an already-bound expression in the same stub View scope.
func (b *binder) eval(src string) (types.TypeAndValue, error) {
	return types.Eval(b.fs, b.pkg, b.pos, src)
}

// declaredNames conservatively reserves identifiers declared in Go snippets
// before choosing generated parameter and import names. It needs no scope
// semantics: over-reserving a name merely chooses another generated name.
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

// qualifyOffsetof rewrites the argument of each unsafe.Offsetof call
// that is a bare name of a receiver member into the receiver's field of
// it, in the expression's source text: the builtin asks for a selector
// by its form, which a bare member name is not until it is qualified
// (#1457). It reports the rewritten source and whether it changed.
func (b *binder) qualifyOffsetof(src string, e ast.Expr) (string, bool) {
	sName := "s"
	if obj := b.scope.Lookup("s"); obj != nil {
		sName = obj.Name()
	}
	file := b.fs.File(e.Pos())
	if file == nil {
		return src, false
	}
	// ParseExprFrom wraps the source in a file of its own: the offsets
	// into src are the tree's, less the expression's own start.
	base := file.Offset(e.Pos())
	type span struct{ start, end int }
	var spans []span
	ast.Inspect(e, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return true
		}
		fun, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || fun.Sel.Name != "Offsetof" {
			return true
		}
		if id, ok := fun.X.(*ast.Ident); !ok || id.Name != "unsafe" {
			return true
		}
		arg, ok := call.Args[0].(*ast.Ident)
		if !ok || b.members[arg.Name] == nil {
			return true
		}
		spans = append(spans, span{file.Offset(arg.Pos()) - base, 0})
		return true
	})
	if len(spans) == 0 {
		return src, false
	}
	// The receiver's name goes before each member's: an insertion, from
	// the end so the earlier offsets stay true.
	for i := len(spans) - 1; i >= 0; i-- {
		at := spans[i].start
		src = src[:at] + sName + "." + src[at:]
	}
	return src, true
}
