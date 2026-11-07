//go:build !windows

package mygo

import "os"

// replaceFile renames from over to: elsewhere than Windows, renaming
// over a file that is open replaces it.
func replaceFile(from, to string) error { return os.Rename(from, to) }
