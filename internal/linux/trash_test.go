//go:build linux && !android && (amd64 || arm64)

package linux

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTrashInfo(t *testing.T, dir, name, orig string, when time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "files"), 0o755); err != nil {
		t.Fatal(err)
	}
	info := filepath.Join(dir, "info", name+".trashinfo")
	body := "[Trash Info]\nPath=" + orig + "\nDeletionDate=" + when.Format("2006-01-02T15:04:05") + "\n"
	if err := os.WriteFile(info, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(info, when, when); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "files", name), []byte("trashed"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFindTrashed(t *testing.T) {
	dir := t.TempDir()
	trash := filepath.Join(dir, "Trash")
	orig := filepath.Join(dir, "report.pdf")
	now := time.Now()
	writeTrashInfo(t, trash, "report.pdf", orig, now)
	writeTrashInfo(t, trash, "other.txt", filepath.Join(dir, "other.txt"), now)

	got, ok := findTrashed(orig, []string{trash})
	if !ok || got != filepath.Join(trash, "files", "report.pdf") {
		t.Errorf("findTrashed = %q, %v, want the trashed report.pdf", got, ok)
	}
	if _, ok := findTrashed(filepath.Join(dir, "never-trashed"), []string{trash}); ok {
		t.Error("findTrashed found a path that was never trashed")
	}
	if _, ok := findTrashed(orig, []string{filepath.Join(dir, "no-trash")}); ok {
		t.Error("findTrashed found something without a trash")
	}

	// The same path trashed twice: the newest info file wins.
	writeTrashInfo(t, trash, "report.2.pdf", orig, now.Add(time.Second))
	got, ok = findTrashed(orig, []string{trash})
	if !ok || got != filepath.Join(trash, "files", "report.2.pdf") {
		t.Errorf("findTrashed after a second trash = %q, %v, want report.2.pdf", got, ok)
	}
}

func TestTrashInfoPath(t *testing.T) {
	for _, c := range []struct {
		body string
		want string
		ok   bool
	}{
		{"[Trash Info]\nPath=/home/u/a b.txt\nDeletionDate=2026-11-21T10:00:00\n", "/home/u/a b.txt", true},
		{"[Trash Info]\r\nPath=/home/u/caf%C3%A9.txt\r\n", "/home/u/café.txt", true},
		{"[Trash Info]\nPath=/home/u/a%2Fb.txt\n", "/home/u/a/b.txt", true},
		{"[Trash Info]\nDeletionDate=2026-11-21T10:00:00\n", "", false},
		{"[Trash Info]\nPath=%zz\n", "", false},
	} {
		got, ok := trashInfoPath([]byte(c.body))
		if got != c.want || ok != c.ok {
			t.Errorf("trashInfoPath(%q) = %q, %v, want %q, %v", c.body, got, ok, c.want, c.ok)
		}
	}
}

func TestUnescapeMount(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"/mnt/data", "/mnt/data"},
		{`/mnt/my\040disk`, "/mnt/my disk"},
		{`/mnt/a\134b`, `/mnt/a\b`},
		{`/mnt/tail\04`, `/mnt/tail\04`},
	} {
		if got := unescapeMount(c.in); got != c.want {
			t.Errorf("unescapeMount(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTrashDirs(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/data/home")
	dirs := trashDirs("/data/home/u/file.txt")
	if dirs[0] != "/data/home/Trash" {
		t.Errorf("home trash = %q, want /data/home/Trash", dirs[0])
	}
}