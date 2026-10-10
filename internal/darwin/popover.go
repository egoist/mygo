//go:build darwin

package darwin

import (
	"github.com/ebitengine/purego/objc"

	"github.com/egoist/mygo/internal/platform"
)

// A flyout with Flyout.Popover shows its web view or surface in an
// NSPopover, which draws the system's material and arrow, and animates.
// AppKit places it against the anchor: the arrow points at the anchor's
// middle, on the side the flyout asks for, which AppKit flips where the
// screen has no room. The popover makes its window as it shows: w.win is
// that window while it shows. AppKit gives a popover that shows the
// keyboard, focusable or not, while its parent stays the key window, which
// the popover's window says it is too. A focusable flyout's popover is
// transient: it asks the delegate (popoverShouldClose:) before it closes on
// a click elsewhere, as WindowHandler.ShouldClose decides.

const (
	popoverBehaviorApplicationDefined = 0
	popoverBehaviorTransient          = 1
)

func (w *window) createPopover(content NSRect) {
	w.delegate = alloc("MyGoWindowDelegate")
	w.createContent(content)
	send(w.view, "setAutoresizingMask:", nsViewWidthHeightSizable)
	w.popoverController = alloc("NSViewController")
	send(w.popoverController, "setView:", uintptr(w.view))
	w.popover = alloc("NSPopover")
	send(w.popover, "setContentViewController:", uintptr(w.popoverController))
	msgSetSize(w.popover, sel("setContentSize:"), content.Size)
	behavior := uintptr(popoverBehaviorApplicationDefined)
	if w.flyout.Focusable {
		behavior = popoverBehaviorTransient
	}
	send(w.popover, "setBehavior:", behavior)
	send(w.popover, "setDelegate:", uintptr(w.delegate))
	w.register()
}

// popoverAnchor returns the anchor in the parent's content view, which is
// not flipped, and the edge of it the popover goes to.
func (w *window) popoverAnchor() (NSRect, uint) {
	a, p := w.flyout.Anchor, w.parent
	h := msgRect(p.view, sel("bounds")).Size.Height
	r := NSRect{
		Origin: NSPoint{float64(a.X), h - float64(a.Y) - float64(max(a.Height, 1))},
		Size:   NSSize{float64(max(a.Width, 1)), float64(max(a.Height, 1))},
	}
	// NSRectEdge
	edge := map[platform.Side]uint{platform.SideBottom: 1, platform.SideTop: 3, platform.SideRight: 2, platform.SideLeft: 0}[w.flyout.Side]
	return r, edge
}

func (w *window) showPopover() {
	p := w.parent
	if p == nil || p.closed || p.view == 0 {
		return
	}
	if !sendBool(w.popover, "isShown") {
		w.popoverClosing = false
		msgSetSize(w.popover, sel("setContentSize:"), NSSize{float64(w.flyoutSize.Width), float64(w.flyoutSize.Height)})
		r, edge := w.popoverAnchor()
		msgRectIDUint(w.popover, sel("showRelativeToRect:ofView:preferredEdge:"), r, p.view, edge)
		w.adoptPopoverWindow()
	}
}

// adoptPopoverWindow makes the window the popover shows in the flyout's.
func (w *window) adoptPopoverWindow() {
	win := send(w.view, "window")
	if win == 0 || win == w.win {
		return
	}
	w.forgetPopoverWindow()
	w.win = win
	w.b.byNSWindow[win] = w
	if w.surface != nil {
		send(win, "makeFirstResponder:", uintptr(w.surface.view))
	}
}

func (w *window) forgetPopoverWindow() {
	if w.win == 0 {
		return
	}
	delete(w.b.byNSWindow, w.win)
	w.win = 0
}

// placePopover moves a popover that shows to the anchor, at the size.
func (w *window) placePopover() {
	if !sendBool(w.popover, "isShown") {
		return
	}
	msgSetSize(w.popover, sel("setContentSize:"), NSSize{float64(w.flyoutSize.Width), float64(w.flyoutSize.Height)})
	r, _ := w.popoverAnchor()
	msgSetRect(w.popover, sel("setPositioningRect:"), r)
}

// hidePopover closes the popover, which shows again with Show, and gives
// the keyboard it had back to the parent.
func (w *window) hidePopover() {
	if !sendBool(w.popover, "isShown") {
		return
	}
	key := w.win != 0 && sendBool(w.win, "isKeyWindow")
	w.popoverClosing = true
	send(w.popover, "close")
	if p := w.parent; key && p != nil && !p.closed && sendBool(p.win, "isVisible") {
		send(p.win, "makeKeyWindow")
	}
}

// closePopover destroys the flyout, at once.
func (w *window) closePopover() {
	send(w.popover, "setAnimates:", 0)
	w.hidePopover()
	w.cleanup()
	w.h.Closed()
}

func (w *window) cleanupPopover() {
	w.forgetPopoverWindow()
	send(w.popover, "setDelegate:", 0)
	if sendBool(w.popover, "isShown") {
		send(w.popover, "setAnimates:", 0)
		send(w.popover, "close")
	}
	send(w.popover, "setContentViewController:", 0)
	release(w.popover)
	release(w.popoverController)
}

func popoverDelegateMethods() []objc.MethodDef {
	b := func() *Backend { return theBackend }
	return []objc.MethodDef{
		// The user clicked elsewhere, in a transient popover.
		method("popoverShouldClose:", func(self id, _ objc.SEL, popover id) bool {
			if w := b().windowFor(self); w != nil {
				return w.h.ShouldClose()
			}
			return true
		}),
		method("popoverDidClose:", func(self id, _ objc.SEL, n id) {
			w := b().windowFor(self)
			if w == nil {
				return
			}
			w.forgetPopoverWindow()
			if w.popoverClosing {
				w.popoverClosing = false
				return
			}
			w.cleanup()
			w.h.Closed()
		}),
	}
}
