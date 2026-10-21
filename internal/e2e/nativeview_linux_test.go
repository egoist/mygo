//go:build linux && (amd64 || arm64)

package e2e

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"

	"github.com/egoist/mygo"
)

// The Linux fixture: a real GtkEntry, in this process, handed to MyGo's
// linux backend as the app's native view. GTK sends the window's own
// mouse and key events to it, and the ui only learns that it holds the
// keyboard through mygo.NativeViewHost's callbacks.
//
// Like the darwin fixture, the entry is not added to anything: the
// backend adds the app's widget to the container it made (host.Parent is
// that container) and places it, exactly as the darwin backend adds the
// app's NSView to its clip view. The fixture only makes the widget, shows
// it, takes a reference of its own (a floating GtkWidget reference belongs
// to the container that adopts it), reports the widget's signals, and
// destroys it once when MyGo destroys the view.
//
// GTK delivers key events to the focus widget, so the fixture's
// key-press-event handler is where Tab and Shift+Tab out of the control
// are observed, and its button-press-event handler is where a press on the
// control is. Both are GTK's own events, sent by the window manager and
// the test's XTest input, never by the test calling a callback.

// gtkAlloc is GtkAllocation, a GdkRectangle in integers.
type gtkAlloc struct{ X, Y, Width, Height int32 }

// GDK key values of Tab and Shift+Tab.
const (
	gdkTab        = 0xff09
	gdkISOLeftTab = 0xfe20
)

var (
	gtkLoadOnce sync.Once
	gtkErr      error

	gtkEntryNew           func() uintptr
	gtkWidgetShow         func(widget uintptr)
	gtkWidgetDestroy      func(widget uintptr)
	gtkWidgetGrabFocus    func(widget uintptr) bool
	gtkWidgetIsFocus      func(widget uintptr) bool
	gtkWidgetGetMapped    func(widget uintptr) bool
	gtkWidgetGetParent    func(widget uintptr) uintptr
	gtkWidgetGetToplevel  func(widget uintptr) uintptr
	gtkWidgetAllocation   func(widget uintptr, alloc *gtkAlloc)
	gtkWidgetTranslate    func(src, dest uintptr, x, y int32, outX, outY *int32) bool
	gtkWidgetGetHasWindow func(widget uintptr) bool
	gtkWidgetGetWindow    func(widget uintptr) uintptr
	gdkWindowGetParent    func(window uintptr) uintptr
	gdkWindowGetPosition  func(window uintptr, x, y *int32)
	gdkEventGetCoords     func(event uintptr, x, y *float64) bool
	gtkEntryGetText       func(entry uintptr) uintptr
	gtkEntrySetText       func(entry uintptr, text uintptr)
	gObjectRefSink        func(object uintptr) uintptr
	gObjectUnref          func(object uintptr)
	gSignalConnectData    func(instance, signal, handler, data, notify uintptr, flags uint32) uintptr
	gtkFocusPressed       uintptr
	gtkKeyPressed         uintptr
	gtkButtonPressed      uintptr
	gtkSignalHandlersOnce sync.Once
)

// gtkLoad opens GTK3 and GObject, and registers the entry's signal
// handlers once: purego callbacks are never freed, so they are made once
// at startup and route by the widget they were connected for.
func gtkLoad() {
	gtkLoadOnce.Do(func() {
		gtk, err := purego.Dlopen("libgtk-3.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			gtkErr = fmt.Errorf("e2e: cannot open GTK3: %w", err)
			return
		}
		gobject, err := purego.Dlopen("libgobject-2.0.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			gtkErr = fmt.Errorf("e2e: cannot open GObject: %w", err)
			return
		}
		// GDK holds the event's coordinates and the GdkWindow hierarchy; the
		// fixture needs both to turn a press into the control's own DIPs.
		gdk, err := purego.Dlopen("libgdk-3.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			gtkErr = fmt.Errorf("e2e: cannot open GDK3: %w", err)
			return
		}
		for _, f := range []struct {
			fn   any
			lib  uintptr
			name string
		}{
			{&gtkEntryNew, gtk, "gtk_entry_new"},
			{&gtkWidgetShow, gtk, "gtk_widget_show"},
			{&gtkWidgetDestroy, gtk, "gtk_widget_destroy"},
			{&gtkWidgetGrabFocus, gtk, "gtk_widget_grab_focus"},
			{&gtkWidgetIsFocus, gtk, "gtk_widget_is_focus"},
			{&gtkWidgetGetMapped, gtk, "gtk_widget_get_mapped"},
			{&gtkWidgetGetParent, gtk, "gtk_widget_get_parent"},
			{&gtkWidgetGetToplevel, gtk, "gtk_widget_get_toplevel"},
			{&gtkWidgetAllocation, gtk, "gtk_widget_get_allocation"},
			{&gtkWidgetTranslate, gtk, "gtk_widget_translate_coordinates"},
			{&gtkWidgetGetHasWindow, gtk, "gtk_widget_get_has_window"},
			{&gtkWidgetGetWindow, gtk, "gtk_widget_get_window"},
			{&gdkWindowGetParent, gdk, "gdk_window_get_parent"},
			{&gdkWindowGetPosition, gdk, "gdk_window_get_position"},
			{&gdkEventGetCoords, gdk, "gdk_event_get_coords"},
			{&gtkEntryGetText, gtk, "gtk_entry_get_text"},
			{&gtkEntrySetText, gtk, "gtk_entry_set_text"},
			{&gObjectRefSink, gobject, "g_object_ref_sink"},
			{&gObjectUnref, gobject, "g_object_unref"},
			{&gSignalConnectData, gobject, "g_signal_connect_data"},
		} {
			addr, err := purego.Dlsym(f.lib, f.name)
			if err != nil {
				gtkErr = fmt.Errorf("e2e: %s: %w", f.name, err)
				return
			}
			purego.RegisterFunc(f.fn, addr)
		}
		gtkSignalHandlersOnce.Do(func() {
			gtkFocusPressed = purego.NewCallback(gtkButtonPress)
			gtkKeyPressed = purego.NewCallback(gtkKeyPressEvent)
		})
	})
}

// gtkPtr is a pointer of a uintptr that came from C: the load of the
// variable's address, never a direct uintptr-to-unsafe.Pointer
// conversion.
func gtkPtr(p uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&p)) }

// gtkEventWindow is the GdkWindow an event was delivered on: every GdkEvent
// starts with its type (a gint32) and then, in GdkEventAny, that pointer,
// aligned after the gint32, which is 8 on the 64-bit platforms this file
// builds for.
func gtkEventWindow(event uintptr) uintptr { return *(*uintptr)(unsafe.Add(gtkPtr(event), 8)) }

// gtkEventKeyval is a GdkEventKey's keyval.
func gtkEventKeyval(event uintptr) uint32 { return *(*uint32)(unsafe.Add(gtkPtr(event), 28)) }

// gtkConnect connects signal of instance to handler, with the instance as
// the handler's data: the handlers are made once and route by it.
func gtkConnect(instance uintptr, signal string, handler uintptr) {
	b := append([]byte(signal), 0)
	gSignalConnectData(instance, uintptr(unsafe.Pointer(&b[0])), handler, instance, 0, 0)
	runtime.KeepAlive(b)
}

// gtkCString copies a C string into a Go string.
func gtkCString(p uintptr) string {
	if p == 0 {
		return ""
	}
	var b []byte
	for i := uintptr(0); ; i++ {
		c := *(*byte)(unsafe.Add(gtkPtr(p), i))
		if c == 0 {
			return string(b)
		}
		b = append(b, c)
	}
}

// gtkFixtures is the control of each native view: a GTK signal handler
// gets only the widget, and the widget is the handler's data.
var gtkFixtures sync.Map // uintptr -> *gtkFixture

func gtkFixtureOf(widget uintptr) *gtkFixture {
	if f, ok := gtkFixtures.Load(widget); ok {
		return f.(*gtkFixture)
	}
	return nil
}

// gtkFixture is the app's control: a real GtkEntry.
type gtkFixture struct {
	nativeViewEvents
	entry uintptr
}

// newNativeViewFixture makes a GtkEntry for the parent MyGo reports. It
// stays out of the container: the backend adds the app's widget.
func newNativeViewFixture(host *mygo.NativeViewHost) (nativeViewFixture, error) {
	if host.Platform != "linux" {
		return nil, fmt.Errorf("e2e: the linux fixture on %q", host.Platform)
	}
	if host.Parent == 0 {
		return nil, errors.New("e2e: the host has no parent widget")
	}
	gtkLoad()
	if gtkErr != nil {
		return nil, gtkErr
	}
	entry := gtkEntryNew()
	if entry == 0 {
		return nil, errors.New("e2e: cannot make a GtkEntry")
	}
	// The container that adopts the entry would own the only reference to
	// it: the fixture takes one of its own, so the control is the app's
	// until MyGo destroys it.
	gObjectRefSink(entry)
	f := &gtkFixture{entry: entry}
	f.bind(host)
	gtkConnect(entry, "button-press-event", gtkFocusPressed)
	gtkConnect(entry, "key-press-event", gtkKeyPressed)
	gtkFixtures.Store(entry, f)
	gtkWidgetShow(entry)
	return f, nil
}

// gtkEventLocal turns a press on entry into the control's own DIPs, what
// mygo.NativeViewHost.PointerDown wants. GDK answers in the coordinates of
// the window that delivered the event — "extract the event window relative
// x/y coordinates from an event",
// https://docs.gtk.org/gdk3/method.Event.get_coords.html — and that window is
// not the entry: GtkEntry is windowless (gtkentry.c:
// gtk_widget_set_has_window (entry, FALSE)) and makes a private text-area
// GdkWindow, a child of the window of the widget the entry sits in, at the
// entry's content allocation, which its border and padding inset. Forwarding
// those raw coordinates would report a point that is not the one pressed, off
// by the text area's origin; this is the same correction the example makes in
// examples/native-view-hosting/text_linux.go.
//
// False means GTK could not answer, or the event's window is not one the
// entry is in: the fixture then reports no press instead of a wrong point.
func gtkEventLocal(entry, event uintptr) (x, y float64, ok bool) {
	if !gdkEventGetCoords(event, &x, &y) {
		return 0, 0, false
	}
	window := gtkWidgetGetWindow(entry)
	if window == 0 {
		return 0, 0, false
	}
	eventX, eventY, ok := gtkWindowOriginIn(gtkEventWindow(event), window)
	if !ok {
		return 0, 0, false
	}
	viewX, viewY, ok := gtkWidgetOrigin(entry)
	if !ok {
		return 0, 0, false
	}
	return x + float64(eventX-viewX), y + float64(eventY-viewY), true
}

// gtkWindowOriginIn is the top-left of window in the coordinates of ancestor,
// walking the GdkWindow hierarchy up with gdk_window_get_parent and adding
// gdk_window_get_position: "the position of the window relative to the
// window's parent window",
// https://docs.gtk.org/gdk3/method.Window.get_position.html. Both are logical
// units, and neither asks the X server for a screen-wide position.
func gtkWindowOriginIn(window, ancestor uintptr) (x, y int32, ok bool) {
	for n := 0; window != 0; n++ {
		if window == ancestor {
			return x, y, true
		}
		if n > 256 { // not a hierarchy this fixture made
			return 0, 0, false
		}
		var px, py int32
		gdkWindowGetPosition(window, &px, &py)
		x, y = x+px, y+py
		window = gdkWindowGetParent(window)
	}
	return 0, 0, false
}

// gtkWidgetOrigin is the top-left of widget in the coordinates of the
// GdkWindow gtk_widget_get_window returns for it: the allocations of the
// windowless widgets from widget up to the first ancestor that owns a window,
// summed, which is where a windowless widget sits in that window.
func gtkWidgetOrigin(widget uintptr) (x, y int32, ok bool) {
	for n := 0; widget != 0; n++ {
		if n > 256 {
			return 0, 0, false
		}
		if gtkWidgetGetHasWindow(widget) {
			return x, y, true
		}
		var a gtkAlloc
		gtkWidgetAllocation(widget, &a)
		x, y = x+a.X, y+a.Y
		widget = gtkWidgetGetParent(widget)
	}
	return 0, 0, false
}

// gtkButtonPress is a press on the control: the app learns where, in the
// control's own DIPs, and that the control is the one taking the keyboard.
func gtkButtonPress(widget, event, data uintptr) uintptr {
	if f := gtkFixtureOf(data); f != nil {
		if x, y, ok := gtkEventLocal(widget, event); ok {
			f.pointerDown(x, y)
		}
		f.tookFocus()
	}
	return 0 // the event goes on to the entry's own handling
}

// gtkKeyPressEvent is Tab (back: Shift+Tab) at the control: the platform's
// own Tab-out, which the ui turns into the focus moving on. The control
// does not see the Tab itself: the ui owns the focus order.
func gtkKeyPressEvent(widget, event, data uintptr) uintptr {
	keyval := gtkEventKeyval(event)
	if keyval != gdkTab && keyval != gdkISOLeftTab {
		return 0
	}
	if f := gtkFixtureOf(data); f != nil {
		f.tabbedOut(keyval == gdkISOLeftTab)
	}
	return 1
}

// nvClick clicks a point of the window's surface as a user does, for the
// native-view tests: on Linux the general click hook already goes through
// the X server, so it is what these tests use too.
func nvClick(w *mygo.Window, x, y float64) bool { return click(w, x, y) }

// nvTypeInto types text into whatever holds the keyboard. XTest keys go to
// the focused widget, which the shared helper reaches with pressKeys, so
// there is nothing to do here.
func nvTypeInto(w *mygo.Window, text string) bool { return false }

// NativeHandle is the control's GtkWidget*.
func (f *gtkFixture) NativeHandle() uintptr { return f.entry }

// Focus gives the control the keyboard, as Tab into the native view does.
// GTK tells the app nothing that says the widget became the focus widget,
// so the app reports it here, after the grab succeeded.
func (f *gtkFixture) Focus(back bool) {
	if f.entry == 0 {
		return
	}
	if gtkWidgetGrabFocus(f.entry) {
		f.tookFocus()
	}
}

// Destroy is called once, after the backend detached the control: the
// fixture owns the reference it took and drops it here. It forgets the
// widget, so a later query from a test reports nothing instead of a
// destroyed one. Main thread, as the adapter's Destroy is.
func (f *gtkFixture) Destroy() {
	f.destroyedControl()
	entry := f.entry
	if entry == 0 {
		return
	}
	f.entry = 0
	gtkFixtures.Delete(entry)
	gtkWidgetDestroy(entry)
	gObjectUnref(entry)
}

// Text is the control's own text. It asks GTK on the main thread: GTK is
// not safe to call from a test goroutine.
func (f *gtkFixture) Text() (text string) {
	mygo.RunOnMain(func() {
		if f.entry == 0 {
			return
		}
		text = gtkCString(gtkEntryGetText(f.entry))
	})
	return text
}

// SetText sets the control's own text, as the app does, on the main thread.
func (f *gtkFixture) SetText(text string) {
	mygo.RunOnMain(func() {
		if f.entry == 0 {
			return
		}
		b := append([]byte(text), 0)
		gtkEntrySetText(f.entry, uintptr(unsafe.Pointer(&b[0])))
		runtime.KeepAlive(b)
	})
}

// HoldsKeyboard reports whether the control is GTK's focus widget, asked of
// GTK on the main thread.
func (f *gtkFixture) HoldsKeyboard() (holds bool) {
	mygo.RunOnMain(func() {
		if f.entry != 0 {
			holds = gtkWidgetIsFocus(f.entry)
		}
	})
	return holds
}

// Box is the control's box and the part of it the window shows, in the
// window surface's DIPs: the widget's allocation translated to the
// toplevel, clipped by the container it was put in. Nothing of a control
// that was hidden or unmapped shows. It runs on the main thread, where GTK
// is.
func (f *gtkFixture) Box() (box, visible mygo.Rectangle) {
	mygo.RunOnMain(func() {
		if f.entry == 0 {
			return
		}
		top := gtkWidgetGetToplevel(f.entry)
		if top == 0 || top == f.entry || !gtkWidgetGetMapped(f.entry) {
			return
		}
		var alloc gtkAlloc
		gtkWidgetAllocation(f.entry, &alloc)
		var x, y int32
		if !gtkWidgetTranslate(f.entry, top, 0, 0, &x, &y) {
			return
		}
		box = mygo.Rectangle{X: int(x), Y: int(y), Width: int(alloc.Width), Height: int(alloc.Height)}
		// The part of the control the window shows: what every ancestor up
		// to the toplevel leaves of it, the clip the backend put it in and
		// the window's own box.
		visible = box
		for w := gtkWidgetGetParent(f.entry); w != 0; w = gtkWidgetGetParent(w) {
			var a gtkAlloc
			var ax, ay int32
			if !gtkWidgetTranslate(w, top, 0, 0, &ax, &ay) {
				break
			}
			gtkWidgetAllocation(w, &a)
			visible = rectangleIntersect(visible, mygo.Rectangle{
				X: int(ax), Y: int(ay), Width: int(a.Width), Height: int(a.Height),
			})
		}
	})
	return box, visible
}

// rectangleIntersect is the part of a both rectangles share, empty when
// they do not meet.
func rectangleIntersect(a, b mygo.Rectangle) mygo.Rectangle {
	x := max(a.X, b.X)
	y := max(a.Y, b.Y)
	right := min(a.X+a.Width, b.X+b.Width)
	bottom := min(a.Y+a.Height, b.Y+b.Height)
	if right <= x || bottom <= y {
		return mygo.Rectangle{}
	}
	return mygo.Rectangle{X: x, Y: y, Width: right - x, Height: bottom - y}
}
