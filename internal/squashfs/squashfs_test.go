package squashfs

import (
	"fmt"
	"io/fs"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/internal/squashfs/squashfstest"
)

// entry is what an image holds at a path, as the test reads it back.
type entry struct {
	kind   uint16 // typeDir, typeFile or typeSymlink
	perm   fs.FileMode
	data   string
	target string
}

// tree is a set of entries to write, by path.
type tree map[string]entry

func sampleTree() tree {
	rng := rand.New(rand.NewPCG(1, 2))
	noise := make([]byte, 3*blockSize+blockSize/2)
	for i := range noise {
		noise[i] = byte(rng.Uint32())
	}
	t := tree{
		"app":                         {kind: typeFile, perm: 0o755, data: strings.Repeat("ELF executable ", 40000)},
		"empty":                       {kind: typeFile, perm: 0o644},
		"exact":                       {kind: typeFile, perm: 0o644, data: strings.Repeat("x", blockSize)},
		"noise":                       {kind: typeFile, perm: 0o644, data: string(noise)},
		"AppRun":                      {kind: typeSymlink, perm: 0o777, target: "app"},
		"usr":                         {kind: typeDir, perm: 0o755},
		"usr/share":                   {kind: typeDir, perm: 0o700},
		"usr/share/empty":             {kind: typeDir, perm: 0o755},
		"usr/share/icons/a/b/c.png":   {kind: typeFile, perm: 0o644, data: "png"},
		"usr/share/icons":             {kind: typeDir, perm: 0o755},
		"usr/share/icons/a":           {kind: typeDir, perm: 0o755},
		"usr/share/icons/a/b":         {kind: typeDir, perm: 0o755},
		strings.Repeat("n", 255):      {kind: typeFile, perm: 0o644, data: "long name"},
		"usr/share/icons/a/b/up.link": {kind: typeSymlink, perm: 0o777, target: "../../../../../app"},
	}
	// Enough entries in one directory for runs of 256, for metadata blocks
	// to end inside inodes and entries, and for a listing beyond the 16
	// bits of a basic directory inode's size.
	t["many"] = entry{kind: typeDir, perm: 0o755}
	for i := range 3000 {
		t[fmt.Sprintf("many/file-with-a-long-name-%04d", i)] = entry{kind: typeFile, perm: 0o644, data: fmt.Sprint(i)}
	}
	return t
}

// write writes t into an image after prefix and returns the file.
func write(t *testing.T, files tree, prefix string) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "image")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.WriteString(prefix)
	w, err := NewWriter(f, time.Unix(1700000000, 0))
	if err != nil {
		t.Fatal(err)
	}
	// Directories first, as callers make them, but files may come before
	// the directories above them too: "usr/share/icons/a/b/c.png" is.
	for name, e := range files {
		var err error
		switch e.kind {
		case typeDir:
			err = w.Mkdir(name, e.perm)
		case typeFile:
			err = w.Create(name, e.perm, strings.NewReader(e.data))
		case typeSymlink:
			err = w.Symlink(e.target, name)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if end, _ := f.Seek(0, 1); end != func() int64 { s, _ := f.Stat(); return s.Size() }() {
		t.Errorf("Close left the file at %d, not its end", end)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRoundTrip(t *testing.T) {
	files := sampleTree()
	const prefix = "\x7fELF runtime of an AppImage"
	b := write(t, files, prefix)
	if !strings.HasPrefix(string(b), prefix) {
		t.Fatal("the bytes before the image changed")
	}
	img := b[len(prefix):]
	if len(img)%4096 != 0 {
		t.Errorf("image of %d bytes, not a multiple of 4 KiB", len(img))
	}
	got := squashfstest.Read(t, img)
	if len(got) != len(files) {
		t.Errorf("%d entries, want %d", len(got), len(files))
	}
	for name, want := range files {
		g, ok := got[name]
		switch {
		case !ok:
			t.Errorf("%s missing", name)
		case g.Kind != int(want.kind) || g.Perm != want.perm || g.Target != want.target:
			t.Errorf("%s: %+v, want kind %d perm %o target %q", name, g, want.kind, want.perm, want.target)
		case g.Data != want.data:
			t.Errorf("%s: %d bytes, want %d", name, len(g.Data), len(want.data))
		}
	}
}

// TestUnsquashfs checks the image with squashfs-tools, which reads it as
// the kernel does, where they are installed.
func TestUnsquashfs(t *testing.T) {
	tool, err := exec.LookPath("unsquashfs")
	if err != nil {
		t.Skip("unsquashfs is not installed")
	}
	files := sampleTree()
	const prefix = "runtime"
	path := filepath.Join(t.TempDir(), "image")
	os.WriteFile(path, write(t, files, prefix), 0o644)
	dir := filepath.Join(t.TempDir(), "out")
	if out, err := exec.Command(tool, "-no-xattrs", "-o", fmt.Sprint(len(prefix)), "-d", dir, path).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for name, want := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		info, err := os.Lstat(p)
		if err != nil {
			t.Error(err)
			continue
		}
		switch want.kind {
		case typeFile:
			b, _ := os.ReadFile(p)
			if string(b) != want.data || info.Mode().Perm() != want.perm {
				t.Errorf("%s: %d bytes, %o", name, len(b), info.Mode().Perm())
			}
		case typeSymlink:
			if l, _ := os.Readlink(p); l != want.target {
				t.Errorf("%s -> %q", name, l)
			}
		case typeDir:
			if !info.IsDir() {
				t.Errorf("%s is not a directory", name)
			}
		}
	}
}

func TestErrors(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "image"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w, err := NewWriter(f, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Create("a", 0o644, strings.NewReader("")); err != nil {
		t.Fatal(err)
	}
	for name, err := range map[string]error{
		"twice":           w.Create("a", 0o644, strings.NewReader("")),
		"under a file":    w.Create("a/b", 0o644, strings.NewReader("")),
		"dir over a file": w.Mkdir("a", 0o755),
		"absolute":        w.Mkdir("/x", 0o755),
		"dot dot":         w.Symlink("t", "../x"),
		"no target":       w.Symlink("", "l"),
		"long name":       w.Create(strings.Repeat("n", 256), 0o644, strings.NewReader("")),
	} {
		if err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}
