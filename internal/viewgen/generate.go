package viewgen

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Generate writes the *_view.go files for the .view files of one
// package. dir is the package's directory; modDir its module root, for
// paths relative to it. It returns the files written, or nothing when
// the package has no .view files. Errors carry .view positions.
//
// The app's own code is not built first (#1457): the package is
// type-checked with stub View methods in memory, the views are bound
// against those types, and only output that type-checks is written.
func Generate(dir string) (written []string, err error) {
	views, err := filepath.Glob(filepath.Join(dir, "*.view"))
	if err != nil || len(views) == 0 {
		return nil, err
	}
	sort.Strings(views)
	// The package's Go files: the generator owns the *_view.go files
	// alone, and never reads them as input.
	goFiles, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}
	var sources []string
	for _, f := range goFiles {
		if strings.HasSuffix(f, "_view.go") {
			continue
		}
		sources = append(sources, f)
	}
	fs := token.NewFileSet()
	var files []*ast.File
	for _, f := range sources {
		af, err := parser.ParseFile(fs, f, nil, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		files = append(files, af)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("%s: no Go files to pair .view files with", dir)
	}
	pkgName := files[0].Name.Name

	// A View method of the app's own fails the generation: the stub set
	// would collide with it. Read the app's own sources alone for it.
	viewOwners := map[string]string{}
	for _, af := range files {
		for _, d := range af.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Name.Name != "View" || fd.Recv == nil || len(fd.Recv.List) != 1 {
				continue
			}
			recv := receiverTypeName(fd.Recv.List[0].Type)
			viewOwners[recv] = af.Name.Name
		}
	}

	// Parse the views, pair each with its receiver type.
	type view struct {
		file  *File
		src   []byte
		recv  string
		owner string // the *_view.go output path
	}
	var vs []view
	for _, path := range views {
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		f, diags, err := Parse(filepath.Base(path), src)
		if err != nil {
			return nil, joinDiags(diags, err)
		}
		if len(diags) > 0 {
			return nil, fmt.Errorf("%s", diags[0])
		}
		recv := f.Type
		if recv == "" {
			base := strings.TrimSuffix(filepath.Base(path), ".view")
			recv = strings.ToUpper(base[:1]) + base[1:]
		}
		if owner, ok := viewOwners[recv]; ok {
			return nil, fmt.Errorf("%s: %s already has a View method (%s)", f.Path, recv, owner)
		}
		vs = append(vs, view{file: f, src: src, recv: recv, owner: strings.TrimSuffix(path, ".view") + "_view.go"})
	}

	// Type-check the package with a stub View for every .view file, so
	// that expressions bind against the receiver's real types.
	importPaths := packageImports(files, []string{"github.com/egoist/mygo/ui"})
	exports, err := listExports(dir, importPaths)
	if err != nil {
		return nil, err
	}
	imp := importer.ForCompiler(fs, "gc", func(path string) (io.ReadCloser, error) {
		file := exports[path]
		if file == "" {
			return nil, fmt.Errorf("missing compiled export for %s", path)
		}
		return os.Open(file)
	})
	var stubFiles []*ast.File
	for _, v := range vs {
		stub := fmt.Sprintf("package %s\nimport %q\nfunc (%s) View(c *%s.Context) {}\n", pkgName, "github.com/egoist/mygo/ui", receiverDecl(v.recv), "ui")
		af, err := parser.ParseFile(fs, v.owner+".stub", stub, 0)
		if err != nil {
			return nil, err
		}
		stubFiles = append(stubFiles, af)
	}
	info := &types.Info{
		Defs:  map[*ast.Ident]types.Object{},
		Uses:  map[*ast.Ident]types.Object{},
		Types: map[ast.Expr]types.TypeAndValue{},
	}
	// The stubs join the same file set as the sources, so that Defs and
	// Uses cover both.
	for _, af := range stubFiles {
		_ = af
	}
	checked, checkErr := (&types.Config{Importer: imp, FakeImportC: true}).Check(pkgName, fs, append(files, stubFiles...), info)
	if checkErr != nil {
		// An error in the app's Go: not the generator's to fix.
		return nil, fmt.Errorf("the package's Go does not check: %w", checkErr)
	}

	// The receiver types and the methods and fields the views reach.
	receivers := map[string]*types.Named{}
	for _, v := range vs {
		obj := checked.Scope().Lookup(v.recv)
		if obj == nil {
			return nil, fmt.Errorf("%s: no type %s in the package", v.file.Path, v.recv)
		}
		named, ok := obj.Type().(*types.Named)
		if !ok {
			return nil, fmt.Errorf("%s: %s is not a struct type", v.file.Path, v.recv)
		}
		if _, isStruct := named.Underlying().(*types.Struct); !isStruct {
			return nil, fmt.Errorf("%s: %s is not a struct type", v.file.Path, v.recv)
		}
		receivers[v.recv] = named
	}

	// Bind and emit every view.
	api, err := LoadAPI(dir)
	if err != nil {
		return nil, err
	}
	var outs []outFile
	for _, v := range vs {
		g := &gen{
			api:      api,
			view:     v.file,
			recvName: receiverDecl(v.recv),
			recvType: receivers[v.recv],
			pkg:      checked,
			info:     info,
			fs:       fs,
			imports:  map[string]bool{"github.com/egoist/mygo/ui": true},
		}
		src, err := g.emit()
		if err != nil {
			return nil, err
		}
		outs = append(outs, outFile{path: v.owner, src: src, view: v.file})
	}

	// Type-check the package once more with the generated files in place
	// of the stubs, through an in-memory overlay: what does not check
	// does not reach the disk.
	over := token.NewFileSet()
	// Parse the app's files and the generated ones into one set.
	var checkFiles []*ast.File
	for _, f := range sources {
		af, err := parser.ParseFile(over, f, nil, 0)
		if err != nil {
			return nil, err
		}
		checkFiles = append(checkFiles, af)
	}
	for _, o := range outs {
		af, err := parser.ParseFile(over, o.path, o.src, 0)
		if err != nil {
			return nil, fmt.Errorf("%s: generated Go does not parse: %w", o.path, err)
		}
		checkFiles = append(checkFiles, af)
	}
	vinfo := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
	_, err = (&types.Config{Importer: imp, FakeImportC: true, Error: func(error) {}}).Check(pkgName, over, checkFiles, vinfo)
	if err != nil {
		return nil, mapTypeErrors(over, err, outs)
	}

	// Write what checked, one file at a time, only where the content
	// changed.
	for _, o := range outs {
		old, readErr := os.ReadFile(o.path)
		if readErr == nil && string(old) == o.src {
			continue
		}
		if readErr == nil && !generatedBy(o.path, old, o.view.Path) {
			return nil, fmt.Errorf("%s exists and is not generated from %s; remove it first", o.path, o.view.Path)
		}
		tmp := o.path + ".tmp"
		if err := os.WriteFile(tmp, []byte(o.src), 0o644); err != nil {
			return nil, err
		}
		if err := os.Rename(tmp, o.path); err != nil {
			return nil, err
		}
		written = append(written, o.path)
	}
	// Outputs whose .view went away belong to no one: the caller prunes
	// them (Generate reports what it owns).
	return written, nil
}

type outFile struct {
	path string
	src  string
	view *File
}

// receiverDecl is the receiver the generated method binds: s *T.
func receiverDecl(typeName string) string { return "s *" + typeName }

// packageImports collects the import paths of the files, plus extra.
func packageImports(files []*ast.File, extra []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range files {
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if path == "C" || seen[path] {
				continue
			}
			seen[path] = true
			out = append(out, path)
		}
	}
	for _, p := range extra {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// listExports returns the export-data files of the import paths, built
// without building the package itself.
func listExports(dir string, paths []string) (map[string]string, error) {
	if len(paths) == 0 {
		return map[string]string{}, nil
	}
	args := append([]string{"list", "-export", "-deps", "-json", "--"}, paths...)
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("go list: %s", ee.Stderr)
		}
		return nil, err
	}
	type pkg struct {
		ImportPath, Export string
		Error              *struct{ Err string }
	}
	exports := map[string]string{}
	dec := json.NewDecoder(strings.NewReader(string(out)))
	for {
		var p pkg
		e := dec.Decode(&p)
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		if p.Error != nil {
			return nil, fmt.Errorf("%s", p.Error.Err)
		}
		if p.Export != "" {
			exports[p.ImportPath] = p.Export
		}
	}
	return exports, nil
}

func joinDiags(diags []Diagnostic, err error) error {
	if len(diags) > 0 {
		return fmt.Errorf("%s", diags[0])
	}
	return err
}

// generatedBy reports whether the file at path is the generator's output
// for the .view source, by its header marker.
func generatedBy(path string, src []byte, viewPath string) bool {
	head := string(src)
	if i := strings.Index(head, "\n\n"); i > 0 {
		head = head[:i]
	}
	return strings.Contains(string(src[:min(len(src), 400)]), "// Code generated by mygo from "+viewPath)
}

func mapTypeErrors(fs *token.FileSet, err error, outs []outFile) error {
	// Errors from the generated files carry //line positions of the
	// .view sources already; others are the app's own.
	return err
}


// receiverTypeName reads the type a receiver declaration names, as
// (*T or T).
func receiverTypeName(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.StarExpr:
		return receiverTypeName(e.X)
	case *ast.IndexExpr:
		return receiverTypeName(e.X)
	case *ast.Ident:
		return e.Name
	}
	return ""
}
