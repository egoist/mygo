package main

import (
	"bytes"
	"debug/pe"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// testdata/dmg.DS_Store was written by the ds_store Python package
// (dmgbuild), which is known to produce layouts Finder honors:
//
//	with DSStore.open(path, "w+") as d:
//	    d["."]["vSrn"] = ("long", 1)
//	    d["."]["bwsp"] = {...}  # the bwsp dictionary of dmgRecords
//	    d["."]["icvp"] = {...}  # the icvp dictionary of dmgRecords
//	    d["."]["icvl"] = (b"type", b"icnv")
//	    d["Hello MyGo.app"]["Iloc"] = (180, 190)
//	    d["Applications"]["Iloc"] = (480, 185)
func TestDSStoreMatchesDMGBuild(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("testdata", "dmg.DS_Store"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := dsStore(dmgRecords("Hello MyGo.app"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		for i := range min(len(got), len(want)) {
			if got[i] != want[i] {
				t.Fatalf("differs from testdata/dmg.DS_Store at byte %#x (len %d, want %d)", i, len(got), len(want))
			}
		}
		t.Fatalf("length %d, want %d", len(got), len(want))
	}
}

func TestBinaryPlist(t *testing.T) {
	data := binaryPlist(map[string]any{
		"flag":    true,
		"off":     false,
		"count":   300,
		"size":    128.0,
		"name":    "none",
		"unicode": "héllo",
		"long":    strings.Repeat("x", 20),
	})
	if !bytes.HasPrefix(data, []byte("bplist00")) {
		t.Fatal("missing header")
	}
	if _, err := exec.LookPath("plutil"); err != nil {
		t.Skip("plutil not available")
	}
	f := filepath.Join(t.TempDir(), "p.plist")
	if err := os.WriteFile(f, data, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("plutil", "-convert", "xml1", "-o", "-", f).CombinedOutput()
	if err != nil {
		t.Fatalf("plutil: %v\n%s", err, out)
	}
	for _, want := range []string{
		"<key>flag</key>\n\t<true/>", "<key>off</key>\n\t<false/>", "<integer>300</integer>",
		"<real>128</real>", "<string>none</string>", "<string>héllo</string>", strings.Repeat("x", 20),
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("plist lacks %q:\n%s", want, out)
		}
	}
}

func TestPackagingArgs(t *testing.T) {
	if got := codesignArgs("A.app", "-", "", true); !slices.Equal(got, []string{"--force", "--deep", "--sign", "-", "A.app"}) {
		t.Errorf("ad hoc: %q", got)
	}
	got := codesignArgs("A.app", "Developer ID Application: X", "e.plist", true)
	want := []string{"--force", "--deep", "--sign", "Developer ID Application: X", "--options", "runtime", "--timestamp", "--entitlements", "e.plist", "A.app"}
	if !slices.Equal(got, want) {
		t.Errorf("production: %q", got)
	}
	if got := codesignArgs("A.app", "Apple Development: X", "", false); slices.Contains(got, "--timestamp") {
		t.Errorf("development builds need no timestamp: %q", got)
	}
	args := hdiutilCreateArgs("My App", "src", "rw.dmg")
	if slices.Contains(args, "-size") {
		t.Errorf("hdiutil create: %q", args)
	}
	c := &Config{Name: "A/B: C", Version: "1.0"}
	if got := dmgFileName(c); got != "A-B- C 1.0.dmg" {
		t.Errorf("dmgFileName = %q", got)
	}
}

func TestSetFinderFlagsUsesSystemXattr(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("needs macOS")
	}
	shadowDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(shadowDir, "xattr"), []byte("#!/bin/sh\nexit 64\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shadowDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if got, err := exec.LookPath("xattr"); err != nil || got != filepath.Join(shadowDir, "xattr") {
		t.Fatalf("shadow xattr = %q, %v", got, err)
	}

	path := filepath.Join(t.TempDir(), "flags")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := setFinderFlags(path, finderHasCustomIcon); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("/usr/bin/xattr", "-px", "com.apple.FinderInfo", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(strings.Fields(string(out)), ""), "0000000000000000040000000000000000000000000000000000000000000000"; got != want {
		t.Errorf("FinderInfo = %q, want %q", got, want)
	}
}

// TestBuildDMG builds a disk image of a minimal app with hdiutil.
func TestBuildDMG(t *testing.T) {
	testBuildDMG(t, 0, false)
}

// Tiny files occupy allocation blocks and catalog records beyond their byte
// sizes. A large volume icon must also be included in the size estimate.
func TestBuildDMGManySmallFiles(t *testing.T) {
	testBuildDMG(t, 20000, false)
}

// Sparse resources must be sized by the space they will occupy on HFS+.
func TestBuildDMGSparseResource(t *testing.T) {
	testBuildDMG(t, 0, true)
}

func testBuildDMG(t *testing.T, smallFiles int, sparse bool) {
	t.Helper()
	if runtime.GOOS != "darwin" || testing.Short() {
		t.Skip("needs macOS")
	}
	dir := t.TempDir()
	c := &Config{Name: "DMG Test", Version: "1.2.3", root: dir}
	c.applyDefaults()
	bin := filepath.Join(dir, "exe")
	if sparse {
		// A real Mach-O permits ad-hoc signing without a certificate.
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		if err := copyFile(exe, bin, 0o755); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	icns, err := pngToICNS(defaultIcon())
	if err != nil {
		t.Fatal(err)
	}
	if smallFiles > 0 {
		// Packaging treats icon contents as opaque; no image decoder is needed.
		icns = bytes.Repeat([]byte("synthetic icon data"), 65536)
	}
	app, err := writeBundle(c, dir, bin, icns, nil)
	if err != nil {
		t.Fatal(err)
	}
	files := filepath.Join(app, "Contents", "Resources", "small")
	if smallFiles > 0 {
		if err := os.Mkdir(files, 0o755); err != nil {
			t.Fatal(err)
		}
		for i := range smallFiles {
			if err := os.WriteFile(filepath.Join(files, fmt.Sprintf("%05d", i)), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	const sparseSize = 64 << 20
	sparseTime := time.Unix(1600000000, 0)
	if sparse {
		payload := filepath.Join(app, "Contents", "Resources", "sparse.bin")
		f, err := os.Create(payload)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Truncate(sparseSize); err != nil {
			f.Close()
			t.Fatal(err)
		}
		for _, offset := range []int64{0, sparseSize - 1} {
			if _, err := f.WriteAt([]byte("x"), offset); err != nil {
				f.Close()
				t.Fatal(err)
			}
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		// Check both contents and metadata across the non-cloning copy.
		if out, err := exec.Command("/usr/bin/xattr", "-w", "org.mygo.test", "sparse resource", payload).CombinedOutput(); err != nil {
			t.Fatalf("xattr: %v\n%s", err, out)
		}
		if err := os.Chmod(payload, 0o640); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(payload, sparseTime, sparseTime); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("codesign", "--force", "--sign", "-", app).CombinedOutput(); err != nil {
			t.Fatalf("codesign: %v\n%s", err, out)
		}
	}
	dmg, err := buildDMG(c, app, dir, buildOptions{sign: "-"})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(dmg) != "DMG Test 1.2.3.dmg" {
		t.Errorf("dmg = %s", dmg)
	}
	mnt := filepath.Join(dir, "mnt")
	if err := os.Mkdir(mnt, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("hdiutil", "attach", "-readonly", "-nobrowse", "-noautoopen", "-mountpoint", mnt, dmg).CombinedOutput(); err != nil {
		t.Fatalf("attach: %v\n%s", err, out)
	}
	defer exec.Command("hdiutil", "detach", "-force", mnt).Run()
	entries, err := os.ReadDir(mnt)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	for _, want := range []string{".DS_Store", ".VolumeIcon.icns", "Applications", "DMG Test.app"} {
		if !slices.Contains(names, want) {
			t.Errorf("image lacks %s: %q", want, names)
		}
	}
	if target, _ := os.Readlink(filepath.Join(mnt, "Applications")); target != "/Applications" {
		t.Errorf("Applications links to %q", target)
	}
	icon, err := os.ReadFile(filepath.Join(mnt, ".VolumeIcon.icns"))
	if err != nil || !bytes.Equal(icon, icns) {
		t.Fatalf("volume icon differs: %v", err)
	}
	icon, err = os.ReadFile(filepath.Join(mnt, filepath.Base(app), "Contents", "Resources", bundleIcon))
	if err != nil || !bytes.Equal(icon, icns) {
		t.Fatalf("bundle icon differs: %v", err)
	}
	flags, err := exec.Command("/usr/bin/xattr", "-px", "com.apple.FinderInfo", mnt).Output()
	if err != nil || strings.Join(strings.Fields(string(flags)), "") != "0000000000000000040000000000000000000000000000000000000000000000" {
		t.Fatalf("volume Finder flags = %q: %v", flags, err)
	}
	wantDS, err := dsStore(dmgRecords(filepath.Base(app)))
	if err != nil {
		t.Fatal(err)
	}
	gotDS, err := os.ReadFile(filepath.Join(mnt, ".DS_Store"))
	if err != nil || !bytes.Equal(gotDS, wantDS) {
		t.Fatalf("layout differs: %v", err)
	}
	for i := range smallFiles {
		data, err := os.ReadFile(filepath.Join(mnt, filepath.Base(app), "Contents", "Resources", "small", fmt.Sprintf("%05d", i)))
		if err != nil || string(data) != "x" {
			t.Fatalf("small file %d differs: %v", i, err)
		}
	}
	if sparse {
		payload := filepath.Join(mnt, filepath.Base(app), "Contents", "Resources", "sparse.bin")
		data, err := os.ReadFile(payload)
		if err != nil {
			t.Fatal(err)
		}
		if len(data) != sparseSize || data[0] != 'x' || data[len(data)-1] != 'x' || bytes.Count(data[1:len(data)-1], []byte{0}) != sparseSize-2 {
			t.Fatal("sparse resource contents differ")
		}
		info, err := os.Stat(payload)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o640 || !info.ModTime().Equal(sparseTime) {
			t.Fatalf("sparse resource metadata differs: mode %v, time %v", info.Mode(), info.ModTime())
		}
		out, err := exec.Command("/usr/bin/xattr", "-p", "org.mygo.test", payload).Output()
		if err != nil || strings.TrimSpace(string(out)) != "sparse resource" {
			t.Fatalf("sparse resource xattr = %q: %v", out, err)
		}
		for _, bundle := range []string{app, filepath.Join(mnt, filepath.Base(app))} {
			if out, err := exec.Command("codesign", "--verify", "--deep", "--strict", bundle).CombinedOutput(); err != nil {
				t.Fatalf("signature of %s: %v\n%s", bundle, err, out)
			}
		}
	}
	info, _ := exec.Command("hdiutil", "imageinfo", dmg).Output()
	if !strings.Contains(string(info), "lzma") {
		t.Error("image is not LZMA compressed")
	}
	if !strings.Contains(string(info), "Apple_HFS") {
		t.Error("image does not contain an HFS+ partition")
	}
}

// TestFrontendOverlay compiles an app with the overlay that embeds its
// frontendDist, which lives outside the app's package.
func TestFrontendOverlay(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a program")
	}
	files := map[string]string{
		"cmd/app/main.go":          "package main\n\nimport \"github.com/egoist/mygo\"\n\nfunc main() { mygo.App.Run() }\n",
		"web/dist/index.html":      "<p>embedded frontend</p>",
		"web/dist/assets/app-1.js": "console.log('embedded asset')",
		"web/dist/_routes/.hidden": "hidden file",
	}
	dir := testModule(t, files)

	c := &Config{root: dir, Main: "./cmd/app", FrontendDist: "web/missing"}
	pkg, err := packageDir(c)
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	if _, err := frontendFiles(c, pkg, work); err == nil || !strings.Contains(err.Error(), "buildCommand") {
		t.Errorf("missing frontendDist: %v", err)
	}
	c.FrontendDist = "web/dist"
	embedded, err := frontendFiles(c, pkg, work)
	if err != nil {
		t.Fatal(err)
	}
	overlay, err := writeOverlay(filepath.Join(work, "overlay.json"), embedded)
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "app")
	if err := buildBinary(c, bin, nil, "-overlay", overlay); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	for _, content := range files {
		if strings.HasPrefix(content, "<p>") || strings.HasPrefix(content, "console") || content == "hidden file" {
			if !bytes.Contains(data, []byte(content)) {
				t.Errorf("the binary lacks %q", content)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "cmd", "app", frontendGenFile)); err == nil {
		t.Error("the overlay must not write into the project")
	}
}

// testModule writes files into a new Go module that requires MyGo from this
// checkout, with MyGo's own requirements so that building it downloads
// nothing, and returns its directory.
func testModule(t *testing.T, files map[string]string) string {
	t.Helper()
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeFiles(t, dir, files)
	mod, err := os.ReadFile(filepath.Join(repo, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	gomod := "module example.com/app\n\nrequire github.com/egoist/mygo v0.0.0\n\nreplace github.com/egoist/mygo => " + repo + "\n"
	for _, line := range strings.Split(string(mod), "\n") {
		if strings.HasPrefix(line, "go ") {
			gomod += line + "\n" // the go version MyGo needs
		}
		if f := strings.Fields(strings.TrimPrefix(strings.TrimSpace(line), "require ")); len(f) >= 2 && strings.Contains(f[0], ".") && strings.HasPrefix(f[1], "v") {
			gomod += "require " + f[0] + " " + f[1] + "\n"
		}
	}
	sum, err := os.ReadFile(filepath.Join(repo, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	writeFiles(t, dir, map[string]string{"go.mod": gomod, "go.sum": string(sum)})
	return dir
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestWindowsResources links the generated resources into Windows
// executables and finds them in the .rsrc section.
func TestWindowsResources(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles programs")
	}
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"go.mod":   "module example.com/restest\n\n" + goDirective(t) + "\n",
		"main.go":  "package main\n\nfunc main() {}\n",
		"icon.png": string(defaultIcon()),
	})
	c := &Config{root: dir, Name: "Res Test", Version: "1.2.3", Identifier: "com.example.restest", Icon: "icon.png"}
	c.applyDefaults()
	for _, arch := range []string{"amd64", "arm64", "386"} {
		cleanup, err := windowsResources(c, dir, arch)
		if err != nil {
			t.Fatal(err)
		}
		exe := filepath.Join(t.TempDir(), "app.exe")
		err = buildBinary(c, exe, []string{"GOOS=windows", "GOARCH=" + arch})
		cleanup()
		if err != nil {
			t.Fatalf("%s: %v", arch, err)
		}
		if left := sysoFiles(dir); len(left) > 0 {
			t.Errorf("%s: left %q in the package", arch, left)
		}
		data := peResources(t, exe)
		for what, want := range map[string][]byte{
			"version":     utf16le("com.example.restest"),
			"version key": utf16le("MyGoIdentifier"),
			"manifest":    []byte("PerMonitorV2"),
			"icon":        []byte("\x89PNG"),
		} {
			if !bytes.Contains(data, want) {
				t.Errorf("%s: the resources lack the %s", arch, what)
			}
		}
	}
}

// goDirective returns the go line of MyGo's go.mod, for the modules that
// tests build.
func goDirective(t *testing.T) string {
	t.Helper()
	repo, _ := filepath.Abs("../..")
	mod, err := os.ReadFile(filepath.Join(repo, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(mod), "\n") {
		if strings.HasPrefix(line, "go ") {
			return strings.TrimSpace(line)
		}
	}
	t.Fatal("go.mod has no go line")
	return ""
}

// peResources returns the .rsrc section of a Windows executable.
func peResources(t *testing.T, exe string) []byte {
	t.Helper()
	f, err := pe.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sec := f.Section(".rsrc")
	if sec == nil {
		t.Fatalf("%s has no .rsrc section", exe)
	}
	data, err := sec.Data()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// utf16le encodes ASCII text as Windows resources store strings.
func utf16le(s string) []byte {
	var b []byte
	for _, r := range s {
		b = append(b, byte(r), 0)
	}
	return b
}
