package update

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCompare(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"1.2.3", "1.2.3", 0},
		{"1.2.3", "1.2.10", -1},
		{"1.10.0", "1.9.9", 1},
		{"v2.0.0", "1.99.0", 1},
		{"1.2", "1.2.0", -1},
		{"1.0.0-beta.1", "1.0.0", -1},
		{"1.0.0-beta.2", "1.0.0-beta.10", -1},
		{"1.0.0-alpha", "1.0.0-beta", -1},
		{"1.0.0+build.5", "1.0.0", 0},
	} {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
		if got := Compare(c.b, c.a); got != -c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.b, c.a, got, -c.want)
		}
	}
}

func TestSignatures(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	pk, err := ParsePublicKey(base64.StdEncoding.EncodeToString(pub))
	if err != nil {
		t.Fatal(err)
	}
	sk, err := ParsePrivateKey(base64.StdEncoding.EncodeToString(priv))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("archive"))
	sig := Sign(sk, sum[:])
	if err := Verify(pk, sum[:], sig); err != nil {
		t.Fatal(err)
	}
	other := sha256.Sum256([]byte("tampered"))
	if Verify(pk, other[:], sig) == nil {
		t.Error("a tampered archive verified")
	}
	otherPub, _, _ := ed25519.GenerateKey(nil)
	if Verify(otherPub, sum[:], sig) == nil {
		t.Error("another key verified the signature")
	}
	if _, err := ParsePublicKey("not a key"); err == nil {
		t.Error("ParsePublicKey accepted garbage")
	}
}

// A signature of a version signs the app, the target, the version and the
// digest, and neither kind of signature stands for the other.
func TestVersionSignatures(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	sum := sha256.Sum256([]byte("archive"))
	sig := SignVersion(priv, "com.example.app", "darwin-arm64", "1.2.0", sum[:])
	if err := VerifyVersion(pub, "com.example.app", "darwin-arm64", "1.2.0", sum[:], sig); err != nil {
		t.Fatal(err)
	}
	other := sha256.Sum256([]byte("tampered"))
	otherPub, _, _ := ed25519.GenerateKey(nil)
	for name, err := range map[string]error{
		"another version": VerifyVersion(pub, "com.example.app", "darwin-arm64", "1.3.0", sum[:], sig),
		"another target":  VerifyVersion(pub, "com.example.app", "darwin-amd64", "1.2.0", sum[:], sig),
		"another app":     VerifyVersion(pub, "com.example.other", "darwin-arm64", "1.2.0", sum[:], sig),
		"another archive": VerifyVersion(pub, "com.example.app", "darwin-arm64", "1.2.0", other[:], sig),
		"another key":     VerifyVersion(otherPub, "com.example.app", "darwin-arm64", "1.2.0", sum[:], sig),
		"no signature":    VerifyVersion(pub, "com.example.app", "darwin-arm64", "1.2.0", sum[:], ""),
		"the digest's":    VerifyVersion(pub, "com.example.app", "darwin-arm64", "1.2.0", sum[:], Sign(priv, sum[:])),
		// Lines that move from a field to the next.
		"shifted lines":  VerifyVersion(pub, "app", "darwin", "arm64\n1.2.0", sum[:], SignVersion(priv, "app", "darwin\narm64", "1.2.0", sum[:])),
		"shifted quotes": VerifyVersion(pub, "app", "darwin", "arm64\"\n\"1.2.0", sum[:], SignVersion(priv, "app", "darwin\"\n\"arm64", "1.2.0", sum[:])),
	} {
		if err == nil {
			t.Errorf("%s verified", name)
		}
	}
	if Verify(pub, sum[:], sig) == nil {
		t.Error("the signature of a version verified as the digest's")
	}
	if n := len(versionStatement("", "", "", sum[:])); n <= len(sum) {
		t.Errorf("the shortest statement takes %d bytes, no more than a digest", n)
	}
}

func TestArchive(t *testing.T) {
	src := t.TempDir()
	write := func(name, content string, mode os.FileMode) {
		p := filepath.Join(src, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	write("App.app/Contents/MacOS/App", "binary", 0o755)
	write("App.app/Contents/Resources/data/a.txt", "a", 0o644)
	if runtime.GOOS != "windows" {
		if err := os.Symlink("data/a.txt", filepath.Join(src, "App.app/Contents/Resources/link")); err != nil {
			t.Fatal(err)
		}
	}
	var buf bytes.Buffer
	if err := WriteArchive(&buf, src, []string{"App.app"}); err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	if err := ExtractArchive(bytes.NewReader(buf.Bytes()), dst); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(dst, "App.app/Contents/Resources/data/a.txt")); err != nil || string(b) != "a" {
		t.Errorf("a.txt = %q, %v", b, err)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(filepath.Join(dst, "App.app/Contents/MacOS/App")); err != nil || info.Mode().Perm()&0o100 == 0 {
			t.Errorf("the executable lost its mode: %v", err)
		}
		if link, err := os.Readlink(filepath.Join(dst, "App.app/Contents/Resources/link")); err != nil || link != "data/a.txt" {
			t.Errorf("link = %q, %v", link, err)
		}
	}
}

func TestReleaseNotes(t *testing.T) {
	changelog := "# Changelog\n\nIntro.\n\n## 1.2.0 - 2026-09-19\n\nFaster.\n\n### Fixed\n- Drafts\n\n## 1.1.0\n- Tray\n"
	if got := ReleaseNotes(changelog, "1.2.0"); got != "Faster.\n\n### Fixed\n- Drafts" {
		t.Errorf("notes = %q", got)
	}
	if got := ReleaseNotes(changelog, "v1.1.0"); got != "- Tray" {
		t.Errorf("notes = %q", got)
	}
	if got := ReleaseNotes(changelog, "1.3.0"); got != "" {
		t.Errorf("notes = %q", got)
	}

	// Keep a Changelog, with a Windows checkout's line endings.
	keep := "# Changelog\r\n\r\n## [Unreleased]\r\n\r\n- Soon\r\n\r\n## [1.2.0] - 2026-09-19\r\n\r\n- Faster\r\n\r\n## [v1.1.0](https://example.com/compare/v1.0.0...v1.1.0)\r\n- Tray\r\n"
	for version, want := range map[string]string{"1.2.0": "- Faster", "1.1.0": "- Tray", "Unreleased": "- Soon", "1.0.0": ""} {
		if got := ReleaseNotes(keep, version); got != want {
			t.Errorf("notes of %s = %q, want %q", version, got, want)
		}
	}
}
