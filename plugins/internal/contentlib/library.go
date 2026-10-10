// Package contentlib contains the loader shared by optional content engines.
// It has no dependency on a window, renderer, or application event loop.
package contentlib

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Open tries an explicit path alone, or packaged files followed by system
// names. An explicit path is never silently replaced by another library.
func Open(path string, names ...string) (uintptr, error) {
	if strings.ContainsRune(path, 0) {
		return 0, errors.New("native content library path contains NUL")
	}
	if !Supported {
		return 0, errors.ErrUnsupported
	}
	paths := []string{path}
	if path == "" {
		paths = nil
		if exe, err := os.Executable(); err == nil {
			for _, dir := range []string{filepath.Join(filepath.Dir(exe), "..", "Resources"), filepath.Dir(exe)} {
				for _, name := range names {
					paths = append(paths, filepath.Join(dir, filepath.Base(name)))
				}
			}
		}
		paths = append(paths, names...)
	} else {
		var err error
		paths[0], err = filepath.Abs(path)
		if err != nil {
			return 0, err
		}
	}
	var failures []string
	for _, p := range paths {
		h, err := open(p)
		if err == nil {
			return h, nil
		}
		failures = append(failures, fmt.Sprintf("%s: %v", p, err))
	}
	return 0, fmt.Errorf("native content library unavailable (%s); supply Options.Library or package the library and its dependencies", strings.Join(failures, "; "))
}

// Bind binds a required symbol. Optional returns false for an absent one.
func Bind(h uintptr, name string, fn any) error {
	a, err := symbol(h, name)
	if err != nil {
		return fmt.Errorf("native content library lacks %s: %w", name, err)
	}
	register(fn, a)
	return nil
}

func Optional(h uintptr, name string, fn any) bool {
	a, err := symbol(h, name)
	if err != nil {
		return false
	}
	register(fn, a)
	return true
}
