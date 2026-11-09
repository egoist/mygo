package mygo

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestAppFileInUse: a file of the app that another program has open, as
// antivirus software scanning it, is replaced once it lets go, and the app
// reading the file while it is replaced over and over neither fails to
// read nor keeps it from being replaced. Nothing here depends on timing:
// the scan ends in the replace's wait for a retry, and the reads share the
// file's deletion, which the replace's POSIX rename goes through, however
// the goroutines interleave. (Written before with the scan ending on a
// timer and reads that held the replace up, it failed now and then on CI's
// busy runners, when the reads kept the replace's retries from ever
// landing between them.)
func TestAppFileInUse(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("only Windows keeps open files from being replaced")
	}
	path := filepath.Join(t.TempDir(), "index.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Go opens files without sharing their deletion, which replacing needs.
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	wait := inUseWait
	defer func() { inUseWait = wait }()
	waits := 0
	inUseWait = func(int) { waits++; f.Close() } // the scan ends
	if err := writeAppFile(path, func(w io.Writer) error { _, err := io.WriteString(w, "new"); return err }); err != nil {
		t.Fatal(err)
	}
	if waits == 0 {
		t.Error("the file another program had open was replaced at once")
	}
	if b, err := readAppFile(path); err != nil || string(b) != "new" {
		t.Errorf("read %q, %v after the replace", b, err)
	}
	inUseWait = wait
	// Reads while it is replaced over and over.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			if err := writeAppFile(path, func(w io.Writer) error { _, err := io.WriteString(w, "x"); return err }); err != nil {
				t.Errorf("write %d: %v", i, err)
				return
			}
		}
	}()
	for {
		select {
		case <-done:
			return
		default:
		}
		if _, err := readAppFile(path); err != nil {
			t.Errorf("read while replaced: %v", err)
			<-done
			return
		}
	}
}
