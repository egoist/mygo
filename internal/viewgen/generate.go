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
	"unicode"
	"unicode/utf8"
)

// Generate writes the *_view.go files for the .view files of one
// package. dir is the package's directory. It returns the files written,
// or nothing when the package has no .view files. Errors carry .view
// positions.
//
// The app's own code is not built first (#1457): the package is
// type-checked with stub View methods in memory, the views are bound
// against those types, and only output that type-checks is written.
func Generate(ctx context.Context, dir string, env []string, flags ...string) (written []string, err error) {
	views, err := dirFilesWithSuffix(dir, ".view")
	if err != nil || len(views) == 0 {
		return nil, err
	}
	// The package's Go files as the build sees them: constrained by the
	// target, without its tests, and without the *_view.go files the
	// generator still owns and never reads as input.
	sources, err := packageSources(ctx, dir, env, flags)
	if err != nil {
		return nil, err
	}
	fs := token.NewFileSet()
	var files []*ast.File
	pkgDecls := map[string]bool{}
	for _, sf := range sources {
		af, err := parser.ParseFile(fs, sf.path, sf.src, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		for _, d := range af.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					pkgDecls[d.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, sp := range d.Specs {
					switch sp := sp.(type) {
					case *ast.ValueSpec:
						for _, id := range sp.Names {
							pkgDecls[id.Name] = true
						}
					case *ast.TypeSpec:
						pkgDecls[sp.Name.Name] = true
					}
				}
			}
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
			r, w := utf8.DecodeRuneInString(base)
			if r == utf8.RuneError && w <= 1 {
				return nil, fmt.Errorf("%s: <view> needs a type naming its receiver", f.Path)
			}
			recv = string(unicode.ToUpper(r)) + base[w:]
			if !token.IsIdentifier(recv) {
				return nil, fmt.Errorf("%s: cannot derive a Go type name from %q; set type on <view>", f.Path, filepath.Base(path))
			}
		}
		if other, ok := owner[recv]; ok {
			return nil, fmt.Errorf("%s and %s both name %s", other, f.Path, recv)
		}
		owner[recv] = f.Path
		ownerPath := strings.TrimSuffix(path, ".view") + "_view.go"
		if old, err := os.ReadFile(ownerPath); err == nil {
			if m := generatedMarker(string(old)); m != "" && !hashIntact(string(old)) {
				return nil, fmt.Errorf("%s: %s was generated from %s and hand-edited since; remove it first", f.Path, filepath.Base(ownerPath), m)
			}
		}
		if ownerFile, ok := viewOwners[recv]; ok {
			return nil, fmt.Errorf("%s: %s already has a View method (%s)", f.Path, recv, ownerFile)
		}
		vs = append(vs, view{file: f, src: src, recv: recv, owner: ownerPath})
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
	stubUI := "__mygo_ui"
	for i := 1; pkgDecls[stubUI]; i++ {
		stubUI = "__mygo_ui" + strconv.Itoa(i)
	}
	var stubs []*ast.File
	for _, v := range vs {
		stub := fmt.Sprintf("package %s\nimport %s %q\nfunc (%s) View(c *%s.Context) {}\n", pkgName, stubUI, "github.com/egoist/mygo/ui", receiverDecl(v.recv), stubUI)
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
		named, ok := types.Unalias(obj.Type()).(*types.Named)
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
		preferred := map[string]string{}
		collectImports := func(af *ast.File, overwrite bool) {
			for _, impSpec := range af.Imports {
				path, _ := strconv.Unquote(impSpec.Path.Value)
				if path == "C" {
					continue
				}
				name := loaded[path].Name()
				if impSpec.Name != nil {
					name = impSpec.Name.Name
				}
				if name == "_" || name == "." {
					continue
				}
				if overwrite {
					aliases[name] = path
					preferred[path] = name
				} else {
					if _, exists := aliases[name]; !exists {
						aliases[name] = path
					}
					if _, exists := preferred[path]; !exists {
						preferred[path] = name
					}
				}
			}
		}
		pair := strings.TrimSuffix(v.file.Path, ".view") + ".go"
		// Package-wide imports serve as fallback; the paired file's
		// aliases take precedence over them.
		for _, af := range files {
			if filepath.Base(fs.Position(af.Package).Filename) != pair {
				collectImports(af, false)
			}
		}
		for _, af := range files {
			if filepath.Base(fs.Position(af.Package).Filename) == pair {
				collectImports(af, true)
			}
		}
		if _, hasUI := aliases["ui"]; !hasUI && checked.Scope().Lookup("ui") == nil {
			aliases["ui"] = "github.com/egoist/mygo/ui"
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
		paramName := func(preferredName string) string {
			name := preferredName
			for i := 1; localNames[name] || name != preferredName && forbidden[name]; i++ {
				name = preferredName + strconv.Itoa(i)
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
			preferred:  preferred,
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
	for _, sf := range sources {
		af, err := parser.ParseFile(over, sf.path, sf.src, 0)
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
		if readErr == nil && normalizeLF(string(old)) == o.src {
			continue
		}
		if readErr == nil {
			marker := generatedMarker(string(old))
			switch {
			case marker == "":
				return nil, fmt.Errorf("%s exists and is not generated from %s; remove it first", o.path, o.view.Path)
			case marker != o.view.Path:
				return nil, fmt.Errorf("%s was generated from %s, not %s; remove it first", o.path, marker, o.view.Path)
			case !hashIntact(string(old)):
				return nil, fmt.Errorf("%s was generated from %s and hand-edited since; remove it first", o.path, o.view.Path)
			}
		}
		write = append(write, o)
	}
	for _, o := range write {
		if err := writeAtomic(dir, o.path, []byte(o.src)); err != nil {
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
	orphans, err := dirFilesWithSuffix(dir, "_view.go")
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

func writeAtomic(dir, dst string, data []byte) error {
	f, err := os.CreateTemp(dir, ".viewgen-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

func dirFilesWithSuffix(dir, suffix string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), suffix) {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out, nil
}

func normalizeLF(s string) string {
	if !strings.Contains(s, "\r\n") {
		return s
	}
	return strings.ReplaceAll(s, "\r\n", "\n")
}

// stampHash adds the content's own hash to a generated file's header:
// pruning removes an orphan output only while the file is still, byte
// for byte, what the generator wrote (#1457).
func stampHash(src string) string {
	src = normalizeLF(src)
	i := strings.IndexByte(src, '\n')
	body := src[i+1:]
	sum := sha256.Sum256([]byte(body))
	line := "// mygo-hash: " + hex.EncodeToString(sum[:8])
	return src[:i+1] + line + "\n" + body
}

// hashIntact reports whether a generated file still carries the hash of
// its own content: a hand edit breaks it, while CRLF checkout does not.
func hashIntact(src string) bool {
	src = normalizeLF(src)
	nl1 := strings.IndexByte(src, '\n')
	if nl1 < 0 {
		return false
	}
	rest := src[nl1+1:]
	nl2 := strings.IndexByte(rest, '\n')
	if nl2 < 0 {
		return false
	}
	line := rest[:nl2]
	if !strings.HasPrefix(line, "// mygo-hash: ") {
		return false
	}
	claimed := strings.TrimPrefix(line, "// mygo-hash: ")
	body := rest[nl2+1:]
	sum := sha256.Sum256([]byte(body))
	return claimed == hex.EncodeToString(sum[:8])
}

func generatedMarker(src string) string {
	first, _, _ := strings.Cut(normalizeLF(src), "\n")
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

type sourceFile struct {
	path string
	src  []byte
}

// packageSources lists the package's Go files as the target build sees
// them: its build constraints and -overlay applied, its tests left out,
// and the generator's own untouched outputs never read as input. go list
// -e reads the package without building it.
func packageSources(ctx context.Context, dir string, env []string, flags []string) ([]sourceFile, error) {
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
	overlay := overlayOf(dir, flags)
	var sources []sourceFile
	for _, name := range append(pkg.GoFiles, pkg.CgoFiles...) {
		path := filepath.Join(dir, name)
		src, keep, err := readSource(path, overlay)
		if err != nil {
			return nil, err
		}
		if keep {
			sources = append(sources, sourceFile{path: path, src: src})
		}
	}
	return sources, nil
}

// overlayOf reads the -overlay Replace mapping of the build's flags:
// files the build provides in place of what the tree has (or lacks).
func overlayOf(dir string, flags []string) map[string]string {
	ovFile := ""
	for i := 0; i < len(flags); i++ {
		f := flags[i]
		switch {
		case (f == "-overlay" || f == "--overlay") && i+1 < len(flags):
			ovFile = flags[i+1]
			i++
		case strings.HasPrefix(f, "-overlay="):
			ovFile = strings.TrimPrefix(f, "-overlay=")
		case strings.HasPrefix(f, "--overlay="):
			ovFile = strings.TrimPrefix(f, "--overlay=")
		}
	}
	if ovFile == "" {
		return nil
	}
	if !filepath.IsAbs(ovFile) {
		if cand := filepath.Join(dir, ovFile); fileReadable(cand) {
			ovFile = cand
		}
	}
	data, err := os.ReadFile(ovFile)
	if err != nil {
		return nil
	}
	var doc struct {
		Replace map[string]string
	}
	if err := json.Unmarshal(data, &doc); err != nil || len(doc.Replace) == 0 {
		return nil
	}
	base := filepath.Dir(ovFile)
	out := make(map[string]string, len(doc.Replace)*2)
	for k, v := range doc.Replace {
		if !filepath.IsAbs(k) {
			k = filepath.Join(base, k)
		}
		k = filepath.Clean(k)
		if v != "" && !filepath.IsAbs(v) {
			v = filepath.Join(base, v)
		}
		out[k] = v
		if realK := canonicalFilePath(k); realK != k {
			out[realK] = v
		}
	}
	return out
}

func fileReadable(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func canonicalFilePath(path string) string {
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Join(parent, filepath.Base(path))
}

func lookupOverlay(overlay map[string]string, path string) (string, bool) {
	if len(overlay) == 0 {
		return "", false
	}
	clean := filepath.Clean(path)
	if rep, ok := overlay[clean]; ok {
		return rep, true
	}
	if realPath := canonicalFilePath(clean); realPath != clean {
		if rep, ok := overlay[realPath]; ok {
			return rep, true
		}
	}
	return "", false
}

// readSource reads path as the build sees it: through -overlay first,
// else from disk, excluding untouched files the generator owns itself.
func readSource(path string, overlay map[string]string) ([]byte, bool, error) {
	if rep, ok := lookupOverlay(overlay, path); ok {
		if rep == "" {
			return nil, false, nil
		}
		src, err := os.ReadFile(rep)
		if err != nil {
			return nil, false, err
		}
		if ownedSource(string(src)) {
			return nil, false, nil
		}
		return src, true, nil
	}
	src, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if ownedSource(string(src)) {
		return nil, false, nil
	}
	return src, true, nil
}

func ownedSource(src string) bool {
	return generatedMarker(src) != "" && hashIntact(src)
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
	cmd.Env = environ(env)
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

// Owned reports whether the file at path is the generator's untouched
// output, by its marker and content hash: one whose rewriting must not
// count as a change to rebuild for, while a hand-edited orphan does.
func Owned(path string) bool {
	src, err := os.ReadFile(path)
	return err == nil && ownedSource(string(src))
}
