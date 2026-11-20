//go:build !(windows && (amd64 || arm64)) && !js

package e2e

import (
	"testing"

	"github.com/egoist/mygo"
)

// pageController is WebView2's (suspend_windows_test.go).
func pageController(t *testing.T, _ *mygo.Window) (bool, int) {
	t.Helper()
	t.Fatal("no WebView2 controller")
	return false, 0
}
