package e2e

import (
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func TestRichClipboardNativeFormats(t *testing.T) {
	plain, html, rtf := mygo.Clipboard.ReadText(), mygo.Clipboard.ReadHTML(), mygo.Clipboard.ReadRTF()
	defer mygo.Clipboard.WriteRichText(plain, html, rtf)
	d := ui.NewRichDocument("Native 😀").WithStyle(ui.TextRange{Start: 0, End: 6}, ui.RichStyle{Weight: 700, Link: "https://example.com/"})
	mygo.Clipboard.WriteRichText(d.String(), d.HTML(), d.RTF())
	if got := mygo.Clipboard.ReadText(); got != d.String() {
		t.Fatalf("plain representation = %q", got)
	}
	h, err := ui.ParseRichHTML(mygo.Clipboard.ReadHTML())
	if err != nil || h.String() != d.String() || h.StyleAt(0).Weight != 700 || h.StyleAt(0).Link == "" {
		t.Fatalf("native HTML clipboard: %q, %v", h.String(), err)
	}
	r, err := ui.ParseRichRTF(mygo.Clipboard.ReadRTF())
	if err != nil || r.String() != d.String() || r.StyleAt(0).Weight != 700 || r.StyleAt(0).Link == "" {
		t.Fatalf("native RTF clipboard: %q, %v", r.String(), err)
	}
	mygo.Clipboard.WriteText("plain")
	if mygo.Clipboard.ReadHTML() != "" || mygo.Clipboard.ReadRTF() != "" {
		t.Fatal("native plain write retained stale rich formats")
	}
}

func TestContentWindowRichTextInputMethod(t *testing.T) {
	var frames atomic.Int32
	d := ui.NewRichDocument("first\ncafe").WithStyle(ui.TextRange{Start: 9, End: 10}, ui.RichStyle{Italic: true, Weight: 700})
	state := ui.NewRichEditorState(d)
	view := func(c *ui.Context) {
		frames.Add(1)
		ui.Column(c).Fill().Padding(20).Children(func() { ui.RichTextEditor(c, state).Height(120) })
	}
	w := newWindow(t, mygo.WindowOptions{Title: "Rich input method", Width: 400, Height: 200, Content: ui.View(view)})
	eventually(t, "a rich editor frame", func() bool { return frames.Load() > 0 })
	if _, _, ok := inputClient(w); !ok {
		t.Skip("input method automation unavailable on this platform")
	}
	if !click(w, 300, 56) {
		t.Skip("click automation unavailable on this platform")
	}
	eventually(t, "the caret after rich text", func() bool {
		sel, doc, _ := inputClient(w)
		return sel == [2]int{10, 0} && doc == "first\ncafe"
	})
	composeOver(w, "e", 1, false, 9, 1)
	eventually(t, "a transactional rich preedit", func() bool {
		return state.Document().String() == d.String() && !state.CanUndo()
	})
	composeOver(w, "é", 1, true, 9, 1)
	eventually(t, "the styled accented letter", func() bool {
		d := state.Document()
		return d.String() == "first\ncafé" && d.StyleAt(9).Italic && d.StyleAt(9).Weight == 700
	})
	state.Undo()
	eventually(t, "undo of the whole rich composition", func() bool { return state.Document().String() == d.String() })
	state.Redo()
	eventually(t, "redo of the rich composition", func() bool { return state.Document().String() == "first\ncafé" })
}
