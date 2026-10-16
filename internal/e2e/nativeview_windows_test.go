//go:build windows && (amd64 || arm64)

package e2e

import (
	"errors"
	"fmt"
	"sync"
	"syscall"
	"testing"
	"unicode/utf16"
	"unsafe"

	"github.com/egoist/mygo"
	win "github.com/egoist/mygo/internal/windows"
)

// The Windows fixture: a real EDIT child window, in this process, added
// to the parent MyGo reports. Its window procedure is subclassed, so the
// ui only learns that it holds the keyboard, that Tab moved the focus on
// and that the pointer went down through mygo.NativeViewHost's callbacks,
// from the messages the system itself sends.
//
// The subclass procedure is made once at startup and routes by the window
// it was set for: syscall callbacks are never freed. The fixture owns the
// child window and destroys it once when MyGo destroys the view.

// winRect and winPoint are RECT and POINT.
type winRect struct{ Left, Top, Right, Bottom int32 }
type winPoint struct{ X, Y int32 }

// The constants of the messages and styles the fixture uses.
const (
	winGWLWndProc = ^uintptr(3)  // GWLP_WNDPROC, -4
	winGWLStyle   = ^uintptr(15) // GWL_STYLE, -16

	winWMSetFocus      = 0x0007
	winWMKeyDown       = 0x0100
	winWMLButtonDown   = 0x0201
	winWMLButtonDblClk = 0x0203
	winVKTab           = 0x09
	winVKShift         = 0x10
	winWSChild         = 0x40000000
	winWSVisible       = 0x10000000
	winWSTabStop       = 0x00010000
	winWSBorder        = 0x00800000
	winESAutoHScroll   = 0x0080

	// The SetWindowPos flags of giving a window made without a border one:
	// nothing moves or resizes, and the frame is worked out again.
	winSWPNoSize       = 0x0001
	winSWPNoMove       = 0x0002
	winSWPNoZOrder     = 0x0004
	winSWPNoActivate   = 0x0010
	winSWPFrameChanged = 0x0020
)

var (
	winUser32 = syscall.NewLazyDLL("user32.dll")

	winCreateWindowExW      = winUser32.NewProc("CreateWindowExW")
	winDestroyWindow        = winUser32.NewProc("DestroyWindow")
	winDefWindowProcW       = winUser32.NewProc("DefWindowProcW")
	winCallWindowProcW      = winUser32.NewProc("CallWindowProcW")
	winGetWindowLongPtrW    = winUser32.NewProc("GetWindowLongPtrW")
	winSetWindowLongPtrW    = winUser32.NewProc("SetWindowLongPtrW")
	winSetWindowPos         = winUser32.NewProc("SetWindowPos")
	winGetParent            = winUser32.NewProc("GetParent")
	winGetFocus             = winUser32.NewProc("GetFocus")
	winSetFocus             = winUser32.NewProc("SetFocus")
	winGetCursorPos         = winUser32.NewProc("GetCursorPos")
	winGetWindowTextW       = winUser32.NewProc("GetWindowTextW")
	winSetWindowTextW       = winUser32.NewProc("SetWindowTextW")
	winGetWindowTextLengthW = winUser32.NewProc("GetWindowTextLengthW")
	winGetWindowRect        = winUser32.NewProc("GetWindowRect")
	winGetClientRect        = winUser32.NewProc("GetClientRect")
	winClientToScreen       = winUser32.NewProc("ClientToScreen")
	winIsWindowVisible      = winUser32.NewProc("IsWindowVisible")
	winGetKeyState          = winUser32.NewProc("GetKeyState")
	winGetDpiForWindow      = winUser32.NewProc("GetDpiForWindow")
	winSendInput            = winUser32.NewProc("SendInput")
)

// winWndProc is the subclass procedure of every fixture window, made once
// at startup.
var winWndProc = syscall.NewCallback(winWindowProc)

// winFixtures is the control of each native view, so the subclass
// procedure, which gets only the window, finds the fixture reporting it.
var winFixtures sync.Map // uintptr -> *winFixture

func winFixtureOf(hwnd uintptr) *winFixture {
	if f, ok := winFixtures.Load(hwnd); ok {
		return f.(*winFixture)
	}
	return nil
}

// winScale is how many device pixels a DIP is for hwnd right now: the
// window's own DPI, which follows it to a display with another scale, rather
// than the scale NativeViewHost.Scale carried when the view was made. That
// one is an initial snapshot, and only the fallback here: a control in a
// window that moved to another display reports and measures in the DPI it has
// now.
func winScale(hwnd uintptr, fallback float64) float64 {
	if hwnd != 0 && winGetDpiForWindow.Find() == nil {
		if dpi, _, _ := winGetDpiForWindow.Call(hwnd); dpi != 0 {
			return float64(dpi) / 96
		}
	}
	if fallback > 0 {
		return fallback
	}
	return 1
}

// winWindowProc reports the messages that are the control's own events
// and lets the control's own procedure do the rest.
func winWindowProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	f := winFixtureOf(hwnd)
	switch msg {
	case winWMSetFocus:
		if f != nil {
			f.tookFocus()
		}
	case winWMKeyDown:
		if wparam == winVKTab {
			shift, _, _ := winGetKeyState.Call(winVKShift)
			if f != nil {
				f.tabbedOut(int16(shift) < 0)
			}
			// The ui owns the focus order: the control does not see it.
			return 0
		}
	case winWMLButtonDown, winWMLButtonDblClk:
		// A double click is a press too: the system sends the second press
		// of one as this message instead of a press of its own, and mygo's
		// own surface counts both as one (internal/windows/surface.go).
		if f != nil {
			// The message carries device pixels in the control's client
			// area, and the contract asks for the DIPs of the view itself:
			// the frame mygo placed the control's whole window at, which a
			// border puts outside its client area. Ask the system where both
			// begin, rather than take the client area for the frame, and
			// report at the DPI the window has now, never the scale it was
			// created with. A press that cannot be placed exactly is not
			// reported at all.
			if x, y, ok := winPressDIPs(hwnd, lparam, f.fallbackScale()); ok {
				f.pointerDown(x, y)
			}
		}
	}
	if f != nil && f.old != 0 {
		r, _, _ := winCallWindowProcW.Call(f.old, hwnd, msg, wparam, lparam)
		return r
	}
	r, _, _ := winDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return r
}

// winPressDIPs is where the point of a press message on hwnd falls in the DIPs
// of the frame mygo placed the view at. GetWindowRect is that frame: the
// control's whole window, which mygo sizes, and which a border leaves larger
// than the client area the message's point counts from. The point becomes the
// screen pixel it was, so a frame on a display left of the primary one, with
// negative coordinates, is placed the same way, and the border's width is
// measured, never assumed. It answers false where the system cannot say, or
// the window has no scale.
func winPressDIPs(hwnd, lparam uintptr, fallback float64) (x, y float64, ok bool) {
	var client winPoint
	if r, _, _ := winClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(&client))); r == 0 {
		return 0, 0, false
	}
	var frame winRect
	if r, _, _ := winGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&frame))); r == 0 {
		return 0, 0, false
	}
	scale := winScale(hwnd, fallback)
	if scale <= 0 {
		return 0, 0, false
	}
	return float64(client.X+int32(int16(lparam&0xffff))-frame.Left) / scale,
		float64(client.Y+int32(int16(lparam>>16))-frame.Top) / scale, true
}

// winFixture is the app's control: a real EDIT child window.
type winFixture struct {
	nativeViewEvents
	hwnd   uintptr
	parent uintptr
	old    uintptr
}

// newNativeViewFixture makes an EDIT child window in the parent MyGo
// reports.
func newNativeViewFixture(host *mygo.NativeViewHost) (nativeViewFixture, error) {
	if host.Platform != "windows" {
		return nil, fmt.Errorf("e2e: the windows fixture on %q", host.Platform)
	}
	if host.Parent == 0 {
		return nil, errors.New("e2e: the host has no parent window")
	}
	class, err := syscall.UTF16PtrFromString("EDIT")
	if err != nil {
		return nil, err
	}
	hwnd, _, errno := winCreateWindowExW.Call(
		0, uintptr(unsafe.Pointer(class)), 0,
		winWSChild|winWSVisible|winWSTabStop|winESAutoHScroll,
		0, 0, 100, 100, host.Parent, 0, 0, 0)
	if hwnd == 0 {
		return nil, fmt.Errorf("e2e: cannot make an EDIT control: %v", errno)
	}
	f := &winFixture{hwnd: hwnd, parent: host.Parent}
	old, _, _ := winSetWindowLongPtrW.Call(hwnd, winGWLWndProc, winWndProc)
	f.old = old
	f.bind(host)
	winFixtures.Store(hwnd, f)
	return f, nil
}

// nvClick clicks a point of the window's surface as a user does, for the
// native-view tests: the app's control is a window of its own, and the
// messages the general click hook sends go to the surface window, which
// Windows delivers to it and to nothing else — a child control hears a click
// only from the system's own input, which is what appClick sends and what
// these tests need (as the darwin and linux fixtures do).
func nvClick(w *mygo.Window, x, y float64) bool { return appClick(w, x, y) }

// nvTypeInto types text into whatever holds the keyboard, as the keyboard
// does, for the native-view tests: the system's own input, which the window
// routes to the focused control and the control handles the way it handles a
// real keyboard's. It sends no click, sets no text (SetWindowTextW and
// WM_SETTEXT are never used), and calls no host callback; and it never gives
// the control the keyboard back, so a test that lost the focus fails on the
// text it reads instead of being quietly repaired here. The window must come
// to the front, where the system delivers input: that is the convention the
// other platform tests of this package use, and where it cannot, this reports
// false and the caller skips rather than type into another application. Each
// packet carries finished UTF-16 text, so what this verifies is the window's
// own routing of the keyboard to the control, not an input method's
// composition (IMM32), which no packet here drives.
func nvTypeInto(w *mygo.Window, text string) (ok bool) {
	packets := winTypePackets(text)
	mygo.RunOnMain(func() {
		hwnd := w.NativeHandle()
		if hwnd == 0 || !win.TestForeground(hwnd) {
			return
		}
		ok = winSendPackets(packets)
	})
	return ok
}

// The INPUT of SendInput, as Windows' headers lay it out on amd64 and arm64
// alike: the tag, four bytes of padding so the union begins eight-byte
// aligned, and the union itself, which is as wide as its largest member, the
// mouse packet. Typing fills only the keyboard member. The three sizes below
// are what the headers say, and TestWindowsInputLayout holds them, and every
// field's place, to unsafe.Sizeof and Offsetof.

// winKEYBDINPUT is the keyboard member of INPUT's union: no virtual key with
// KEYEVENTF_UNICODE, the UTF-16 code unit in the scan field, the flags, and
// the system's time and the caller's extra information, both left zero.
type winKEYBDINPUT struct {
	Vk        uint16
	Scan      uint16
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

// winMOUSEINPUT is the mouse member: the widest of the union, and so what
// makes an INPUT the size it is.
type winMOUSEINPUT struct {
	Dx, Dy    int32
	MouseData uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

// winINPUT is one INPUT: the tag and the union, of which only the keyboard
// member is filled, the rest being the bytes the mouse member would take.
type winINPUT struct {
	Type uint32
	_    uint32 // the union is eight-byte aligned: this puts it at offset 8
	Ki   winKEYBDINPUT
	_    [winMOUSEINPUTBytes - winKEYBDINPUTBytes]byte // the union's other bytes
}

// What Windows' headers make these structures on amd64 and arm64:
// Sizeof(INPUT), Sizeof(KEYBDINPUT) and Sizeof(MOUSEINPUT).
const (
	winINPUTBytes      = 40
	winKEYBDINPUTBytes = 24
	winMOUSEINPUTBytes = 32
)

// The keyboard INPUT type and the flags of its packets: the Unicode flag
// types text by code unit, whatever keyboard layout the machine has, and the
// key-up flag makes each unit a press and a release.
const (
	winINPUTKeyboard    = 1
	winKEYEVENTFKeyUp   = 0x0002
	winKEYEVENTFUnicode = 0x0004
)

// winTypePackets is the INPUT packets that type text: every UTF-16 code unit
// of it, a key-down and then a key-up of the same unit, so the system
// delivers the text as it delivers a keyboard's — through TranslateMessage
// and the focused control's own handling of the characters. A character
// outside the BMP is the pair of surrogates it is, one unit each: the system
// assembles the pair and the control sees the one character. The packets name
// no virtual key, so no layout takes part and the text is typed exactly as it
// is given.
func winTypePackets(text string) []winINPUT {
	units := utf16.Encode([]rune(text))
	packets := make([]winINPUT, 0, 2*len(units))
	for _, unit := range units {
		packets = append(packets, winKeyPacket(unit, 0), winKeyPacket(unit, winKEYEVENTFKeyUp))
	}
	return packets
}

// winKeyPacket is one INPUT that presses (releases, with winKEYEVENTFKeyUp)
// the UTF-16 code unit as the character it is.
func winKeyPacket(unit uint16, flags uint32) winINPUT {
	return winINPUT{Type: winINPUTKeyboard, Ki: winKEYBDINPUT{Scan: unit, Flags: flags | winKEYEVENTFUnicode}}
}

// winSendPackets hands the packets to the system's input and reports whether
// it took every one of them: SendInput answers how many it inserted, and a
// short answer means the input was blocked — by a window of a higher
// integrity level, for one — which must not pass for a typing the test asked
// for. It runs on the main thread, the one whose window has the input.
func winSendPackets(packets []winINPUT) bool {
	if len(packets) == 0 {
		return true
	}
	n, _, _ := winSendInput.Call(uintptr(len(packets)), uintptr(unsafe.Pointer(&packets[0])), unsafe.Sizeof(winINPUT{}))
	return int(n) == len(packets)
}

// NativeHandle is the control's HWND.
func (f *winFixture) NativeHandle() uintptr { return f.hwnd }

// Focus gives the control the keyboard, as Tab into the native view does:
// the system sends it WM_SETFOCUS, which reports the focus to the host.
// Main thread, as the adapter's Focus is.
func (f *winFixture) Focus(back bool) {
	if f.hwnd == 0 {
		return
	}
	winSetFocus.Call(f.hwnd)
}

// Destroy is called once, after the backend detached the control: the
// fixture owns the child window and destroys it here. It forgets the
// window, so a later query from a test reports nothing instead of a
// destroyed one. Main thread, as the adapter's Destroy is.
func (f *winFixture) Destroy() {
	f.destroyedControl()
	hwnd := f.hwnd
	if hwnd == 0 {
		return
	}
	f.hwnd = 0
	winFixtures.Delete(hwnd)
	if f.old != 0 {
		winSetWindowLongPtrW.Call(hwnd, winGWLWndProc, f.old)
	}
	winDestroyWindow.Call(hwnd)
}

// Text is the control's own text. It asks the system on the main thread:
// the user32 calls belong to the thread that made the window.
func (f *winFixture) Text() (text string) {
	mygo.RunOnMain(func() {
		if f.hwnd == 0 {
			return
		}
		n, _, _ := winGetWindowTextLengthW.Call(f.hwnd)
		buf := make([]uint16, int(n)+1)
		if len(buf) == 0 {
			return
		}
		winGetWindowTextW.Call(f.hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		text = syscall.UTF16ToString(buf)
	})
	return text
}

// SetText sets the control's own text, as the app does, on the main thread.
func (f *winFixture) SetText(text string) {
	mygo.RunOnMain(func() {
		if f.hwnd == 0 {
			return
		}
		str, err := syscall.UTF16PtrFromString(text)
		if err != nil {
			return
		}
		winSetWindowTextW.Call(f.hwnd, uintptr(unsafe.Pointer(str)))
	})
}

// HoldsKeyboard reports whether the control is the window's focus, asked of
// the system on the main thread.
func (f *winFixture) HoldsKeyboard() (holds bool) {
	mygo.RunOnMain(func() {
		if f.hwnd == 0 {
			return
		}
		hwnd, _, _ := winGetFocus.Call()
		holds = hwnd == f.hwnd
	})
	return holds
}

// fallbackScale is the scale the factory heard, the window's scale when the
// view was made: what winScale answers with where the system cannot.
func (f *winFixture) fallbackScale() float64 {
	if f.host != nil && f.host.Scale > 0 {
		return f.host.Scale
	}
	return 1
}

// border gives the control the border the fixture of a real app has, and asks
// the system to work its frame out again: the outer rectangle stays where it
// was, only the client area moves inside it. It answers whether the control
// now has a border, and runs on the main thread, where the window lives.
func (f *winFixture) border() (ok bool) {
	mygo.RunOnMain(func() {
		style, _, _ := winGetWindowLongPtrW.Call(f.hwnd, uintptr(winGWLStyle))
		if style&winWSBorder != 0 {
			ok = true
			return
		}
		winSetWindowLongPtrW.Call(f.hwnd, uintptr(winGWLStyle), style|winWSBorder)
		winSetWindowPos.Call(f.hwnd, 0, 0, 0, 0, 0,
			uintptr(winSWPNoMove|winSWPNoSize|winSWPNoZOrder|winSWPNoActivate|winSWPFrameChanged))
		after, _, _ := winGetWindowLongPtrW.Call(f.hwnd, uintptr(winGWLStyle))
		ok = after&winWSBorder != 0
	})
	return ok
}

// clientOffset is how far the control's client area begins inside its own
// window rectangle, in the system's physical pixels: the border the control
// has, and whatever else the system keeps outside the client area. It is the
// amount the old press transform missed. It runs on the main thread.
func (f *winFixture) clientOffset() (x, y int32, ok bool) {
	mygo.RunOnMain(func() {
		var frame winRect
		if r, _, _ := winGetWindowRect.Call(f.hwnd, uintptr(unsafe.Pointer(&frame))); r == 0 {
			return
		}
		var origin winPoint
		if r, _, _ := winClientToScreen.Call(f.hwnd, uintptr(unsafe.Pointer(&origin))); r == 0 {
			return
		}
		x, y, ok = origin.X-frame.Left, origin.Y-frame.Top, true
	})
	return x, y, ok
}

// pointerInFrame is where the pointer is now, in the DIPs of the frame mygo
// placed the control's whole window at: the point a press there must report.
// The test asks it after a click it made itself, to compare with what the
// control's own message reported, so the two are measured the same way -- from
// the system, not from the code under test. It runs on the main thread.
func (f *winFixture) pointerInFrame() (x, y float64, ok bool) {
	mygo.RunOnMain(func() {
		var at winPoint
		if r, _, _ := winGetCursorPos.Call(uintptr(unsafe.Pointer(&at))); r == 0 {
			return
		}
		var frame winRect
		if r, _, _ := winGetWindowRect.Call(f.hwnd, uintptr(unsafe.Pointer(&frame))); r == 0 {
			return
		}
		scale := winScale(f.hwnd, f.fallbackScale())
		if scale <= 0 {
			return
		}
		x = float64(at.X-frame.Left) / scale
		y = float64(at.Y-frame.Top) / scale
		ok = true
	})
	return x, y, ok
}

// Box is the control's box and the part of it the window shows, in the window
// surface's DIPs, which is the space the click hooks and the tests measure in:
// the child window's rectangle and the clip window's client area, both in the
// surface's client coordinates, intersected, and scaled back from the system's
// physical pixels at the DPI the window has now. The child's rectangle is the
// frame mygo placed it at, its whole window, which is what the content asks
// for and what a press is reported in. Nothing of a control that was hidden
// shows. It runs on the main thread, where the window lives.
func (f *winFixture) Box() (box, visible mygo.Rectangle) {
	mygo.RunOnMain(func() {
		if f.hwnd == 0 || f.parent == 0 {
			return
		}
		if shown, _, _ := winIsWindowVisible.Call(f.hwnd); shown == 0 {
			return
		}
		surface, _, _ := winGetParent.Call(f.parent)
		if surface == 0 {
			return
		}
		var origin winPoint
		winClientToScreen.Call(surface, uintptr(unsafe.Pointer(&origin)))
		scale := winScale(f.hwnd, f.fallbackScale())
		var frame winRect
		winGetWindowRect.Call(f.hwnd, uintptr(unsafe.Pointer(&frame)))
		// The clip window's client area: GetClientRect answers in the clip
		// window's own coordinates, and ClientToScreen says where those
		// begin on screen, so adding the origin makes the rectangle a screen
		// one -- the space winBoxOn takes and the surface's origin is in.
		var clip winRect
		winGetClientRect.Call(f.parent, uintptr(unsafe.Pointer(&clip)))
		var clipOrigin winPoint
		winClientToScreen.Call(f.parent, uintptr(unsafe.Pointer(&clipOrigin)))
		clip = winRect{
			Left:   clip.Left + clipOrigin.X,
			Top:    clip.Top + clipOrigin.Y,
			Right:  clip.Right + clipOrigin.X,
			Bottom: clip.Bottom + clipOrigin.Y,
		}
		box = winBoxOn(frame, origin, scale)
		visible = intersectRectangles(box, winBoxOn(clip, origin, scale))
	})
	return box, visible
}

// winBoxOn is r in the DIPs of the window whose client area begins at origin,
// at the given scale.
func winBoxOn(r winRect, origin winPoint, scale float64) mygo.Rectangle {
	return mygo.Rectangle{
		X:      int(float64(r.Left-origin.X) / scale),
		Y:      int(float64(r.Top-origin.Y) / scale),
		Width:  int(float64(r.Right-r.Left) / scale),
		Height: int(float64(r.Bottom-r.Top) / scale),
	}
}

// intersectRectangles is the part of a both rectangles share, empty when
// they do not meet.
func intersectRectangles(a, b mygo.Rectangle) mygo.Rectangle {
	x := max(a.X, b.X)
	y := max(a.Y, b.Y)
	right := min(a.X+a.Width, b.X+b.Width)
	bottom := min(a.Y+a.Height, b.Y+b.Height)
	if right <= x || bottom <= y {
		return mygo.Rectangle{}
	}
	return mygo.Rectangle{X: x, Y: y, Width: right - x, Height: bottom - y}
}

// winPressAt is how far inside the control's frame the press test clicks:
// well past the border of any control, and well inside it.
const winPressAt = 20

// TestNativeViewWindowsPressIsFrameLocal holds the transform of a press still
// against a real click. The message a control's own window receives carries
// device pixels in that control's client area, while NativeViewHost.PointerDown
// asks for the DIPs of the view itself: the frame mygo placed the control's
// whole window at. A control with a border is where the two differ, and the
// fixture of a plain control has none, so this test gives the control the
// border a real control has, clicks a point of the frame it knows, and compares
// the point the control reported with where the system says the pointer was.
// Both are measured from the same frame, and the pointer's place is read from
// the system, never from the code under test.
func TestNativeViewWindowsPressIsFrameLocal(t *testing.T) {
	n := newNativeViewTest(t)
	f, ok := n.fixture.(*winFixture)
	if !ok {
		t.Fatalf("the fixture is %T, not the Windows one", n.fixture)
	}
	if !f.border() {
		t.Fatal("the control would not take a border, so its client area would not differ from its frame")
	}
	box, visible := n.fixture.Box()
	if box.Width < 2*winPressAt || box.Height < 2*winPressAt {
		t.Fatalf("the control's box %+v is too small to click well inside it", box)
	}
	if visible != box {
		t.Fatalf("the window shows %+v of the control's box %+v, want all of it", visible, box)
	}
	if dx, dy, ok := f.clientOffset(); !ok || (dx == 0 && dy == 0) {
		t.Fatalf("the control's client area begins at (%d, %d) inside its frame: without an offset this test could not tell the frame from the client area", dx, dy)
	}
	n.w.Focus()
	if !win.TestForeground(n.w.NativeHandle()) {
		t.Skip("the window could not be brought to the front, where a real click is delivered")
	}
	n.click(float64(box.X)+winPressAt, float64(box.Y)+winPressAt)
	eventually(t, "the control to report the press", func() bool {
		return n.fixture.Reports().Presses() >= 1
	})
	gotX, gotY := n.fixture.Reports().At()
	wantX, wantY, ok := f.pointerInFrame()
	if !ok {
		t.Fatal("the pointer's place in the control's frame could not be read")
	}
	if !winNear(gotX, wantX) || !winNear(gotY, wantY) {
		t.Errorf("the press was reported at (%v, %v) in the control's frame, but the pointer was at (%v, %v): the press was not measured from the frame the view was placed at", gotX, gotY, wantX, wantY)
	}
	if !winNear(wantX, winPressAt) || !winNear(wantY, winPressAt) {
		t.Errorf("the pointer was at (%v, %v) in the control's frame, want about (%v, %v): the click did not land where it was asked", wantX, wantY, winPressAt, winPressAt)
	}
	if gotX < 0 || gotY < 0 || gotX > float64(box.Width) || gotY > float64(box.Height) {
		t.Errorf("the press was reported at (%v, %v), outside the control's box %+v", gotX, gotY, box)
	}
}

// winNear reports whether a and b are the same point, to a hundredth of a DIP:
// the press and the pointer are read through different calls.
func winNear(a, b float64) bool {
	const epsilon = 0.01
	d := a - b
	return d <= epsilon && d >= -epsilon
}

// TestWindowsInputLayout holds the INPUT packets nvTypeInto sends to the
// layout Windows reads them with, which is the same on amd64 and arm64: a tag,
// the union eight-byte aligned at offset 8, the keyboard member's own fields
// where WINAPI's KEYBDINPUT has them, and a whole INPUT of 40 bytes, the size
// of the widest member of the union. SendInput is told that size and reads the
// packets with it, so a wrong one would hand it whatever follows in memory. It
// needs no window, no desktop and no clock.
func TestWindowsInputLayout(t *testing.T) {
	if got := unsafe.Sizeof(winINPUT{}); got != winINPUTBytes {
		t.Errorf("Sizeof(INPUT) is %d, want %d", got, winINPUTBytes)
	}
	if got, want := unsafe.Alignof(winINPUT{}), unsafe.Alignof(uintptr(0)); got != want {
		t.Errorf("Alignof(INPUT) is %d, want the alignment of ULONG_PTR, %d", got, want)
	}
	if got := unsafe.Offsetof(winINPUT{}.Type); got != 0 {
		t.Errorf("INPUT.Type is at offset %d, want 0", got)
	}
	if got := unsafe.Offsetof(winINPUT{}.Ki); got != 8 {
		t.Errorf("INPUT.Ki is at offset %d, want 8: the union begins eight-byte aligned", got)
	}
	if got := unsafe.Sizeof(winKEYBDINPUT{}); got != winKEYBDINPUTBytes {
		t.Errorf("Sizeof(KEYBDINPUT) is %d, want %d", got, winKEYBDINPUTBytes)
	}
	if got := unsafe.Sizeof(winMOUSEINPUT{}); got != winMOUSEINPUTBytes {
		t.Errorf("Sizeof(MOUSEINPUT) is %d, want %d", got, winMOUSEINPUTBytes)
	}
	// The union is as wide as the mouse member, and the padding after the
	// keyboard packet is exactly the difference: nothing of the packets
	// nvTypeInto sends is read as another member, and none of them is cut off.
	if got, want := unsafe.Offsetof(winINPUT{}.Ki)+winMOUSEINPUTBytes, unsafe.Sizeof(winINPUT{}); got != want {
		t.Errorf("the union of INPUT ends at %d, but INPUT is %d bytes", got, want)
	}
	// The fields of the keyboard packet, as WINAPI's KEYBDINPUT has them. A
	// packet's flags and unit are read by these offsets: the wrong ones would
	// make SendInput read the union's other bytes as a code unit.
	for _, c := range []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"wVk", unsafe.Offsetof(winKEYBDINPUT{}.Vk), 0},
		{"wScan", unsafe.Offsetof(winKEYBDINPUT{}.Scan), 2},
		{"dwFlags", unsafe.Offsetof(winKEYBDINPUT{}.Flags), 4},
		{"time", unsafe.Offsetof(winKEYBDINPUT{}.Time), 8},
		{"dwExtraInfo", unsafe.Offsetof(winKEYBDINPUT{}.ExtraInfo), 16},
	} {
		if c.got != c.want {
			t.Errorf("KEYBDINPUT.%s is at offset %d, want %d", c.name, c.got, c.want)
		}
	}
}

// TestWindowsTypePackets holds the packets nvTypeInto sends to the text they
// type: every UTF-16 code unit is a key-down and then a key-up of that unit
// itself, carrying KEYEVENTF_UNICODE and no virtual key, so no keyboard layout
// takes part, and the units the presses carry decode back to the text exactly.
// A character outside the BMP goes as its two surrogates, one unit each, which
// the system assembles into the one character. It needs no window, no desktop
// and no clock.
func TestWindowsTypePackets(t *testing.T) {
	for _, text := range []string{"", "typed", "aA1 !?", "日本語", "héllo", "\U0001F600", "a\U0001F600b"} {
		units := utf16.Encode([]rune(text))
		packets := winTypePackets(text)
		if len(packets) != 2*len(units) {
			t.Fatalf("%q: %d packets, want a press and a release per code unit, %d", text, len(packets), 2*len(units))
		}
		var pressed []uint16
		for i, p := range packets {
			unit := units[i/2]
			if p.Type != winINPUTKeyboard {
				t.Errorf("%q: packet %d is of type %d, want the keyboard type %d", text, i, p.Type, winINPUTKeyboard)
			}
			if p.Ki.Vk != 0 {
				t.Errorf("%q: packet %d carries the virtual key %#x: a Unicode packet carries none", text, i, p.Ki.Vk)
			}
			if p.Ki.Flags&winKEYEVENTFUnicode == 0 {
				t.Errorf("%q: packet %d lacks KEYEVENTF_UNICODE", text, i)
			}
			if p.Ki.Time != 0 || p.Ki.ExtraInfo != 0 {
				t.Errorf("%q: packet %d carries time %d and the extra information %#x, want the system's and none", text, i, p.Ki.Time, p.Ki.ExtraInfo)
			}
			if up := p.Ki.Flags&winKEYEVENTFKeyUp != 0; up != (i%2 == 1) {
				t.Errorf("%q: packet %d is a release=%t: each unit is a press and then a release", text, i, up)
			}
			if p.Ki.Scan != unit {
				t.Errorf("%q: packet %d carries the code unit %#x, want %#x", text, i, p.Ki.Scan, unit)
			}
			if i%2 == 0 {
				pressed = append(pressed, p.Ki.Scan)
			}
		}
		if got := string(utf16.Decode(pressed)); got != text {
			t.Errorf("the packets type %q, want %q", got, text)
		}
	}
}
