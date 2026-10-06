package platform

// NativeViewHost owns a clipping container and the platform view adopted
// into it. All methods and handler calls run on the main thread.
type NativeViewHost interface {
	Parent() uintptr
	// Attach adopts a newly created NSView (+1), a parentless GTK widget
	// (floating or +1), or a child HWND created with Parent as its parent.
	// On error ownership stays with the caller.
	Attach(view uintptr) error
	Place(NativeViewPlacement)
	Focus(backward bool)
	// Blur returns focus to the surface only if this host has it.
	Blur()
	// Command sends a MyGo Edit menu role to the focused native control.
	Command(command string) bool
	// Close calls Closing before releasing the adopted view, exactly once.
	Close()
}

// NativeViewHandler connects a host's native focus and lifetime to the core.
type NativeViewHandler interface {
	Focused()
	Traverse(backward bool)
	Closing()
	// Message handles Windows notifications sent to the parent HWND.
	Message(message uint32, wparam, lparam uintptr) (uintptr, bool)
}

// NativeViewPlacement describes the view in surface coordinates (DIPs).
type NativeViewPlacement struct {
	ID uint64
	// Bounds is the entire layout box; Clip is its visible intersection
	// with rectangular ancestor clips and the surface.
	Bounds, Clip     RectF
	Visible, Enabled bool
	// ManagesTab leaves Tab to the control; its adapter calls Traverse
	// when navigation should leave the native subtree.
	ManagesTab bool
}
