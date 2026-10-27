//go:build windows && (amd64 || arm64)

package windows

import (
	"encoding/json"
	"strings"

	"github.com/egoist/mygo/internal/platform"
)

// Input for a page goes through the DevTools protocol's Input domain, which
// feeds Chromium's input pipeline as the system does: the page's events are
// trusted, and it needs neither the window's focus nor a visible window.
//
// A page that is not visible (a web view the content does not show, a
// minimized window) answers a mouse move only when it draws a frame, which
// it does not: Chromium holds the move back until another event of the page
// flushes it, or five seconds go by. Every other event is answered at once.
// So SendMouse does not wait for the answer to a move, and returns when the
// call is made: the move still reaches the page before the next event.

// Page.SendMouse finds PageInput at run time: this has the compiler check it.
var _ platform.PageInput = (*window)(nil)

// cdpModifiers returns the DevTools protocol's modifier bits: Alt 1,
// Control 2, Meta 4, Shift 8.
func cdpModifiers(m platform.Modifiers) int {
	bits := 0
	if m&platform.ModAlt != 0 {
		bits |= 1
	}
	if m&platform.ModCtrl != 0 {
		bits |= 2
	}
	if m&platform.ModSuper != 0 {
		bits |= 4
	}
	if m&platform.ModShift != 0 {
		bits |= 8
	}
	return bits
}

// input calls a method of the Input domain and reports its end to done.
func (w *window) input(method string, params map[string]any, done func(error)) {
	body, _ := json.Marshal(params)
	w.withWebViewOr(func() {
		w.devtools(method, string(body), func(_ string, err error) { done(err) })
	}, done)
}

// inputQueued calls a method of the Input domain and reports to done as
// soon as the call is made, without waiting for the page's answer.
func (w *window) inputQueued(method string, params map[string]any, done func(error)) {
	body, _ := json.Marshal(params)
	w.withWebViewOr(func() {
		w.devtools(method, string(body), nil)
		done(nil)
	}, done)
}

func (w *window) SendMouse(e platform.PageMouseEvent, done func(error)) {
	// The protocol takes the point in CSS pixels of the viewport, whatever
	// the web view's ZoomFactor is.
	params := map[string]any{"x": e.X, "y": e.Y, "modifiers": cdpModifiers(e.Mods)}
	switch e.Kind {
	case platform.PageMouseMove:
		params["type"], params["button"], params["buttons"] = "mouseMoved", "none", 0
		w.inputQueued("Input.dispatchMouseEvent", params, done)
		return
	case platform.PageMouseDown:
		params["type"], params["button"], params["buttons"], params["clickCount"] = "mousePressed", "left", 1, e.ClickCount
	case platform.PageMouseUp:
		params["type"], params["button"], params["buttons"], params["clickCount"] = "mouseReleased", "left", 0, e.ClickCount
	}
	w.input("Input.dispatchMouseEvent", params, done)
}

func (w *window) SendKey(e platform.PageKeyEvent, done func(error)) {
	k := e.Key
	text := k.Text
	name := k.Name
	if e.Mods&platform.ModShift != 0 && len(name) == 1 && name[0] >= 'a' && name[0] <= 'z' {
		name, text = strings.ToUpper(name), strings.ToUpper(text)
	}
	// A shortcut types nothing.
	if e.Mods&(platform.ModCtrl|platform.ModAlt|platform.ModSuper) != 0 {
		text = ""
	}
	event := func(typ string) map[string]any {
		p := map[string]any{
			"type": typ, "key": name, "code": k.Code, "modifiers": cdpModifiers(e.Mods),
			"windowsVirtualKeyCode": k.Win, "nativeVirtualKeyCode": k.Win,
		}
		if typ == "keyDown" {
			p["text"], p["unmodifiedText"] = text, k.Text
		}
		return p
	}
	// keyDown makes the page's keypress for a key with text; rawKeyDown
	// is a key down that does not.
	down := "rawKeyDown"
	if text != "" {
		down = "keyDown"
	}
	switch e.Kind {
	case platform.PageKeyDown:
		w.input("Input.dispatchKeyEvent", event(down), done)
	case platform.PageKeyUp:
		w.input("Input.dispatchKeyEvent", event("keyUp"), done)
	case platform.PageKeyPress:
		w.input("Input.dispatchKeyEvent", event(down), func(err error) {
			if err != nil {
				done(err)
				return
			}
			w.input("Input.dispatchKeyEvent", event("keyUp"), done)
		})
	}
}

func (w *window) InsertText(text string, done func(error)) {
	w.input("Input.insertText", map[string]any{"text": text}, done)
}
