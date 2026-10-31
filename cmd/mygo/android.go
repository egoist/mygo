package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"

	androidHost "github.com/egoist/mygo/internal/mobile/android"
)

// The Android app is the Kotlin host (internal/mobile/android), a Gradle
// project mygo build writes into .mygo/android and builds, with the app's
// Go code built as a shared library for each ABI by the NDK's clang:
// Android loads Go into its process, which needs cgo to link. Gradle
// builds the .apk and the .aab with the SDK's tools.

// androidABIs are the ABIs apps ship for: what Google Play asks of apps
// with native code, and the emulators'.
var androidABIs = []struct{ abi, goarch, triple string }{
	{"arm64-v8a", "arm64", "aarch64-linux-android"},
	{"armeabi-v7a", "arm", "armv7a-linux-androideabi"},
	{"x86_64", "amd64", "x86_64-linux-android"},
}

// androidToolchain is the Android SDK, its NDK and the JDK Gradle runs on.
type androidToolchain struct {
	sdk, ndkBin, javaHome string
}

func newAndroidToolchain() (*androidToolchain, error) {
	t := &androidToolchain{}
	home, _ := os.UserHomeDir()
	for _, d := range []string{os.Getenv("ANDROID_HOME"), os.Getenv("ANDROID_SDK_ROOT"),
		filepath.Join(home, "Library", "Android", "sdk"), filepath.Join(home, "Android", "Sdk"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Android", "Sdk")} {
		if d != "" && isDir(filepath.Join(d, "platforms")) {
			t.sdk = d
			break
		}
	}
	if t.sdk == "" {
		return nil, errors.New("no Android SDK: install Android Studio, or set ANDROID_HOME")
	}
	ndk := os.Getenv("ANDROID_NDK_HOME")
	if ndk == "" {
		versions, _ := filepath.Glob(filepath.Join(t.sdk, "ndk", "*"))
		sort.Slice(versions, func(i, j int) bool { return versionLess(filepath.Base(versions[i]), filepath.Base(versions[j])) })
		if len(versions) > 0 {
			ndk = versions[len(versions)-1]
		}
	}
	if ndk == "" {
		return nil, errors.New("no Android NDK: install it in Android Studio (SDK Manager › SDK Tools › NDK), or set ANDROID_NDK_HOME")
	}
	hosts, _ := filepath.Glob(filepath.Join(ndk, "toolchains", "llvm", "prebuilt", "*", "bin"))
	if len(hosts) == 0 {
		return nil, fmt.Errorf("the NDK in %s has no clang", ndk)
	}
	t.ndkBin = hosts[0]
	t.javaHome = os.Getenv("JAVA_HOME")
	if t.javaHome == "" {
		for _, d := range []string{"/Applications/Android Studio.app/Contents/jbr/Contents/Home",
			filepath.Join(home, "Applications", "Android Studio.app", "Contents", "jbr", "Contents", "Home"),
			"/opt/android-studio/jbr", filepath.Join(os.Getenv("ProgramFiles"), "Android", "Android Studio", "jbr")} {
			if isDir(d) {
				t.javaHome = d
				break
			}
		}
	}
	return t, nil
}

// versionLess orders NDK versions, 27.1.12297006 after 27.0.12077973.
func versionLess(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		na, _ := strconv.Atoi(pa[i])
		nb, _ := strconv.Atoi(pb[i])
		if na != nb {
			return na < nb
		}
	}
	return len(pa) < len(pb)
}

// buildGo builds the app's Go code for abi into jniLibs/<abi>/libmygoapp.so.
func (t *androidToolchain) buildGo(ctx context.Context, c, dc *Config, tk *task, opts buildOptions, abi, jniLibs string) error {
	var a struct{ abi, goarch, triple string }
	for _, x := range androidABIs {
		if x.abi == abi {
			a = x
		}
	}
	clang := filepath.Join(t.ndkBin, a.triple+strconv.Itoa(max(c.Android.MinSdk, 21))+"-clang")
	if runtime.GOOS == "windows" {
		clang += ".cmd"
	}
	env := []string{"GOOS=android", "GOARCH=" + a.goarch, "CGO_ENABLED=1", "CC=" + clang,
		// 16 KB pages, which new devices have and Google Play asks for.
		"CGO_LDFLAGS=-Wl,-z,max-page-size=16384"}
	if a.goarch == "arm" {
		env = append(env, "GOARM=7")
	}
	ldflags := "-s -w" + packageFlags(dc)
	if !opts.debug {
		ldflags += " -X github.com/egoist/mygo.production=1"
	}
	ldflags += extraLDFlags()
	flags := []string{"-trimpath", "-buildmode=c-shared", "-ldflags", ldflags}
	flags = append(flags, productionTags(opts.debug)...)
	overlay, err := writeOverlay(filepath.Join(opts.work, "overlay-android.json"), opts.overlay)
	if err != nil {
		return err
	}
	if overlay != "" {
		flags = append(flags, "-overlay", overlay)
	}
	out := filepath.Join(jniLibs, abi, "libmygoapp.so")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	tk.set("compiling Go for " + abi)
	if err := goBuild(ctx, c, tk, out, env, flags...); err != nil {
		return err
	}
	os.Remove(strings.TrimSuffix(out, ".so") + ".h")
	return nil
}

// project writes the host's Gradle project into dir, where Gradle keeps
// its outputs between builds, and leaves files that did not change as
// they are.
func (t *androidToolchain) project(dir string) error {
	return fs.WalkDir(androidHost.Files, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || path == "." {
			return err
		}
		dst := filepath.Join(dir, filepath.FromSlash(path))
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		data, err := fs.ReadFile(androidHost.Files, path)
		if err != nil {
			return err
		}
		if old, err := os.ReadFile(dst); err == nil && bytes.Equal(old, data) {
			return nil
		}
		mode := os.FileMode(0o644)
		if filepath.Base(path) == "gradlew" {
			mode = 0o755
		}
		return os.WriteFile(dst, data, mode)
	})
}

// manifest is the app's AndroidManifest.xml: its permissions and URL
// schemes over the host's.
func androidManifest(c *Config) []byte {
	perms := []string{"android.permission.INTERNET", "android.permission.POST_NOTIFICATIONS"}
	for _, p := range c.Android.Permissions {
		if !strings.Contains(p, ".") {
			p = "android.permission." + p
		}
		if !slices.Contains(perms, p) {
			perms = append(perms, p)
		}
	}
	var b strings.Builder
	b.WriteString(`<manifest xmlns:android="http://schemas.android.com/apk/res/android">` + "\n")
	for _, p := range perms {
		fmt.Fprintf(&b, "    <uses-permission android:name=%q />\n", p)
	}
	b.WriteString(`    <application
        android:label="${mygoAppName}"
        android:icon="${mygoIcon}"
        android:supportsRtl="true"
        android:enableOnBackInvokedCallback="true"
        android:theme="@style/Theme.MyGo">
        <activity
            android:name="dev.mygo.MyGoActivity"
            android:exported="true"
            android:launchMode="singleTask"
            android:configChanges="orientation|screenSize|screenLayout|smallestScreenSize|keyboard|keyboardHidden|navigation|uiMode|fontScale|density|locale|layoutDirection"
            android:windowSoftInputMode="adjustResize">
            <intent-filter>
                <action android:name="android.intent.action.MAIN" />
                <category android:name="android.intent.category.LAUNCHER" />
            </intent-filter>
`)
	for _, s := range c.URLSchemes {
		fmt.Fprintf(&b, `            <intent-filter>
                <action android:name="android.intent.action.VIEW" />
                <category android:name="android.intent.category.DEFAULT" />
                <category android:name="android.intent.category.BROWSABLE" />
                <data android:scheme=%q />
            </intent-filter>
`, s)
	}
	b.WriteString("        </activity>\n    </application>\n</manifest>\n")
	return []byte(b.String())
}

// icons writes the launcher icon at each density into res.
func androidIcons(c *Config, res string) (bool, error) {
	if c.Icon == "" {
		return false, nil
	}
	src, err := os.ReadFile(c.path(c.Icon))
	if err != nil {
		return false, err
	}
	img, err := png.Decode(bytes.NewReader(src))
	if err != nil {
		return false, fmt.Errorf("%s: %v", c.Icon, err)
	}
	for density, size := range map[string]int{"mdpi": 48, "hdpi": 72, "xhdpi": 96, "xxhdpi": 144, "xxxhdpi": 192} {
		dir := filepath.Join(res, "mipmap-"+density)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return false, err
		}
		var b bytes.Buffer
		if err := png.Encode(&b, resize(img, size)); err != nil {
			return false, err
		}
		if err := os.WriteFile(filepath.Join(dir, "ic_launcher.png"), b.Bytes(), 0o644); err != nil {
			return false, err
		}
	}
	return true, nil
}

// versionCode is the integer version Google Play orders uploads by.
func (o Android) versionCode(version string) int {
	if o.VersionCode > 0 {
		return o.VersionCode
	}
	p := strings.SplitN(strings.SplitN(version, "-", 2)[0], ".", 3)
	n := 0
	for i, mul := range []int{10000, 100, 1} {
		if i < len(p) {
			v, _ := strconv.Atoi(p[i])
			n += v * mul
		}
	}
	return max(n, 1)
}

// gradle builds the app with the host's project in dir: tasks such as
// :app:assembleRelease, with the app's Go libraries in jniLibs and its
// resources, manifest and icons in work.
func (t *androidToolchain) gradle(ctx context.Context, c, dc *Config, dir, work, jniLibs string, tasks ...string) error {
	res := filepath.Join(work, "res")
	os.RemoveAll(res)
	hasIcon, err := androidIcons(dc, res)
	if err != nil {
		return err
	}
	manifest := filepath.Join(work, "AndroidManifest.xml")
	if err := os.WriteFile(manifest, androidManifest(dc), 0o644); err != nil {
		return err
	}
	assets := filepath.Join(work, "assets")
	os.RemoveAll(assets)
	list, err := dc.appResources("android", "arm64")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(assets, "resources"), 0o755); err != nil {
		return err
	}
	if err := copyResources(list, filepath.Join(assets, "resources")); err != nil {
		return err
	}
	props := map[string]string{
		"mygoApplicationId": dc.Identifier,
		"mygoAppName":       dc.Name,
		"mygoVersionCode":   strconv.Itoa(dc.Android.versionCode(dc.Version)),
		"mygoVersionName":   dc.Version,
		"mygoMinSdk":        strconv.Itoa(dc.Android.MinSdk),
		"mygoJniLibs":       jniLibs,
		"mygoAssets":        assets,
		"mygoRes":           res,
		"mygoManifest":      manifest,
	}
	if hasIcon {
		props["mygoIcon"] = "@mipmap/ic_launcher"
	}
	if ks := dc.Android.Keystore; ks != "" {
		pass := os.Getenv("MYGO_ANDROID_KEYSTORE_PASSWORD")
		if pass == "" {
			return errors.New("android.keystore needs its password in MYGO_ANDROID_KEYSTORE_PASSWORD")
		}
		props["mygoKeystore"] = c.path(ks)
		props["mygoKeystorePassword"] = pass
		props["mygoKeyAlias"] = dc.Android.KeyAlias
		if kp := os.Getenv("MYGO_ANDROID_KEY_PASSWORD"); kp != "" {
			props["mygoKeyPassword"] = kp
		}
	}
	args := []string{"-q", "--console=plain"}
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var env []string
	for _, k := range keys {
		// Passwords go in the environment, out of the command line that
		// other users' ps shows.
		env = append(env, "ORG_GRADLE_PROJECT_"+k+"="+props[k])
	}
	args = append(args, tasks...)
	gradlew := filepath.Join(dir, "gradlew")
	if runtime.GOOS == "windows" {
		gradlew += ".bat"
	}
	cmd := exec.CommandContext(ctx, gradlew, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "ANDROID_HOME="+t.sdk)
	if t.javaHome != "" {
		cmd.Env = append(cmd.Env, "JAVA_HOME="+t.javaHome)
	}
	cmd.Env = append(cmd.Env, env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("gradle failed: %v\n%s", err, bytes.TrimSpace(out))
	}
	return nil
}

// buildAndroid builds the app for Android: an .apk and an .aab for Google
// Play, for every ABI, or one (android/arm64, android/arm, android/amd64).
func buildAndroid(c *Config, opts buildOptions, arch string) ([]string, error) {
	t, err := newAndroidToolchain()
	if err != nil {
		return nil, err
	}
	var abis []string
	for _, a := range androidABIs {
		if arch == "" || arch == a.goarch || arch == a.abi {
			abis = append(abis, a.abi)
		}
	}
	if len(abis) == 0 {
		return nil, fmt.Errorf("unknown Android architecture %q: arm64, arm or amd64", arch)
	}
	dir := c.path(filepath.Join(".mygo", "android"))
	if err := t.project(dir); err != nil {
		return nil, err
	}
	work := filepath.Join(opts.work, "android")
	jniLibs := filepath.Join(work, "jniLibs")
	os.RemoveAll(jniLibs)
	tk := con.start("Building for Android")
	err = func() error {
		for _, abi := range abis {
			if err := t.buildGo(context.Background(), c, c, tk, opts, abi, jniLibs); err != nil {
				return err
			}
		}
		tk.set("building the APK and the bundle (Gradle)")
		return t.gradle(context.Background(), c, c, dir, work, jniLibs, ":app:assembleRelease", ":app:bundleRelease")
	}()
	if err := tk.end(err, "Built for Android"); err != nil {
		return nil, err
	}
	out := c.path(filepath.Join(c.Out, "android"))
	if err := os.MkdirAll(out, 0o755); err != nil {
		return nil, err
	}
	name := c.slug() + "-" + c.Version
	var artifacts []string
	for src, dst := range map[string]string{
		filepath.Join(dir, "app", "build", "outputs", "apk", "release", "app-release.apk"):    name + ".apk",
		filepath.Join(dir, "app", "build", "outputs", "bundle", "release", "app-release.aab"): name + ".aab",
	} {
		data, err := os.ReadFile(src)
		if err != nil {
			return nil, err
		}
		p := filepath.Join(out, dst)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			return nil, err
		}
		artifacts = append(artifacts, p)
	}
	sort.Strings(artifacts)
	if c.Android.Keystore == "" {
		warnf("Signed with the debug key: set android.keystore for Google Play")
	}
	return artifacts, nil
}

// adb runs adb of the SDK.
func adb(serial string, args ...string) *exec.Cmd {
	bin := "adb"
	if t, err := newAndroidToolchain(); err == nil {
		bin = filepath.Join(t.sdk, "platform-tools", "adb")
	}
	if serial != "" {
		args = append([]string{"-s", serial}, args...)
	}
	return exec.Command(bin, args...)
}

// androidDevice finds the device or emulator to run on: id if set (adb's
// serial), else the only one adb reaches, or the first.
func androidDevice(id string) (*mobileDevice, error) {
	out, err := adb("", "devices", "-l").Output()
	if err != nil {
		return nil, fmt.Errorf("adb: %v (install the SDK's platform tools)", err)
	}
	for _, l := range strings.Split(string(out), "\n")[1:] {
		f := strings.Fields(l)
		if len(f) < 2 || f[1] != "device" {
			continue
		}
		if id != "" && f[0] != id {
			continue
		}
		name := f[0]
		for _, x := range f[2:] {
			if m, ok := strings.CutPrefix(x, "model:"); ok {
				name = strings.ReplaceAll(m, "_", " ")
			}
		}
		return &mobileDevice{"android", f[0], name}, nil
	}
	if id != "" {
		return nil, fmt.Errorf("adb reaches no device %q (adb devices lists them)", id)
	}
	return nil, errors.New("adb reaches no device: start an emulator, or connect a device with USB debugging on")
}

// buildAndroidDev builds the debug APK for the device's ABI.
func (s *devSession) buildAndroidDev(ctx context.Context, c, dc *Config, t *task, opts buildOptions, stage string) (string, error) {
	tc, err := newAndroidToolchain()
	if err != nil {
		return "", err
	}
	abiOut, err := adb(s.device.id, "shell", "getprop", "ro.product.cpu.abi").Output()
	if err != nil {
		return "", fmt.Errorf("adb: %v", err)
	}
	abi := strings.TrimSpace(string(abiOut))
	dir := c.path(filepath.Join(".mygo", "android"))
	if err := tc.project(dir); err != nil {
		return "", err
	}
	jniLibs := filepath.Join(opts.work, "jniLibs")
	os.RemoveAll(jniLibs)
	if err := tc.buildGo(ctx, c, dc, t, opts, abi, jniLibs); err != nil {
		return "", err
	}
	t.set("building the APK (Gradle)")
	if err := tc.gradle(ctx, c, dc, dir, opts.work, jniLibs, ":app:assembleDebug"); err != nil {
		return "", err
	}
	apk := filepath.Join(stage, dc.slug()+".apk")
	data, err := os.ReadFile(filepath.Join(dir, "app", "build", "outputs", "apk", "debug", "app-debug.apk"))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(apk, 0o755); err != nil {
		return "", err
	}
	// A directory holding the APK, as replacePath and hashTree take.
	return apk, os.WriteFile(filepath.Join(apk, "app.apk"), data, 0o644)
}

// launchAndroid installs the APK and starts the app with env, its log the
// output of the process it returns.
func (s *devSession) launchAndroid(dc *Config, app string, env []string) (*exec.Cmd, func(), error) {
	apk := filepath.Join(app, "app.apk")
	if out, err := adb(s.device.id, "install", "-r", "-t", apk).CombinedOutput(); err != nil {
		return nil, nil, fmt.Errorf("adb install: %v\n%s", err, bytes.TrimSpace(out))
	}
	adb(s.device.id, "shell", "am", "force-stop", dc.Identifier).Run()
	adb(s.device.id, "logcat", "-c").Run()
	quoted := strings.ReplaceAll(strings.Join(env, "\n"), "'", `'\''`)
	if out, err := adb(s.device.id, "shell", "am start -W -n "+dc.Identifier+"/dev.mygo.MyGoActivity --es mygo.env '"+quoted+"'").CombinedOutput(); err != nil {
		return nil, nil, fmt.Errorf("adb am start: %v\n%s", err, bytes.TrimSpace(out))
	}
	// The app's log: what it writes (the tag mygo) and its crashes.
	cmd := adb(s.device.id, "logcat", "-v", "raw", "-s", "mygo:V", "AndroidRuntime:E", "DEBUG:V")
	serial, id := s.device.id, dc.Identifier
	return cmd, func() { adb(serial, "shell", "am", "force-stop", id).Run() }, nil
}

// reachDevURL returns the URL at which the device reaches the dev server
// at u: the simulator shares the Mac's network, and Android's devices
// reach it at the same port through adb reverse.
func (d *mobileDevice) reachDevURL(u string) (string, error) {
	if d.platform != "android" {
		return u, nil
	}
	p, err := url.Parse(u)
	if err != nil {
		return u, err
	}
	port := p.Port()
	if port == "" {
		port = map[string]string{"http": "80", "https": "443"}[p.Scheme]
	}
	if out, err := adb(d.id, "reverse", "tcp:"+port, "tcp:"+port).CombinedOutput(); err != nil {
		return u, fmt.Errorf("adb reverse: %v\n%s", err, bytes.TrimSpace(out))
	}
	if h := p.Hostname(); h != "localhost" && h != "127.0.0.1" {
		return u, nil
	}
	p.Host = "localhost:" + port
	return p.String(), nil
}
