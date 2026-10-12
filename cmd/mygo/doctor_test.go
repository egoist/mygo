package main

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestRegStringValue(t *testing.T) {
	// A real `reg query` line: CRLF endings, and a value full of spaces,
	// which no last field of it is.
	out := "\r\nHKEY_LOCAL_MACHINE\\SOFTWARE\\WOW6432Node\\Microsoft\\EdgeUpdate\\ClientState\\{F3017226}\r\n" +
		"    EBWebView    REG_SZ    C:\\Program Files (x86)\\Microsoft\\EdgeWebView\\Application\\154.0.4258.62\r\n\r\n"
	want := `C:\Program Files (x86)\Microsoft\EdgeWebView\Application\154.0.4258.62`
	if got := regStringValue(out, "EBWebView"); got != want {
		t.Errorf("regStringValue = %q, want %q", got, want)
	}

	// Another value's line is no answer, and neither is another type.
	for name, out := range map[string]string{
		"other value": "    pv    REG_SZ    154.0.4258.62\r\n",
		"other type":  "    EBWebView    REG_DWORD    0x1\r\n",
		"no value":    "\r\nHKEY_LOCAL_MACHINE\\Software\\Microsoft\\EdgeUpdate\r\n\r\n",
	} {
		if got := regStringValue(out, "EBWebView"); got != "" {
			t.Errorf("%s: regStringValue = %q, want \"\"", name, got)
		}
	}
}

func TestWebView2Client(t *testing.T) {
	dir := t.TempDir()
	if got := webView2Client(dir); got != "" {
		t.Errorf("webView2Client of an empty directory = %q, want \"\"", got)
	}

	// The Edge browser keeps the runtime's client library beside itself, with
	// no browser process behind it: a load there fails with 0x80070032, so it
	// is no runtime, and doctor may not promise it as one.
	edge := filepath.Join(dir, webView2Value, webView2Arch(), webView2ClientName)
	if err := os.MkdirAll(filepath.Dir(edge), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(edge, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := webView2Client(dir); got != "" {
		t.Errorf("webView2Client of the Edge browser's layout = %q, want \"\"", got)
	}

	// A runtime has its browser process too, and then the library of this
	// architecture is what it loads.
	if err := os.WriteFile(filepath.Join(dir, webView2BrowserName), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := webView2Client(dir); got != edge {
		t.Errorf("webView2Client = %q, want %q", got, edge)
	}

	// The library of another architecture is no runtime for this one, as the
	// backend loads that of its own and no other.
	if err := os.RemoveAll(filepath.Dir(edge)); err != nil {
		t.Fatal(err)
	}
	other := "x64"
	if webView2Arch() == "x64" {
		other = "arm64"
	}
	otherDir := filepath.Join(dir, webView2Value, other)
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(otherDir, webView2ClientName), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := webView2Client(dir); got != "" {
		t.Errorf("webView2Client found %q, which is no %s runtime", got, webView2Arch())
	}
}

// TestWebView2StateKeys checks doctor reads where the backend reads:
// EdgeUpdate records a machine-wide install in the 32-bit view of HKLM and a
// per-user one in HKCU, and the version it registers in Clients outlives the
// files it describes, so it says nothing about either.
func TestWebView2StateKeys(t *testing.T) {
	channel := webView2Channels[0]
	want := []string{
		`HKLM\SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\ClientState\` + channel,
		`HKCU\SOFTWARE\Microsoft\EdgeUpdate\ClientState\` + channel,
	}
	if got := webView2StateKeys(channel); !slices.Equal(got, want) {
		t.Errorf("webView2StateKeys = %v, want %v", got, want)
	}
	for _, key := range webView2StateKeys(channel) {
		if strings.Contains(key, `\Clients\`) {
			t.Errorf("%s registers an install, not the runtime's files", key)
		}
	}
}

func TestCompareWebView2Versions(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"154.0.4258.62", "154.0.4258.62", 0},
		{"155.0.1.1", "154.9.9.9", 1},
		{"154.0.4258.62", "154.0.4258.100", -1},
		{"154.0", "154.0.1", -1}, // fewer parts are the smaller version
		{"not-a-version", "1.0.0.0", -1},
	} {
		a, b := parseWebView2Version(tc.a), parseWebView2Version(tc.b)
		if got := compareWebView2Versions(a, b); got != tc.want {
			t.Errorf("compareWebView2Versions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

// TestWebView2RuntimeLayout keeps doctor's idea of a runtime the backend's: it
// reports the runtime a window would get, so a name, or a directory, the two
// disagree about would make doctor answer for an environment no window can use.
func TestWebView2RuntimeLayout(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "internal", "windows", "webview2.go"))
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{
		"browserExe":    webView2BrowserName,
		"clientDLLName": webView2ClientName,
		"clientDirName": webView2Value,
	} {
		m := regexp.MustCompile(`(?m)^\s*` + name + `\s*=\s*"([^"]+)"`).FindStringSubmatch(string(src))
		if m == nil {
			t.Errorf("internal/windows declares no %s", name)
			continue
		}
		if m[1] != want {
			t.Errorf("doctor's %s is %q, internal/windows %q", name, want, m[1])
		}
	}
}

// TestWebView2Channels keeps doctor's channel list the backend's: it reports
// the runtime a window would get, so a channel the backend stops trying, or
// starts, would make doctor answer for an environment no window can use.
func TestWebView2Channels(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "internal", "windows", "webview2.go"))
	if err != nil {
		t.Fatal(err)
	}
	// The file is built only on Windows, so read it as text rather than
	// import it: the CLI runs everywhere.
	m := regexp.MustCompile(`(?s)var webView2Channels = \[\]string\{(.*?)\n\}`).FindSubmatch(
		[]byte(strings.ReplaceAll(string(src), "\r\n", "\n"))) // a Windows checkout
	if m == nil {
		t.Fatal("internal/windows declares no webView2Channels")
	}
	found := regexp.MustCompile(`\{[0-9A-Fa-f-]+\}`).FindAllString(string(m[1]), -1)
	if !slices.Equal(found, webView2Channels) {
		t.Errorf("doctor checks %v, internal/windows %v", webView2Channels, found)
	}
	// The Stable channel first: the backend takes the newest of the channels
	// it tries, and doctor must report the same runtime.
	const stable = "{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}"
	if webView2Channels[0] != stable {
		t.Errorf("doctor checks %v first, want %s", webView2Channels[0], stable)
	}
}
