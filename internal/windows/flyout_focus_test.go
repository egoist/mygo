//go:build windows && (amd64 || arm64)

package windows

import (
	"runtime"
	"syscall"
	"testing"
	"unsafe"

	"github.com/egoist/mygo/internal/platform"
)

// TestFlyoutPressKeepsKeyboard presses the surface of a flyout, with the
// mouse and with a finger: one that is not focusable leaves the keyboard
// in its parent, and a focusable one takes it (#2462).
func TestFlyoutPressKeepsKeyboard(t *testing.T) {
	runtime.LockOSThread() // the keyboard's focus is the thread's
	defer runtime.UnlockOSThread()
	class, _ := syscall.UTF16PtrFromString("STATIC")
	create := func(ex uintptr) uintptr {
		h, _, err := procCreateWindowExW.Call(ex, uintptr(unsafe.Pointer(class)), 0, wsPopup|wsVisible, 0, 0, 100, 100, 0, 0, 0, 0)
		if h == 0 {
			t.Fatalf("CreateWindowEx: %v", err)
		}
		t.Cleanup(func() { procDestroyWindow.Call(h) })
		return h
	}
	parent := create(0)
	for _, focusable := range []bool{false, true} {
		for _, input := range []string{"mouse", "finger"} {
			fly := create(wsExNoActivate)
			s := &surface{hwnd: fly, w: &window{h: &touchHandler{}, opts: &platform.WindowOptions{Flyout: &platform.Flyout{Focusable: focusable}}}}
			procSetFocus.Call(parent)
			if f, _, _ := procGetFocus.Call(); f != parent {
				t.Fatalf("the parent did not take the keyboard: %#x has it", f)
			}
			if input == "mouse" {
				lp := uintptr(10)<<16 | 10
				s.message(fly, wmLButtonDown, 1, lp)
				s.message(fly, wmLButtonUp, 0, lp)
			} else {
				s.sendTouch([]platform.SurfaceEvent{{Kind: platform.PointerDown, X: 10, Y: 10}}, 0.5, 0)
			}
			procReleaseCapture.Call()
			want, who := parent, "the parent"
			if focusable {
				want, who = fly, "the flyout"
			}
			if f, _, _ := procGetFocus.Call(); f != want {
				t.Errorf("focusable %v, a press of the %s: %#x has the keyboard, not %s (%#x)", focusable, input, f, who, want)
			}
			if h := s.w.h.(*touchHandler); len(h.events) == 0 || h.events[0].Kind != platform.PointerDown {
				t.Errorf("focusable %v, a press of the %s: the content got %v, not the press", focusable, input, h.events)
			}
		}
	}
}
