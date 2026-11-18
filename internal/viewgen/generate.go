package viewgen

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	"slices"
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
func Generate(ctx context.Context, dir string, env []string, flags ...string) (written []string, err error) {
	views, err := filepath.Glob(filepath.Join(dir, "*.view"))
	if err != nil || len(views) == 0 {
		return nil, err
	}
	sort.Strings(views)
	// The package's Go files as the build sees them: constrained by the
	// host, without its tests, and without the *_view.go files the
	// generator owns and never reads as input.
	sources, err := packageSources(ctx, dir, env, flags)
	if err != nil {
		return nil, err
	}
	fs := token.NewFileSet()
	overlay := overlayOf(flags)
	var files []*ast.File
	for _, f := range sources {
		src, err := os.ReadFile(f)
		if err != nil {
			if rep, ok := overlay[f]; ok {
				src, err = os.ReadFile(rep)
			}
			if err != nil {
				return nil, err
			}
		}
		af, err := parser.ParseFile(fs, f, src, parser.ParseComments)
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
			viewOwners[recv] = filepath.Base(fs.Position(af.Package).Filename)
		}
	}

	type view struct {
		file  *File
		src   []byte
		recv  string
		owner string // the *_view.go output path
	}
	var vs []view
	owner := map[string]string{}
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
		if other, ok := owner[recv]; ok {
			return nil, fmt.Errorf("%s and %s both name %s", other, f.Path, recv)
		}
		owner[recv] = f.Path
		if ownerFile, ok := viewOwners[recv]; ok {
			return nil, fmt.Errorf("%s: %s already has a View method (%s)", f.Path, recv, ownerFile)
		}
		vs = append(vs, view{file: f, src: src, recv: recv, owner: strings.TrimSuffix(path, ".view") + "_view.go"})
	}

	// Type-check the package with a stub View for every .view file, so
	// that expressions bind against the receiver's real types.
	importPaths := packageImports(files, []string{"github.com/egoist/mygo/ui", "fmt"})
	exports, err := listExports(ctx, dir, env, flags, importPaths)
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
	var stubs []*ast.File
	for _, v := range vs {
		stub := fmt.Sprintf("package %s\nimport %q\nfunc (%s) View(c *%s.Context) {}\n", pkgName, "github.com/egoist/mygo/ui", receiverDecl(v.recv), "ui")
		af, err := parser.ParseFile(fs, v.owner+".stub", stub, 0)
		if err != nil {
			return nil, err
		}
		stubs = append(stubs, af)
	}
	info := &types.Info{
		Defs:   map[*ast.Ident]types.Object{},
		Uses:   map[*ast.Ident]types.Object{},
		Types:  map[ast.Expr]types.TypeAndValue{},
		Scopes: map[ast.Node]*types.Scope{},
	}
	checked, checkErr := (&types.Config{Importer: imp, FakeImportC: true}).Check(pkgName, fs, append(files, stubs...), info)
	if checkErr != nil {
		// An error in the app's Go: not the generator's to fix.
		return nil, fmt.Errorf("the package's Go does not check: %w", checkErr)
	}

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

	uiPackage, err := imp.Import("github.com/egoist/mygo/ui")
	if err != nil {
		return nil, err
	}
	api := describeAPI(uiPackage)
	loaded := map[string]*types.Package{}
	for _, path := range importPaths {
		p, err := imp.Import(path)
		if err != nil {
			return nil, err
		}
		loaded[path] = p
	}
	var outs []outFile
	for vi, v := range vs {
		aliases := map[string]string{}
		pair := strings.TrimSuffix(v.file.Path, ".view") + ".go"
		for _, af := range files {
			base := filepath.Base(fs.Position(af.Package).Filename)
			if base != pair {
				continue
			}
			for _, imp := range af.Imports {
				path, _ := strconv.Unquote(imp.Path.Value)
				if path == "C" {
					continue
				}
				name := loaded[path].Name()
				if imp.Name != nil {
					name = imp.Name.Name
				}
				aliases[name] = path
			}
		}
		if len(aliases) == 0 {
			// No paired file: any import of the package serves.
			for _, af := range files {
				for _, imp := range af.Imports {
					path, _ := strconv.Unquote(imp.Path.Value)
					if path == "C" {
						continue
					}
					name := loaded[path].Name()
					if imp.Name != nil {
						name = imp.Name.Name
					}
					aliases[name] = path
				}
			}
		}
		localNames := declaredNames(v.file)
		forbidden := map[string]bool{"c": true, "s": true}
		for _, name := range checked.Scope().Names() {
			forbidden[name] = true
		}
		for name := range memberNames(receivers[v.recv], checked) {
			forbidden[name] = true
		}
		for name := range aliases {
			forbidden[name] = true
		}
		for name := range localNames {
			forbidden[name] = true
		}
		paramName := func(preferred string) string {
			name := preferred
			for i := 1; localNames[name] || name != preferred && forbidden[name]; i++ {
				name = preferred + strconv.Itoa(i)
			}
			forbidden[name] = true
			return name
		}
		receiver, context := paramName("s"), paramName("c")
		fn := stubs[vi].Decls[1].(*ast.FuncDecl)
		scope := info.Scopes[fn.Type]
		g := &gen{
			api:        api,
			view:       v.file,
			recvName:   receiver + " *" + v.recv,
			recvType:   receivers[v.recv],
			pkg:        checked,
			imports:    map[string]string{},
			forbidden:  forbidden,
			localNames: localNames,
			loaded:     loaded,
			scope:      scope,
			receiver:   receiver,
			context:    context,
		}
		aliasPackages := map[string]*types.Package{}
		for name, path := range aliases {
			aliasPackages[name] = loaded[path]
		}
		binding, err := newBinder(receivers[v.recv], checked, fs, fn.Body.Lbrace+1, scope, aliasPackages, receiver, context, g.useImport)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", v.file.Path, err)
		}
		g.binding = binding
		src, err := g.emit()
		if err != nil {
			return nil, err
		}
		src = stampHash(src)
		outs = append(outs, outFile{path: v.owner, src: src, view: v.file})
	}

	// Type-check the package once more with the generated files in place
	// of the stubs, through an in-memory overlay: what does not check
	// does not reach the disk.
	over := token.NewFileSet()
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
	var typeErrs []error
	_, err = (&types.Config{Importer: imp, FakeImportC: true, Error: func(err error) { typeErrs = append(typeErrs, err) }}).Check(pkgName, over, checkFiles, vinfo)
	if err != nil {
		return nil, errors.Join(typeErrs...)
	}

	// Every output's way must be clear before any is written: one the
	// generator may not take leaves all of them as they were (#1457).
	write := make([]outFile, 0, len(outs))
	for _, o := range outs {
		old, readErr := os.ReadFile(o.path)
		if readErr == nil && string(old) == o.src {
			continue
		}
		if readErr == nil && generatedMarker(string(old)) == "" {
			return nil, fmt.Errorf("%s exists and is not generated from %s; remove it first", o.path, o.view.Path)
		}
		write = append(write, o)
	}
	for _, o := range write {
		tmp := o.path + ".tmp"
		defer os.Remove(tmp) // no .tmp stays behind, whatever came of it
		if err := os.WriteFile(tmp, []byte(o.src), 0o644); err != nil {
			return nil, err
		}
		if err := os.Rename(tmp, o.path); err != nil {
			return nil, err
		}
		written = append(written, o.path)
	}
	// Outputs whose .view went away go with it — but only in a build
	// that still has views, and each removal said aloud: a package that
	// left the .view world keeps every file, hand-edited or not.
	live := map[string]bool{}
	for _, v := range vs {
		live[v.file.Path] = true
	}
	orphans, err := filepath.Glob(filepath.Join(dir, "*_view.go"))
	if err != nil {
		return written, err
	}
	removed := []string{}
	for _, path := range orphans {
		if slices.Contains(written, path) {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		marker := generatedMarker(string(src))
		if marker == "" || live[marker] {
			continue
		}
		if !hashIntact(string(src)) {
			// Hand-edited after it was generated: its author's now.
			fmt.Fprintf(os.Stderr, "mygo: %s was generated from %s and hand-edited since; leaving it\n", filepath.Base(path), marker)
			continue
		}
		if err := os.Remove(path); err != nil {
			return written, err
		}
		removed = append(removed, fmt.Sprintf("%s (%s gone)", filepath.Base(path), marker))
	}
	if len(removed) > 0 {
		fmt.Fprintf(os.Stderr, "mygo: removed %s\n", strings.Join(removed, ", "))
	}
	return written, nil
}

// GeneratedMarker reads the .view path a *_view.go file was generated
// from, "" for none.
func GeneratedMarker(src string) string { return generatedMarker(src) }

// stampHash adds the content's own hash to a generated file's header:
// pruning removes an orphan output only while the file is still, byte
// for byte, what the generator wrote (#1457).
func stampHash(src string) string {
	body := src[strings.Index(src, "\n")+1:]
	sum := sha256.Sum256([]byte(body))
	line := "// mygo-hash: " + hex.EncodeToString(sum[:8])
	i := strings.Index(src, "\n")
	return src[:i+1] + line + "\n" + src[i+1:]
}

// hashIntact reports whether a generated file still carries the hash of
// its own content: a hand edit breaks it.
func hashIntact(src string) bool {
	lineAt := func(i int) string {
		if j := strings.Index(src[i:], "\n"); j >= 0 {
			return src[i : i+j]
		}
		return src[i:]
	}
	first := strings.Index(src, "\n") + 1
	if !strings.HasPrefix(lineAt(first), "// mygo-hash: ") {
		return false
	}
	claimed := strings.TrimPrefix(lineAt(first), "// mygo-hash: ")
	body := src[strings.Index(src[first:], "\n")+first+1:]
	sum := sha256.Sum256([]byte(body))
	return claimed == hex.EncodeToString(sum[:8])
}

func generatedMarker(src string) string {
	first := strings.SplitN(src, "\n", 2)[0]
	// Code generated by mygo from counter.view. DO NOT EDIT.
	if !strings.HasPrefix(first, "// Code generated by mygo from ") {
		return ""
	}
	rest := strings.TrimPrefix(first, "// Code generated by mygo from ")
	if i := strings.Index(rest, ". DO NOT EDIT."); i >= 0 {
		return rest[:i]
	}
	return ""
}

// packageSources lists the package's Go files as the host's build sees
// them: its build constraints applied, its tests left out, and the
// generator's own outputs never read as input. go list -e reads the
// package without building it.
func packageSources(ctx context.Context, dir string, env []string, flags []string) ([]string, error) {
	args := append([]string{"list", "-e", "-json"}, flags...)
	args = append(args, "--", ".")
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	cmd.Env = environ(env)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("go list: %s", ee.Stderr)
		}
		return nil, err
	}
	var pkg struct {
		GoFiles, CgoFiles []string
	}
	if err := json.Unmarshal(out, &pkg); err != nil {
		return nil, err
	}
	overlay := overlayOf(flags)
	var sources []string
	for _, name := range append(pkg.GoFiles, pkg.CgoFiles...) {
		path := filepath.Join(dir, name)
		if generatedSource(path, overlay) {
			sources = append(sources, path)
		}
	}
	return sources, nil
}

// overlayOf reads the -overlay mapping of the build's flags: files the
// build provides in place of what the tree has (or lacks).
func overlayOf(flags []string) map[string]string {
	for i, f := range flags {
		if f == "-overlay" && i+1 < len(flags) {
			data, err := os.ReadFile(flags[i+1])
			if err != nil {
				return nil
			}
			var ov map[string]string
			if json.Unmarshal(data, &ov) != nil {
				return nil
			}
			return ov
		}
	}
	return nil
}

// generatedSource reports whether path is a source the generator reads:
// a file on disk that is not its own earlier output, or one the build's
// overlay provides in its place (#1457).
func generatedSource(path string, overlay map[string]string) bool {
	if _, ok := overlay[path]; ok {
		return true
	}
	if _, err := os.Stat(path); err != nil {
		return false
	}
	return !generatedFile(path)
}

// generatedFile reports whether the file is the generator's output, by
// the marker its first line carries.
func generatedFile(path string) bool {
	src, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return generatedMarker(string(src)) != ""
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
func listExports(ctx context.Context, dir string, env []string, flags []string, paths []string) (map[string]string, error) {
	if len(paths) == 0 {
		return map[string]string{}, nil
	}
	args := append([]string{"list", "-export", "-deps", "-json"}, flags...)
	args = append(args, "--")
	args = append(args, paths...)
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
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

// Owned reports whether the file at path is the generator's output, by
// its marker: one whose rewriting must not count as a change to rebuild
// for.
func Owned(path string) bool {
	src, err := os.ReadFile(path)
	return err == nil && generatedMarker(string(src)) != ""
}
