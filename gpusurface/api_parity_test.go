package gpusurface

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// The package's API is the same on every platform, where it does
// something or not: these compile only if each platform's files declare
// all of it, with the same signatures and field types. go vet and go test
// -c for each GOOS check it; the tests below check that no platform has
// more.

var (
	_ func(Options) (*Surface, error)           = New
	_ func() int64                              = Now
	_ func(fw, fh, dw, dh int) (x, y, w, h int) = FitRect

	_ error = ErrUnsupported
	_ error = ErrFormat
	_ error = ErrHDRNeeds10Bit

	_ int          = MaxOverlayRects
	_ [4]Format    = [...]Format{FormatBGRA8, FormatNV12, FormatP010, FormatBGR10A2}
	_ fmt.Stringer = Format(0)
	_              = func(f *Format) *uint8 { return (*uint8)(f) }

	// Pointers to the fields pin their exact types: an untyped constant in
	// a composite literal would convert to another.
	_ = func(o *Options) (*uintptr, *string, *int, *bool, *bool, *bool) {
		return &o.Window, &o.Adapter, &o.Timings, &o.Composite, &o.Drawable10Bit, &o.HDR10
	}
	_ = func(t *Timing) (*int64, *int64, *int64) { return &t.Submitted, &t.Encoded, &t.Presented }
	_ = func(r *OverlayRect) (*float32, *float32, *float32, *float32, *float32, *[4]float32, *[4]float32) {
		return &r.X, &r.Y, &r.W, &r.H, &r.Radius, &r.Color, &r.UV
	}
)

type surfaceAPI interface {
	Present(uintptr) error
	PresentTagged(uintptr, int64) error
	PresentSlice(texture uintptr, slice, width, height int) error
	OnPresented(func(tag, submitted, presented int64))
	Timings() []Timing

	Resize(w, h int) error
	Size() (w, h int)
	Scale() float64
	ScreenSize() (w, h int)

	SetDirect(bool) error
	Direct() bool
	SetHDR10(bool) error
	HDR10() bool
	SetOverlay([]OverlayRect)
	SetOverlayAtlas(pix []byte, w, h int) error

	Device() uintptr
	Queue() uintptr
	Context() uintptr
	SwapChain() uintptr
	Tearing() bool
	AdapterName() string

	Close()
}

var _ surfaceAPI = (*Surface)(nil)

func methodNames(t reflect.Type) []string {
	names := make([]string, t.NumMethod())
	for i := range names {
		names[i] = t.Method(i).Name
	}
	return names
}

// TestSurfaceMethods: *Surface has surfaceAPI's methods and no other, and
// Surface none of its own.
func TestSurfaceMethods(t *testing.T) {
	want := reflect.TypeFor[surfaceAPI]()
	if got := reflect.TypeFor[*Surface](); got.NumMethod() != want.NumMethod() {
		t.Errorf("*Surface has %d methods %v, want the %d of surfaceAPI %v", got.NumMethod(), methodNames(got), want.NumMethod(), methodNames(want))
	}
	if got := reflect.TypeFor[Surface](); got.NumMethod() != 0 {
		t.Errorf("Surface has methods %v, want none", methodNames(got))
	}
	if got := reflect.TypeFor[Format](); got.NumMethod() != 1 {
		t.Errorf("Format has methods %v, want String alone", methodNames(got))
	}
}

// TestStructFields: the exported structs have the fields pinned above and
// no other.
func TestStructFields(t *testing.T) {
	for typ, want := range map[reflect.Type]int{
		reflect.TypeFor[Options]():     6,
		reflect.TypeFor[Timing]():      3,
		reflect.TypeFor[OverlayRect](): 7,
	} {
		if typ.NumField() != want {
			t.Errorf("%s has %d fields, want %d", typ, typ.NumField(), want)
		}
	}
}

var parityTargets = []string{
	"darwin/arm64", "darwin/amd64", "ios/arm64",
	"windows/amd64", "windows/arm64", "windows/386",
	"linux/amd64", "linux/386", "freebsd/amd64", "js/wasm",
}

// TestAPIParity reads the package's files as each target builds them and
// compares the exported declarations, with their types, across targets:
// it covers the targets the tests cannot run on.
func TestAPIParity(t *testing.T) {
	var first []string
	for i, target := range parityTargets {
		api, err := exportedAPI(target)
		if err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		if i == 0 {
			first = api
			continue
		}
		for _, d := range api {
			if !slices.Contains(first, d) {
				t.Errorf("%s declares %q, which %s does not", target, d, parityTargets[0])
			}
		}
		for _, d := range first {
			if !slices.Contains(api, d) {
				t.Errorf("%s lacks %q, which %s declares", target, d, parityTargets[0])
			}
		}
	}
}

func exportedAPI(target string) ([]string, error) {
	goos, goarch, _ := strings.Cut(target, "/")
	ctx := build.Default
	ctx.GOOS, ctx.GOARCH, ctx.CgoEnabled = goos, goarch, false
	pkg, err := ctx.ImportDir(".", 0)
	if err != nil {
		return nil, err
	}
	var api []string
	fset := token.NewFileSet()
	for _, name := range pkg.GoFiles {
		f, err := parser.ParseFile(fset, filepath.Join(pkg.Dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		for _, decl := range f.Decls {
			api = append(api, exportedDecls(decl)...)
		}
	}
	slices.Sort(api)
	return api, nil
}

func exportedDecls(decl ast.Decl) []string {
	var out []string
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if !d.Name.IsExported() {
			break
		}
		recv := ""
		if d.Recv != nil {
			recv = "(" + types.ExprString(d.Recv.List[0].Type) + ") "
		}
		out = append(out, "func "+recv+d.Name.Name+signature(d.Type))
	case *ast.GenDecl:
		for _, spec := range d.Specs {
			switch s := spec.(type) {
			case *ast.TypeSpec:
				if !s.Name.IsExported() {
					continue
				}
				st, ok := s.Type.(*ast.StructType)
				if !ok {
					out = append(out, "type "+s.Name.Name+" "+types.ExprString(s.Type))
					continue
				}
				out = append(out, "type "+s.Name.Name+" struct")
				for _, f := range st.Fields.List {
					// An embedded field, exported or not, can promote
					// exported methods and fields.
					if len(f.Names) == 0 {
						out = append(out, "embed "+s.Name.Name+" "+types.ExprString(f.Type))
					}
					for _, n := range f.Names {
						if n.IsExported() {
							out = append(out, "field "+s.Name.Name+"."+n.Name+" "+types.ExprString(f.Type))
						}
					}
				}
			case *ast.ValueSpec:
				for _, n := range s.Names {
					if n.IsExported() {
						out = append(out, d.Tok.String()+" "+n.Name)
					}
				}
			}
		}
	}
	return out
}

// signature is a function type's parameter and result types, without
// their names.
func signature(ft *ast.FuncType) string {
	list := func(fl *ast.FieldList) string {
		if fl == nil {
			return "()"
		}
		var ts []string
		for _, f := range fl.List {
			for range max(1, len(f.Names)) {
				ts = append(ts, types.ExprString(f.Type))
			}
		}
		return "(" + strings.Join(ts, ", ") + ")"
	}
	return list(ft.Params) + " " + list(ft.Results)
}
