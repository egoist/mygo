package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/internal/squashfs/squashfstest"
	"github.com/egoist/mygo/internal/update"
)

// The AppImage is the runtime followed by an image of the app's directory
// with what an AppDir has besides, executable; it is signed into the
// manifest, and the build that has one is told so.
func TestAppImage(t *testing.T) {
	const runtimeBytes = "\x7fELF the runtime"
	rt := filepath.Join(t.TempDir(), "runtime")
	os.WriteFile(rt, []byte(runtimeBytes), 0o755)
	saved := appImageRuntimeFile
	t.Cleanup(func() { appImageRuntimeFile = saved })
	var archs []string
	appImageRuntimeFile = func(arch string) (string, error) { archs = append(archs, arch); return rt, nil }

	root := t.TempDir()
	f, err := os.Create(filepath.Join(root, "icon.png"))
	if err != nil {
		t.Fatal(err)
	}
	err = png.Encode(f, image.NewNRGBA(image.Rect(0, 0, 300, 300)))
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	pub, priv, _ := ed25519.GenerateKey(nil)
	t.Setenv("MYGO_UPDATER_PRIVATE_KEY", base64.StdEncoding.EncodeToString(priv))
	config := `{"name": "Note Pad", "version": "1.2.0", "icon": "icon.png", "linux": {"formats": ["appimage"]},
		"updates": {"publicKey": "` + base64.StdEncoding.EncodeToString(pub) + `", "url": "https://example.com/updates"}}`
	if err := os.WriteFile(filepath.Join(root, jsonConfig), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := loadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	stage := t.TempDir()
	os.WriteFile(filepath.Join(stage, "note-pad"), []byte("the app"), 0o755)
	os.MkdirAll(filepath.Join(stage, "resources", "fonts"), 0o755)
	os.WriteFile(filepath.Join(stage, "resources", "fonts", "a.ttf"), []byte("font"), 0o644)
	if _, err := writeLinuxDesktop(c, stage, "note-pad"); err != nil {
		t.Fatal(err)
	}
	installed, err := entryNames(stage)
	if err != nil {
		t.Fatal(err)
	}
	written, err := writeLinuxPackages(c, stage, "linux-amd64", "amd64", installed)
	if err != nil {
		t.Fatal(err)
	}
	const name = "note-pad-1.2.0-linux-x86_64.AppImage"
	var names []string
	for _, w := range written {
		names = append(names, filepath.Base(w))
	}
	if want := []string{name, "note-pad-1.2.0-linux-amd64.tar.gz", "update-linux-amd64.json"}; !slices.Equal(names, want) || !slices.Equal(archs, []string{"x86_64"}) {
		t.Fatalf("wrote %q with the runtimes of %q, want %q", names, archs, want)
	}

	// Windows has no executable bit: a stage there has no executables, as
	// the archive and the DEB built there have none.
	exe := os.FileMode(0o755)
	if runtime.GOOS == "windows" {
		exe = 0o644
	}
	path := filepath.Join(stage, name)
	info, err := os.Stat(path)
	if err != nil || runtime.GOOS != "windows" && info.Mode().Perm() != 0o755 {
		t.Fatalf("AppImage %v %v, want it executable", info, err)
	}
	b, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(b), runtimeBytes) {
		t.Fatal("the AppImage does not start with the runtime")
	}
	entries := squashfstest.Read(t, b[len(runtimeBytes):])
	file := func(p string, perm os.FileMode) string {
		t.Helper()
		e, ok := entries[p]
		if !ok || e.Kind != squashfstest.File || e.Perm != perm {
			t.Errorf("%s: %+v, want a file of mode %o", p, e, perm)
		}
		return e.Data
	}
	if file("note-pad", exe) != "the app" || file("resources/fonts/a.ttf", 0o644) != "font" {
		t.Error("the app's files differ")
	}
	if d := file("note-pad.desktop", 0o644); !strings.Contains(d, "Exec=note-pad") || !strings.Contains(d, "Icon=note-pad") {
		t.Errorf("desktop entry:\n%s", d)
	}
	if run := file("AppRun", 0o755); !strings.HasPrefix(run, "#!/bin/sh\n") || !strings.Contains(run, `/note-pad" "$@"`) {
		t.Errorf("AppRun:\n%s", run)
	}
	file("note-pad.png", 0o644)
	file("usr/share/icons/hicolor/256x256/apps/note-pad.png", 0o644)
	file("usr/share/icons/hicolor/512x512/apps/note-pad.png", 0o644)
	if e := entries[".DirIcon"]; e.Kind != squashfstest.Symlink || e.Target != "note-pad.png" {
		t.Errorf(".DirIcon: %+v", e)
	}

	var m update.Manifest
	data, _ := os.ReadFile(written[len(written)-1])
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	if m.AppImage == nil || m.AppImage.URL != "https://example.com/updates/"+name || m.AppImage.Size != int64(len(b)) || update.Verify(pub, sum[:], m.AppImage.Signature) != nil {
		t.Errorf("the manifest's AppImage: %+v", m.AppImage)
	}

	if files, _, _ := uploads(written); !slices.ContainsFunc(files, func(f string) bool { return filepath.Base(f) == name }) {
		t.Errorf("the AppImage is not uploaded: %q", files)
	}
	for target, want := range map[string]bool{"linux-amd64": true, "linux-arm64": true, "linux-386": false, "darwin-arm64": false} {
		if got := strings.Contains(updateFlags(c, target), "packageAppImage=1"); got != want {
			t.Errorf("%s: told of an AppImage %v, want %v", target, got, want)
		}
	}
	c.Linux.Formats = nil
	if strings.Contains(updateFlags(c, "linux-amd64"), "packageAppImage") {
		t.Error("a build without an AppImage was told it has one")
	}
}
