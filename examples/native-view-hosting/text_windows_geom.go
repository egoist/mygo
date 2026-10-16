package main

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
