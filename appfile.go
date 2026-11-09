package mygo

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// Windows refuses to replace or delete a file that another program has
// open without sharing its deletion, as Go opens files, and antivirus
// software and indexers do while they scan new files; and it refuses to
// open a file while it is being replaced. The app's own files (window
// states, recovery copies, recent documents) are replaced in place, so
// their renames and reads retry for a few seconds while Windows reports
// them in use, rather than lose what they hold.

// rename renames a file of the app, retrying while Windows reports it in
// use.
func rename(from, to string) error {
	return retryInUse(func() error { return os.Rename(from, to) })
}

// replace puts the file from in place of the app's file to, retrying while
// Windows reports it in use. On Windows it replaces with POSIX semantics
// (replaceFile), which readAppFile's reads, sharing deletion, don't hold
// up: the app reading its file no longer keeps it from being replaced.
func replace(from, to string) error {
	return retryInUse(func() error { return replaceFile(from, to) })
}

// readAppFile reads a file of the app, retrying while Windows reports it
// in use, or reports it missing while it is being replaced: Windows can
// fail to open the name for a moment while a rename replaces the file.
func readAppFile(path string) (data []byte, err error) {
	read := func() error {
		// Shared, so that replace goes through while the file is read.
		f, err := openShared(path)
		if err != nil {
			return err
		}
		defer f.Close()
		data, err = io.ReadAll(f)
		return err
	}
	for i := 1; ; i++ {
		err = retryInUse(read)
		if i == 5 || runtime.GOOS != "windows" || !errors.Is(err, fs.ErrNotExist) {
			return data, err
		}
		if !beingReplaced(path) {
			// A replace that finished since the read left the file in place.
			err = retryInUse(read)
			return data, err
		}
		time.Sleep(time.Duration(i) * 10 * time.Millisecond)
	}
}

// beingReplaced reports whether path is being replaced: writeAppFile's
// temporary sibling (createSibling) stays in place until its rename is
// done.
func beingReplaced(path string) bool {
	dir, base := filepath.Split(path)
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if name := e.Name(); strings.HasPrefix(name, "."+base+".") && strings.HasSuffix(name, ".tmp") {
			return true
		}
	}
	return false
}

func retryInUse(f func() error) error {
	for i := 1; ; i++ {
		err := f()
		if err == nil || i == 10 || runtime.GOOS != "windows" ||
			// ERROR_SHARING_VIOLATION, or ERROR_ACCESS_DENIED for a file
			// being deleted or replaced, or a directory holding an open file.
			!errors.Is(err, syscall.Errno(32)) && !errors.Is(err, syscall.Errno(5)) {
			return err
		}
		inUseWait(i)
	}
}

// inUseWait waits before the ith retry of what Windows reported in use;
// tests replace it.
var inUseWait = func(i int) { time.Sleep(time.Duration(i) * 100 * time.Millisecond) }
