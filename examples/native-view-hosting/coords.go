package main

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
