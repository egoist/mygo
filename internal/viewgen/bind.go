package viewgen

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
)

// binder rewrites the Go of expressions and handler bodies from a .view
// file's namespace into the generated method's: unqualified names that
// name a member of the receiver become s.Name, everything else stays.
// The rewriting is scope-aware: locals of the expression itself shadow
// the receiver, and struct literal keys and selectors' fields are not
// names at all.
type binder struct {
	// members are the receiver's fields and methods, by name.
	members map[string]bool
	// pkgNames are the package's own top-level names.
	pkgNames map[string]bool
}

func newBinder(recv *types.Named, scope *types.Scope) *binder {
	b := &binder{members: map[string]bool{}, pkgNames: map[string]bool{}}
	if st, ok := recv.Underlying().(*types.Struct); ok {
		for i := 0; i < st.NumFields(); i++ {
			b.members[st.Field(i).Name()] = true
		}
	}
	for i := 0; i < recv.NumMethods(); i++ {
		b.members[recv.Method(i).Name()] = true
	}
	if scope != nil {
		for _, name := range scope.Names() {
			b.pkgNames[name] = true
		}
	}
	return b
}

// expr rewrites one Go expression, returning its source.
func (b *binder) expr(src string, pos Position, viewPath string) (string, error) {
	e, err := parser.ParseExpr(src)
	if err != nil {
		return "", Diagnostic{Pos: pos, Path: viewPath, Msg: fmt.Sprintf("not a Go expression: %v", err)}
	}
	r := &rewriter{binder: b, bound: map[string]bool{}}
	ast.Inspect(e, r.visit)
	var sb strings.Builder
	formatNode(token.NewFileSet(), e, &sb)
	return sb.String(), nil
}

// body rewrites a handler body: Go statements.
func (b *binder) body(src string, pos Position, viewPath string) (string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "handler.go", "package p\nfunc _(){"+src+"\n}", 0)
	if err != nil {
		return "", Diagnostic{Pos: pos, Path: viewPath, Msg: fmt.Sprintf("not Go statements: %v", err)}
	}
	fn := f.Decls[0].(*ast.FuncDecl)
	r := &rewriter{binder: b, bound: map[string]bool{}}
	for _, stmt := range fn.Body.List {
		ast.Inspect(stmt, r.visit)
	}
	var sb strings.Builder
	for _, stmt := range fn.Body.List {
		formatNode(fset, stmt, &sb)
	}
	out := sb.String()
	out = strings.TrimSuffix(out, "\n")
	if strings.Contains(out, "\n") {
		// A block of statements keeps its own lines, indented one tab by
		// the caller.
		return out, nil
	}
	return out, nil
}

// rewriter walks one expression or statement tree, rewriting receiver
// members to s.Name.
type rewriter struct {
	*binder
	bound map[string]bool
	depth int
}

// visit rewrites in place: an ident naming a receiver member and nothing
// in an inner scope becomes a selector on s.
func (r *rewriter) visit(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.SelectorExpr:
		// The field itself is never a name; the base may be.
		ast.Inspect(n.X, r.visit)
		return false
	case *ast.KeyValueExpr:
		if _, isIdent := n.Key.(*ast.Ident); isIdent {
			// A composite literal field name, or a map key spelled bare:
			// not a name the receiver owns. Values are still walked.
			ast.Inspect(n.Value, r.visit)
			return false
		}
	case *ast.FuncLit:
		inner := &rewriter{binder: r.binder, bound: map[string]bool{}}
		for _, p := range n.Type.Params.List {
			for _, id := range p.Names {
				inner.bound[id.Name] = true
			}
		}
		for _, stmt := range n.Body.List {
			ast.Inspect(stmt, inner.visit)
		}
		return false
	case *ast.AssignStmt:
		// Short declarations bind their names for the rest of the
		// statements: bind after walking the values.
		for _, v := range n.Rhs {
			ast.Inspect(v, r.visit)
		}
		if n.Tok == token.DEFINE {
			for _, id := range n.Lhs {
				if ident, ok := id.(*ast.Ident); ok {
					r.bound[ident.Name] = true
				}
			}
		} else {
			for _, id := range n.Lhs {
				ast.Inspect(id, r.visit)
			}
		}
		return false
	case *ast.RangeStmt:
		inner := &rewriter{binder: r.binder, bound: map[string]bool{}}
		for _, id := range []*ast.Ident{} {
			_ = id
		}
		ast.Inspect(n.X, inner.visit)
		if n.Key != nil {
			if id, ok := n.Key.(*ast.Ident); ok && id.Name != "_" {
				inner.bound[id.Name] = true
			}
		}
		if n.Value != nil {
			if id, ok := n.Value.(*ast.Ident); ok && id.Name != "_" {
				inner.bound[id.Name] = true
			}
		}
		for _, stmt := range n.Body.List {
			ast.Inspect(stmt, inner.visit)
		}
		return false
	case *ast.Ident:
		if n.Name == "_" || r.bound[n.Name] || !r.members[n.Name] {
			return true
		}
		// A receiver member: s.Name. The ident is replaced by writing the
		// selector into the tree.
		n.Name = "s." + n.Name
		return true
	}
	return true
}
