//go:build !js

package mygo

import (
	"testing"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/surface"
)

// TestWebViewLifecycle checks a web view's own methods: the window it is
// in, the keyboard, the screenshot, the native view it gives its own
// window's surface alone, and what destroying it leaves.
func TestWebViewLifecycle(t *testing.T) {
	show := true
	w, v, s, fv := webViewWindow(t, &show)
	if v.Window() != w {
		t.Error("the web view is not of its window")
	}
	if v.Page() == nil || v.Page().WebView() != v {
		t.Error("the web view's page is not its own")
	}

	// The keyboard goes to the web view when asked, as a press does.
	if onMainValue(func() bool { return fv.Focused }) {
		t.Error("a web view takes the keyboard only when asked")
	}
	v.Focus()
	if !onMainValue(func() bool { return fv.Focused }) {
		t.Error("Focus did not give the web view the keyboard")
	}

	// Its own window's surface gets the native web view; another window's
	// gets none.
	native := onMainValue(func() platform.WebView { return v.SurfaceWebView(&surface.Conn{Window: w}) })
	if native != platform.WebView(fv) {
		t.Errorf("its window's surface got %v", native)
	}
	other, _ := testWindow(t, WindowOptions{})
	if got := onMainValue(func() platform.WebView { return v.SurfaceWebView(&surface.Conn{Window: other}) }); got != nil {
		t.Errorf("a web view of another window was given away: %v", got)
	}

	// CapturePage is a screenshot of the web view's page.
	png, err := v.CapturePage()
	if err != nil || string(png) != "png" {
		t.Errorf("CapturePage = %q, %v", png, err)
	}

	// Destroy closes it: gone from the surface, told of no more focus or
	// screenshots, and it gives its native view to no one.
	v.Destroy()
	if !v.IsDestroyed() {
		t.Error("the destroyed web view does not say so")
	}
	if onMainValue(func() bool { return fv.Focused }) {
		t.Error("Focus of a destroyed web view kept the keyboard")
	}
	if _, err := v.CapturePage(); err == nil {
		t.Error("CapturePage of a destroyed web view gave a screenshot")
	}
	if onMainValue(func() bool { return !fv.IsClosed() }) {
		t.Error("the native web view was not closed")
	}
	if n := len(onMainValue(s.WebViews)); n != 0 {
		t.Errorf("the surface still holds %d web views", n)
	}
	if got := onMainValue(func() platform.WebView { return v.SurfaceWebView(&surface.Conn{Window: w}) }); got != nil {
		t.Errorf("a destroyed web view gave its native view: %v", got)
	}
}
