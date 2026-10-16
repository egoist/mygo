package main

// The coordinate arithmetic of the three platforms' adapters, with nothing of
// any toolkit in it: mygo.NativeViewHost.PointerDown asks for DIPs from the
// top-left of the view MyGo hosts, whatever the platform's own coordinates
// are, and each block below is one platform's. No block needs a build tag:
// the arithmetic is pure, so any host runs its test, and only the adapters
// that call it are per-platform.

// mygo.NativeViewHost.PointerDown reports a press in DIPs from the top-left of
// the view MyGo hosts, whatever the platform's own coordinates are: AppKit
// counts Y upward from a view's bottom-left unless the view is flipped, and it
// counts it from the bounds origin, which a scroll view moves as its document
// scrolls. This file holds the arithmetic, away from the events themselves, so
// it is testable without a native event.

// point is a point in a view's own coordinates.
type point struct{ X, Y float64 }

// bounds is a view's bounds rectangle: its origin is where the visible part of
// the view's content starts in the view's coordinates, and its size is how big
// that visible part is. A scroll view moves the origin as its document
// scrolls; a plain view leaves it at (0, 0).
type bounds struct {
	X, Y, Width, Height float64
}

// localDIPs turns p, a point in the coordinates of the view MyGo hosts, into
// what PointerDown wants: DIPs from the top-left of that view's visible part,
// which the view's scroll position does not change.
//
// p must already be in the view's own coordinates (macOS: convertPoint:fromView:
// with that view as the receiver). Those carry the view's bounds origin, the
// scroll offset of a scrolled view, so the origin comes off first. A view that
// is not flipped counts Y upward from the bottom of its bounds, which inverts
// against the bounds height, not the frame's.
func localDIPs(p point, b bounds, flipped bool) point {
	p.X -= b.X
	p.Y -= b.Y
	if !flipped {
		p.Y = b.Height - p.Y
	}
	return p
}

// A GTK press arrives in the coordinates of the GdkWindow that delivered it,
// and that window is not always the hosted widget's own: GtkEntry has no
// window of its own (gtkentry.c calls gtk_widget_set_has_window (entry,
// FALSE)), and its realize makes a private text-area GdkWindow, a child of
// the window of the widget the entry sits in, to receive the mouse and to
// talk to the input method window:
//
//	priv->text_area = gdk_window_new (gtk_widget_get_window (widget), &attributes, ...);
//	gdk_window_move_resize (priv->text_area, priv->text_allocation.x, priv->text_allocation.y, ...);
//
// where priv->text_allocation is the entry's content allocation, the entry's
// own allocation inset by its CSS border and padding. GDK's accessor says the
// same in its own words: gdk_event_get_coords "extracts the event window
// relative x/y coordinates from an event"
// (https://docs.gtk.org/gdk3/method.Event.get_coords.html).
//
// So a press on the entry is a point in its text area, while PointerDown
// wants a point in the view MyGo hosts: the entry. Two origins are enough to
// fix that up, which is what this file holds, away from the event itself, so
// it can be tested without a display.

// eventToViewDIPs turns p, a pointer position in the coordinates of the
// GdkWindow that delivered the event, into the view-local DIPs
// mygo.NativeViewHost.PointerDown wants.
//
// eventOrigin is the top-left of the window that delivered the event, and
// viewOrigin the top-left of the hosted view, both in the coordinates of the
// window the view's own coordinates are in: the sum of gdk_window_get_position
// up the GdkWindow hierarchy for eventOrigin (gdk_window_get_position returns
// "the position of the window ... relative to the window's parent window",
// https://docs.gtk.org/gdk3/method.Window.get_position.html), and the view's
// allocation, walked up through windowless ancestors, for viewOrigin. Every
// one of those is a logical unit: GDK reports event coordinates, window
// positions and allocations in application pixels and divides X11's device
// pixels by the window's scale factor itself (gdk/x11/gdkdevicemanager-core-x11.c:
// "event->button.x = (gdouble) xevent->xbutton.x / scale"), so nothing here
// multiplies by a scale factor and the same arithmetic holds at GDK_SCALE 1
// and 2.
func eventToViewDIPs(p, eventOrigin, viewOrigin point) point {
	p.X += eventOrigin.X - viewOrigin.X
	p.Y += eventOrigin.Y - viewOrigin.Y
	return p
}

// The Windows press arithmetic, with nothing of user32 in it. It carries no
// build tag, as coords.go (AppKit) and coords_window.go (GTK) do not: the
// arithmetic is pure, so any host runs its test, and only text_windows.go,
// which calls it, is Windows alone.
//
// A press arrives in the client area of the app's control, in device pixels of
// that control's own window. mygo.NativeViewHost.PointerDown asks for the DIPs
// of the view itself, and mygo shows a view by sizing the control's whole
// window rectangle to the frame the content placed the view at
// (internal/windows/nativeview.go, placeNative: SetWindowPos sets the outer
// rectangle). A control with a border — the EDIT this example makes has
// WS_BORDER — therefore has its client area strictly inside that frame, and
// the point the message carries is not yet a point of the frame.

// pressDIPs turns the point of a mouse message in the client area of a
// control's window into what PointerDown wants: DIPs from the top-left of the
// frame mygo placed the view at. client is the point the message carried;
// clientOrigin is, in screen device pixels, where the control's client area
// begins, and frameOrigin where the control's own window rectangle begins,
// which is the frame; scale is the device pixels of a DIP at the DPI the
// window has now.
//
// The border is measured by the caller, never assumed: SM_CXBORDER is one
// pixel on a standard display, but nothing promises that width. It answers
// false for a scale that cannot describe a DIP, so that a press which cannot
// be placed exactly is not reported at all rather than reported at the point
// of the client area.
func pressDIPs(client, clientOrigin, frameOrigin point, scale float64) (point, bool) {
	if !(scale > 0) {
		return point{}, false
	}
	return point{
		X: (clientOrigin.X + client.X - frameOrigin.X) / scale,
		Y: (clientOrigin.Y + client.Y - frameOrigin.Y) / scale,
	}, true
}
