//go:build !js

package mygo

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestWebViewSupported: the call reports what the backend says, so an app
// can ask before it creates its first window showing a page, as NewWindow
// panics where the platform has no webview.
func TestWebViewSupported(t *testing.T) {
	if !WebViewSupported() {
		t.Error("WebViewSupported = false with a webview the fake has")
	}
	onMain(func() { fb.NoWebView = true })
	t.Cleanup(func() { onMain(func() { fb.NoWebView = false }) })
	if WebViewSupported() {
		t.Error("WebViewSupported = true with NoWebView set")
	}
}

// TestWebViewSupportedForce: force asks for the runtime to be really tried,
// which a backend answers by probing it, and the fake counts the probes: an
// app that decides by this whether to show a page at all is heard.
func TestWebViewSupportedForce(t *testing.T) {
	onMain(func() { fb.ForcedProbes = 0 })
	t.Cleanup(func() { onMain(func() { fb.ForcedProbes = 0 }) })

	if !WebViewSupported() {
		t.Error("WebViewSupported() = false with a webview the fake has")
	}
	if n := onMainValue(func() int { return fb.ForcedProbes }); n != 0 {
		t.Errorf("the cheap answer probed the runtime %d times, want none", n)
	}

	if !WebViewSupported(true) {
		t.Error("WebViewSupported(true) = false with a webview the fake has")
	}
	if n := onMainValue(func() int { return fb.ForcedProbes }); n != 1 {
		t.Errorf("WebViewSupported(true) probed the runtime %d times, want 1", n)
	}

	// With no webview, the forced answer is no, and still probes.
	onMain(func() { fb.NoWebView = true })
	if WebViewSupported(true) {
		t.Error("WebViewSupported(true) = true with NoWebView set")
	}
	if n := onMainValue(func() int { return fb.ForcedProbes }); n != 2 {
		t.Errorf("the forced probe of a machine without a webview ran %d times, want 2", n)
	}
}

// TestWebViewSupportedChoosesWindowKind: what the call is for, the guard an
// app writes, opens the window the machine can show, and both kinds work
// here: with no webview it opens one showing native UI, which needs none.
func TestWebViewSupportedChoosesWindowKind(t *testing.T) {
	for _, noWebView := range []bool{false, true} {
		name := "webview"
		if noWebView {
			name = "no webview"
		}
		t.Run(name, func(t *testing.T) {
			onMain(func() { fb.NoWebView = noWebView })
			t.Cleanup(func() { onMain(func() { fb.NoWebView = false }) })

			var w *Window
			if WebViewSupported(true) {
				w = NewWindow(WindowOptions{URL: "https://example.com"})
			} else {
				w = NewWindow(WindowOptions{Content: ui.View(func(c *ui.Context) { ui.Box(c) })})
			}
			t.Cleanup(w.Destroy)

			// The window is of the kind the guard chose: a page, or native
			// UI, which has none and needs no webview to be built.
			if noWebView {
				if w.Page() != nil {
					t.Error("a window of native UI has a page")
				}
				if _, err := w.NewWebView(WebViewOptions{}); err != nil {
					t.Errorf("NewWebView in a window of native UI: %v", err)
				}
				return
			}
			if w.Page() == nil {
				t.Error("a window showing a page has none")
			}
		})
	}
}
