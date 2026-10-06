//go:build linux && (amd64 || arm64)

package linux

import (
	"sync"
	"unsafe"
)

var nativeTestsOnce sync.Once
var gtkLayoutGetBinWindow func(ptr) ptr
var gdkWindowGetPosition func(ptr, *int32, *int32)
var gdkEventNew func(int32) ptr
var gtkWidgetEvent func(ptr, ptr) bool
var gtkWidgetTranslateCoordinates func(ptr, ptr, int32, int32, *int32, *int32) bool

func loadNativeTests() {
	nativeTestsOnce.Do(func() {
		mustBind(libGTK, &gtkLayoutGetBinWindow, "gtk_layout_get_bin_window")
		mustBind(libGDK, &gdkWindowGetPosition, "gdk_window_get_position")
		mustBind(libGDK, &gdkEventNew, "gdk_event_new")
		mustBind(libGTK, &gtkWidgetEvent, "gtk_widget_event")
		mustBind(libGTK, &gtkWidgetTranslateCoordinates, "gtk_widget_translate_coordinates")
	})
}

// TestNativeViewFrame reads actual GTK allocations and the scrolled bin
// window's offset. It must run on the main thread.
func TestNativeViewFrame(parent uintptr) (bounds, clip [4]float64, visible, focused bool) {
	n := hostedViews[parent]
	if n == nil || n.closed {
		return
	}
	loadNativeTests()
	var c, v gdkRectangle
	gtkWidgetGetAllocation(n.clip, &c)
	gtkWidgetGetAllocation(n.view, &v)
	var x, y int32
	gdkWindowGetPosition(gtkLayoutGetBinWindow(n.clip), &x, &y)
	// GtkOverlay gives each overlay a separate GdkWindow, and allocates
	// its widget at (0,0) inside that window. Translate back to the surface.
	var cx, cy int32
	gtkWidgetTranslateCoordinates(n.clip, n.s.area, 0, 0, &cx, &cy)
	clip = [4]float64{float64(cx), float64(cy), float64(c.Width), float64(c.Height)}
	bounds = [4]float64{float64(cx + x + v.X), float64(cy + y + v.Y), float64(v.Width), float64(v.Height)}
	return bounds, clip, gtkWidgetGetVisible(n.clip), n.hasFocus()
}

// TestNativeViewTab sends a GdkEventKey through the GtkWindow's event
// processing, while its native text field has the focus.
func TestNativeViewTab(parent uintptr, backward bool) bool {
	n := hostedViews[parent]
	if n == nil || n.closed || !n.hasFocus() {
		return false
	}
	loadNativeTests()
	e := gdkEventNew(8) // GDK_KEY_PRESS
	*slot(e, 8) = gObjectRef(gtkWidgetGetWindow(n.s.w.win))
	*(*uint32)(unsafe.Pointer(slot(e, 28))) = 0xff09 // GDK_KEY_Tab
	if backward {
		*(*uint32)(unsafe.Pointer(slot(e, 24))) = 1
	}
	defer gdkEventFree(e)
	return gtkWidgetEvent(n.s.w.win, e)
}

// TestNativeViewAccessParent verifies that GTK's native accessible is
// parented to a logical MyGo node rather than to the physical overlay.
func TestNativeViewAccessParent(parent uintptr) bool {
	n := hostedViews[parent]
	if n == nil || n.closed || atkObjectGetParent == nil {
		return false
	}
	return accessObjects[atkObjectGetParent(gtkWidgetGetAccessible(n.clip))] != nil
}
