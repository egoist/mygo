//go:build darwin

package darwin

import (
	"errors"
	"strings"

	"github.com/egoist/mygo/internal/platform"
)

// Input for a page is made of AppKit events that MyGo builds and hands to
// the web view's responder methods (mouseDown:, keyDown:, insertText:), as
// NSWindow's sendEvent: hands it the user's. WebKit takes them for the
// system's and the page's events are trusted. They go to the web view
// itself, not through the window's event routing, so a window that is not
// key, or an application that is not active, does not hold them back, and
// they never raise the window.

// NSEventType values of the events SendMouse and SendKey make.
const (
	nsLeftMouseDown = 1
	nsLeftMouseUp   = 2
	nsMouseMoved    = 5
	nsKeyDown       = 10
	nsKeyUp         = 11
)

// NSEventModifierFlag values.
const (
	nsShiftMask    = 1 << 17
	nsControlMask  = 1 << 18
	nsAlternate    = 1 << 19
	nsCommandMask  = 1 << 20
	nsNumericPad   = 1 << 21
	nsFunctionMask = 1 << 23
)

// Page.SendMouse finds PageInput at run time: this has the compiler check it.
var _ platform.PageInput = (*window)(nil)

var errNoPageView = errors.New("mygo: the web view is not in a window")

// pageView returns the web view that takes input, or the reason it cannot.
func (w *window) pageView() error {
	if w.closed || w.web == 0 || send(w.web, "window") == 0 {
		return errNoPageView
	}
	return nil
}

func eventFlags(m platform.Modifiers) uint {
	var f uint
	if m&platform.ModShift != 0 {
		f |= nsShiftMask
	}
	if m&platform.ModCtrl != 0 {
		f |= nsControlMask
	}
	if m&platform.ModAlt != 0 {
		f |= nsAlternate
	}
	if m&platform.ModSuper != 0 {
		f |= nsCommandMask
	}
	return f
}

// eventTime is the timestamp of an event made now: the seconds since the
// system started, as the system stamps its own.
func eventTime() float64 {
	return msgFloat(send(class("NSProcessInfo"), "processInfo"), sel("systemUptime"))
}

func (w *window) SendMouse(e platform.PageMouseEvent, done func(error)) {
	if err := w.pageView(); err != nil {
		done(err)
		return
	}
	withPool(func() {
		// The point is in CSS pixels of the page, which the page's zoom
		// makes zoom points of the web view.
		zoom := msgFloat(w.web, sel("pageZoom"))
		if zoom <= 0 {
			zoom = 1
		}
		p := NSPoint{e.X * zoom, e.Y * zoom}
		if !sendBool(w.web, "isFlipped") {
			p.Y = msgRect(w.web, sel("bounds")).Size.Height - p.Y
		}
		inWindow := msgPointFromView(w.web, sel("convertPoint:toView:"), p, 0)
		typ, method, pressure := uint(nsMouseMoved), "mouseMoved:", float32(0)
		switch e.Kind {
		case platform.PageMouseDown:
			typ, method, pressure = nsLeftMouseDown, "mouseDown:", 1
			// A click puts the keyboard in the view it clicks, which
			// the window does before it sends the press.
			if !w.hasKeyboard() {
				send(w.win, "makeFirstResponder:", uintptr(w.web))
			}
		case platform.PageMouseUp:
			typ, method = nsLeftMouseUp, "mouseUp:"
		}
		ev := msgMouseEvent(class("NSEvent"), sel("mouseEventWithType:location:modifierFlags:timestamp:windowNumber:context:eventNumber:clickCount:pressure:"),
			typ, inWindow, eventFlags(e.Mods), eventTime(), sendInt(w.win, "windowNumber"), 0, 0, e.ClickCount, pressure)
		if e.Kind != platform.PageMouseMove {
			send(w.web, method, uintptr(ev))
			return
		}
		// AppKit delivers a mouse move to the owner of the tracking area
		// that holds the pointer, not to the view that has the area: the
		// view's own mouseMoved: is not where WebKit listens. NSTrackingArea
		// documents owner as that recipient, so the move goes to the owners
		// of the web view's areas, as it would for the user's pointer.
		areas := send(w.web, "trackingAreas")
		for i := range sendInt(areas, "count") {
			owner := send(send(areas, "objectAtIndex:", uintptr(i)), "owner")
			if owner != 0 && owner != w.web && respondsTo(owner, method) {
				send(owner, method, uintptr(ev))
			}
		}
	})
	done(nil)
}

func (w *window) SendKey(e platform.PageKeyEvent, done func(error)) {
	if err := w.pageView(); err != nil {
		done(err)
		return
	}
	k := e.Key
	flags := eventFlags(e.Mods)
	chars, plain := k.Chars, k.Chars
	if len(k.Name) == 1 && k.Name[0] >= 'a' && k.Name[0] <= 'z' {
		if e.Mods&platform.ModShift != 0 {
			chars, plain = strings.ToUpper(chars), strings.ToUpper(plain)
		}
		if e.Mods&platform.ModCtrl != 0 {
			chars = string(rune(k.Name[0] & 0x1f))
		}
	}
	switch k.Name {
	case "Delete", "Home", "End", "PageUp", "PageDown":
		flags |= nsFunctionMask
	case "ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown":
		flags |= nsFunctionMask | nsNumericPad
	}
	withPool(func() {
		number := sendInt(w.win, "windowNumber")
		key := func(typ uint, method string) {
			ev := msgKeyEvent(class("NSEvent"), sel("keyEventWithType:location:modifierFlags:timestamp:windowNumber:context:characters:charactersIgnoringModifiers:isARepeat:keyCode:"),
				typ, NSPoint{}, flags, eventTime(), number, 0, nsString(chars), nsString(plain), false, k.Mac)
			send(w.web, method, uintptr(ev))
		}
		if e.Kind != platform.PageKeyUp {
			key(nsKeyDown, "keyDown:")
		}
		if e.Kind != platform.PageKeyDown {
			key(nsKeyUp, "keyUp:")
		}
	})
	done(nil)
}

func (w *window) InsertText(text string, done func(error)) {
	if err := w.pageView(); err != nil {
		done(err)
		return
	}
	if !respondsTo(w.web, "insertText:replacementRange:") {
		done(errors.New("mygo: this version of WebKit does not take text from an input method"))
		return
	}
	withPool(func() {
		// An NSRange is two words: the location NSNotFound, and a length.
		send(w.web, "insertText:replacementRange:", uintptr(nsString(text)), uintptr(nsNotFound), 0)
	})
	done(nil)
}
