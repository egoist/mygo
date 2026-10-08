// Package uimigrate migrates source syntax to the UI's checked value API.
// It resolves the actual MyGo import alias and local declaration types;
// it never performs global text replacements.
package uimigrate

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
)

// Result is a source migration and any changes requiring human review.
type Result struct {
	Source  []byte
	Changed bool
	Notes   []string
}

type migration struct {
	alias   string
	kinds   map[*ast.Object]string
	structs map[string]map[string]string
	objects map[*ast.Object]string
	notes   []string
}

// File transforms one Go file. Dot imports are reported for manual review.
func File(filename string, source []byte) (Result, error) {
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, filename, source, parser.ParseComments)
	if err != nil {
		return Result{}, err
	}
	m := &migration{kinds: map[*ast.Object]string{}, structs: map[string]map[string]string{}, objects: map[*ast.Object]string{}}
	for _, im := range f.Imports {
		if strings.Trim(im.Path.Value, "\"") == "github.com/egoist/mygo/ui" {
			m.alias = "ui"
			if im.Name != nil {
				m.alias = im.Name.Name
			}
		}
	}
	if m.alias == "" || m.alias == "_" {
		return Result{Source: source}, nil
	}
	if m.alias == "." {
		return Result{Source: source, Notes: []string{"replace the dot import with an explicit MyGo UI alias before migrating"}}, nil
	}
	m.collect(f)
	rewriteTypes(reflect.ValueOf(f), m)
	ast.Walk(visitor{m: m}, f)
	var b bytes.Buffer
	if err := format.Node(&b, fs, f); err != nil {
		return Result{}, err
	}
	formatted, err := format.Source(b.Bytes())
	if err != nil {
		return Result{}, err
	}
	changed := !bytes.Equal(source, formatted)
	return Result{Source: formatted, Changed: changed, Notes: m.notes}, nil
}

func (m *migration) uiType(e ast.Expr) string {
	if star, ok := e.(*ast.StarExpr); ok {
		e = star.X
	}
	if ix, ok := e.(*ast.IndexExpr); ok {
		e = ix.X
	}
	if ix, ok := e.(*ast.IndexListExpr); ok {
		e = ix.X
	}
	if s, ok := e.(*ast.SelectorExpr); ok {
		if p, ok := s.X.(*ast.Ident); ok && p.Name == m.alias {
			switch s.Sel.Name {
			case "Context", "Frame":
				return "Frame"
			case "Element":
				return "Element"
			case "SelectParts", "ComboboxParts", "TabsParts", "CollapsibleParts", "SegmentedParts", "ToastParts":
				return s.Sel.Name
			}
		}
	}
	return ""
}

func (m *migration) collect(f *ast.File) {
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.TypeSpec:
			if s, ok := n.Type.(*ast.StructType); ok {
				fields := map[string]string{}
				for _, field := range s.Fields.List {
					if k := m.uiType(field.Type); k != "" {
						for _, id := range field.Names {
							fields[id.Name] = k
						}
						m.notes = append(m.notes, fmt.Sprintf("review stored %s fields on %s; use Ref for persistent control identity", k, n.Name.Name))
					}
				}
				m.structs[n.Name.Name] = fields
			}
		case *ast.Field:
			k := m.uiType(n.Type)
			for _, id := range n.Names {
				if k != "" && id.Obj != nil {
					m.kinds[id.Obj] = k
				}
				typ := n.Type
				if s, ok := typ.(*ast.StarExpr); ok {
					typ = s.X
				}
				if name, ok := typ.(*ast.Ident); ok && id.Obj != nil {
					m.objects[id.Obj] = name.Name
				}
			}
		case *ast.ValueSpec:
			k := m.uiType(n.Type)
			for _, id := range n.Names {
				if k != "" && id.Obj != nil {
					m.kinds[id.Obj] = k
				}
			}
		}
		return true
	})
	for range 3 {
		ast.Inspect(f, func(n ast.Node) bool {
			if a, ok := n.(*ast.AssignStmt); ok {
				for i, lhs := range a.Lhs {
					if i >= len(a.Rhs) {
						break
					}
					if id, ok := lhs.(*ast.Ident); ok && id.Obj != nil {
						if k := m.kind(a.Rhs[i]); k != "" {
							m.kinds[id.Obj] = k
						}
					}
				}
			}
			return true
		})
	}
}

var queries = map[string]bool{"Clicked": true, "Clicks": true, "Changed": true, "Submitted": true, "Hovered": true, "Pressed": true, "Focused": true, "FocusVisible": true, "FocusWithin": true, "Valid": true, "Shortcut": true, "ID": true, "Bounds": true, "IsDisabled": true, "PointerPosition": true, "Dragged": true, "TextSelection": true, "Composing": true, "DroppedFiles": true, "FileDragOver": true, "Dragging": true, "ClickModifiers": true, "DoubleClicked": true, "RightClicked": true, "PressedOutside": true, "Highlighted": true, "Loop": true, "Animate": true, "AnimateWith": true}
var nonElements = map[string]bool{"View": true, "NewTester": true, "Render": true, "Local": true, "RGB": true, "RGBA": true, "Hex": true, "LightTheme": true, "DarkTheme": true, "ParseSVG": true, "MustParseSVG": true, "NewBitmap": true, "DecodeBitmap": true, "Shape": true, "ShapeText": true, "ShapeRichText": true, "NewRouter": true, "NewTextBuffer": true, "Fixed": true, "Fr": true, "FitContent": true, "Overlay": true, "Drop": true, "DragOver": true, "DropData": true, "DataDragOver": true, "RegisterFont": true, "AlertDialog": true}

func (m *migration) kind(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.Ident:
		return m.kinds[e.Obj]
	case *ast.SelectorExpr:
		if kind := m.kind(e.X); kind == "Parts" || strings.HasSuffix(kind, "Parts") {
			switch e.Sel.Name {
			case "Trigger", "Input", "Track", "List", "Root":
				return "Element"
			}
		}
		if id, ok := e.X.(*ast.Ident); ok {
			return m.structs[m.objects[id.Obj]][e.Sel.Name]
		}
	case *ast.CallExpr:
		fn := e.Fun
		if ix, ok := fn.(*ast.IndexExpr); ok {
			fn = ix.X
		}
		if ix, ok := fn.(*ast.IndexListExpr); ok {
			fn = ix.X
		}
		if s, ok := fn.(*ast.SelectorExpr); ok {
			if p, ok := s.X.(*ast.Ident); ok && p.Name == m.alias && !nonElements[s.Sel.Name] {
				if strings.HasSuffix(s.Sel.Name, "Base") && (s.Sel.Name == "SelectBase" || s.Sel.Name == "TabsBase" || s.Sel.Name == "ComboboxBase" || s.Sel.Name == "CollapsibleBase" || s.Sel.Name == "SegmentedBase" || s.Sel.Name == "ToastBase") {
					return "Parts"
				}
				return "Element"
			}
			if m.kind(s.X) == "Element" && !queries[s.Sel.Name] {
				return "Element"
			}
			if m.kind(s.X) == "Frame" && s.Sel.Name == "Root" {
				return "Element"
			}
		}
	}
	return ""
}

var exprType = reflect.TypeOf((*ast.Expr)(nil)).Elem()

func rewriteTypes(v reflect.Value, m *migration) {
	if !v.IsValid() {
		return
	}
	if v.Kind() == reflect.Interface {
		if v.IsNil() {
			return
		}
		if v.Type() == exprType && v.CanSet() {
			if star, ok := v.Interface().(*ast.StarExpr); ok {
				if k := m.uiType(star); k != "" {
					if s, ok := star.X.(*ast.SelectorExpr); ok {
						s.Sel.Name = k
					}
					v.Set(reflect.ValueOf(ast.Expr(star.X)))
					return
				}
			}
			if s, ok := v.Interface().(*ast.SelectorExpr); ok {
				if k := m.uiType(s); k != "" {
					s.Sel.Name = k
				}
			}
		}
		rewriteTypes(v.Elem(), m)
		return
	}
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return
		}
		// Object/scope links form cycles and are not syntax children.
		if v.Type() == reflect.TypeOf((*ast.Object)(nil)) || v.Type() == reflect.TypeOf((*ast.Scope)(nil)) {
			return
		}
		rewriteTypes(v.Elem(), m)
		return
	}
	if v.Kind() == reflect.Struct {
		for i := 0; i < v.NumField(); i++ {
			rewriteTypes(v.Field(i), m)
		}
	}
	if v.Kind() == reflect.Slice {
		for i := 0; i < v.Len(); i++ {
			rewriteTypes(v.Index(i), m)
		}
	}
}

type visitor struct {
	m       *migration
	frame   string
	returns []string
}

func (v visitor) Visit(n ast.Node) ast.Visitor {
	if n == nil {
		return nil
	}
	switch n := n.(type) {
	case *ast.FuncDecl:
		return v.function(n.Type)
	case *ast.FuncLit:
		return v.function(n.Type)
	case *ast.BinaryExpr:
		if n.Op == token.EQL || n.Op == token.NEQ {
			var e ast.Expr
			if isNil(n.X) {
				e = n.Y
			} else if isNil(n.Y) {
				e = n.X
			}
			if e != nil && v.m.kind(e) != "" {
				// Parent rewriting handles replacement of the whole expression.
			}
		}
	case *ast.CallExpr:
		if !v.builderCall(n) {
			break
		}
		for _, arg := range n.Args {
			if fn, ok := arg.(*ast.FuncLit); ok && (fn.Type.Results == nil || len(fn.Type.Results.List) == 0) && !hasFrame(fn.Type, v.m) {
				if callbackHasRow(fn.Type) || !buildCallback(fn.Type) {
					continue
				}
				name := v.frame
				if name == "" {
					name = "frame"
				}
				if fn.Type.Params == nil {
					fn.Type.Params = &ast.FieldList{}
				}
				fn.Type.Params.List = append([]*ast.Field{{Names: []*ast.Ident{ast.NewIdent(name)}, Type: &ast.SelectorExpr{X: ast.NewIdent(v.m.alias), Sel: ast.NewIdent("Frame")}}}, fn.Type.Params.List...)
			}
		}
		if s, ok := n.Fun.(*ast.SelectorExpr); ok && s.Sel.Name == "Rows" && v.m.kind(s.X) == "Element" && len(n.Args) == 1 {
			if _, ok := n.Args[0].(*ast.FuncLit); !ok {
				s.Sel.Name = "GridRows"
			}
		}
	case *ast.AssignStmt:
		for i, rhs := range n.Rhs {
			if i < len(n.Lhs) && isNil(rhs) {
				if k := v.m.kind(n.Lhs[i]); k == "Frame" || k == "Element" {
					n.Rhs[i] = v.zero(k)
				}
			}
		}
	case *ast.ReturnStmt:
		for i, rhs := range n.Results {
			if i < len(v.returns) && isNil(rhs) && v.returns[i] != "" {
				n.Results[i] = v.zero(v.returns[i])
			}
		}
	}
	// Replace nil comparisons in expression fields without altering other
	// pointer comparisons. This pass uses collected declaration identities.
	replaceComparisons(reflect.ValueOf(n), v.m)
	return v
}

func (v visitor) function(t *ast.FuncType) visitor {
	if t.Params != nil {
		for _, p := range t.Params.List {
			if v.m.uiType(p.Type) == "Frame" && len(p.Names) > 0 {
				v.frame = p.Names[0].Name
			}
		}
	}
	v.returns = nil
	if t.Results != nil {
		for _, r := range t.Results.List {
			for range max(len(r.Names), 1) {
				v.returns = append(v.returns, v.m.uiType(r.Type))
			}
		}
	}
	return v
}

func (v visitor) builderCall(call *ast.CallExpr) bool {
	fn := call.Fun
	if ix, ok := fn.(*ast.IndexExpr); ok {
		fn = ix.X
	}
	if ix, ok := fn.(*ast.IndexListExpr); ok {
		fn = ix.X
	}
	s, ok := fn.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if id, ok := s.X.(*ast.Ident); ok && id.Name == v.m.alias {
		return !nonElements[s.Sel.Name] || s.Sel.Name == "Overlay" || s.Sel.Name == "View" || s.Sel.Name == "NewTester" || s.Sel.Name == "Render"
	}
	switch s.Sel.Name {
	case "Children", "Rows":
		return v.m.kind(s.X) == "Element"
	case "Popup", "Panel":
		kind := v.m.kind(s.X)
		return kind == "Parts" || strings.HasSuffix(kind, "Parts")
	case "View":
		return len(call.Args) > 0 && v.m.kind(call.Args[0]) == "Frame"
	}
	return false
}

func hasFrame(t *ast.FuncType, m *migration) bool {
	if t.Params != nil {
		for _, p := range t.Params.List {
			if m.uiType(p.Type) == "Frame" {
				return true
			}
		}
	}
	return false
}
func buildCallback(t *ast.FuncType) bool {
	if t.Params != nil {
		for _, p := range t.Params.List {
			e := p.Type
			if s, ok := e.(*ast.StarExpr); ok {
				e = s.X
			}
			switch e := e.(type) {
			case *ast.Ident:
				if e.Name == "error" {
					return false
				}
			case *ast.SelectorExpr:
				if e.Sel.Name == "Menu" || e.Sel.Name == "Painter" || e.Sel.Name == "InputEvent" {
					return false
				}
			}
		}
	}
	return true
}
func (v visitor) zero(k string) ast.Expr {
	return &ast.CompositeLit{Type: &ast.SelectorExpr{X: ast.NewIdent(v.m.alias), Sel: ast.NewIdent(k)}}
}
func isNil(e ast.Expr) bool { id, ok := e.(*ast.Ident); return ok && id.Name == "nil" }

func replaceComparisons(v reflect.Value, m *migration) {
	if !v.IsValid() {
		return
	}
	if v.Kind() == reflect.Interface && v.Type() == exprType && v.CanSet() && !v.IsNil() {
		if b, ok := v.Interface().(*ast.BinaryExpr); ok && (b.Op == token.EQL || b.Op == token.NEQ) {
			var e ast.Expr
			if isNil(b.X) {
				e = b.Y
			} else if isNil(b.Y) {
				e = b.X
			}
			if e != nil && (m.kind(e) == "Element" || m.kind(e) == "Frame") {
				var replacement ast.Expr = &ast.CallExpr{Fun: &ast.SelectorExpr{X: e, Sel: ast.NewIdent("Valid")}}
				if b.Op == token.EQL {
					replacement = &ast.UnaryExpr{Op: token.NOT, X: replacement}
				}
				v.Set(reflect.ValueOf(replacement))
				return
			}
		}
	}
	// Only direct expression children are needed here; ast.Walk visits the rest.
	if v.Kind() == reflect.Pointer && !v.IsNil() {
		v = v.Elem()
	}
	if v.Kind() == reflect.Struct {
		for i := 0; i < v.NumField(); i++ {
			f := v.Field(i)
			if f.Kind() == reflect.Interface && f.Type() == exprType {
				replaceComparisons(f, m)
			} else if f.Kind() == reflect.Slice {
				for j := 0; j < f.Len(); j++ {
					if f.Index(j).Kind() == reflect.Interface && f.Index(j).Type() == exprType {
						replaceComparisons(f.Index(j), m)
					}
				}
			}
		}
	}
}

func callbackHasRow(t *ast.FuncType) bool {
	if t.Params != nil {
		for _, p := range t.Params.List {
			if s, ok := p.Type.(*ast.SelectorExpr); ok && s.Sel.Name == "ListRow" {
				return true
			}
		}
	}
	return false
}
