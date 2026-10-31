//go:build !windows

package main

import "os"

func renamePackageStage(from, to string) error {
	return os.Rename(from, to)
}
