package main

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
