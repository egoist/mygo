package mygo

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync/atomic"

	"github.com/egoist/mygo/internal/platform"
)

// Modifiers are the modifier keys held while an input event happens.
type Modifiers uint8

// The modifier keys; combine them with |.
const (
	ModShift = Modifiers(platform.ModShift)
	ModCtrl  = Modifiers(platform.ModCtrl)
	ModAlt   = Modifiers(platform.ModAlt)
	// ModSuper is Command on macOS and the Windows key on Windows.
	ModSuper = Modifiers(platform.ModSuper)
)

// MouseEventType is what a MouseEvent does.
type MouseEventType uint8

// The things a mouse event does.
const (
	// MouseMove moves the pointer to the point.
	MouseMove MouseEventType = iota
	// MouseDown presses the primary button at the point.
	MouseDown
	// MouseUp releases it.
	MouseUp
)

// MouseEvent is the pointer and its primary button acting on a page.
type MouseEvent struct {
	Type MouseEventType
	// X and Y are in CSS pixels from the top-left corner of the page's
	// viewport, where getBoundingClientRect puts the elements.
	X, Y float64
	// ClickCount is how many presses in a row this one is: 1, the
	// default, for a click, 2 for the second press of a double click, as
	// the user's presses count. MouseMove ignores it.
	ClickCount int
	Modifiers  Modifiers
}

// KeyEventType is what a KeyEvent does.
type KeyEventType uint8

// The things a key event does.
const (
	// KeyPress presses the key and releases it: the page gets keydown,
	// keypress for a key that types a character, and keyup.
	KeyPress KeyEventType = iota
	// KeyDown presses the key and holds it.
	KeyDown
	// KeyUp releases it.
	KeyUp
)

// KeyEvent is a key of the keyboard acting on a page.
type KeyEvent struct {
	Type KeyEventType
	// Key is the key's KeyboardEvent.key value, as of a US keyboard:
	// "Enter", "Tab", "Escape", "Backspace", "Delete", "ArrowLeft",
	// "ArrowRight", "ArrowUp", "ArrowDown", "Home", "End", "PageUp",
	// "PageDown", " ", or a letter or a digit. A capital letter is the
	// letter with Shift held. To type other text, use Page.InsertText.
	Key       string
	Modifiers Modifiers
}

var (
	errMouseType  = errors.New("mygo: SendMouse: unknown event type")
	errMousePoint = errors.New("mygo: SendMouse: the point is not a finite number")
	errKeyType    = errors.New("mygo: SendKey: unknown event type")
)

// SendMouse delivers a mouse event to the page as the system delivers the
// mouse's: the page gets mousedown, mouseup, click and the other events
// that follow, with isTrusted true, which a script's dispatchEvent never
// has. A click is a MouseMove, a MouseDown and a MouseUp at one point; a
// double click is a click and a second one with ClickCount 2:
//
//	for _, t := range []mygo.MouseEventType{mygo.MouseMove, mygo.MouseDown, mygo.MouseUp} {
//		err := page.SendMouse(mygo.MouseEvent{Type: t, X: 220, Y: 172})
//	}
//
// Input needs no key window and no active app: the page gets the event
// where the point is. X and Y stay CSS pixels at any page zoom. SendMouse
// returns when the page took the event, not when the page handled it. The
// platforms differ in what they hold back:
//
//   - WebKit queues mouse events while one is in flight and takes keys at
//     once, so wait for what a click did (the focus it gave) before
//     SendKey or InsertText.
//   - On macOS a MouseDown gives the keyboard to the web view, as a click
//     does, and WebKit drops a MouseMove at a point that the window's
//     content paints over the web view.
//   - On macOS a page that is not shown takes input: a window that is
//     hidden, a web view the window's view does not build, one a box
//     covers. A web view that was never shown has no size, and hears no
//     mouse event, though it takes keys and text.
//   - On Windows a page that is not shown takes input too: a window that
//     is hidden or minimized, a web view that the window's view does not
//     build or never built, one a box covers. Such a page draws no
//     frames, and Chromium answers a mouse move only when it draws one, or
//     after five seconds, so SendMouse returns for a MouseMove when it
//     sent it, not when the page took it: the page gets it before the next
//     event.
//   - On Linux the web view must be shown, not in a hidden window: WebKitGTK
//     drops the presses of one that is not, so SendMouse returns an error.
//
// In browsers it returns an error matching errors.ErrUnsupported.
func (p *Page) SendMouse(e MouseEvent) error {
	if p.w.content != nil {
		return errNoPage
	}
	if e.Type > MouseUp {
		return errMouseType
	}
	if math.IsNaN(e.X) || math.IsNaN(e.Y) || math.IsInf(e.X, 0) || math.IsInf(e.Y, 0) {
		return errMousePoint
	}
	if e.ClickCount <= 0 {
		e.ClickCount = 1
	}
	pe := platform.PageMouseEvent{Kind: platform.PageMouseKind(e.Type), X: e.X, Y: e.Y, ClickCount: e.ClickCount, Mods: platform.Modifiers(e.Modifiers)}
	return p.input(func(n platform.PageInput, done func(error)) { n.SendMouse(pe, done) })
}

// SendKey delivers a key event to the page as the system delivers the
// keyboard's: the page gets keydown, keypress and keyup with isTrusted true,
// at the element that has the focus, and the key does what it does in a
// browser: Enter in a text field starts a line, Tab moves the focus.
// Pressing Enter to send a message:
//
//	err := page.SendKey(mygo.KeyEvent{Key: "Enter"})
//
// Like SendMouse it returns when the page took the event, not when the page
// handled it, needs no key window and, except on Linux, no shown window (see
// SendMouse for the pages that are not shown), and gives an error for a key
// it does not know. The keys are those of a US keyboard, with no input
// method composing, and a shortcut with ModSuper is not reliable: macOS
// answers it as a key equivalent, not as a key event. In browsers it returns
// an error matching errors.ErrUnsupported.
func (p *Page) SendKey(e KeyEvent) error {
	if p.w.content != nil {
		return errNoPage
	}
	if e.Type > KeyUp {
		return errKeyType
	}
	k, shift, ok := platform.PageKeyByName(e.Key)
	if !ok {
		return fmt.Errorf("mygo: SendKey: unknown key %q", e.Key)
	}
	mods := platform.Modifiers(e.Modifiers)
	if shift {
		mods |= platform.ModShift
	}
	pe := platform.PageKeyEvent{Kind: platform.PageKeyKind(e.Type), Key: k, Mods: mods}
	return p.input(func(n platform.PageInput, done func(error)) { n.SendKey(pe, done) })
}

// InsertText inserts text at the caret of the element that has the focus,
// as an input method does when it commits what it composed: the element's
// beforeinput and input events have the inputType "insertText", with
// isTrusted true, and an editor in the page takes it as typed. The text goes
// in as one edit, and newlines in it are not Enter keys. An element that does
// not take text ignores it. It returns when the page took the text, not when
// the page handled it, and needs no key window or shown window. In browsers
// it returns an error matching errors.ErrUnsupported. A page that moves the
// focus by itself can put the text in another element than the one the
// caller checked: see InsertTextInto.
func (p *Page) InsertText(text string) error {
	if p.w.content != nil {
		return errNoPage
	}
	return p.input(func(n platform.PageInput, done func(error)) { n.InsertText(text, done) })
}

// ErrWrongFocus is what InsertTextInto returns, with nothing inserted, when
// the element that would take the text is not the one it names.
var ErrWrongFocus = errors.New("mygo: InsertTextInto: the focus is not on the element it names")

var (
	errInsertEmpty    = errors.New("mygo: InsertTextInto: no text to insert")
	errInsertFrame    = errors.New("mygo: InsertTextInto: the focus is in a frame, whose input the page's window does not see")
	errInsertNothing  = errors.New("mygo: InsertTextInto: the page took no text: no element that takes text has the focus")
	errInsertNavigate = errors.New("mygo: InsertTextInto: the page changed before the insert could be checked")
)

// insertGuards numbers the guards InsertTextInto puts in pages.
var insertGuards atomic.Uint64

// insertGuardArm puts a guard in the page: a capturing beforeinput listener
// on the window, which runs inside the page's own task of inserting the
// text, before the text goes in, so that a page that moves the focus
// between the caller's check and the insert cannot slip past it. It refuses
// the trusted insert events of the text, the way a page cancels them, and
// stops them reaching the page's own handlers (which a model that handles
// beforeinput itself would take as the text); it lets through those whose
// target, or an ancestor of it across shadow roots, matches the selector.
// The format's arguments are the selector, the text and the guard's name,
// as JSON.
const insertGuardArm = `(() => {
  const sel = %s, text = %s, key = %s;
  try { document.createDocumentFragment().querySelector(sel); } catch (e) { return {state: "selector", message: String(e && e.message || e)}; }
  let f = document.activeElement;
  while (f && f.shadowRoot && f.shadowRoot.activeElement) f = f.shadowRoot.activeElement;
  if (f && (f.localName === "iframe" || f.localName === "frame")) return {state: "frame"};
  const g = {};
  const on = (e) => {
    if (!e.isTrusted || !/^insert(Text|LineBreak|Paragraph)$/.test(e.inputType) || (e.inputType === "insertText" && !text.includes(e.data))) return;
    const path = e.composedPath(), t = path[0] instanceof Element ? path[0] : e.target;
    for (let n = t; n; n = n.parentNode || n.host) {
      if (n.nodeType === 1 && n.matches(sel)) { g.state = g.state || "inserted"; return; }
    }
    e.preventDefault();
    e.stopImmediatePropagation();
    g.state = "refused";
    g.target = t instanceof Element ? (t.localName + (t.id ? "#" + t.id : "")).slice(0, 80) : "";
  };
  addEventListener("beforeinput", on, true);
  window[key] = () => { removeEventListener("beforeinput", on, true); delete window[key]; return g; };
  return {state: "armed"};
})()`

// insertGuardCollect takes the guard of a name out of the page and returns
// what it decided.
const insertGuardCollect = `(() => { const f = window[%s]; return f ? f() : {state: "lost"}; })()`

type insertGuardResult struct {
	State   string `json:"state"`
	Message string `json:"message"`
	Target  string `json:"target"`
}

// InsertTextInto is InsertText that inserts nothing unless the element that
// takes the text matches selector, a CSS selector: the focused text field or
// editing host, or an element it is inside, shadow roots included. It is for
// a page that moves the focus by itself, where checking the focus and then
// calling InsertText may put the text in another field: the check is made
// inside the page, in the task that inserts the text, after the focus is
// final, so no focus change can come between the check and the insert. When
// the element does not match, the text does not reach the page at all, not
// even its handlers of beforeinput, and InsertTextInto returns an error that
// matches ErrWrongFocus and says what had the focus:
//
//	err := page.InsertTextInto("[data-composer]", "Xin chào")
//	if errors.Is(err, mygo.ErrWrongFocus) {
//		// the page moved the focus: click the composer again
//	}
//
// It returns an error, and the text may have gone in, when no check could be
// made: when the page did not tell that it took the text, which is also what
// a focus on an element that takes no text looks like (nothing went in then),
// or when its first document went away. The guard is a listener of the top
// document's window: a page that stops its events before MyGo's listener
// runs (a capturing listener of the window registered earlier) hides them,
// and a focus in a frame is refused before the insert, as its events do not
// reach the window. Call it one at a time for a page. Like InsertText it
// returns when the page took the text, and in browsers it returns an error.
func (p *Page) InsertTextInto(selector, text string) error {
	if p.w.content != nil {
		return errNoPage
	}
	if text == "" {
		return errInsertEmpty
	}
	args := make([]any, 3)
	key := fmt.Sprintf("__mygoInsert%d", insertGuards.Add(1))
	for i, v := range []string{selector, text, key} {
		b, _ := json.Marshal(v)
		args[i] = string(b)
	}
	armed, err := EvalAs[insertGuardResult](p, fmt.Sprintf(insertGuardArm, args...))
	if err != nil {
		return err
	}
	switch armed.State {
	case "armed":
	case "selector":
		return fmt.Errorf("mygo: InsertTextInto: invalid selector %q: %s", selector, armed.Message)
	case "frame":
		return errInsertFrame
	default:
		return fmt.Errorf("mygo: InsertTextInto: unexpected guard state %q", armed.State)
	}
	insertErr := p.InsertText(text)
	// The guard is read through the connection the text went in by, so that
	// the page ran the insert first.
	res, err := EvalAs[insertGuardResult](p, fmt.Sprintf(insertGuardCollect, args[2]))
	if insertErr != nil {
		return insertErr
	}
	if err != nil {
		return err
	}
	switch res.State {
	case "inserted":
		return nil
	case "refused":
		if res.Target != "" {
			return fmt.Errorf("%w (it is on %s)", ErrWrongFocus, res.Target)
		}
		return ErrWrongFocus
	case "lost":
		return errInsertNavigate
	default:
		return errInsertNothing
	}
}

// input runs send on the main thread, with the page's backend if it takes
// input, and waits for its done.
func (p *Page) input(send func(n platform.PageInput, done func(error))) error {
	ch := make(chan error, 1)
	ran := false
	p.w.do(func(n platform.Window) {
		ran = true
		in, ok := pageInput(n)
		if !ok {
			deliver(ch, platform.ErrUnsupported)
			return
		}
		send(in, func(err error) { deliver(ch, err) })
	})
	if !ran {
		return errDestroyed
	}
	return await(ch)
}

// pageInput returns what takes the input of the page of a window or a web
// view, if its backend has it.
func pageInput(n platform.Window) (platform.PageInput, bool) {
	if v, ok := n.(*webViewNative); ok {
		in, ok := v.web.(platform.PageInput)
		return in, ok
	}
	in, ok := n.(platform.PageInput)
	return in, ok
}
