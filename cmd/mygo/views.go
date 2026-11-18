package main

import (
	"context"
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
func prepareViews(ctx context.Context, c *Config, env []string, flags ...string) (found bool, err error) {
	// The common mains name a directory: look there before any tool
	// runs, so builds without views pay for one Glob.
	dir := ""
	if c.Main == "." || strings.HasPrefix(c.Main, "./") || filepath.IsAbs(c.Main) {
		dir = filepath.Join(c.root, c.Main)
	} else if st, err := os.Stat(filepath.Join(c.root, c.Main)); err == nil && st.IsDir() {
		dir = filepath.Join(c.root, c.Main)
	} else {
		// A module path: go list finds its directory. When it cannot,
		// the compile says why; views are not the build's first problem.
		d, err := listMainDir(ctx, c, env, flags)
		if err != nil {
			return false, nil
		}
		dir = d
	}
	views, err := filepath.Glob(filepath.Join(dir, "*.view"))
	if err != nil || len(views) == 0 {
		return false, err
	}
	written, err := viewgen.Generate(ctx, dir, env, flags...)
	if err != nil {
		return true, fmt.Errorf("generate views: %w", err)
	}
	for _, w := range written {
		con.note("info", "generated "+filepath.Base(w))
	}
	return true, nil
}

// listMainDir is the directory of a main named as a module path, in the
// build's own environment.
func listMainDir(ctx context.Context, c *Config, env []string, flags []string) (string, error) {
	args := append([]string{"list", "-e", "-f", "{{.Dir}}"}, flags...)
	args = append(args, "--", c.Main)
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = c.root
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		return "", fmt.Errorf("no package at %s", c.Main)
	}
	return dir, nil
}

// generatedMarkerOf reads the .view path a file was generated from, ""
// for none.
func generatedMarkerOf(src string) string {
	return viewgen.GeneratedMarker(src)
}
