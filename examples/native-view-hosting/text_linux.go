//go:build linux

package main

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// ptr is what the GTK functions take for a pointer, as in internal/linux.
type ptr = uintptr

// This file drives GTK 3 through purego, the way internal/linux does: no
// cgo. GTK owns the text, the caret, the selection and the input method (the
// GTK IM context); MyGo owns the hole the entry is hosted in. The window's
// GTK library is already loaded when the factory runs, so dlopen returns the
// same handle the backend uses.

var (
	libGTK     uintptr
	libGObject uintptr
	libGDK     uintptr

	gtkEntryNew            func() uintptr
	gtkEntrySetText        func(entry uintptr, text string)
	gtkWidgetGrabFocus     func(w uintptr)
	gtkWidgetShow          func(w uintptr)
	gtkWidgetDestroy       func(w uintptr)
	gtkWidgetGetParent     func(w uintptr) uintptr
	gtkWidgetGetHasWindow  func(w uintptr) bool
	gtkWidgetGetWindow     func(w uintptr) uintptr
	gtkWidgetGetAllocation func(w uintptr, alloc *gdkRect)

	gObjectRefSink     func(obj uintptr) uintptr
	gObjectUnref       func(obj uintptr)
	gSignalConnectData func(instance uintptr, signal string, handler uintptr, data uintptr, destroy uintptr, flags int32) uint64

	gdkWindowGetParent   func(window uintptr) uintptr
	gdkWindowGetPosition func(window uintptr, x, y *int32)
	gdkEventGetCoords    func(event uintptr, x, y *float64) bool

	gtkOnce sync.Once
)

func loadGTK() {
	gtkOnce.Do(func() {
		libGTK = dlopen("libgtk-3.so.0")
		libGObject = dlopen("libgobject-2.0.so.0")
		libGDK = dlopen("libgdk-3.so.0")
		purego.RegisterLibFunc(&gtkEntryNew, libGTK, "gtk_entry_new")
		purego.RegisterLibFunc(&gtkEntrySetText, libGTK, "gtk_entry_set_text")
		purego.RegisterLibFunc(&gtkWidgetGrabFocus, libGTK, "gtk_widget_grab_focus")
		purego.RegisterLibFunc(&gtkWidgetShow, libGTK, "gtk_widget_show")
		purego.RegisterLibFunc(&gtkWidgetDestroy, libGTK, "gtk_widget_destroy")
		purego.RegisterLibFunc(&gtkWidgetGetParent, libGTK, "gtk_widget_get_parent")
		purego.RegisterLibFunc(&gtkWidgetGetHasWindow, libGTK, "gtk_widget_get_has_window")
		purego.RegisterLibFunc(&gtkWidgetGetWindow, libGTK, "gtk_widget_get_window")
		purego.RegisterLibFunc(&gtkWidgetGetAllocation, libGTK, "gtk_widget_get_allocation")
		purego.RegisterLibFunc(&gObjectRefSink, libGObject, "g_object_ref_sink")
		purego.RegisterLibFunc(&gObjectUnref, libGObject, "g_object_unref")
		purego.RegisterLibFunc(&gSignalConnectData, libGObject, "g_signal_connect_data")
		purego.RegisterLibFunc(&gdkWindowGetParent, libGDK, "gdk_window_get_parent")
		purego.RegisterLibFunc(&gdkWindowGetPosition, libGDK, "gdk_window_get_position")
		purego.RegisterLibFunc(&gdkEventGetCoords, libGDK, "gdk_event_get_coords")
	})
}

func dlopen(name string) uintptr {
	h, err := purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		panic("native-view-hosting: dlopen " + name + ": " + err.Error())
	}
	return h
}

// GDK key values and modifier bits the key handler below reads.
const (
	gdkKeyTab        = 0xff09
	gdkKeyISOLeftTab = 0xfe20 // Shift+Tab at the start of the view
	gdkShiftMask     = 1 << 0
)

// gdkRect is a GdkRectangle: a rectangle of whole units.
type gdkRect struct{ X, Y, Width, Height int32 }

// eventWindowOffset is where the GdkWindow pointer of an event sits: every
// GdkEvent starts with its type (a gint32) and then, in GdkEventAny, the
// GdkWindow that delivered it, the pointer aligned after the gint32. The
// compiler works the offset out, so it holds on 32-bit too; GdkEvent is
// opaque and GDK has no accessor for a generic event's window.
var eventWindowOffset = unsafe.Offsetof(struct {
	typ    int32
	window ptr
}{}.window)

// viewLocal turns a press on widget into the view-local DIPs PointerDown
// wants. The event arrives on the GdkWindow GDK picked, and for GtkEntry that
// is its private text-area child window, inset by the entry's padding, not
// the entry's own coordinates: gdk_event_get_coords answers "the event window
// relative x/y coordinates from an event"
// (https://docs.gtk.org/gdk3/method.Event.get_coords.html), so the event
// window's origin and the view's own have to come off (eventToViewDIPs).
//
// It reports false, and the caller then reports nothing, when GDK has no
// coordinates for the event or the event's window is not one the view is in:
// a guessed point would be worse than no point.
func viewLocal(widget, event ptr) (point, bool) {
	var x, y float64
	if !gdkEventGetCoords(event, &x, &y) {
		return point{}, false
	}
	window := gtkWidgetGetWindow(widget)
	if window == 0 {
		return point{}, false
	}
	eventX, eventY, ok := windowOriginIn(field[ptr](event, eventWindowOffset), window)
	if !ok {
		return point{}, false
	}
	viewX, viewY, ok := widgetOrigin(widget)
	if !ok {
		return point{}, false
	}
	return eventToViewDIPs(
		point{X: x, Y: y},
		point{X: float64(eventX), Y: float64(eventY)},
		point{X: float64(viewX), Y: float64(viewY)},
	), true
}

// windowOriginIn is the top-left of window in the coordinates of ancestor:
// the GdkWindow hierarchy is walked up with gdk_window_get_parent, adding
// gdk_window_get_position, which answers "the position of the window relative
// to the window's parent window"
// (https://docs.gtk.org/gdk3/method.Window.get_position.html) from the last
// configure event. Neither asks the X server, or the window system, for a
// screen-wide position, and both are logical units.
func windowOriginIn(window, ancestor ptr) (x, y int32, ok bool) {
	for n := 0; window != 0; n++ {
		if window == ancestor {
			return x, y, true
		}
		if n > 256 { // not a hierarchy this app made: do not walk it forever
			return 0, 0, false
		}
		var px, py int32
		gdkWindowGetPosition(window, &px, &py)
		x, y = x+px, y+py
		window = gdkWindowGetParent(window)
	}
	return 0, 0, false
}

// widgetOrigin is the top-left of widget in the coordinates of the GdkWindow
// gtk_widget_get_window returns for it: the allocations of the windowless
// widgets from widget up to the first ancestor that owns a window, summed. A
// widget without a window of its own sits at its allocation inside that
// window, and so does every windowless ancestor above it — GtkEntry is one of
// those widgets — while the windowed ancestor's own window is already at its
// allocation.
func widgetOrigin(widget ptr) (x, y int32, ok bool) {
	for n := 0; widget != 0; n++ {
		if n > 256 {
			return 0, 0, false
		}
		if gtkWidgetGetHasWindow(widget) {
			return x, y, true
		}
		var a gdkRect
		gtkWidgetGetAllocation(widget, &a)
		x, y = x+a.X, y+a.Y
		widget = gtkWidgetGetParent(widget)
	}
	return 0, 0, false
}

var (
	entriesMu sync.Mutex
	entries   = map[uintptr]*linuxText{}
)

// linuxText is one control. The GTK signals carry a pointer to it as user
// data, so the callback that receives it can report for the right entry
// without a callback of its own.
type linuxText struct {
	widget uintptr
	host   host

	mu        sync.Mutex
	destroyed bool
}

// The three signal handlers are made once for the whole app, one per
// signature, never per control (AGENTS.md): the user data says which entry
// the signal is for.

// focus-in-event reports that the entry took focus. Returning FALSE lets
// GTK's own handler run, so the entry really does get the keyboard.
var focusInHandler = purego.NewCallback(func(widget, event, data ptr) bool {
	if t := controlOf(data); t != nil {
		t.host.focus()
	}
	return false // let GTK's own handler run
})

// button-press-event reports the press before GTK places the caret. GDK
// answers in the coordinates of the window that delivered the event, and for
// the windowless GtkEntry that window is its private text-area child, inset
// by the entry's border and padding, so the answer is not yet the view-local
// DIPs the contract asks for: viewLocal brings it there, and reports nothing
// at all when it cannot.
var buttonPressHandler = purego.NewCallback(func(widget, event, data ptr) bool {
	if t := controlOf(data); t != nil {
		if p, ok := viewLocal(widget, event); ok {
			t.host.press(p.X, p.Y)
		}
	}
	return false // let the entry place its caret
})

// key-press-event takes Tab and Shift+Tab away from GTK and reports them:
// returning TRUE consumes the key, so focus leaves the view instead of
// walking the window's focus chain behind MyGo's back. Every other key goes
// through, the input method included.
var keyPressHandler = purego.NewCallback(func(widget, event, data ptr) bool {
	t := controlOf(data)
	if t == nil {
		return false
	}
	// GdkEventKey: type 0, state 24, keyval 28 (internal/linux/menu.go reads
	// the same bytes; GDK 3 has no accessor for them and GdkEvent is opaque).
	state, keyval := field[uint32](event, 24), field[uint32](event, 28)
	switch keyval {
	case gdkKeyTab:
		t.host.tab(state&gdkShiftMask != 0)
		return true // consumed: focus leaves the view
	case gdkKeyISOLeftTab:
		t.host.tab(true)
		return true
	}
	return false
})

// field reads a field of a native struct by byte offset, without cgo: the
// pointer goes through its own address so the compiler and vet see a Go
// pointer, never a uintptr turned back into one (internal/linux does the
// same).
func field[T any](p ptr, offset uintptr) T {
	return *(*T)(unsafe.Add(*(*unsafe.Pointer)(unsafe.Pointer(&p)), offset))
}

func controlOf(data uintptr) *linuxText {
	entriesMu.Lock()
	defer entriesMu.Unlock()
	return entries[data]
}

// newTextView makes the platform's text control in the host's window. The
// factory runs on the main thread, and so do handle, focus and destroy.
func newTextView(h host) (textView, error) {
	loadGTK()
	widget := gtkEntryNew()
	if widget == 0 {
		return nil, fmt.Errorf("gtk_entry_new returned NULL: is there a display?")
	}
	// gtk_entry_new hands back a floating reference: sink it, so this example
	// owns exactly one reference to release in destroy.
	gObjectRefSink(widget)
	t := &linuxText{widget: widget, host: h}
	// The map keeps t reachable for as long as the signals can fire, which is
	// what makes the pointer the signals carry valid (Go's collector does not
	// move heap objects).
	key := uintptr(unsafe.Pointer(t))
	entriesMu.Lock()
	entries[key] = t
	entriesMu.Unlock()
	gSignalConnectData(widget, "focus-in-event", focusInHandler, key, 0, 0)
	gSignalConnectData(widget, "button-press-event", buttonPressHandler, key, 0, 0)
	gSignalConnectData(widget, "key-press-event", keyPressHandler, key, 0, 0)
	gtkEntrySetText(widget, "Type here: this text is edited by the system.")
	// The host decides what is visible: show the entry, and let MyGo hide the
	// part of the view that is scrolled or clipped away.
	gtkWidgetShow(widget)
	return t, nil
}

// handle is the GtkWidget* MyGo puts where the view belongs: the host adds it
// to its parent and takes it out again.
func (t *linuxText) handle() uintptr { return t.widget }

// focus hands the entry the keyboard. The text and the selection stay; one
// control is the whole view, so back changes nothing here.
func (t *linuxText) focus(back bool) { gtkWidgetGrabFocus(t.widget) }

// destroy releases what newTextView made, exactly once. MyGo detaches the
// entry from its parent first and calls this on the main thread: destroy
// takes the container's reference away, and the unref releases the one
// newTextView took, in that order and never twice.
func (t *linuxText) destroy() {
	t.mu.Lock()
	if t.destroyed {
		t.mu.Unlock()
		return
	}
	t.destroyed = true
	t.mu.Unlock()

	key := uintptr(unsafe.Pointer(t))
	entriesMu.Lock()
	delete(entries, key)
	entriesMu.Unlock()
	gtkWidgetDestroy(t.widget)
	gObjectUnref(t.widget)
	t.widget = 0
}
