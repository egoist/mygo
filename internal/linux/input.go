//go:build linux && (amd64 || arm64)

package linux

import (
	"errors"
	"strings"
	"unsafe"

	"github.com/egoist/mygo/internal/platform"
)

// Input for a page is made of the GdkEvents that the pointer and the
// keyboard make, which MyGo builds and hands to the web view's widget with
// gtk_widget_event, as GTK hands it the user's. WebKit takes them for the
// system's and the page's events are trusted. They go to the web view
// itself, not through the window's event routing, so a window that is not
// focused, or covered by another, does not hold them back.

// The GdkEventType values of the events SendMouse and SendKey make.
const (
	gdkMotionNotify  = 3
	gdkButtonPress   = 4
	gdkButtonRelease = 7
	gdkKeyPress      = 8
	gdkKeyRelease    = 9
)

// The GdkModifierType bits of the modifiers.
const (
	gdkShiftMask   = 1 << 0
	gdkControlMask = 1 << 2
	gdkMod1Mask    = 1 << 3 // Alt
	gdkSuperMask   = 1 << 26
)

// Page.SendMouse finds PageInput at run time: this has the compiler check it.
var _ platform.PageInput = (*window)(nil)

var (
	errInputNotShown = errors.New("mygo: the web view is not shown")
	errInputKeyboard = errors.New("mygo: the keyboard has no key for the key")
)

// inputFuncs are the functions that input uses on top of the ones of the
// windows; they are bound when input is first sent, so that an app that
// sends none does not pay for them.
var inputFuncs struct {
	tried, ok                  bool
	eventNew                   func(typ int32) ptr
	eventSetDevice             func(event, device ptr)
	eventSetSource             func(event, device ptr)
	seatGetKeyboard            func(seat ptr) ptr
	editingCommandWithArgument func(web ptr, command, arg *byte)
	monotonicTime              func() int64
}

func loadInput() bool {
	f := &inputFuncs
	if !f.tried {
		f.tried = true
		f.ok = bind(libGDK, &f.eventNew, "gdk_event_new") && bind(libGDK, &f.eventSetDevice, "gdk_event_set_device") &&
			bind(libGDK, &f.eventSetSource, "gdk_event_set_source_device") &&
			bind(libGDK, &f.seatGetKeyboard, "gdk_seat_get_keyboard") &&
			bind(libWebKit, &f.editingCommandWithArgument, "webkit_web_view_execute_editing_command_with_argument") &&
			bind(libGLib, &f.monotonicTime, "g_get_monotonic_time") &&
			(gtkWidgetEvent != nil || bind(libGTK, &gtkWidgetEvent, "gtk_widget_event"))
	}
	return f.ok
}

// inputReady returns the reason the page cannot take input, if there is one.
func (w *window) inputReady() error {
	if w.closed || w.web == 0 || !loadInput() {
		return platform.ErrUnsupported
	}
	return nil
}

// pageWindow returns the GdkWindow that takes the page's mouse and keys.
// WebKitGTK drops the presses of a web view that is not mapped (in a
// window that is hidden, or minimized on X11), which it does without a
// word, so that is an error here.
func (w *window) pageWindow() (ptr, error) {
	if err := w.inputReady(); err != nil {
		return 0, err
	}
	win := gtkWidgetGetWindow(w.web)
	if win == 0 || !gtkWidgetGetMapped(w.web) {
		return 0, errInputNotShown
	}
	return win, nil
}

func put[T any](p ptr, offset uintptr, v T) {
	*(*T)(unsafe.Add(*(*unsafe.Pointer)(unsafe.Pointer(&p)), offset)) = v
}

func gdkState(m platform.Modifiers) uint32 {
	var s uint32
	if m&platform.ModShift != 0 {
		s |= gdkShiftMask
	}
	if m&platform.ModCtrl != 0 {
		s |= gdkControlMask
	}
	if m&platform.ModAlt != 0 {
		s |= gdkMod1Mask
	}
	if m&platform.ModSuper != 0 {
		s |= gdkSuperMask
	}
	return s
}

// inputClock is the time of the last event sent and of the last button
// press, in the milliseconds GDK stamps its events with.
var inputClock struct{ last, press uint32 }

// stamp returns the time of the next event: now, or after the last one.
// WebKit counts the clicks of a double click itself, from the time and the
// distance between presses (the double click of GTK's settings), so a
// press is stamped by its ClickCount, not by when the caller sent it: a
// first click is a double click time after the last press, and the next
// of a series is right after it.
func stamp(press bool, clickCount int) uint32 {
	t := uint32(inputFuncs.monotonicTime() / 1000)
	if int32(t-inputClock.last) <= 0 {
		t = inputClock.last + 1
	}
	if press {
		within := uint32(doubleClick().Time.Milliseconds())
		switch since := int32(t - inputClock.press); {
		case clickCount > 1 && since > int32(within):
			t = inputClock.press + 1
		case clickCount <= 1 && since <= int32(within):
			t = inputClock.press + within + 1
		}
		inputClock.press = t
	}
	inputClock.last = t
	return t
}

// sendEvent makes an event of typ for the web view's window and hands it to
// the web view; fill sets its fields.
func (w *window) sendEvent(typ int32, win, device ptr, time uint32, fill func(ev ptr)) {
	ev := inputFuncs.eventNew(typ)
	put(ev, 8, gObjectRef(win))
	put(ev, 16, int8(1)) // send_event
	put(ev, 20, time)
	fill(ev)
	inputFuncs.eventSetDevice(ev, device)
	inputFuncs.eventSetSource(ev, device)
	gtkWidgetEvent(w.web, ev)
	gdkEventFree(ev)
}

func (w *window) SendMouse(e platform.PageMouseEvent, done func(error)) {
	win, err := w.pageWindow()
	if err != nil {
		done(err)
		return
	}
	device := gdkSeatGetPointer(gdkDisplayGetDefaultSeat(gdkDisplayGetDefault()))
	// The point is in CSS pixels of the page, which the page's zoom makes
	// pixels of the web view.
	zoom := webkitWebViewGetZoomLevel(w.web)
	if zoom <= 0 {
		zoom = 1
	}
	x, y := e.X*zoom, e.Y*zoom
	var ox, oy int32
	gdkWindowGetOrigin(win, &ox, &oy)
	typ := int32(gdkMotionNotify)
	switch e.Kind {
	case platform.PageMouseDown:
		typ = gdkButtonPress
	case platform.PageMouseUp:
		typ = gdkButtonRelease
	}
	state := gdkState(e.Mods)
	// GdkEventButton and GdkEventMotion: x 24, y 32, state 48, button 52
	// (is_hint for a motion), device 56, x_root 64, y_root 72.
	w.sendEvent(typ, win, device, stamp(typ == gdkButtonPress, e.ClickCount), func(ev ptr) {
		put(ev, 24, x)
		put(ev, 32, y)
		put(ev, 48, state)
		if typ != gdkMotionNotify {
			put(ev, 52, uint32(1))
		}
		put(ev, 64, x+float64(ox))
		put(ev, 72, y+float64(oy))
	})
	done(nil)
}

// gdkKeyvals are the keyvals of the named keys.
var gdkKeyvals = map[string]uint32{
	"Enter": 0xff0d, "Tab": 0xff09, "Escape": 0xff1b, "Backspace": 0xff08, "Delete": 0xffff,
	"ArrowLeft": 0xff51, "ArrowUp": 0xff52, "ArrowRight": 0xff53, "ArrowDown": 0xff54,
	"Home": 0xff50, "End": 0xff57, "PageUp": 0xff55, "PageDown": 0xff56,
}

// keyboardKey returns the key of the keyboard's layout that types keyval:
// its hardware keycode and group.
func keyboardKey(keyval uint32) (code uint16, group uint8, ok bool) {
	km := gdkKeymapGetForDisplay(gdkDisplayGetDefault())
	var keys ptr
	var n int32
	if km == 0 || !gdkKeymapGetEntriesForKeyval(km, keyval, &keys, &n) || n == 0 {
		return 0, 0, false
	}
	defer gFree(keys)
	// GdkKeymapKey: keycode, group, level, each 4 bytes.
	return uint16(field[uint32](keys, 0)), uint8(field[int32](keys, 4)), true
}

func (w *window) SendKey(e platform.PageKeyEvent, done func(error)) {
	win, err := w.pageWindow()
	if err != nil {
		done(err)
		return
	}
	k := e.Key
	keyval, ok := gdkKeyvals[k.Name]
	if !ok {
		keyval = uint32(k.Name[0]) // the keyval of a space, letter or digit is its ASCII
		if e.Mods&platform.ModShift != 0 && keyval >= 'a' && keyval <= 'z' {
			keyval -= 'a' - 'A'
		}
	}
	code, group, ok := keyboardKey(keyval)
	if !ok {
		done(errInputKeyboard)
		return
	}
	device := inputFuncs.seatGetKeyboard(gdkDisplayGetDefaultSeat(gdkDisplayGetDefault()))
	state := gdkState(e.Mods)
	key := func(typ int32) {
		// GdkEventKey: state 24, keyval 28, hardware_keycode 48, group 50.
		w.sendEvent(typ, win, device, stamp(false, 0), func(ev ptr) {
			put(ev, 24, state)
			put(ev, 28, keyval)
			put(ev, 48, code)
			put(ev, 50, group)
		})
	}
	if e.Kind != platform.PageKeyUp {
		key(gdkKeyPress)
	}
	if e.Kind != platform.PageKeyDown {
		key(gdkKeyRelease)
	}
	done(nil)
}

func (w *window) InsertText(text string, done func(error)) {
	if err := w.inputReady(); err != nil {
		done(err)
		return
	}
	inputFuncs.editingCommandWithArgument(w.web, cs("InsertText"), cs(strings.ReplaceAll(text, "\x00", "")))
	done(nil)
}
