//go:build linux && (amd64 || arm64)

package linux

import "github.com/ebitengine/purego"

// TestWindowOnScreen captures the window's actual backing pixels, including
// alpha at its outside corners, rather than the content's own screenshot.
// GDK provides this on X11; on Wayland it returns nil.
func TestWindowOnScreen(handle uintptr) []byte {
	w := windowByHandle(handle)
	var getFromWindow func(win ptr, x, y, width, height int32) ptr
	if w == nil || !theBackend.onX11 || !bind(libGDK, &getFromWindow, "gdk_pixbuf_get_from_window") {
		return nil
	}
	var a gdkRectangle
	gtkWidgetGetAllocation(w.win, &a)
	pix := getFromWindow(gtkWidgetGetWindow(w.win), 0, 0, a.Width, a.Height)
	if pix == 0 {
		return nil
	}
	defer gObjectUnref(pix)
	return pngFromPixbuf(pix)
}

// TestWindowOutlineEnvironment reports whether pixels can be captured and
// whether the screen can show transparent corners.
func TestWindowOutlineEnvironment(handle uintptr) (x11, composited bool) {
	var screenComposited func(screen ptr) bool
	return theBackend.onX11, bind(libGDK, &screenComposited, "gdk_screen_is_composited") && screenComposited(gtkWidgetGetScreen(handle))
}

// TestWindowInputAt reads the X server's input shape, in device pixels.
func TestWindowInputAt(handle uintptr, x, y int) (contains, available bool) {
	w := windowByHandle(handle)
	if w == nil || !theBackend.onX11 {
		return false, false
	}
	lib, err := open("libXext.so.6")
	if err != nil {
		return false, false
	}
	defer purego.Dlclose(lib)
	xlib, err := open("libX11.so.6")
	if err != nil {
		return false, false
	}
	defer purego.Dlclose(xlib)
	var display func(d ptr) ptr
	var xid func(w ptr) uintptr
	var rectangles func(d ptr, window uintptr, kind int32, count, ordering *int32) ptr
	var free func(p ptr) int32
	if !bind(libGDK, &display, "gdk_x11_display_get_xdisplay") || !bind(libGDK, &xid, "gdk_x11_window_get_xid") ||
		!bind(lib, &rectangles, "XShapeGetRectangles") || !bind(xlib, &free, "XFree") {
		return false, false
	}
	var count, ordering int32
	p := rectangles(display(gdkDisplayGetDefault()), xid(gtkWidgetGetWindow(w.win)), 2, &count, &ordering) // ShapeInput
	if p == 0 {
		return false, true
	}
	defer free(p)
	for i := int32(0); i < count; i++ {
		r := p + uintptr(i)*8
		rx, ry := int(field[int16](r, 0)), int(field[int16](r, 2))
		width, height := int(field[uint16](r, 4)), int(field[uint16](r, 6))
		if x >= rx && x < rx+width && y >= ry && y < ry+height {
			return true, true
		}
	}
	return false, true
}
