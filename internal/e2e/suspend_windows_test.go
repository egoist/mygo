//go:build windows && (amd64 || arm64)

package e2e

import (
	"testing"

	"github.com/egoist/mygo"
	win "github.com/egoist/mygo/internal/windows"
)

// pageController reports whether the WebView2 controller of w's page is
// visible, and the page's MemoryUsageTargetLevel, -1 on runtimes without
// one.
func pageController(t *testing.T, w *mygo.Window) (visible bool, memoryTarget int) {
	t.Helper()
	var ok bool
	mygo.RunOnMain(func() { visible, memoryTarget, ok = win.TestPageController(w.NativeHandle()) })
	if !ok {
		t.Fatal("the window's page has no web view")
	}
	return visible, memoryTarget
}
