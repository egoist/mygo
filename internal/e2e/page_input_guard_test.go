//go:build !js

package e2e

import (
	"errors"
	"fmt"
	"testing"

	"github.com/egoist/mygo"
)

// stealOnGuard makes the page move the focus to the element id the moment
// a script puts a beforeinput listener on its window: after the caller's
// own check of the focus, and before the text goes in, as a page that moves
// the focus on its own does.
func (ip *inputPage) stealOnGuard(id string) {
	ip.t.Helper()
	ip.eval(fmt.Sprintf(`{
		const add = EventTarget.prototype.addEventListener;
		EventTarget.prototype.addEventListener = function (type, ...rest) {
			if (type === 'beforeinput' && this === window) {
				EventTarget.prototype.addEventListener = add;
				document.getElementById(%q).focus();
			}
			return add.call(this, type, ...rest);
		};
	}`, id))
}

// focus gives an element of the fixture the focus with a click and waits
// for it.
func (ip *inputPage) focus(id string) {
	ip.t.Helper()
	ip.eval("R.reset()")
	c := ip.center(id)
	ip.click(c.X, c.Y, 1)
	ip.wait(fmt.Sprintf("document.activeElement.id === %q", id))
}

// Page.InsertTextInto closes the race of InsertText: a page that moves the
// focus between the caller's check and the insert gets the text in another
// field with InsertText, and nothing with InsertTextInto, which checks the
// focus inside the page's task that inserts the text.
func TestPageInsertTextInto(t *testing.T) {
	w := newWindow(t, mygo.WindowOptions{Title: "Insert text into", Width: 520, Height: 400})
	ip := newInputPage(t, w)

	// The control: check the focus, then insert, as an app does with
	// InsertText. The page moves the focus after the check.
	t.Run("InsertText races", func(t *testing.T) {
		ip.t = t
		ip.focus("ce")
		if id, err := mygo.EvalAs[string](ip.p, "document.activeElement.id"); err != nil || id != "ce" {
			t.Fatalf("the check saw %q (%v)", id, err)
		}
		ip.eval("document.getElementById('ta').focus()")
		if err := ip.p.InsertText("misplaced"); err != nil {
			t.Fatal(err)
		}
		ip.wait("R.state().dom.ta === 'misplaced'")
		if s := ip.state(); s.DOM["ce"] != "" {
			t.Errorf("the composer took the text: %+v", s.DOM)
		}
	})

	t.Run("matches", func(t *testing.T) {
		ip.t = t
		for _, c := range []struct{ id, selector, text string }{
			{"ce", "#ce", "xin chào"},
			{"be", ".box[contenteditable]", "Xin chào, cảm ơn bạn ❤ 🙂"},
			{"ta", "textarea", "dòng 1\ndòng 2\n\ndòng 4"},
			{"ce", "div:not(#be)", "hai dòng"},
			{"be", "html", "an ancestor"},
		} {
			ip.focus(c.id)
			if err := ip.p.InsertTextInto(c.selector, c.text); err != nil {
				t.Fatalf("%s: %v", c.selector, err)
			}
			ip.wait(fmt.Sprintf("R.state().model.%[1]s === %[2]q", c.id, c.text))
		}
	})

	t.Run("wrong focus", func(t *testing.T) {
		ip.t = t
		for _, c := range []struct{ from, to string }{{"ce", "be"}, {"be", "ta"}, {"ta", "ce"}} {
			ip.focus(c.from)
			ip.stealOnGuard(c.to)
			err := ip.p.InsertTextInto("#"+c.from, "misplaced")
			if !errors.Is(err, mygo.ErrWrongFocus) {
				t.Fatalf("from %s to %s: err = %v, want ErrWrongFocus", c.from, c.to, err)
			}
			ip.wait(fmt.Sprintf("document.activeElement.id === %q", c.to))
			// Nothing went in: not into the field the page moved to, and
			// not into a model that takes beforeinput by itself.
			s := ip.state()
			for who, text := range s.DOM {
				if text != "" || s.Model[who] != "" {
					t.Errorf("from %s to %s: %s holds %q (model %q)", c.from, c.to, who, text, s.Model[who])
				}
			}
		}
	})

	t.Run("no element takes text", func(t *testing.T) {
		ip.t = t
		ip.focus("ce")
		ip.eval("document.activeElement.blur(); getSelection().removeAllRanges()")
		err := ip.p.InsertTextInto("#ce", "x")
		if err == nil || errors.Is(err, mygo.ErrWrongFocus) {
			t.Errorf("err = %v, want an error that is not ErrWrongFocus", err)
		}
		if s := ip.state(); s.DOM["ce"] != "" {
			t.Errorf("the composer took the text: %+v", s.DOM)
		}
	})

	t.Run("errors", func(t *testing.T) {
		ip.t = t
		ip.focus("ce")
		if err := ip.p.InsertTextInto("[", "x"); err == nil {
			t.Error("a selector that is not one worked")
		}
		if err := ip.p.InsertTextInto("#ce", ""); err == nil {
			t.Error("no text worked")
		}
		if n, err := mygo.EvalAs[int](ip.p, "Object.keys(window).filter((k) => k.startsWith('__mygoInsert')).length"); err != nil || n != 0 {
			t.Errorf("guards left in the page: %d (%v)", n, err)
		}
		if s := ip.state(); s.DOM["ce"] != "" {
			t.Errorf("the composer took the text: %+v", s.DOM)
		}
	})

	// The element may be inside a shadow root, which the selector then
	// names by its host.
	t.Run("shadow root", func(t *testing.T) {
		ip.t = t
		ip.eval(`{
			const host = document.createElement('div');
			host.id = 'host';
			host.style.cssText = 'position:absolute;left:20px;top:360px;width:300px;height:30px';
			const root = host.attachShadow({ mode: 'open' });
			root.innerHTML = '<textarea id="inner" style="width:290px;height:24px"></textarea>';
			document.body.appendChild(host);
			window.inner = root.getElementById('inner');
		}`)
		ip.eval("R.reset(); inner.focus()")
		if err := ip.p.InsertTextInto("#host", "in the shadow"); err != nil {
			t.Fatal(err)
		}
		ip.wait("inner.value === 'in the shadow'")
		ip.eval("inner.value = ''; inner.focus()")
		ip.stealOnGuard("ce")
		if err := ip.p.InsertTextInto("#host", "lost"); !errors.Is(err, mygo.ErrWrongFocus) {
			t.Errorf("err = %v, want ErrWrongFocus", err)
		}
		ip.wait("document.activeElement.id === 'ce'")
		if v, _ := mygo.EvalAs[string](ip.p, "inner.value"); v != "" {
			t.Errorf("the shadow field took %q", v)
		}
	})

	// The guard reads its verdict after the insert ran, every time.
	t.Run("repeated", func(t *testing.T) {
		ip.t = t
		ip.focus("ta")
		for i := range 100 {
			if err := ip.p.InsertTextInto("#ta", "x"); err != nil {
				t.Fatalf("insert %d: %v", i, err)
			}
		}
		ip.wait("R.state().model.ta === 'x'.repeat(100)")
	})
}
