// Command native-view-hosting shows MyGo hosting a real text control of the
// platform: an NSTextView in an NSScrollView on macOS, an EDIT control on
// Windows, a GtkEntry on Linux. MyGo draws the window and the controls
// around it and leaves a hole for the native one, which draws and edits
// itself, native input method included.
//
// This file is what the platform files share, and it imports nothing from
// mygo: each platform file makes a textView, and adapter presents that view
// to mygo as its NativeViewAdapter (the assertion in main.go checks it).
package main

// host is what a platform control needs of the mygo.NativeViewHost its
// factory hears of. main.go fills it in.
type host struct {
	// platform is the native platform's name: "darwin", "windows", "linux".
	platform string
	// parent is the native container MyGo made for this view: an NSView* on
	// macOS, an HWND on Windows, a GtkWidget* on Linux. MyGo owns it and
	// lends it to the factory for the view's lifetime; it is not the app's
	// whole window, and the factory must not take it over, release it or
	// draw outside it. macOS hosts the view in a clip view of its own
	// (internal/darwin/nativeview.go), which is the parent its factory
	// hears; Windows needs the parent when the control is created.
	parent uintptr
	// scale is how many device pixels a DIP was when the view was made:
	// mygo.NativeViewHost.Scale is an initial snapshot, not the window's
	// current scale. AppKit's points are already DIPs, so macOS needs no
	// conversion; Windows asks the system for the DPI the window has now
	// (text_windows.go) and keeps this only as the fallback.
	scale float64
	// focused, tabOut and pointerDown report what the native control did to
	// the app. main.go sets them, and the three methods below hand the
	// report on to mygo. They are always set: mygo fills the callbacks of
	// the NativeViewHost it passes to the factory before it runs the
	// factory, so they are ready when the control is created, not after.
	focused     func()
	tabOut      func(back bool)
	pointerDown func(x, y float64)
}

// The three reports below are what the native control calls. The platform
// calls them on the main thread (a window message, an AppKit override, a
// GTK signal). They are never nil: mygo fills them in before the factory
// creates the control, so calling one needs no nil check. A view is not live
// until the factory has returned and mygo has hosted the handle, though, and
// a report that arrives before that reaches no window: a control that reports
// during its own creation is ignored.
func (h host) focus() {
	h.focused()
}

func (h host) tab(back bool) {
	h.tabOut(back)
}

func (h host) press(x, y float64) {
	h.pointerDown(x, y)
}

// textView is the platform control behind the adapter: handle is the
// NSView*/HWND/GtkWidget* mygo hosts (the one the factory created, never a
// borrowed one), focus(back) moves the keyboard into it, and destroy
// releases exactly what the factory made.
type textView interface {
	handle() uintptr
	focus(back bool)
	destroy()
}

// adapter is the control as mygo sees it. mygo calls these on the main
// thread only: NativeHandle while it hosts the view, Focus when focus moves
// into it, and Destroy once when the view is destroyed.
type adapter struct{ v textView }

func (a *adapter) NativeHandle() uintptr { return a.v.handle() }
func (a *adapter) Focus(back bool)       { a.v.focus(back) }
func (a *adapter) Destroy()              { a.v.destroy() }
