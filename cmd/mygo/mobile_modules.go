package main

import (
	"context"
	"debug/elf"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/egoist/mygo/internal/nativelib"
)

// Native modules are built for phones as desktop builds build them from
// their Zig sources (see buildNative): zig build for the phone's target.
// Zig brings macOS's SDK, not the phones', so the CLI names the platform's
// in the search prefixes and libc file it passes, made here: a phone's
// build carries the module in the app, as embedFrameworks carries the
// plugins' C on iOS and copyAndroidNatives on Android.

// phoneModules returns the native modules the app's packages are, for
// goos/goarch, in the Zig implementation phones build: what a phone's app
// carries.
func (c *Config) phoneModules(goos, goarch string) ([]phoneModule, error) {
	pkgs, err := c.nativePackages(goos, goarch)
	if err != nil {
		return nil, err
	}
	var out []phoneModule
	seen := map[string]bool{}
	for _, p := range pkgs {
		if p.module == nil || seen[p.module.Name] {
			continue
		}
		l, ok := p.manifest.Lookup(p.module.Name)
		if !ok {
			continue
		}
		l = p.library(l, goos, "zig")
		if l.Build == nil {
			// No language implements the module here: its constructors
			// return errors.ErrUnsupported, as on desktops.
			continue
		}
		seen[p.module.Name] = true
		out = append(out, phoneModule{dir: p.dir, name: p.module.Name, lib: l})
	}
	return out, nil
}

// phoneModule is a native module's library to build for a phone.
type phoneModule struct {
	dir  string
	name string
	lib  nativelib.Library
}

// build builds the module with zig build for triple, whose builds are
// cached as key among the module's, and returns the library whose
// extension is ext and whose file name is want.
func (m phoneModule) build(triple, key, ext, want string, tk *task, extra ...string) (string, error) {
	root, err := nativelib.ModuleRoot(m.dir)
	if err != nil {
		return "", err
	}
	_, src := m.lib.Build.Tool()
	src = filepath.Join(m.dir, filepath.FromSlash(src))
	if tk != nil {
		tk.set("compiling " + m.name)
	}
	lib, err := buildZigTarget(root, src, m.lib, triple, key, ext, want, extra...)
	if err != nil {
		err = fmt.Errorf("building native module %s for %s: %w", m.name, key, err)
		if strings.Contains(err.Error(), "__dyld_get_image_header_containing_address") {
			return "", fmt.Errorf("%w\nthe module's generated Zig (src/mygo.zig) predates phone builds: run mygo generate in it", err)
		}
		return "", err
	}
	return lib, nil
}

// iosModuleTriple returns the Zig target of the app's iOS toolchain: the
// device's, or the simulator's, whose minimum version Zig picks itself.
func iosModuleTriple(t *iosToolchain) string {
	if t.simulator {
		return "aarch64-ios-simulator"
	}
	min := strings.TrimSpace(t.minimumVersion)
	if min == "" {
		min = "15.0"
	}
	return "aarch64-ios." + min
}

// androidModuleTriple returns the Zig target of goarch on Android, at
// api: 32-bit ARM is androideabi, soft float's, and arm's android triple
// would ask the hard float arm's clang refuses.
func androidModuleTriple(goarch string, api int) (string, error) {
	arch, abi := "", "android"
	switch goarch {
	case "amd64":
		arch = "x86_64"
	case "arm64":
		arch = "aarch64"
	case "arm":
		arch, abi = "arm", "androideabi"
	case "386":
		arch = "x86"
	default:
		return "", fmt.Errorf("cannot build native libraries for android/%s", goarch)
	}
	return arch + "-linux-" + abi + "." + strconv.Itoa(api), nil
}

// appleModulesPrefix makes the search prefix with which zig build finds
// the SDK's headers and libraries, which it brings for macOS only, and
// returns it.
func appleModulesPrefix(dir, sdk string) (string, error) {
	prefix := filepath.Join(dir, "prefix")
	if err := os.RemoveAll(prefix); err != nil {
		return "", err
	}
	if err := os.MkdirAll(prefix, 0o755); err != nil {
		return "", err
	}
	for _, d := range []string{"include", "lib"} {
		if err := os.Symlink(filepath.Join(sdk, "usr", d), filepath.Join(prefix, d)); err != nil {
			return "", err
		}
	}
	return prefix, nil
}

// androidModulesSDK writes the files with which zig build links for an
// Android ABI at api, the NDK's libc and libraries, and returns the libc
// file and the search prefix: Zig brings Android's C library neither,
// and cannot link a C library that -L alone names.
func androidModulesSDK(dir string, t *androidToolchain, abi string, api int) (libc, prefix string, err error) {
	machine, ok := map[string]elf.Machine{"arm64-v8a": elf.EM_AARCH64, "armeabi-v7a": elf.EM_ARM, "x86_64": elf.EM_X86_64}[abi]
	if !ok {
		return "", "", fmt.Errorf("Android has no ABI %s", abi)
	}
	sysroot := filepath.Join(filepath.Dir(t.ndkBin), "sysroot")
	triple, err := ndkSysrootTriple(sysroot, strconv.Itoa(api), abi, machine)
	if err != nil {
		return "", "", err
	}
	libdir := filepath.Join(sysroot, "usr", "lib", triple, strconv.Itoa(api))
	if err := os.RemoveAll(dir); err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(filepath.Join(dir, "prefix", "lib"), 0o755); err != nil {
		return "", "", err
	}
	// The keys of the file `zig libc` writes for a system.
	libc = filepath.Join(dir, "libc.txt")
	content := strings.Join([]string{
		"include_dir=" + filepath.Join(sysroot, "usr", "include"),
		"sys_include_dir=" + filepath.Join(sysroot, "usr", "include", triple),
		"crt_dir=" + libdir,
		"msvc_lib_dir=",
		"kernel32_lib_dir=",
		"gcc_dir=",
		"",
	}, "\n")
	if err := os.WriteFile(libc, []byte(content), 0o644); err != nil {
		return "", "", err
	}
	prefix = filepath.Join(dir, "prefix")
	entries, err := os.ReadDir(libdir)
	if err != nil {
		return "", "", err
	}
	for _, e := range entries {
		if err := os.Symlink(filepath.Join(libdir, e.Name()), filepath.Join(prefix, "lib", e.Name())); err != nil {
			return "", "", err
		}
	}
	return libc, prefix, nil
}

// ndkSysrootTriple returns the sysroot's directory of an ABI's libraries
// and headers at api: the NDK's clang names ARM's clang triple
// armv7a-linux-androideabi where its sysroot has arm-linux-androideabi,
// so the directory is the one whose libc.so is of the ABI's machine.
func ndkSysrootTriple(sysroot, api, abi string, machine elf.Machine) (string, error) {
	libs := filepath.Join(sysroot, "usr", "lib")
	entries, err := os.ReadDir(libs)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if !e.IsDir() || !isDir(filepath.Join(sysroot, "usr", "include", e.Name())) {
			continue
		}
		f, err := elf.Open(filepath.Join(libs, e.Name(), api, "libc.so"))
		if err != nil {
			continue
		}
		m := f.Machine
		f.Close()
		if m == machine {
			return e.Name(), nil
		}
	}
	return "", fmt.Errorf("the NDK in %s has no %s libraries for API %s", filepath.Dir(sysroot), abi, api)
}

// embedModuleFrameworks builds the app's native modules for iOS into the
// bundle's Frameworks, where iOS takes code besides the app's executable,
// and signs them with the app: signing takes what is in the bundle as it
// is.
func (c *Config) embedModuleFrameworks(ctx context.Context, t *iosToolchain, tk *task, work, bundle string) error {
	mods, err := c.phoneModules("ios", "arm64")
	if err != nil {
		return err
	}
	if len(mods) == 0 {
		return nil
	}
	prefix, err := appleModulesPrefix(filepath.Join(work, "modules"), t.sdk)
	if err != nil {
		return err
	}
	for _, m := range mods {
		lib, err := m.linkForIOS(ctx, t, work, prefix, tk)
		if err != nil {
			return err
		}
		if err := writeFramework(bundle, m.name, c, t, lib); err != nil {
			return err
		}
	}
	return nil
}

// linkForIOS builds the module for iOS and links it into the dynamic
// library a framework holds with the SDK's clang. Zig builds the module as
// a static library there: the dynamic libraries Zig's own linker makes for
// iOS fail the device's code signature check, while the SDK's linker, as
// Xcode's, passes it.
func (m phoneModule) linkForIOS(ctx context.Context, t *iosToolchain, work, prefix string, tk *task) (string, error) {
	name := strings.TrimPrefix(m.name, "lib")
	archive, err := m.build(iosModuleTriple(t), "ios-"+t.sdkName, ".a", "lib"+name+".a", tk, "--search-prefix", prefix)
	if err != nil {
		return "", err
	}
	lib := filepath.Join(work, "modules", name+".dylib")
	args := []string{"-target", t.target, "-isysroot", t.sdk, "-dynamiclib",
		// The archive's objects are all linked, those nothing else names
		// included: the module's entry points are found by name at run time.
		"-Wl,-force_load," + archive,
		"-install_name", "@rpath/" + m.name + ".framework/" + m.name,
		"-o", lib}
	if o, err := exec.CommandContext(ctx, t.clang, args...).CombinedOutput(); err != nil {
		return "", fmt.Errorf("linking native module %s for iOS: %v\n%s", m.name, err, strings.TrimSpace(string(o)))
	}
	return lib, nil
}

// copyAndroidModuleNatives builds the app's native modules for abi into
// jniLibs, which the APK takes with the app's Go code, named as
// mygo.NativeLibrary names them: lib<module>.so.
func (c *Config) copyAndroidModuleNatives(ctx context.Context, t *androidToolchain, tk *task, abi, goarch, work, jniLibs string) error {
	mods, err := c.phoneModules("android", goarch)
	if err != nil {
		return err
	}
	if len(mods) == 0 {
		return nil
	}
	api := max(c.Android.MinSdk, 21)
	triple, err := androidModuleTriple(goarch, api)
	if err != nil {
		return err
	}
	libc, prefix, err := androidModulesSDK(filepath.Join(work, "modules-"+abi), t, abi, api)
	if err != nil {
		return err
	}
	for _, m := range mods {
		lib, err := m.build(triple, "android-"+abi+"-"+strconv.Itoa(api), nativelib.Ext("android"), nativelib.FileName(m.name, "android"), tk, "--libc", libc, "--search-prefix", prefix)
		if err != nil {
			return err
		}
		out := filepath.Join(jniLibs, abi, "lib"+m.name+".so")
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		if err := replaceFile(lib, out); err != nil {
			return err
		}
	}
	return nil
}
