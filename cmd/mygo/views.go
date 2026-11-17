package main

import (
	"fmt"
	"path/filepath"

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
	dir := filepath.Join(c.root, c.Main)
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
