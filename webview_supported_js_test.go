//go:build js

package mygo

import (
	"fmt"
	"os"
	"syscall/js"
	"testing"
)

// TestMain runs the app in the browser backend, as a page does. Node runs
// the WebAssembly without a page, and Init refuses to draw without one, so
// the globals it reads are stubbed to the least a page has; nothing here
// draws, which takes a canvas and a browser, as internal/gpu/gl's tests do
// (they skip without one).
func TestMain(m *testing.M) {
	js.Global().Call("eval", `
		if (typeof document === "undefined") {
			globalThis.document = {
				hidden: false,
				documentElement: {
					getAttribute: () => null,
					setAttribute: () => {},
					style: {},
					classList: { add() {}, remove() {} },
				},
				createElement: () => ({ style: {}, appendChild() {}, getContext: () => null, addEventListener() {} }),
				body: { appendChild() {}, style: {} },
			};
			globalThis.navigator = { platform: "Linux", userAgent: "node" };
			globalThis.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {} });
			globalThis.MutationObserver = class { observe() {} disconnect() {} takeRecords() { return []; } };
			globalThis.devicePixelRatio = 1;
			globalThis.innerWidth = 800;
			globalThis.innerHeight = 600;
		}
	`)
	App.SetName("MyGoTest")
	App.OnWindowAllClosed(func() {})
	App.WhenReady(func() { go func() { os.Exit(m.Run()) }() })
	if err := App.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// TestWebViewSupported: browsers have no system webview, so the call says
// so, and an app opens a window of native UI instead of one showing a page
// it could not show.
func TestWebViewSupported(t *testing.T) {
	if WebViewSupported() {
		t.Error("WebViewSupported = true in a browser, which has no webview")
	}
}
