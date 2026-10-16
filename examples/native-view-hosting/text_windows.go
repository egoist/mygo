//go:build windows

package main

import (
	"fmt"
	"sync"
	"syscall"
	"unsafe"
)

// This file makes a real multiline EDIT control through user32, the way
// internal/windows calls native code: syscall, no cgo. Windows owns the
// text, the caret, the selection and the input method; MyGo owns the hole
// the control is hosted in.

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")

	procCreateWindowExW   = user32.NewProc("CreateWindowExW")
	procDefWindowProcW    = user32.NewProc("DefWindowProcW")
	procCallWindowProcW   = user32.NewProc("CallWindowProcW")
	procDestroyWindow     = user32.NewProc("DestroyWindow")
	procSetFocus          = user32.NewProc("SetFocus")
	procSetWindowLongPtrW = user32.NewProc("SetWindowLongPtrW")
	procGetKeyState       = user32.NewProc("GetKeyState")
	procGetDpiForWindow   = user32.NewProc("GetDpiForWindow")
	procSendMessageW      = user32.NewProc("SendMessageW")
	procClientToScreen    = user32.NewProc("ClientToScreen")
	procGetWindowRect     = user32.NewProc("GetWindowRect")
	procGetModuleHandleW  = kernel32.NewProc("GetModuleHandleW")
	procGetStockObject    = gdi32.NewProc("GetStockObject")
)

// winPoint and winRect are POINT and RECT, in screen device pixels.
type winPoint struct{ X, Y int32 }
type winRect struct{ Left, Top, Right, Bottom int32 }

const (
	wsChild   = 0x40000000
	wsVisible = 0x10000000
	wsVScroll = 0x00200000
	wsBorder  = 0x00800000
	wsTabStop = 0x00010000

	esMultiline   = 0x0004 // more than one line
	esAutoVScroll = 0x0020 // scroll as the text grows
	esWantReturn  = 0x1000 // Enter is a newline, not the default button
	esNoHideSel   = 0x0100 // keep the selection visible when focus leaves

	gwlpWndProc = ^uintptr(3) // GWLP_WNDPROC, -4: the only thing subclassed here

	wmSetFocus    = 0x0007
	wmSetFont     = 0x0030
	wmKeyDown     = 0x0100
	wmLButtonDown = 0x0201

	vkTab          = 0x09
	vkShift        = 0x10
	defaultGuiFont = 17 // DEFAULT_GUI_FONT
)

// controls routes the messages Windows sends to the one subclass procedure
// back to the control they belong to: syscall.NewCallback makes one callback
// per signature ever, never one per control (AGENTS.md), and the window
// handle is the key.
var (
	controlsMu sync.Mutex
	controls   = map[uintptr]*winText{}
)

// winText is one control, subclassed so the example hears the focus, the
// press and Tab before the EDIT handles them.
type winText struct {
	hwnd uintptr
	old  uintptr // the procedure the EDIT started with
	host host
	// fallbackScale is the scale the factory heard
	// (mygo.NativeViewHost.Scale), which is what the window's scale was when
	// the view was made: an initial snapshot, and only the answer used where
	// the system cannot say what the scale is now. windowScale asks the
	// system on every event instead.
	fallbackScale float64

	mu        sync.Mutex
	destroyed bool
}

// windowScale is how many device pixels a DIP is for hwnd right now: the
// window's own DPI, which follows it across displays, rather than the scale it
// was created with. fallback is the initial snapshot NativeViewHost.Scale
// carries (<= 0, or the system not answering, means 96 DPI).
func windowScale(hwnd uintptr, fallback float64) float64 {
	if hwnd != 0 && procGetDpiForWindow.Find() == nil {
		if dpi, _, _ := procGetDpiForWindow.Call(hwnd); dpi != 0 {
			return float64(dpi) / 96
		}
	}
	if fallback > 0 {
		return fallback
	}
	return 1
}

// pressOrigins is where a control's client area and its own window rectangle
// begin, in the same screen device pixels: mygo places the frame it hosts a
// view at on the control's window rectangle (internal/windows/nativeview.go,
// placeNative), which a border leaves outside the client area the mouse
// message counts from. It answers false when the system cannot say where they
// are, or where the window has no scale at all, so that a press which cannot
// be placed exactly is not reported rather than reported at the wrong point.
func pressOrigins(hwnd uintptr) (clientOrigin, frameOrigin point, ok bool) {
	var client winPoint
	if r, _, _ := procClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(&client))); r == 0 {
		return point{}, point{}, false
	}
	var frame winRect
	if r, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&frame))); r == 0 {
		return point{}, point{}, false
	}
	return point{X: float64(client.X), Y: float64(client.Y)},
		point{X: float64(frame.Left), Y: float64(frame.Top)}, true
}

// wndProc is the subclass procedure of every control: it reports to the app
// and hands everything else — typing, the caret, the selection, the input
// method — to the EDIT's own procedure.
var wndProc = syscall.NewCallback(func(hwnd, msg, wparam, lparam uintptr) uintptr {
	controlsMu.Lock()
	t := controls[hwnd]
	controlsMu.Unlock()
	if t != nil {
		switch msg {
		case wmSetFocus:
			t.host.focus()
		case wmLButtonDown:
			// The press in the control's client area, in device pixels. The
			// contract asks for the DIPs of the view itself, and mygo sizes
			// the control's whole window — the frame it placed the view at —
			// not its client area: an EDIT with WS_BORDER has its client
			// area inside that frame, so the message's point is not yet a
			// point of the view. Ask the system where both begin, rather
			// than assume a border width, and report at the DPI the window
			// has now and not the one it had when it was created (a window
			// dragged to a display with another scale would otherwise report
			// the wrong place for every press).
			client := point{
				X: float64(int16(lparam & 0xffff)),
				Y: float64(int16((lparam >> 16) & 0xffff)),
			}
			if clientOrigin, frameOrigin, ok := pressOrigins(hwnd); ok {
				if p, ok := pressDIPs(client, clientOrigin, frameOrigin, windowScale(hwnd, t.fallbackScale)); ok {
					t.host.press(p.X, p.Y)
				}
			}
		case wmKeyDown:
			if wparam == vkTab {
				shift, _, _ := procGetKeyState.Call(vkShift)
				t.host.tab(int16(shift) < 0)
				return 0 // consumed: focus leaves the view
			}
		}
	}
	if t == nil || t.old == 0 {
		r, _, _ := procDefWindowProcW.Call(hwnd, msg, wparam, lparam)
		return r
	}
	r, _, _ := procCallWindowProcW.Call(t.old, hwnd, msg, wparam, lparam)
	return r
})

// newTextView makes the platform's text control in the host's window. The
// factory runs on the main thread, and so do handle, focus and destroy.
func newTextView(h host) (textView, error) {
	if h.parent == 0 {
		return nil, fmt.Errorf("the window has no native container to host it (parent HWND is 0)")
	}
	instance, _, _ := procGetModuleHandleW.Call(0)
	style := uintptr(wsChild | wsVisible | wsVScroll | wsBorder | wsTabStop |
		esMultiline | esAutoVScroll | esWantReturn | esNoHideSel)
	hwnd, _, _ := procCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(utf16("EDIT"))), uintptr(unsafe.Pointer(utf16(""))),
		style, 0, 0, 320, 160, h.parent, 0, instance, 0)
	if hwnd == 0 {
		return nil, fmt.Errorf("CreateWindowExW(EDIT): the control was not created")
	}
	v := &winText{hwnd: hwnd, host: h, fallbackScale: h.scale}
	// Put the control in the table before subclassing it: until the window
	// procedure is replaced, no message can find it there anyway.
	controlsMu.Lock()
	controls[hwnd] = v
	controlsMu.Unlock()
	old, _, _ := procSetWindowLongPtrW.Call(hwnd, gwlpWndProc, wndProc)
	if old == 0 {
		controlsMu.Lock()
		delete(controls, hwnd)
		controlsMu.Unlock()
		procDestroyWindow.Call(hwnd)
		return nil, fmt.Errorf("SetWindowLongPtrW: the control was not subclassed")
	}
	v.old = old
	// The system's UI font, so the text sits like the MyGo controls around it.
	if font, _, _ := procGetStockObject.Call(defaultGuiFont); font != 0 {
		procSendMessageW.Call(hwnd, wmSetFont, font, 1)
	}
	return v, nil
}

// handle is the HWND MyGo puts where the view belongs.
func (v *winText) handle() uintptr { return v.hwnd }

// focus gives the control the keyboard. The text and the selection stay; one
// control is the whole view, so back changes nothing here.
func (v *winText) focus(back bool) { procSetFocus.Call(v.hwnd) }

// destroy releases what newTextView made, exactly once. MyGo destroys the
// window hosting it and calls this on the main thread.
func (v *winText) destroy() {
	v.mu.Lock()
	if v.destroyed {
		v.mu.Unlock()
		return
	}
	v.destroyed = true
	v.mu.Unlock()

	controlsMu.Lock()
	delete(controls, v.hwnd)
	controlsMu.Unlock()
	// Give the EDIT its own procedure back before destroying it, so nothing
	// is left pointing at this example's callback.
	if v.old != 0 {
		procSetWindowLongPtrW.Call(v.hwnd, gwlpWndProc, v.old)
	}
	procDestroyWindow.Call(v.hwnd)
	v.hwnd = 0
}

func utf16(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		panic("native-view-hosting: " + err.Error())
	}
	return p
}
