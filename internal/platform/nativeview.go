package platform

// NativeView is a platform view an app makes itself and shows inside the
// content of a surface: mygo.NewNativeView. It is the app's own
// object, which mygo attaches, shows where its element is, and destroys.
type NativeView interface {
	// NativeHandle returns the platform handle of the view: an NSView* on
	// macOS, an HWND on Windows, a GtkWidget* on Linux. Zero when the view
	// has none, which fails the creation.
	NativeHandle() uintptr
	// Focus gives the view the keyboard as Tab does, at its first control
	// taking the focus, or its last when back, as Shift+Tab does. Tab past
	// the other end comes back as NativeViewHost.TabOut.
	Focus(back bool)
	// Destroy destroys the view. mygo calls it once, on the main thread,
	// when the app destroys the native view or its window closes.
	Destroy()
}

// NativeViewHost is what a NativeViewSurface hears of the window a native
// view goes in: what its factory makes the view with.
type NativeViewHost struct {
	// Platform is the platform the view runs on: "darwin", "windows" or
	// "linux".
	Platform string
	// Parent is the platform handle of the container the surface made for
	// this view and lends the factory: an NSView* on macOS, an HWND on
	// Windows, a GtkWidget* on Linux. It is the surface's for the view's
	// lifetime and is not the window's whole content view: the app's
	// control is made inside it and does not take it over or remove it.
	Parent uintptr
	// Scale is the window's scale when the surface made the view: how many
	// device pixels a DIP was. An initial snapshot, not the window's
	// current scale: a platform that needs the scale now asks its own
	// toolkit for it (Windows: GetDpiForWindow).
	Scale float64
}

// NativeViewSurface is the surface of a backend that shows native views
// (mygo.NewNativeView). It is optional: a surface that is not one
// reports native views unsupported, and apps that show none do not carry
// the code of the backends that do. Backends implement it where they
// build their surface, which registers it for the apps that ask.
type NativeViewSurface interface {
	// NewNativeView makes the platform side of a native view: it calls
	// make with the host of the view, and returns what make made, which
	// it has attached to the window by the time it returns. An error, or
	// a nil view, fails the creation: mygo destroys the view make
	// returned, so the surface must not. Main thread only.
	NewNativeView(make func(NativeViewHost) (NativeView, error)) (NativeView, error)
	// PlaceNativeViews shows the native views of the surface where the
	// content's last frame shows them, in the order it painted them among
	// the web views too (NativeViewPlacement.Order), and hides the others,
	// which keep their state. A view shows within its Clip, with what its
	// Covers hold painted over it, and takes the pointer there: a press
	// over a cover is the surface's. Main thread only.
	PlaceNativeViews(views []NativeViewPlacement)
	// TakeFocus gives the content the keyboard back from a native view,
	// as Tab leaving its last control does. Main thread only.
	TakeFocus()
}

// NativeViewPlacement is where the content of a surface shows a native
// view (Surface.PlaceNativeViews), in DIPs relative to the surface.
type NativeViewPlacement struct {
	View NativeView
	// Frame is the view's box; Clip the part of it that shows, which the
	// frames leave transparent but where the content painted over it.
	Frame, Clip RectF
	// Covers are the boxes, within Clip, of what the content painted over
	// the view that takes the pointer: there the pointer is the
	// surface's, elsewhere in Clip the view's.
	Covers []RectF
	// Order is where the content painted the view among the web views and
	// native views of the frame, first painted first: a view shows over
	// what was painted before it.
	Order int
}

// At reports whether the pointer at x, y is the native view's.
func (p *NativeViewPlacement) At(x, y float64) bool {
	if !p.Clip.Contains(x, y) {
		return false
	}
	for _, c := range p.Covers {
		if c.Contains(x, y) {
			return false
		}
	}
	return true
}
