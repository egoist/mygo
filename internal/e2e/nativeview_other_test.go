//go:build !js && !darwin && !windows && !(linux && (amd64 || arm64))

package e2e

import "github.com/egoist/mygo"

// newNativeViewFixture makes no view on the platforms that have no native
// view fixture: their backends show no native views at all (as
// internal/unsupported does), so the tests of nativeview_test.go skip
// there. This file is what keeps the package building for every GOOS.
func newNativeViewFixture(*mygo.NativeViewHost) (nativeViewFixture, error) {
	return nil, errNoNativeViewFixture
}

// nvClick reports that this platform cannot automate a click: the tests of
// nativeview_test.go never get as far as one, the fixture itself being
// unavailable.
func nvClick(w *mygo.Window, x, y float64) bool { return false }

// nvTypeInto reports that this platform cannot automate keys, for the same
// reason.
func nvTypeInto(w *mygo.Window, text string) bool { return false }
