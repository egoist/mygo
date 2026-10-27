//go:build !js

package e2e

import (
	"fmt"
	"math"
	"runtime"
	"strings"
	"testing"

	"github.com/egoist/mygo"
)

// inputFixture is a page that distrusts scripts, as the web apps that
// motivate Page.SendMouse, SendKey and InsertText do: every handler
// rejects an event whose isTrusted is false, and a window-level listener
// records what the engine says of each event it hears.
//
//   - ce is a contenteditable whose model is rebuilt from DOM mutations.
//   - be is a contenteditable whose model changes only through beforeinput,
//     which its handler cancels (the way Lexical and Slate work).
//   - ta is a textarea whose model changes only through input.
//   - Enter sends a composer's model as a message, from a trusted keydown.
//   - send needs a click, and row1 and row2 a mousedown and a mouseup.
const inputFixture = `<!doctype html><meta charset="utf-8">
<style>
  html, body { margin: 0; font: 14px system-ui, sans-serif; }
  .box { position: absolute; box-sizing: border-box; border: 1px solid #667; padding: 6px; background: #fff; overflow: hidden; width: 300px; height: 44px; left: 20px; }
  #ce { top: 20px; } #be { top: 80px; } #ta { top: 140px; resize: none; font: inherit; }
  #send { top: 200px; width: 90px; } #row1 { top: 20px; left: 340px; } #row2 { top: 80px; left: 340px; }
  #thread { top: 260px; height: 80px; }
</style>
<div id="ce" class="box" contenteditable="true"></div>
<div id="be" class="box" contenteditable="true"></div>
<textarea id="ta" class="box"></textarea>
<div id="send" class="box">Send</div>
<div id="row1" class="box">row 1</div>
<div id="row2" class="box">row 2</div>
<div id="thread" class="box"></div>
<script>
const $ = (id) => document.getElementById(id);
const composers = ['ce', 'be', 'ta'];
const models = { ce: '', be: '', ta: '' };
const R = window.R = { events: [], acts: [], selected: '', trusted: 0, untrusted: 0, moves: 0 };
for (const type of ['keydown', 'keypress', 'keyup', 'beforeinput', 'input', 'mousedown', 'mouseup', 'click', 'pointerdown']) {
  window.addEventListener(type, (e) => {
    if (e.isTrusted) R.trusted++; else R.untrusted++;
    R.events.push(e.type + ':' + e.isTrusted);
  }, true);
}
window.addEventListener('mousemove', (e) => { if (e.isTrusted) R.moves++; }, true);
const act = (what) => R.acts.push(what);
new MutationObserver(() => { models.ce = $('ce').textContent; })
  .observe($('ce'), { childList: true, characterData: true, subtree: true });
$('be').addEventListener('beforeinput', (e) => {
  if (!e.isTrusted) return;
  e.preventDefault();
  if (e.inputType === 'insertText') models.be += e.data;
  else if (e.inputType === 'deleteContentBackward') models.be = models.be.slice(0, -1);
  else return;
  $('be').textContent = models.be;
  const r = document.createRange(); r.selectNodeContents($('be')); r.collapse(false);
  getSelection().removeAllRanges(); getSelection().addRange(r);
});
$('ta').addEventListener('input', (e) => { if (e.isTrusted) models.ta = $('ta').value; });
function send(who) {
  if (!models[who]) return;
  const m = document.createElement('div');
  m.textContent = who + ':' + models[who];
  $('thread').appendChild(m);
  models[who] = '';
  if (who === 'ta') $('ta').value = ''; else $(who).textContent = '';
}
for (const who of composers) {
  $(who).addEventListener('keydown', (e) => {
    if (e.key !== 'Enter' || e.shiftKey || !e.isTrusted) return;
    e.preventDefault(); act(who + ':enter'); send(who);
  });
}
$('send').addEventListener('click', (e) => { if (e.isTrusted) act('send:click'); });
for (const id of ['row1', 'row2']) {
  let down = false;
  $(id).addEventListener('mousedown', (e) => { down = e.isTrusted; });
  $(id).addEventListener('mouseup', (e) => { if (down && e.isTrusted) R.selected = id; down = false; });
}
R.center = (id) => {
  const r = $(id).getBoundingClientRect(), x = r.left + r.width / 2, y = r.top + r.height / 2;
  return { x, y, hit: $(id).contains(document.elementFromPoint(x, y)) };
};
// word is the center of the first word of an element's text.
R.word = (id) => {
  const n = $(id).firstChild, r = document.createRange();
  r.setStart(n, 0); r.setEnd(n, n.data.indexOf(' '));
  const b = r.getBoundingClientRect();
  return { x: b.left + b.width / 2, y: b.top + b.height / 2 };
};
R.state = () => ({
  active: document.activeElement.id || document.activeElement.nodeName.toLowerCase(),
  dom: { ce: $('ce').textContent, be: $('be').textContent, ta: $('ta').value },
  model: { ...models },
  thread: [...$('thread').children].map((m) => m.textContent),
  selected: R.selected, acts: R.acts, trusted: R.trusted, untrusted: R.untrusted, moves: R.moves,
  selection: String(getSelection()), events: R.events,
});
R.reset = () => {
  R.events = []; R.acts = []; R.selected = ''; R.trusted = R.untrusted = R.moves = 0;
  for (const k of composers) models[k] = '';
  $('ce').textContent = $('be').textContent = ''; $('ta').value = ''; $('thread').textContent = '';
  document.activeElement.blur(); getSelection().removeAllRanges();
};
R.ready = true;
</script>`

type inputState struct {
	Active    string            `json:"active"`
	DOM       map[string]string `json:"dom"`
	Model     map[string]string `json:"model"`
	Thread    []string          `json:"thread"`
	Selected  string            `json:"selected"`
	Acts      []string          `json:"acts"`
	Trusted   int               `json:"trusted"`
	Untrusted int               `json:"untrusted"`
	Moves     int               `json:"moves"`
	Selection string            `json:"selection"`
	Events    []string          `json:"events"`
}

type inputPoint struct {
	X   float64 `json:"x"`
	Y   float64 `json:"y"`
	Hit bool    `json:"hit"`
}

// inputPage drives the fixture of a window as an app does.
type inputPage struct {
	t *testing.T
	w *mygo.Window
	p *mygo.Page
}

func newInputPage(t *testing.T, w *mygo.Window) *inputPage {
	ip := newInputPageOf(t, w.Page())
	ip.w = w
	return ip
}

// newInputPageOf loads the fixture into a page, as a web view's.
func newInputPageOf(t *testing.T, p *mygo.Page) *inputPage {
	p.LoadHTML(inputFixture, "")
	waitForPage(t, p, "window.R && R.ready")
	return &inputPage{t: t, p: p}
}

func (ip *inputPage) eval(code string) {
	ip.t.Helper()
	if _, err := ip.p.Eval(code); err != nil {
		ip.t.Fatal(err)
	}
}

func (ip *inputPage) state() inputState {
	ip.t.Helper()
	s, err := mygo.EvalAs[inputState](ip.p, "R.state()")
	if err != nil {
		ip.t.Fatal(err)
	}
	return s
}

// wait waits for a condition on the page.
func (ip *inputPage) wait(expr string) {
	ip.t.Helper()
	waitForPage(ip.t, ip.p, expr)
}

func (ip *inputPage) point(call string) inputPoint {
	ip.t.Helper()
	pt, err := mygo.EvalAs[inputPoint](ip.p, call)
	if err != nil {
		ip.t.Fatal(err)
	}
	return pt
}

// center returns the center of an element, which must be what the page
// shows there.
func (ip *inputPage) center(id string) inputPoint {
	ip.t.Helper()
	pt := ip.point(fmt.Sprintf("R.center(%q)", id))
	if !pt.Hit {
		ip.t.Fatalf("%s is not under its own center", id)
	}
	return pt
}

// click moves the pointer to the point and presses and releases the button.
func (ip *inputPage) click(x, y float64, count int) {
	ip.t.Helper()
	for _, typ := range []mygo.MouseEventType{mygo.MouseMove, mygo.MouseDown, mygo.MouseUp} {
		if err := ip.p.SendMouse(mygo.MouseEvent{Type: typ, X: x, Y: y, ClickCount: count}); err != nil {
			ip.t.Fatal(err)
		}
	}
}

func (ip *inputPage) key(key string, mods mygo.Modifiers) {
	ip.t.Helper()
	if err := ip.p.SendKey(mygo.KeyEvent{Key: key, Modifiers: mods}); err != nil {
		ip.t.Fatal(err)
	}
}

// reply is what an app does to answer in a composer: click it, wait for
// its focus (a click does not wait for its own effect), insert the text,
// read it back, press Enter and wait for the message.
func (ip *inputPage) reply(who, text string) {
	ip.t.Helper()
	ip.eval("R.reset()")
	c := ip.center(who)
	ip.click(c.X, c.Y, 1)
	ip.wait(fmt.Sprintf("document.activeElement.id === %q", who))
	if err := ip.p.InsertText(text); err != nil {
		ip.t.Fatal(err)
	}
	ip.wait(fmt.Sprintf("R.state().model.%[1]s === %[2]q && R.state().dom.%[1]s === %[2]q", who, text))
	ip.key("Enter", 0)
	ip.wait(fmt.Sprintf("R.state().thread.includes(%q)", who+":"+text))
	if s := ip.state(); s.Untrusted != 0 {
		ip.t.Errorf("the page heard events that were not trusted: %v", s.Events)
	}
}

// Page.SendMouse, SendKey and InsertText reach the page as the system's
// input: every event it hears has isTrusted true, which a script's
// dispatchEvent never gives, so a page that rejects the others takes them.
func TestPageInput(t *testing.T) {
	const vi = "Xin chào, cảm ơn bạn ❤ 🙂"
	w := newWindow(t, mygo.WindowOptions{Title: "Page input", Width: 520, Height: 400})
	ip := newInputPage(t, w)

	t.Run("reply", func(t *testing.T) {
		ip.t = t
		for _, who := range []string{"ce", "be", "ta"} {
			ip.reply(who, vi)
		}
	})

	// The control: the page turns down what a script makes.
	t.Run("scripts", func(t *testing.T) {
		ip.t = t
		ip.eval("R.reset(); document.getElementById('ce').focus()")
		ip.eval(`document.getElementById('send').dispatchEvent(new MouseEvent('click', { bubbles: true }));
			document.execCommand('insertText', false, 'by script');
			document.getElementById('ce').dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))`)
		ip.wait("R.untrusted >= 2")
		if s := ip.state(); len(s.Acts) != 0 || len(s.Thread) != 0 {
			t.Errorf("the page took what a script made: %+v", s)
		}
	})

	t.Run("click", func(t *testing.T) {
		ip.t = t
		ip.eval("R.reset()")
		c := ip.center("send")
		ip.click(c.X, c.Y, 1)
		ip.wait("R.acts.includes('send:click')")
		// A row that needs the press and the release.
		c = ip.center("row2")
		ip.click(c.X, c.Y, 1)
		ip.wait("R.selected === 'row2'")
		if s := ip.state(); s.Untrusted != 0 {
			t.Errorf("the page heard events that were not trusted: %v", s.Events)
		}
	})

	t.Run("move", func(t *testing.T) {
		ip.t = t
		ip.eval("R.reset()")
		c := ip.center("row1")
		if err := ip.p.SendMouse(mygo.MouseEvent{Type: mygo.MouseMove, X: c.X, Y: c.Y}); err != nil {
			t.Fatal(err)
		}
		ip.wait("R.moves > 0")
	})

	t.Run("keys", func(t *testing.T) {
		ip.t = t
		ip.eval("R.reset()")
		c := ip.center("ta")
		ip.click(c.X, c.Y, 1)
		ip.wait("document.activeElement.id === 'ta'")
		ip.key("h", 0)
		ip.key("i", 0)
		ip.key("H", 0)
		ip.wait("R.state().model.ta === 'hiH'")
		ip.key("Backspace", 0)
		ip.wait("R.state().model.ta === 'hi'")
		// Shift+Enter starts a line and sends nothing.
		ip.key("Enter", mygo.ModShift)
		ip.wait("R.state().model.ta === 'hi\\n'")
		if s := ip.state(); len(s.Acts) != 0 {
			t.Errorf("Shift+Enter did something: %v", s.Acts)
		}
		ip.key("Tab", 0)
		ip.wait("document.activeElement.id !== 'ta'")
	})

	t.Run("double click", func(t *testing.T) {
		ip.t = t
		ip.eval("R.reset()")
		c := ip.center("ce")
		ip.click(c.X, c.Y, 1)
		ip.wait("document.activeElement.id === 'ce'")
		if err := ip.p.InsertText("hello world"); err != nil {
			t.Fatal(err)
		}
		ip.wait("R.state().dom.ce === 'hello world'")
		pt := ip.point("R.word('ce')")
		ip.click(pt.X, pt.Y, 1)
		ip.click(pt.X, pt.Y, 2)
		ip.wait("R.state().selection.trim() === 'hello'")
	})

	t.Run("zoom", func(t *testing.T) {
		ip.t = t
		w.Page().SetZoomFactor(1.5)
		t.Cleanup(func() { w.Page().SetZoomFactor(1) })
		ip.eval("R.reset()")
		ip.wait("devicePixelRatio > 0")
		// The point stays in CSS pixels, which the zoom made larger.
		c := ip.center("ta")
		ip.click(c.X, c.Y, 1)
		ip.wait("document.activeElement.id === 'ta'")
	})

	t.Run("errors", func(t *testing.T) {
		ip.t = t
		ip.eval("R.reset()")
		for _, e := range []mygo.KeyEvent{{Key: "F13"}, {Key: ""}, {Type: 9, Key: "a"}} {
			if err := ip.p.SendKey(e); err == nil {
				t.Errorf("SendKey(%+v) succeeded", e)
			}
		}
		if err := ip.p.SendMouse(mygo.MouseEvent{Type: mygo.MouseDown, X: 1, Y: math.Inf(1)}); err == nil {
			t.Error("SendMouse at infinity succeeded")
		}
		if s := ip.state(); len(s.Events) != 0 {
			t.Errorf("refused input reached the page: %v", s.Events)
		}
		// With nothing focused, text goes nowhere, without an error.
		if err := ip.p.InsertText("x"); err != nil {
			t.Error(err)
		}
	})

	// The page takes input in a window that is not the one that has the
	// keyboard, and in one that is hidden.
	t.Run("without the keyboard", func(t *testing.T) {
		ip.t = t
		other := newWindow(t, mygo.WindowOptions{Title: "Other", Width: 200, Height: 100})
		other.Focus()
		ip.reply("ce", "no key window")
	})

	t.Run("hidden", func(t *testing.T) {
		ip.t = t
		c := ip.center("be")
		w.Hide()
		t.Cleanup(w.Show)
		if runtime.GOOS == "linux" {
			// WebKitGTK drops the presses of a web view that is not
			// mapped, so SendMouse and SendKey say so; it still takes text.
			if err := ip.p.SendMouse(mygo.MouseEvent{Type: mygo.MouseDown, X: c.X, Y: c.Y}); err == nil {
				t.Error("SendMouse to a hidden web view succeeded")
			}
			if err := ip.p.SendKey(mygo.KeyEvent{Key: "a"}); err == nil {
				t.Error("SendKey to a hidden web view succeeded")
			}
			ip.eval("R.reset(); document.getElementById('be').focus()")
			if err := ip.p.InsertText("hidden window"); err != nil {
				t.Fatal(err)
			}
			ip.wait("R.state().model.be === 'hidden window'")
			return
		}
		ip.reply("be", "hidden window")
	})

	t.Run("destroyed", func(t *testing.T) {
		d := newWindow(t, mygo.WindowOptions{Title: "Gone", Width: 200, Height: 100})
		p := d.Page()
		d.Destroy()
		if err := p.SendKey(mygo.KeyEvent{Key: "Enter"}); err == nil || !strings.Contains(err.Error(), "destroyed") {
			t.Errorf("SendKey to a destroyed window: %v", err)
		}
	})
}
