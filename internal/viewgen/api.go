package viewgen

import (
	"context"
	"encoding/json"
	"fmt"
	"go/importer"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"strings"
)

// API is the public ui the generator binds markup against: the
// constructors tags stand for, the Element methods attributes and events
// reach, read with go/types from the ui the app uses (#1457), so no
// schema is maintained by hand.
type API struct {
	Constructors map[string]*Constructor // by kebab tag
	Methods      map[string]*Method      // by kebab name
	Package      string                  // the ui package's import path
	pkg          *types.Package          // the ui package, for its constants
	consts       map[string]map[string]string
}

// Constructor is a package-level function of ui taking a *ui.Context
// first and returning a ui.Element.
type Constructor struct {
	GoName string
	// TypeParams names the function's type parameters, as Radio's
	// [T comparable]. They are inferred from the arguments at the call.
	TypeParams []string
	Params     []Param // after the Context
	// Unsupported, when not empty, is why markup cannot build with this
	// constructor yet: its signature's shape, as "func Form(c *Context,
	// fn func(Element)) (Element, Parts)".
	Unsupported string
	Signature   string
}

// Param is one parameter of a constructor or method.
type Param struct {
	Name string
	Type types.Type
	// Ptr reports that the parameter is a pointer: *T for &T bindings.
	Ptr bool
	// Fn reports a func parameter (unsupported shapes carry these).
	Fn       bool
	Variadic bool
	// Required: not a variadic tail.
	Required bool
}

// Method is a method of ui.Element: fluent calls attributes make, and
// the zero-argument bool methods events ask.
type Method struct {
	GoName string
	Kind   MethodKind
	Params []Param
	// Unsupported, when not empty, is why an attribute cannot call it.
	Unsupported string
	Signature   string
}

type MethodKind uint8

const (
	// MethodFluent returns the Element: gap="16" -> .Gap(16).
	MethodFluent MethodKind = iota
	// MethodQuery takes nothing and tells whether something happened:
	// on-clicked -> .Clicked().
	MethodQuery
	// MethodOther returns or takes something else: not an attribute.
	MethodOther
)

// Kebab names a Go identifier as markup writes it: PrimaryButton ->
// primary-button, ID -> id, HTMLPanel -> html-panel.
func Kebab(name string) string {
	var b strings.Builder
	runes := []rune(name)
	for i, r := range runes {
		if r >= 'A' && r <= 'Z' {
			nextLower := i+1 < len(runes) && (runes[i+1] < 'A' || runes[i+1] > 'Z')
			if i > 0 && (runes[i-1] < 'A' || runes[i-1] > 'Z' || nextLower) {
				b.WriteByte('-')
			}
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}

// listPackage runs go list for one package in dir, without building the
// app itself: the package asked for is a dependency, as ui.
func listPackage(ctx context.Context, dir string, env []string, flags []string, path string) (dir_ string, export string, err error) {
	args := append([]string{"list", "-export", "-deps", "-json"}, flags...)
	args = append(args, "--", path)
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	cmd.Env = environ(env)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", "", fmt.Errorf("go list %s: %s", path, ee.Stderr)
		}
		return "", "", err
	}
	type pkg struct {
		ImportPath, Dir, Export string
		DepOnly                 bool
		Error                   *struct{ Err string }
	}
	found := ""
	dec := json.NewDecoder(strings.NewReader(string(out)))
	for {
		var p pkg
		e := dec.Decode(&p)
		if e == io.EOF {
			break
		}
		if e != nil {
			return "", "", e
		}
		if p.Error != nil {
			return "", "", fmt.Errorf("%s", p.Error.Err)
		}
		if p.ImportPath == path && !p.DepOnly {
			found, dir_, export = path, p.Dir, p.Export
		}
	}
	if found == "" {
		return "", "", fmt.Errorf("go list found no package %s", path)
	}
	return dir_, export, nil
}

// loadAPI reads the ui package's public API from its export data, the
// version the app in dir builds with: go.mod, workspace and replace
// decide it, not the CLI's own build (#1457).
func loadAPI(ctx context.Context, dir string, env []string, flags []string) (*API, error) {
	_, export, err := listPackage(ctx, dir, env, flags, "github.com/egoist/mygo/ui")
	if err != nil {
		return nil, err
	}
	f, err := os.Open(export)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fs := token.NewFileSet()
	imp := importer.ForCompiler(fs, "gc", func(path string) (io.ReadCloser, error) {
		if path == "github.com/egoist/mygo/ui" {
			return os.Open(export)
		}
		// The dependencies ui itself needs: listed apart.
		_, dep, err := listPackage(ctx, dir, env, flags, path)
		if err != nil {
			return nil, err
		}
		return os.Open(dep)
	})
	pkg, err := imp.Import("github.com/egoist/mygo/ui")
	if err != nil {
		return nil, fmt.Errorf("read ui's export data: %w", err)
	}
	return describeAPI(pkg), nil
}

// Reuse the checked package to preserve named type identity.
func describeAPI(pkg *types.Package) *API {
	api := &API{Package: pkg.Path(), pkg: pkg, Constructors: map[string]*Constructor{}, Methods: map[string]*Method{}, consts: map[string]map[string]string{}}
	scope := pkg.Scope()
	ctx := scope.Lookup("Context").Type()
	ct := types.NewPointer(ctx)
	// A name stands for the element when the call's result is exactly it.
	isElement := func(t types.Type) bool {
		n, ok := t.(*types.Named)
		return ok && n.Obj().Pkg() == pkg && n.Obj().Name() == "Element"
	}
	for _, name := range scope.Names() {
		fn, ok := scope.Lookup(name).(*types.Func)
		if !ok || !fn.Exported() {
			continue
		}
		sig := fn.Type().(*types.Signature)
		c := describeFunc(fn, sig, ct, isElement)
		if c == nil {
			continue
		}
		api.Constructors[Kebab(name)] = c
	}
	// The methods of Element.
	if obj := scope.Lookup("Element"); obj != nil {
		if named, ok := obj.Type().(*types.Named); ok {
			for i := 0; i < named.NumMethods(); i++ {
				m := named.Method(i)
				if !m.Exported() {
					continue
				}
				sig := m.Type().(*types.Signature)
				mm := &Method{GoName: m.Name(), Signature: types.TypeString(sig, nil)}
				switch {
				case sig.Params().Len() == 0 && sig.Results().Len() == 1 && sig.Results().At(0).Type() == types.Typ[types.Bool]:
					mm.Kind = MethodQuery
				case sig.Results().Len() == 1 && isElement(sig.Results().At(0).Type()):
					mm.Kind = MethodFluent
					mm.Params = describeParams(sig, 0)
					if bad := firstBadParam(mm.Params); bad != "" {
						mm.Unsupported = fmt.Sprintf("%s takes %s: write the value as a Go expression", m.Name(), bad)
					}
				default:
					mm.Kind = MethodOther
					mm.Unsupported = fmt.Sprintf("%s%s does not return the Element", m.Name(), strings.TrimPrefix(mm.Signature, "func"))
				}
				api.Methods[Kebab(m.Name())] = mm
			}
		}
	}
	return api
}

// describeFunc catalogs fn as a constructor when its first parameter is
// a *ui.Context, returning nil otherwise.
func describeFunc(fn *types.Func, sig *types.Signature, ct types.Type, isElement func(types.Type) bool) *Constructor {
	params := sig.Params()
	if params.Len() == 0 {
		return nil
	}
	if !types.Identical(params.At(0).Type(), ct) {
		return nil
	}
	c := &Constructor{GoName: fn.Name(), Signature: types.TypeString(sig, nil)}
	if tparams := sig.TypeParams(); tparams != nil {
		for i := 0; i < tparams.Len(); i++ {
			c.TypeParams = append(c.TypeParams, tparams.At(i).Obj().Name())
		}
	}
	c.Params = describeParams(sig, 1)
	var why []string
	if sig.Results().Len() != 1 || !isElement(sig.Results().At(0).Type()) {
		why = append(why, fmt.Sprintf("returns %s, not an Element", resultNames(sig)))
	} else {
		if bad := firstBadParam(c.Params); bad != "" {
			why = append(why, "takes "+bad)
		}
	}
	if len(why) > 0 {
		c.Unsupported = fmt.Sprintf("%s %s", fn.Name(), strings.Join(why, ", "))
	}
	return c
}

func resultNames(sig *types.Signature) string {
	res := sig.Results()
	if res.Len() == 0 {
		return "nothing"
	}
	var names []string
	for i := 0; i < res.Len(); i++ {
		names = append(names, types.TypeString(res.At(i).Type(), nil))
	}
	return strings.Join(names, ", ")
}

// describeParams lists a signature's parameters, from skip on:
// constructors skip their first, the *ui.Context.
func describeParams(sig *types.Signature, skip int) []Param {
	params := sig.Params()
	out := make([]Param, 0, params.Len())
	for i := skip; i < params.Len(); i++ {
		p := Param{Name: params.At(i).Name(), Type: params.At(i).Type()}
		if sig.Variadic() && i == params.Len()-1 {
			p.Variadic = true
			p.Type = p.Type.(*types.Slice).Elem()
		} else {
			p.Required = true
		}
		if _, ok := p.Type.(*types.Pointer); ok {
			p.Ptr = true
		}
		if _, ok := p.Type.Underlying().(*types.Signature); ok {
			p.Fn = true
		}
		out = append(out, p)
	}
	return out
}

// firstBadParam names the first parameter markup cannot fill from an
// attribute: a function, as Children's, which needs Go's own braces.
func firstBadParam(params []Param) string {
	for _, p := range params {
		if p.Fn {
			return fmt.Sprintf("a function (%s %s): build its children in Go", p.Name, types.TypeString(p.Type, nil))
		}
	}
	return ""
}

// constantOf reads the enum constant named lit (any case) of the type t,
// "" for none. The ui package's constants are read once, per type.
func (api *API) constantOf(lit string, t types.Type) (string, bool) {
	key := types.TypeString(t, nil)
	if _, ok := api.consts[key]; !ok {
		consts := map[string]string{}
		scope := api.pkg.Scope()
		for _, name := range scope.Names() {
			if c, ok := scope.Lookup(name).(*types.Const); ok && c.Exported() && types.Identical(c.Type(), t) {
				consts[strings.ToLower(name)] = name
			}
		}
		api.consts[key] = consts
	}
	// Markup writes the constant as it writes tags: space-between is
	// SpaceBetween.
	for _, name := range api.consts[key] {
		if Kebab(name) == strings.ToLower(lit) {
			return name, true
		}
	}
	return "", false
}
