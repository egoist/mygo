//go:build !js

package mygo

import (
	"errors"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

func TestPageInput(t *testing.T) {
	w := NewWindow(WindowOptions{Hidden: true, URL: "https://example.com/"})
	t.Cleanup(w.Destroy)
	fw := fb.Windows()[len(fb.Windows())-1]
	p := w.Page()

	// Enter, and a capital letter, which is its letter with Shift held.
	for _, e := range []KeyEvent{{Key: "Enter"}, {Type: KeyDown, Key: "A", Modifiers: ModCtrl}, {Type: KeyUp, Key: "7"}} {
		if err := p.SendKey(e); err != nil {
			t.Fatalf("SendKey(%+v): %v", e, err)
		}
	}
	if len(fw.Keys) != 3 {
		t.Fatalf("keys %+v", fw.Keys)
	}
	if k := fw.Keys[0]; k.Kind != platform.PageKeyPress || k.Key.Name != "Enter" || k.Key.Mac != 36 || k.Key.Win != 0x0D || k.Key.Text != "\r" {
		t.Errorf("Enter reached the backend as %+v", k)
	}
	if k := fw.Keys[1]; k.Kind != platform.PageKeyDown || k.Key.Name != "a" || k.Key.Code != "KeyA" || k.Key.Win != 'A' || k.Mods != platform.ModShift|platform.ModCtrl {
		t.Errorf("Ctrl+A reached the backend as %+v", k)
	}
	if k := fw.Keys[2]; k.Kind != platform.PageKeyUp || k.Key.Name != "7" || k.Key.Code != "Digit7" || k.Key.Mac != 26 {
		t.Errorf("7 reached the backend as %+v", k)
	}

	// A click defaults to a single click; a move has no click count to give.
	for _, typ := range []MouseEventType{MouseMove, MouseDown, MouseUp} {
		if err := p.SendMouse(MouseEvent{Type: typ, X: 12.5, Y: 40}); err != nil {
			t.Fatalf("SendMouse: %v", err)
		}
	}
	if len(fw.Mouse) != 3 || fw.Mouse[1] != (platform.PageMouseEvent{Kind: platform.PageMouseDown, X: 12.5, Y: 40, ClickCount: 1}) {
		t.Errorf("mouse %+v", fw.Mouse)
	}

	if err := p.InsertText("xin chào"); err != nil || !slices.Equal(fw.Inserted, []string{"xin chào"}) {
		t.Errorf("InsertText: %v, inserted %q", err, fw.Inserted)
	}

	// Input the page cannot take is an error, not a no-op.
	if err := p.SendKey(KeyEvent{Key: "F13"}); err == nil {
		t.Error("SendKey of an unknown key succeeded")
	}
	if err := p.SendKey(KeyEvent{Type: 9, Key: "a"}); err == nil {
		t.Error("SendKey of an unknown type succeeded")
	}
	if err := p.SendMouse(MouseEvent{Type: MouseDown, X: math.NaN()}); err == nil {
		t.Error("SendMouse at NaN succeeded")
	}
	if len(fw.Keys) != 3 || len(fw.Mouse) != 3 {
		t.Errorf("refused events reached the backend: keys %d, mouse %d", len(fw.Keys), len(fw.Mouse))
	}

	w.Destroy()
	if err := p.SendKey(KeyEvent{Key: "Enter"}); err == nil {
		t.Error("SendKey to a destroyed window succeeded")
	}
}

// InsertTextInto arms a guard in the page, inserts the text, and reads what
// the guard decided: the guard is armed before the text goes in and read
// after, and what it says becomes the error.
func TestInsertTextInto(t *testing.T) {
	w := NewWindow(WindowOptions{Hidden: true, URL: "https://example.com/"})
	t.Cleanup(w.Destroy)
	fw := fb.Windows()[len(fb.Windows())-1]
	p := w.Page()

	var calls []string
	var arm, collect string
	fw.AsyncFunction = func(body string) (string, error) {
		switch {
		case strings.Contains(body, "createDocumentFragment"):
			calls = append(calls, "arm")
			if !strings.Contains(body, `sel = "#composer", text = "xin chào"`) {
				t.Errorf("the guard is not given the selector and the text: %s", body)
			}
			return arm, nil
		case strings.Contains(body, "window["):
			calls = append(calls, "collect")
			return collect, nil
		}
		t.Errorf("unexpected script %s", body)
		return "", errors.New("unexpected")
	}
	reply := func(state string) string { return `{"ok":true,"v":{"state":"` + state + `"}}` }
	insert := func(armState, collectState string) error {
		calls, arm, collect = nil, reply(armState), reply(collectState)
		fw.Inserted = nil
		err := p.InsertTextInto("#composer", "xin chào")
		if armState == "armed" && !slices.Equal(calls, []string{"arm", "collect"}) {
			t.Errorf("calls %v, want the guard armed, then read", calls)
		}
		return err
	}

	if err := insert("armed", "inserted"); err != nil || !slices.Equal(fw.Inserted, []string{"xin chào"}) {
		t.Errorf("a match: %v, inserted %q", err, fw.Inserted)
	}
	err := insert("armed", "none")
	if err == nil || errors.Is(err, ErrWrongFocus) {
		t.Errorf("no text input event: %v", err)
	}
	if err := insert("armed", "lost"); err == nil || errors.Is(err, ErrWrongFocus) {
		t.Errorf("a page that went away: %v", err)
	}

	collect = `{"ok":true,"v":{"state":"refused","target":"textarea#other"}}`
	calls, arm = nil, reply("armed")
	err = p.InsertTextInto("#composer", "xin chào")
	if !errors.Is(err, ErrWrongFocus) || !strings.Contains(err.Error(), "textarea#other") {
		t.Errorf("a refusal: %v", err)
	}

	// Nothing is inserted when the guard cannot be armed.
	for _, state := range []string{"selector", "frame"} {
		if err := insert(state, "inserted"); err == nil || len(fw.Inserted) != 0 || len(calls) != 1 {
			t.Errorf("%s: %v, inserted %q, calls %v", state, err, fw.Inserted, calls)
		}
	}
	if err := p.InsertTextInto("#composer", ""); err == nil {
		t.Error("InsertTextInto of no text succeeded")
	}

	w.Destroy()
	if err := p.InsertTextInto("#composer", "x"); err == nil {
		t.Error("InsertTextInto to a destroyed window succeeded")
	}
}

// A web view in a window of native UI has a page that takes input as a
// window's does: the core hands the events to the web view's backend, not
// to the window it is in.
func TestPageInputWebView(t *testing.T) {
	show := true
	_, v, _, fv := webViewWindow(t, &show)
	fw := fb.Windows()[len(fb.Windows())-1]
	p := v.Page()

	if err := p.SendKey(KeyEvent{Key: "Enter"}); err != nil {
		t.Fatalf("SendKey: %v", err)
	}
	if err := p.SendMouse(MouseEvent{Type: MouseDown, X: 3, Y: 4}); err != nil {
		t.Fatalf("SendMouse: %v", err)
	}
	if err := p.InsertText("xin chào"); err != nil {
		t.Fatalf("InsertText: %v", err)
	}
	if len(fv.Keys) != 1 || len(fv.Mouse) != 1 || !slices.Equal(fv.Inserted, []string{"xin chào"}) {
		t.Errorf("the web view got keys %+v, mouse %+v, text %q", fv.Keys, fv.Mouse, fv.Inserted)
	}
	if len(fw.Keys) != 0 || len(fw.Mouse) != 0 || len(fw.Inserted) != 0 {
		t.Errorf("the window got the web view's input: %+v %+v %q", fw.Keys, fw.Mouse, fw.Inserted)
	}
}

// bareWindow is a platform window without the optional interfaces of the
// one it wraps, as the browsers' windows are.
type bareWindow struct{ platform.Window }

// A page whose backend does not take input says so, as errors.ErrUnsupported.
func TestPageInputUnsupported(t *testing.T) {
	w := NewWindow(WindowOptions{Hidden: true, URL: "https://example.com/"})
	t.Cleanup(w.Destroy)
	p := w.Page()
	var full platform.Window
	onMain(func() { full, w.native = w.native, bareWindow{w.native} })
	t.Cleanup(func() { onMain(func() { w.native = full }) })

	for name, err := range map[string]error{
		"SendMouse":  p.SendMouse(MouseEvent{Type: MouseDown}),
		"SendKey":    p.SendKey(KeyEvent{Key: "Enter"}),
		"InsertText": p.InsertText("x"),
	} {
		if !errors.Is(err, errors.ErrUnsupported) {
			t.Errorf("%s: %v, want errors.ErrUnsupported", name, err)
		}
	}
}
