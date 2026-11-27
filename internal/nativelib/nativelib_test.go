package nativelib

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	sum := strings.Repeat("ab", 32)
	ok := `{"libraries": [{"name": "adder", "build": {"cargo": "./rust/adder", "args": ["--features", "x"]}, "release": "https://example.com/r",
		"files": {"linux-amd64": {"name": "libadder.so", "url": "https://example.com/r/adder-linux-amd64.so", "sha256": "` + sum + `"}}}]}`
	m, err := Parse([]byte(ok))
	if err != nil {
		t.Fatal(err)
	}
	l, found := m.Lookup("adder")
	if tool, dir := l.Build.Tool(); !found || tool != "cargo" || dir != "./rust/adder" {
		t.Fatalf("Lookup = %+v, %v", l, found)
	}
	if l.FileName("linux", "amd64") != "libadder.so" || l.FileName("windows", "arm64") != "adder.dll" {
		t.Error("FileName")
	}
	for _, bad := range []string{
		`{"libraries": [{"name": ""}]}`,
		`{"libraries": [{"name": "../x"}]}`,
		`{"libraries": [{"name": "my lib"}]}`,
		`{"libraries": [{"name": ".hidden"}]}`,
		`{"libraries": [{"name": "x"}, {"name": "x"}]}`,
		`{"libraries": [{"name": "x", "build": {}}]}`,
		`{"libraries": [{"name": "x", "build": {"cargo": "a", "zig": "b"}}]}`,
		`{"libraries": [{"name": "x", "build": {"zig": "../elsewhere"}}]}`,
		`{"libraries": [{"name": "x", "build": {"zig": "/abs"}}]}`,
		`{"libraries": [{"name": "x", "files": {"linux-amd64": {"name": "..", "sha256": "` + sum + `"}}}]}`,
		`{"libraries": [{"name": "x", "files": {"linux-amd64": {"name": "x.so", "sha256": "` + strings.ToUpper(sum) + `"}}}]}`,
		`{"libraries": [{"name": "x", "files": {"linux-amd64": {"name": "x.so", "sha256": "../../etc"}}}]}`,
	} {
		if _, err := Parse([]byte(bad)); err == nil {
			t.Errorf("Parse(%s) accepted it", bad)
		}
	}
}

func TestFileName(t *testing.T) {
	for _, c := range []struct{ name, goos, want string }{
		{"adder", "darwin", "libadder.dylib"},
		{"adder", "linux", "libadder.so"},
		{"adder", "windows", "adder.dll"},
		{"libghostty-vt", "darwin", "libghostty-vt.dylib"},
		{"libghostty-vt", "windows", "ghostty-vt.dll"},
		{"mygo-sqlite3", "linux", "libmygo-sqlite3.so"},
	} {
		if got := FileName(c.name, c.goos); got != c.want {
			t.Errorf("FileName(%q, %q) = %q, want %q", c.name, c.goos, got, c.want)
		}
	}
}

func TestMinMacOS(t *testing.T) {
	for _, c := range []struct{ app, lib, want string }{
		{"", "", "12.0"},
		{"", "13.0", "13.0"},
		{"14.0", "13.0", "14.0"},
		{"13.2", "13.10", "13.10"},
		{"15", "", "15"},
	} {
		if got := (&Build{MacOS: c.lib}).MinMacOS(c.app); got != c.want {
			t.Errorf("MinMacOS(%q) with %q = %q, want %q", c.app, c.lib, got, c.want)
		}
	}
	if _, err := Parse([]byte(`{"libraries": [{"name": "x", "build": {"zig": ".", "macOS": "13; rm"}}]}`)); err == nil {
		t.Error("accepted an invalid macOS version")
	}
}

func TestModuleRootRelative(t *testing.T) {
	// The package's directory, below the module's root, as a relative
	// path whose walk up passes the working directory.
	root, err := ModuleRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("ModuleRoot(\".\") = %s, which has no go.mod", root)
	}
	if _, err := ModuleRoot(t.TempDir()); err == nil {
		t.Error("ModuleRoot outside a module succeeded")
	}
}

// TestLibraryFile checks published-build lookup: an exact platform match wins,
// macOS falls back to darwin-universal when no architecture-specific build is
// listed, and other platforms do not.
func TestLibraryFile(t *testing.T) {
	sum := strings.Repeat("01", 32)
	universal := File{Name: "libx.tar.gz", SHA256: sum}
	arm := File{Name: "libx-arm64.dylib", SHA256: sum}
	linux := File{Name: "libx.so", SHA256: sum}

	l := Library{
		Name: "x",
		Files: map[string]File{
			Universal:      universal,
			"darwin-arm64": arm,
			"linux-amd64":  linux,
		},
	}
	if got, ok := l.File("darwin", "arm64"); !ok || got != arm {
		t.Errorf("darwin/arm64 = %+v, %v; want %+v", got, ok, arm)
	}
	if got, ok := l.File("darwin", "amd64"); !ok || got != universal {
		t.Errorf("darwin/amd64 = %+v, %v; want universal %+v", got, ok, universal)
	}
	if got, ok := l.File("linux", "amd64"); !ok || got != linux {
		t.Errorf("linux/amd64 = %+v, %v; want %+v", got, ok, linux)
	}
	if got, ok := l.File("linux", "arm64"); ok {
		t.Errorf("linux/arm64 unexpectedly matched %+v", got)
	}

	// Only .tar.gz names an archive.
	for _, c := range []struct {
		name string
		want bool
	}{
		{"libx.tar.gz", true},
		{"libx.dylib", false},
		{"libx.so", false},
		{"libx.gz", false},
		{"", false},
	} {
		if got := IsArchive(c.name); got != c.want {
			t.Errorf("IsArchive(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestBuildToolAndSwift checks Tool and IsSwift across Cargo, Zig, Swift and
// nil builds.
func TestBuildToolAndSwift(t *testing.T) {
	if (*Build)(nil).IsSwift() {
		t.Error("nil build reported IsSwift")
	}
	for _, c := range []struct {
		b         Build
		wantTool  string
		wantDir   string
		wantSwift bool
	}{
		{Build{Cargo: "./rust"}, "cargo", "./rust", false},
		{Build{Zig: "./zig"}, "zig", "./zig", false},
		{Build{Swift: "./swift"}, "swift", "./swift", true},
	} {
		tool, dir := c.b.Tool()
		if tool != c.wantTool || dir != c.wantDir {
			t.Errorf("%+v.Tool() = (%q, %q), want (%q, %q)", c.b, tool, dir, c.wantTool, c.wantDir)
		}
		if got := c.b.IsSwift(); got != c.wantSwift {
			t.Errorf("%+v.IsSwift() = %v, want %v", c.b, got, c.wantSwift)
		}
	}
}

// TestSkipped checks which names SourceHash leaves out: hidden entries and
// tool build directories, not ordinary sources.
func TestSkipped(t *testing.T) {
	for _, name := range []string{"target", "zig-out", "zig-cache", "zig-pkg", "node_modules", ".git", ".DS_Store"} {
		if !Skipped(name) {
			t.Errorf("Skipped(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"src", "Cargo.toml", "build.zig", "targets", "my_target"} {
		if Skipped(name) {
			t.Errorf("Skipped(%q) = true, want false", name)
		}
	}
}

// TestSourceHash checks that SourceHash is stable, normalizes CRLF to LF,
// ignores skipped files and directories, reacts to source and build option
// changes, and fails on a missing directory.
func TestSourceHash(t *testing.T) {
	write := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	dirLF := t.TempDir()
	write(filepath.Join(dirLF, "src", "lib.rs"), "pub fn add(a: i32, b: i32) -> i32 {\n    a + b\n}\n")
	write(filepath.Join(dirLF, "Cargo.toml"), "[package]\nname = \"adder\"\n")

	dirCRLF := t.TempDir()
	write(filepath.Join(dirCRLF, "src", "lib.rs"), "pub fn add(a: i32, b: i32) -> i32 {\r\n    a + b\r\n}\r\n")
	write(filepath.Join(dirCRLF, "Cargo.toml"), "[package]\r\nname = \"adder\"\r\n")

	b := &Build{Cargo: "."}
	hLF, err := SourceHash(dirLF, b)
	if err != nil {
		t.Fatalf("SourceHash(dirLF): %v", err)
	}
	if len(hLF) != 64 {
		t.Fatalf("hash length = %d, want 64 hex chars", len(hLF))
	}
	hCRLF, err := SourceHash(dirCRLF, b)
	if err != nil {
		t.Fatalf("SourceHash(dirCRLF): %v", err)
	}
	if hLF != hCRLF {
		t.Errorf("CRLF and LF hashed differently: %s vs %s", hCRLF, hLF)
	}

	// Skipped files and build directories do not change the hash.
	write(filepath.Join(dirLF, ".hidden"), "secret")
	write(filepath.Join(dirLF, "target", "debug", "libadder.so"), "binary")
	write(filepath.Join(dirLF, "node_modules", "pkg", "index.js"), "module.exports = 1")
	hSkipped, err := SourceHash(dirLF, b)
	if err != nil {
		t.Fatal(err)
	}
	if hSkipped != hLF {
		t.Errorf("skipped files changed the hash: %s vs %s", hSkipped, hLF)
	}

	// Build options (Args, MacOS) are part of the hash.
	hArgs, err := SourceHash(dirLF, &Build{Cargo: ".", Args: []string{"--features", "simd"}})
	if err != nil {
		t.Fatal(err)
	}
	if hArgs == hLF {
		t.Error("changing Build.Args did not change the hash")
	}
	hMac, err := SourceHash(dirLF, &Build{Cargo: ".", MacOS: "14.0"})
	if err != nil {
		t.Fatal(err)
	}
	if hMac == hLF {
		t.Error("changing Build.MacOS did not change the hash")
	}

	// Changing a source file changes the hash.
	write(filepath.Join(dirLF, "src", "lib.rs"), "pub fn add(a: i32, b: i32) -> i32 {\n    a + b + 1\n}\n")
	hChanged, err := SourceHash(dirLF, b)
	if err != nil {
		t.Fatal(err)
	}
	if hChanged == hLF {
		t.Error("changing a source file did not change the hash")
	}

	if _, err := SourceHash(filepath.Join(dirLF, "does-not-exist"), b); err == nil {
		t.Error("SourceHash on a missing directory succeeded")
	}
}

// TestPaths checks Ext, LocalDir and CachePath.
func TestPaths(t *testing.T) {
	for _, c := range []struct{ goos, want string }{
		{"darwin", ".dylib"},
		{"windows", ".dll"},
		{"linux", ".so"},
		{"freebsd", ".so"},
	} {
		if got := Ext(c.goos); got != c.want {
			t.Errorf("Ext(%q) = %q, want %q", c.goos, got, c.want)
		}
	}

	root := filepath.Join(t.TempDir(), "app")
	wantLocal := filepath.Join(root, ".mygo", "native", "linux-amd64")
	if got := LocalDir(root, "linux", "amd64"); got != wantLocal {
		t.Errorf("LocalDir = %q, want %q", got, wantLocal)
	}

	cache, err := os.UserCacheDir()
	if err != nil {
		t.Skipf("os.UserCacheDir: %v", err)
	}
	f := File{Name: "libadder.so", SHA256: strings.Repeat("cd", 32)}
	gotCache, err := CachePath(f)
	if err != nil {
		t.Fatalf("CachePath: %v", err)
	}
	wantCache := filepath.Join(cache, "mygo", "natives", f.SHA256, f.Name)
	if gotCache != wantCache {
		t.Errorf("CachePath = %q, want %q", gotCache, wantCache)
	}
}
