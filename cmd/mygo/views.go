package main

import (
	"fmt"
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
	if err != nil || len(views) == 0 {
		return false, err
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
