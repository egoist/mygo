package ui

import (
	"testing"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/surface"
)

type noNativeCalls struct{}

func (noNativeCalls) PlaceNativeView(*surface.Conn, platform.NativeViewPlacement) uintptr {
	panic("Tester called a native hook")
}
func (noNativeCalls) FocusNativeView(*surface.Conn, bool, bool) { panic("Tester called a native hook") }

func TestHostViewHeadless(t *testing.T) {
	test := NewTester(func(c *Context) {
		HostView(c, noNativeCalls{}).Key("host").Label("Native placeholder").Size(100, 40)
	}, 300, 200)
	if r, ok := test.Find("Native placeholder"); !ok || r.W != 100 || r.H != 40 {
		t.Errorf("placeholder bounds %+v, found=%v", r, ok)
	}
}
