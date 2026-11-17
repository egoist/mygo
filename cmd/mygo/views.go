package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/internal/viewgen"
)

// prepareViews writes the *_view.go files of the app's main package,
// from the .view markup its screens are written in (#1457). Every
// compile of the app goes through here first (goBuild), so mygo
// generate, build, build -platform web and dev all build its views
// without each knowing of them. The generated files are committed, so a
// plain go build needs none of this.
//
// A package with no .view files returns at once: nothing is parsed, no
// export data is read, nothing changes.
func prepareViews(c *Config) (found bool, err error) {
	dir, err := mainPackageDir(c)
	if err != nil {
		return false, err
	}
	views, err := filepath.Glob(filepath.Join(dir, "*.view"))
	if err != nil {
		return false, err
	}
	if len(views) == 0 {
		// No views: the generator's own outputs of views that went away
		// still belong to it, and go with them (#1457).
		return false, pruneViews(dir)
	}
	written, err := viewgen.Generate(dir)
	if err != nil {
		return true, fmt.Errorf("generate views: %w", err)
	}
	for _, w := range written {
		con.note("info", "generated "+filepath.Base(w))
	}
	return true, nil
}

// mainPackageDir is the directory the main package's sources live in,
// however Config.Main names it: a path from the module root, or a module
// path, as the rest of the CLI resolves it with go list.
func mainPackageDir(c *Config) (string, error) {
	cmd := exec.Command("go", "list", "-e", "-f", "{{.Dir}}", "--", c.Main)
	cmd.Dir = c.root
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("go list %s: %s", c.Main, ee.Stderr)
		}
		return "", err
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		return "", fmt.Errorf("no package at %s", c.Main)
	}
	return dir, nil
}

// pruneViews removes the generator's outputs whose .view files are no
// more, by the marker naming each one's source. Anything else, whatever
// its name, is left alone.
func pruneViews(dir string) error {
	outs, err := filepath.Glob(filepath.Join(dir, "*_view.go"))
	if err != nil || len(outs) == 0 {
		return err
	}
	for _, path := range outs {
		src, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		marker := generatedMarkerOf(string(src))
		if marker == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
			continue
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	return nil
}

// generatedMarkerOf reads the .view path a file was generated from, ""
// for none.
func generatedMarkerOf(src string) string {
	return viewgen.GeneratedMarker(src)
}
