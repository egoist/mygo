//go:build !js

package mygo

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/egoist/mygo/internal/update"
)

// appImageUpdate serves image as the AppImage of version 99.0.0 and
// returns its manifest and the key it is signed with.
func appImageUpdate(t *testing.T, image string) (update.Manifest, string) {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(image))
	}))
	t.Cleanup(srv.Close)
	sum := sha256.Sum256([]byte(image))
	sig := update.Sign(priv, sum[:])
	return update.Manifest{
		Version: "99.0.0", URL: srv.URL + "/app.tar.gz", Size: 1, Signature: sig,
		AppImage: &update.Image{URL: srv.URL + "/app.AppImage", Size: int64(len(image)), Signature: sig},
	}, base64.StdEncoding.EncodeToString(pub)
}

// elfImage is the start of an AppImage, an executable for machine, then
// what tells it apart.
func elfImage(machine uint16, rest string) string {
	h := make([]byte, 20)
	copy(h, "\x7fELF")
	h[18], h[19] = byte(machine), byte(machine>>8)
	return string(h) + rest
}

// newImage is an AppImage for this machine.
var newImage = elfImage(elfMachine[runtime.GOARCH], "new image")

// An AppImage is replaced whole by the update's, at its path, still
// executable; a bad one, one that is no AppImage of this machine, or none,
// leaves it as it was. Nothing is left next to it either way.
func TestInstallAppImage(t *testing.T) {
	if runtime.GOOS == "windows" || elfMachine[runtime.GOARCH] == 0 {
		t.Skip("AppImages are Linux's, on amd64 and arm64")
	}
	ctx := context.Background()
	m, key := appImageUpdate(t, newImage)
	dir := t.TempDir()
	target := filepath.Join(dir, "my-app-1.0.0-linux-x86_64.AppImage")
	write := func() {
		t.Helper()
		if err := os.WriteFile(target, []byte("old image"), 0o750); err != nil {
			t.Fatal(err)
		}
		os.Chmod(target, 0o750)
	}
	alone := func(what string) {
		t.Helper()
		if entries, _ := os.ReadDir(dir); len(entries) != 1 {
			t.Errorf("%s: %d entries next to the AppImage", what, len(entries)-1)
		}
	}
	unchanged := func(what string) {
		t.Helper()
		if b, _ := os.ReadFile(target); string(b) != "old image" {
			t.Errorf("%s changed the AppImage to %q", what, b)
		}
		alone(what)
	}

	write()
	if err := installAppImage(ctx, httpGet, m, key, target, nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(target); string(b) != newImage {
		t.Errorf("after the update %q", b)
	}
	if info, err := os.Stat(target); err != nil || info.Mode().Perm() != 0o751 {
		t.Errorf("mode %v %v, want the old one, executable by all", info.Mode(), err)
	}
	alone("an update")

	write()
	bad := m
	bad.AppImage = &update.Image{URL: m.AppImage.URL, Size: m.AppImage.Size, Signature: base64.StdEncoding.EncodeToString(make([]byte, 64))}
	if err := installAppImage(ctx, httpGet, bad, key, target, nil); err == nil || !strings.Contains(err.Error(), "signed") {
		t.Errorf("bad signature: %v", err)
	}
	unchanged("a bad signature")

	short := m
	short.AppImage = &update.Image{URL: m.AppImage.URL, Size: m.AppImage.Size + 1, Signature: m.AppImage.Signature}
	if err := installAppImage(ctx, httpGet, short, key, target, nil); err == nil {
		t.Error("an AppImage shorter than the manifest says was installed")
	}
	unchanged("a short download")

	// Signed with the app's key, as the archive is, but no AppImage of
	// this machine: the manifest is not signed, so it may name one.
	other := elfMachine["amd64"]
	if runtime.GOARCH == "amd64" {
		other = elfMachine["arm64"]
	}
	for what, image := range map[string]string{"an archive": "\x1f\x8b gzip", "another machine's AppImage": elfImage(other, "")} {
		wrong, wrongKey := appImageUpdate(t, image)
		if err := installAppImage(ctx, httpGet, wrong, wrongKey, target, nil); err == nil {
			t.Errorf("%s was installed", what)
		}
		unchanged(what)
	}

	none := m
	none.AppImage = nil
	if err := installAppImage(ctx, httpGet, none, key, target, nil); err == nil {
		t.Error("an update without an AppImage was installed")
	}
	unchanged("an update without an AppImage")
}

// An app running as an AppImage checks for and installs updates when its
// releases have AppImages, through the link it may have been started by,
// which stays a link; an app whose releases have none keeps updates off,
// and a release without one is not offered.
func TestUpdaterAppImage(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("APPIMAGE is set by the runtime of AppImages, on Linux")
	}
	ctx := context.Background()
	m, key := appImageUpdate(t, newImage)
	saved := [...]string{packageUpdateFeed, packageUpdateKey, packageAppImage}
	t.Cleanup(func() {
		packageUpdateFeed, packageUpdateKey, packageAppImage = saved[0], saved[1], saved[2]
		Updater.SetSource(nil)
	})
	packageUpdateFeed, packageUpdateKey = "https://example.com/update.json", key
	t.Setenv("MYGO_ENV", "production")
	dir := t.TempDir()
	real := filepath.Join(dir, "my-app-1.0.0-linux-x86_64.AppImage")
	if err := os.WriteFile(real, []byte("old image"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "my-app.AppImage")
	if err := os.Symlink(filepath.Base(real), link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APPIMAGE", link)
	src := &testSource{m: m}
	Updater.SetSource(src)

	// Set for an app the AppImage started, which runs from elsewhere: not
	// the app's AppImage.
	t.Setenv("APPDIR", t.TempDir())
	packageAppImage = "1"
	if p := appImage(); p != "" {
		t.Errorf("an AppImage that started the app taken for its own: %s", p)
	}
	// The runtime's mount, which the test binary stands for.
	t.Setenv("APPDIR", filepath.Dir(startExe))
	if appImage() != link {
		t.Fatal("the AppImage the app runs from not found")
	}

	packageAppImage = ""
	if Updater.Enabled() {
		t.Error("updates on in an AppImage of an app whose releases have none")
	}
	if _, err := Updater.Check(ctx); !errors.Is(err, ErrUpdatesDisabled) {
		t.Errorf("Check: %v", err)
	}

	packageAppImage = "1"
	if !Updater.Enabled() {
		t.Fatal("updates off in an AppImage of an app whose releases have one")
	}
	src.m.AppImage = nil
	if up, err := Updater.Check(ctx); err == nil || up != nil {
		t.Errorf("a release without an AppImage: %+v, %v", up, err)
	}
	src.m = m
	up, err := Updater.Check(ctx)
	if err != nil || up == nil || up.Version != "99.0.0" {
		t.Fatalf("Check = %+v, %v", up, err)
	}
	// The source's Open downloads only service: URLs; the feed's through
	// httpGet, as this one.
	up.source = nil
	if err := up.Install(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(real); string(b) != newImage {
		t.Errorf("after the update %q", b)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the link the app started by is no longer one: %v %v", info, err)
	}

	// What an update stopped in the middle left is removed at launch, and
	// nothing else.
	left := filepath.Join(dir, workPattern(real)+"123")
	os.MkdirAll(left, 0o755)
	os.WriteFile(filepath.Join(left, filepath.Base(real)), []byte("half"), 0o644)
	cleanUpdateLeftovers()
	if _, err := os.Stat(left); !os.IsNotExist(err) {
		t.Errorf("the leftover of an update stays: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Errorf("%d entries next to the AppImage, want it and its link", len(entries))
	}
}
