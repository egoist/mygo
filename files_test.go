//go:build !js

package mygo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBeingReplaced checks how a file of the app is seen while it is
// replaced: the temporary sibling the app leaves next to it says so, and
// no other file does.
func TestBeingReplaced(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if beingReplaced(path) {
		t.Error("a file that is not being replaced is being replaced")
	}

	// The sibling writeAppFile leaves next to the file it replaces.
	sibling := filepath.Join(dir, ".state.json.abcdefgh.tmp")
	if err := os.WriteFile(sibling, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !beingReplaced(path) {
		t.Error("a file with a temporary sibling is not being replaced")
	}
	if err := os.Remove(sibling); err != nil {
		t.Fatal(err)
	}

	// The sibling of another file is not this file's.
	other := filepath.Join(dir, ".other.json.abcdefgh.tmp")
	if err := os.WriteFile(other, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if beingReplaced(path) {
		t.Error("a file with another file's temporary sibling is being replaced")
	}

	// A file that is not there is not being replaced either.
	if beingReplaced(filepath.Join(dir, "missing.json")) {
		t.Error("a file that is not there is being replaced")
	}
}

// TestCopyFile checks how a document's file is copied to another place:
// what it holds, that it replaces the file that was there, that it
// leaves no temporary sibling behind, and that a file that is not there
// is an error.
func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	from := filepath.Join(dir, "a.txt")
	to := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(from, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(from, to); err != nil {
		t.Fatalf("copyFile: %v", err)
	}
	got, err := os.ReadFile(to)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Errorf("the copy holds %q, want %q", got, "hello")
	}

	// The copy replaces the file that was there.
	if err := os.WriteFile(from, []byte("world"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(from, to); err != nil {
		t.Fatalf("copyFile: %v", err)
	}
	got, err = os.ReadFile(to)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "world" {
		t.Errorf("the copy holds %q, want %q", got, "world")
	}

	// The temporary sibling the copy writes into is gone.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("the copy left %q behind", e.Name())
		}
	}

	// A file that is not there is an error.
	if err := copyFile(filepath.Join(dir, "missing.txt"), to); err == nil {
		t.Error("copying a file that is not there did not fail")
	}
}

// TestResolveFile checks how the file a page shows is found: an absolute
// path is the file itself, and a relative one is looked for in the
// working directory, then next to the executable.
func TestResolveFile(t *testing.T) {
	dir := t.TempDir()
	page := filepath.Join(dir, "page.html")
	if err := os.WriteFile(page, []byte("<html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := resolveFile(page)
	if err != nil {
		t.Fatalf("resolveFile: %v", err)
	}
	if got != page {
		t.Errorf("an absolute path = %q, want %q", got, page)
	}

	// An absolute path that is not there is an error.
	if _, err := resolveFile(filepath.Join(dir, "missing.html")); err == nil {
		t.Error("an absolute path that is not there did not fail")
	}

	// A relative path is looked for in the working directory.
	t.Chdir(dir)
	if err := os.WriteFile("relative.html", []byte("<html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = resolveFile("relative.html")
	if err != nil {
		t.Fatalf("resolveFile: %v", err)
	}
	if !sameFile(t, got, filepath.Join(dir, "relative.html")) {
		t.Errorf("a relative path = %q, want the file of the working directory", got)
	}

	// A relative path that is nowhere is an error.
	if _, err := resolveFile("missing.html"); err == nil {
		t.Error("a relative path that is nowhere did not fail")
	}

	// And next to the executable, as an app's bundled file is.
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	name := "next-to-the-executable.html"
	exeFile := filepath.Join(filepath.Dir(exe), name)
	if err := os.WriteFile(exeFile, []byte("<html>"), 0o644); err != nil {
		t.Logf("the executable's directory is not writable: %v", err)
		return
	}
	t.Cleanup(func() { os.Remove(exeFile) })
	got, err = resolveFile(name)
	if err != nil {
		t.Fatalf("resolveFile: %v", err)
	}
	if !sameFile(t, got, exeFile) {
		t.Errorf("a relative path = %q, want the file next to the executable", got)
	}
}

// sameFile reports whether two paths are the same file, as a path that
// passes through a symbolic link is not the same string.
func sameFile(t *testing.T, a, b string) bool {
	t.Helper()
	ai, err := os.Stat(a)
	if err != nil {
		t.Errorf("stat %q: %v", a, err)
		return false
	}
	bi, err := os.Stat(b)
	if err != nil {
		t.Errorf("stat %q: %v", b, err)
		return false
	}
	return os.SameFile(ai, bi)
}
