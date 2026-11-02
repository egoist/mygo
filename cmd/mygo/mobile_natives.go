package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// nativeName is what a plugin's C library may be named, which paths are made of.
var nativeName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)

// pluginNative is C code of a plugin (mobile.native in its mygo-plugin.json),
// which the CLI compiles for the phone at hand, with the compiler phones need
// anyway: an app gets it as a dynamic framework on iOS and a shared library on
// Android, what mygo.NativeLibrary names. Phones can't take libraries built
// elsewhere as desktops do: on iOS they are signed with the app, and only a
// compiler of the SDK targets them.
//
//	{"mobile": {"id": "sqlite", "native": {
//		"name": "mygo-sqlite3",
//		"archive": {"url": "https://...zip", "sha256": "...", "files": ["sqlite3.c", "sqlite3.h"]},
//		"sources": ["native/sqlite.c"],
//		"cflags": ["-DSQLITE_THREADSAFE=1"],
//		"abis": ["arm64-v8a", "x86_64"]
//	}}}
type pluginNative struct {
	Name string `json:"name"`
	// Archive is a zip of more sources, checked against its SHA-256, of which
	// the files named, wherever in it, are taken.
	Archive *nativeArchive `json:"archive"`
	// Sources are patterns of C files within the package.
	Sources []string `json:"sources"`
	Cflags  []string `json:"cflags"`
	Ldflags []string `json:"ldflags"`
	// ABIs limits Android's builds; all by default.
	ABIs []string `json:"abis"`
}

type nativeArchive struct {
	URL    string   `json:"url"`
	SHA256 string   `json:"sha256"`
	Files  []string `json:"files"`
}

// nativeTarget says how to compile for one target of phones.
type nativeTarget struct {
	// key names the target among the builds cached.
	key string
	// compiler is the command, and args come first after it.
	compiler []string
	args     []string
	// output is the name of the file.
	output func(name string) string
	tk     *task
}

// compile returns the file of p built for t, from the user's cache when it
// compiled before: SQLite takes half a minute, which a build needs once.
func (n *pluginNative) compile(ctx context.Context, p *mobilePlugin, t nativeTarget) (string, error) {
	sources, err := p.sourcesOf(n.Sources)
	if err != nil {
		return "", err
	}
	if n.Archive == nil && len(sources) == 0 {
		return "", fmt.Errorf("plugin %s: mobile.native has no sources", p.ID)
	}
	if !nativeName.MatchString(n.Name) {
		return "", fmt.Errorf("plugin %s: mobile.native.name %q must be a name", p.ID, n.Name)
	}
	h := sha256.New()
	spec, err := json.Marshal([]any{n, t.key, t.compiler, t.args})
	if err != nil {
		return "", err
	}
	h.Write(spec)
	// With the headers beside them, which they may include.
	hashed := slices.Clone(sources)
	seen := map[string]bool{}
	for _, f := range sources {
		dir := filepath.Dir(f)
		if !seen[dir] {
			seen[dir] = true
			headers, _ := filepath.Glob(filepath.Join(dir, "*.h"))
			hashed = append(hashed, headers...)
		}
	}
	for _, f := range hashed {
		data, err := os.ReadFile(f)
		if err != nil {
			return "", err
		}
		io.WriteString(h, filepath.Base(f)+"\x00")
		h.Write(data)
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cache, "mygo", "natives-built", hex.EncodeToString(h.Sum(nil)))
	out := filepath.Join(dir, t.output(n.Name))
	if fileExists(out) {
		return out, nil
	}
	if err := os.MkdirAll(cache, 0o755); err != nil {
		return "", err
	}
	work, err := os.MkdirTemp(cache, "mygo-native-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(work)
	args := append(append(append([]string{}, t.compiler[1:]...), t.args...), "-O2", "-fPIC", "-DNDEBUG", "-w", "-I", work)
	args = append(args, n.Cflags...)
	if n.Archive != nil {
		files, err := n.extract(work)
		if err != nil {
			return "", fmt.Errorf("plugin %s: %w", p.ID, err)
		}
		for _, f := range files {
			if strings.HasSuffix(f, ".c") {
				args = append(args, f)
			}
		}
	}
	args = append(args, sources...)
	args = append(args, n.Ldflags...)
	built := filepath.Join(work, t.output(n.Name))
	args = append(args, "-o", built)
	if t.tk != nil {
		t.tk.set("compiling " + n.Name)
	}
	if o, err := exec.CommandContext(ctx, t.compiler[0], args...).CombinedOutput(); err != nil {
		return "", fmt.Errorf("plugin %s: compiling %s: %v\n%s", p.ID, n.Name, err, strings.TrimSpace(string(o)))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(built, out); err != nil {
		return "", err
	}
	return out, nil
}

// extract puts the archive's files in dir, and returns them.
func (n *pluginNative) extract(dir string) ([]string, error) {
	a := n.Archive
	archive, err := fetchNative(nativeFile{Name: path.Base(a.URL), URL: a.URL, SHA256: a.SHA256})
	if err != nil {
		return nil, err
	}
	z, err := zip.OpenReader(archive)
	if err != nil {
		return nil, err
	}
	defer z.Close()
	var out []string
	for _, f := range z.File {
		name := path.Base(f.Name)
		if f.FileInfo().IsDir() || !slices.Contains(a.Files, name) {
			continue
		}
		if !filepath.IsLocal(name) || filepath.Base(name) != name {
			return nil, fmt.Errorf("%s: a file named %q", a.URL, name)
		}
		r, err := f.Open()
		if err != nil {
			return nil, err
		}
		to := filepath.Join(dir, name)
		w, err := os.Create(to)
		if err == nil {
			_, err = io.Copy(w, r)
			if cerr := w.Close(); err == nil {
				err = cerr
			}
		}
		r.Close()
		if err != nil {
			return nil, err
		}
		out = append(out, to)
	}
	if len(out) != len(a.Files) {
		return nil, fmt.Errorf("%s holds %d of the files %v", a.URL, len(out), a.Files)
	}
	return out, nil
}

// embedFrameworks compiles the plugins' C into frameworks of the bundle, where
// iOS takes code besides the app's executable, and signs them: signing the
// app takes those inside it as they are.
func (ps mobilePlugins) embedFrameworks(ctx context.Context, t *iosToolchain, tk *task, c *Config, bundle string) error {
	for _, p := range ps {
		n := p.Native
		if n == nil {
			continue
		}
		lib, err := n.compile(ctx, p, nativeTarget{
			key:      "ios-" + t.target,
			compiler: []string{t.clang},
			args: []string{"-target", t.target, "-isysroot", t.sdk, "-dynamiclib",
				"-install_name", "@rpath/" + n.Name + ".framework/" + n.Name},
			output: func(name string) string { return name },
			tk:     tk,
		})
		if err != nil {
			return err
		}
		dir := filepath.Join(bundle, "Frameworks", n.Name+".framework")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		data, err := os.ReadFile(lib)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, n.Name), data, 0o755); err != nil {
			return err
		}
		info := map[string]any{
			"CFBundleIdentifier":            c.Identifier + "." + n.Name,
			"CFBundleExecutable":            n.Name,
			"CFBundleName":                  n.Name,
			"CFBundlePackageType":           "FMWK",
			"CFBundleShortVersionString":    c.Version,
			"CFBundleVersion":               c.Version,
			"CFBundleInfoDictionaryVersion": "6.0",
			"MinimumOSVersion":              t.minimumVersion,
			"CFBundleSupportedPlatforms":    []any{map[bool]string{true: "iPhoneSimulator", false: "iPhoneOS"}[t.simulator]},
		}
		if err := os.WriteFile(filepath.Join(dir, "Info.plist"), plistBytes(info), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// signForSimulator signs the bundle for a simulator, with no identity. Entitlements
// don't go in such a signature, which the simulator then refuses to launch: they're
// in the executable (simulatorEntitlements), as Xcode has them.
func signForSimulator(bundle string) error {
	if err := signFrameworks(bundle, "-"); err != nil {
		return err
	}
	return runQuiet("codesign", "--force", "--sign", "-", bundle)
}

// simulatorEntitlements returns the flags that link what the plugins need the app
// signed with into a simulator's executable.
func (ps mobilePlugins) simulatorEntitlements(work string) ([]string, error) {
	ent := ps.entitlements()
	if len(ent) == 0 {
		return nil, nil
	}
	path := filepath.Join(work, "simulator.entitlements")
	if err := os.WriteFile(path, plistBytes(ent), 0o644); err != nil {
		return nil, err
	}
	return []string{"-Xlinker", "-sectcreate", "-Xlinker", "__TEXT", "-Xlinker", "__entitlements", "-Xlinker", path}, nil
}

// signFrameworks signs what embedFrameworks put in the bundle.
func signFrameworks(bundle, identity string) error {
	dirs, err := filepath.Glob(filepath.Join(bundle, "Frameworks", "*.framework"))
	if err != nil {
		return err
	}
	for _, dir := range dirs {
		if err := runQuiet("codesign", "--force", "--sign", identity, dir); err != nil {
			return err
		}
	}
	return nil
}

// copyAndroidNatives compiles the plugins' C for abi into jniLibs, which the
// APK takes with the app's Go code.
func (ps mobilePlugins) copyAndroidNatives(ctx context.Context, t *androidToolchain, tk *task, c *Config, abi, jniLibs string) error {
	for _, p := range ps {
		n := p.Native
		if n == nil || (len(n.ABIs) > 0 && !slices.Contains(n.ABIs, abi)) {
			continue
		}
		name := "lib" + n.Name + ".so"
		lib, err := n.compile(ctx, p, nativeTarget{
			key:      "android-" + abi + "-" + strconv.Itoa(c.Android.MinSdk),
			compiler: []string{t.clang(c, abi)},
			args:     []string{"-shared", "-Wl,-z,max-page-size=16384", "-Wl,-soname," + name},
			output:   func(string) string { return name },
			tk:       tk,
		})
		if err != nil {
			return err
		}
		data, err := os.ReadFile(lib)
		if err != nil {
			return err
		}
		out := filepath.Join(jniLibs, abi, name)
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(out, data, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// builtForPhones reports whether the file of a plugin compiles a library
// itself for phones: its downloads are for desktops.
func builtForPhones(data []byte, name string) bool {
	var f struct {
		Mobile *struct {
			Native *pluginNative `json:"native"`
		} `json:"mobile"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return false
	}
	return f.Mobile != nil && f.Mobile.Native != nil && f.Mobile.Native.Name == name
}
