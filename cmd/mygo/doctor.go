package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// webView2Download tells people where to get the runtime.
const webView2Download = "https://go.microsoft.com/fwlink/p/?LinkId=2124703"

// The WebView2 Runtime channels, most stable first, in the order the Windows
// backend tries them (internal/windows/webview2.go, whose webView2Channels
// this mirrors; a test keeps the two lists the same).
var webView2Channels = []string{
	"{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}", // Stable (Evergreen)
	"{2CD8A007-E189-409D-A2C8-9AF4EF3C72AA}", // Beta
	"{0D50BFEC-CD6A-4F9A-964C-C7416E3ACB10}", // Dev
	"{65C35B14-6C1D-4122-AC46-7148CC9D6497}", // Canary
}

func runDoctor(args []string) error {
	flags := newFlags("doctor", "", "Checks the tools and system libraries MyGo needs.")
	if err := flags.Parse(args); err != nil {
		return err
	}
	ok := true
	check := func(name string, good bool, detail string) {
		mark := "\033[32m✓\033[0m"
		if !good {
			mark, ok = "\033[31m✗\033[0m", false
		}
		fmt.Printf("%s %-10s %s\n", mark, name, detail)
	}

	goOut, err := exec.Command("go", "version").Output()
	check("go", err == nil, strings.TrimSpace(string(goOut)))
	if bunOut, err := exec.Command("bun", "--version").Output(); err == nil {
		check("bun", true, strings.TrimSpace(string(bunOut)))
	} else {
		fmt.Println("- bun        not found (optional, used by the frontend template: https://bun.sh)")
	}

	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("sw_vers", "-productVersion").Output()
		v := strings.TrimSpace(string(out))
		major, _ := strconv.Atoi(strings.Split(v, ".")[0])
		check("macOS", err == nil && major >= 12, v+" (WKWebView; macOS 12 or later is required)")
	case "linux":
		out, _ := exec.Command("sh", "-c", "ldconfig -p 2>/dev/null | grep -o 'libwebkit2gtk-4\\.[01]\\.so[.0-9]*' | sort -u").Output()
		libs := strings.TrimSpace(string(out))
		check("webkit", libs != "", or(libs, "WebKitGTK not found: install libwebkit2gtk-4.1-0 (Debian/Ubuntu) or webkit2gtk4.1 (Fedora)"))
	case "windows":
		version, dir, stale := webView2Runtime()
		switch {
		case version != "":
			check("webview2", true, version+" at "+dir)
		case stale != "":
			// EdgeUpdate keeps an install's registry entries after its files
			// are gone, and the Edge browser keeps one of the runtime's own
			// libraries beside itself: reading either alone promises a runtime
			// no window can use.
			check("webview2", false, "registered at "+stale+", which holds no runtime (no "+webView2BrowserName+" or "+webView2ClientName+"); reinstall it from "+webView2Download)
		default:
			check("webview2", false, "WebView2 runtime not found: "+webView2Download)
		}
	default:
		check(runtime.GOOS, false, "no MyGo backend for this platform yet")
	}

	// Tools of optional mygo build features.
	optional := func(name, feature string, found bool, detail string) {
		if found {
			fmt.Printf("\033[32m✓\033[0m %-10s %s\n", name, detail)
		} else {
			fmt.Printf("- %-10s not found (optional: %s)\n", name, feature)
		}
	}
	tool := func(name string) string {
		p, _ := exec.LookPath(name)
		return p
	}
	if nsis := makensis(); nsis != "" || runtime.GOOS != "windows" {
		optional("makensis", "the Windows installer, https://nsis.sourceforge.io", nsis != "", nsis)
	} else {
		fmt.Printf("- %-10s not found (mygo build downloads NSIS %s for the Windows installer)\n", "makensis", nsisRelease.version)
	}
	if runtime.GOOS == "windows" {
		st := signtool()
		optional("signtool", "signing Windows apps, from the Windows SDK", st != "", st)
	} else {
		ossl := tool("osslsigncode")
		optional("osslsign", "signing Windows apps here (osslsigncode)", ossl != "", ossl)
	}
	gh := tool("gh")
	optional("gh", "mygo build -upload to GitHub releases, https://cli.github.com", gh != "", gh)
	if runtime.GOOS == "darwin" {
		out, _ := exec.Command("security", "find-identity", "-v", "-p", "codesigning").Output()
		ids := strings.Count(string(out), "Developer ID Application")
		optional("codesign", "a Developer ID to sign apps for other Macs", ids > 0, fmt.Sprintf("%d Developer ID identities", ids))
	}

	if !ok {
		return fmt.Errorf("some checks failed")
	}
	return nil
}

func or(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

const (
	webView2Value       = "EBWebView"
	webView2ClientName  = "EmbeddedBrowserWebView.dll"
	webView2BrowserName = "msedgewebview2.exe"
)

// webView2Runtime reports the WebView2 Runtime a window showing a web page
// would get: its version and the directory EdgeUpdate points at. Both are
// empty when there is none, in which case stale names a registered runtime
// whose files are missing, or is empty too.
//
// This is the check the backend makes before it fails with "the Microsoft
// Edge WebView2 Runtime is not installed", and not the install EdgeUpdate
// registers, which outlives the runtime it describes.
func webView2Runtime() (version, dir, stale string) {
	// An official loader next to the executable, and a fixed version runtime
	// named by the environment, come before what is installed.
	if fixed := os.Getenv("WEBVIEW2_BROWSER_EXECUTABLE_FOLDER"); fixed != "" {
		if webView2Client(fixed) == "" {
			return "", "", fixed
		}
		return filepath.Base(fixed), fixed, ""
	}
	for _, channel := range webView2Channels {
		var bestDir, missing string
		var bestVersion []int
		for _, key := range webView2StateKeys(channel) {
			// A value's path holds spaces, so it is read whole rather than as
			// the last field of the line.
			dir := regValue(key, webView2Value)
			if dir == "" {
				continue
			}
			// EdgeUpdate names a channel's runtime by the directory it installs
			// it to, whose last part is its version.
			v := parseWebView2Version(filepath.Base(dir))
			switch {
			case webView2Client(dir) != "":
				if bestDir == "" || compareWebView2Versions(v, bestVersion) > 0 {
					bestDir, bestVersion = dir, v
				}
			case missing == "":
				missing = dir
			}
		}
		if bestDir != "" {
			return filepath.Base(bestDir), bestDir, ""
		}
		if missing != "" && stale == "" {
			stale = missing
		}
	}
	return "", "", stale
}

// webView2StateKeys are where EdgeUpdate records where a channel's runtime
// is: a machine-wide install in the 32-bit view of HKLM, a per-user one in
// HKCU. The backend reads those two, in that order, and no other.
func webView2StateKeys(channel string) []string {
	key := `Microsoft\EdgeUpdate\ClientState\` + channel
	return []string{
		`HKLM\SOFTWARE\WOW6432Node\` + key,
		`HKCU\SOFTWARE\` + key,
	}
}

// parseWebView2Version splits a runtime's version, and compareWebView2Versions
// orders two of them, shorter parts first, as the backend does when it picks
// the newest of a channel's installs.
func parseWebView2Version(s string) []int {
	var v []int
	for _, part := range strings.Split(s, ".") {
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil
		}
		v = append(v, n)
	}
	return v
}

func compareWebView2Versions(a, b []int) int {
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// webView2Client returns the client library of the runtime installed in
// dir, or "" when there is no runtime there: what the backend loads, so its
// absence means no window can show a page. The library alone is no runtime —
// the Edge browser keeps one of that name beside itself, with no browser
// process behind it, and only creating an environment finds that out — so the
// runtime's browser process is required too.
func webView2Client(dir string) string {
	if _, err := os.Stat(filepath.Join(dir, webView2BrowserName)); err != nil {
		return ""
	}
	p := filepath.Join(dir, webView2Value, webView2Arch(), webView2ClientName)
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// webView2Arch names the runtime's directory for this architecture.
func webView2Arch() string {
	if runtime.GOARCH == "arm64" {
		return "arm64"
	}
	return "x64"
}

// regValue reads a string value of a registry key, or "" when it, or reg,
// cannot be read.
func regValue(key, name string) string {
	out, err := exec.Command("reg", "query", key, "/v", name).Output()
	if err != nil {
		return ""
	}
	return regStringValue(string(out), name)
}

// regStringValue finds name's value in the output of `reg query`, whose
// lines are "    name    REG_SZ    the value".
func regStringValue(out, name string) string {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != name || fields[1] != "REG_SZ" {
			continue
		}
		if i := strings.Index(line, "REG_SZ"); i >= 0 {
			return strings.TrimSpace(line[i+len("REG_SZ"):])
		}
	}
	return ""
}
