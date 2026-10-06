package ui

import (
	"slices"
	"sync"
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

func richTester(state *RichEditorState) (*Tester, *editor) {
	tt := NewTester(func(c *Context) { RichTextEditor(c, state).Fill().AutoFocus() }, 360, 180)
	for _, st := range tt.rt.states {
		if st.editor != nil {
			return tt, st.editor
		}
	}
	panic("missing rich editor")
}

func TestRichEditorFormattingAndUndoGroups(t *testing.T) {
	d := NewRichDocument("first\nsecond")
	s := NewRichEditorState(d)
	s.Select(TextSelection{5, 0}) // backward selection survives undo
	s.BeginUndoGroup()
	s.Format(FormatBold)
	s.BeginUndoGroup()
	s.SetLink("https://example.com/guide")
	s.Format(FormatItalic)
	s.EndUndoGroup()
	s.SetParagraphStyle(ParagraphStyle{Alignment: Center})
	s.EndUndoGroup()
	formatted := s.Document()
	if formatted.StyleAt(0).Weight != 700 || !formatted.StyleAt(0).Italic || formatted.StyleAt(0).Link == "" || formatted.paragraph(0).Alignment != Center {
		t.Fatal("group lost formatting")
	}
	if !s.Undo() || !s.Document().equal(d) || s.Selection() != (TextSelection{5, 0}) || s.CanUndo() {
		t.Fatal("undo did not restore one whole group and selection")
	}
	if !s.Redo() || !s.Document().equal(formatted) {
		t.Fatal("redo lost document attributes")
	}
	s.Format(FormatBold)
	if s.Document().StyleAt(0).Weight != 400 || !s.Document().StyleAt(0).Italic {
		t.Fatal("toggle did not preserve unrelated attributes")
	}
	s.Undo()
	s.Insert("replace")
	if s.CanRedo() {
		t.Fatal("new edit retained stale redo history")
	}
}

func TestRichEditorTypingStyleAndKeyboardGroups(t *testing.T) {
	s := NewRichEditorState(NewRichDocument(""))
	tt, _ := richTester(s)
	defer tt.rt.close()
	tt.Key(Cmd, KeyB)
	tt.Type("a")
	tt.Type("b")
	if s.Document().String() != "ab" || s.Document().StyleAt(1).Weight != 700 {
		t.Fatal("collapsed formatting did not style typing")
	}
	tt.Command("undo")
	if s.Document().String() != "" || s.TypingStyle().Weight != 700 {
		t.Fatal("typing undo lost style or was not coalesced")
	}
	tt.Command("undo")
	if s.TypingStyle().Weight != 0 {
		t.Fatal("typing style did not undo independently")
	}
	tt.Command("redo")
	tt.Command("redo")
	tt.Key(0, KeyLeft)
	tt.Type("x")
	tt.Command("undo")
	if s.Document().String() != "ab" || s.Selection().Caret != 1 {
		t.Fatal("caret movement did not break typing group")
	}
}

func TestRichEditorCompositionIsTransactional(t *testing.T) {
	d := NewRichDocument("cafe").WithStyle(TextRange{3, 4}, RichStyle{Italic: true, Color: RGB(10, 20, 30)})
	s := NewRichEditorState(d)
	s.Select(TextSelection{3, 4})
	tt, ed := richTester(s)
	defer tt.rt.close()
	tt.Compose("e", 1)
	tt.Compose("é", 2)
	if !s.Document().equal(d) || s.CanUndo() || ed.buf.s != "caf" || ed.compose != "é" {
		t.Fatal("preedit changed committed document or undo history")
	}
	tt.Compose("", 0)
	if !s.Document().equal(d) || ed.buf.s != "cafe" || s.Selection() != (TextSelection{3, 4}) {
		t.Fatal("cancel did not restore selection and styled text")
	}
	tt.Compose("e", 1)
	tt.Type("é")
	if s.Document().String() != "café" || !s.Document().StyleAt(3).Italic || s.Document().StyleAt(3).Color != RGB(10, 20, 30) {
		t.Fatal("commit lost replacement or style")
	}
	tt.Type("x")
	tt.Command("undo")
	if s.Document().String() != "café" {
		t.Fatal("typing after composition joined its undo step")
	}
	tt.Command("undo")
	if !s.Document().equal(d) || s.Selection() != (TextSelection{3, 4}) {
		t.Fatal("composition commit was not a single reversible replacement")
	}
	tt.Command("redo")
	if s.Document().String() != "café" {
		t.Fatal("composition redo")
	}
	tt.Command("undo")
	tt.Compose("e", 1)
	// AppKit ends marked text before insertText, with replacement offsets
	// in the preview's surrounding text.
	tt.Compose("", 0)
	tt.send(platform.SurfaceEvent{Kind: platform.TextInput, Text: "é", Replace: true, From: 3, To: 3})
	if s.Document().String() != "café" || !s.Document().StyleAt(3).Italic {
		t.Fatal("native end/commit sequence lost the replacement range")
	}
	tt.Type("x")
	tt.Command("undo")
	if s.Document().String() != "café" {
		t.Fatal("native commit joined later typing")
	}
}

func TestRichEditorIMEReplacementAndExternalUpdate(t *testing.T) {
	d := NewRichDocument("first\ncafe").WithStyle(TextRange{9, 10}, RichStyle{Weight: 700})
	s := NewRichEditorState(d)
	tt, ed := richTester(s)
	defer tt.rt.close()
	tt.send(platform.SurfaceEvent{Kind: platform.TextComposition, Text: "e", Caret: 1, Replace: true, From: 9, To: 10})
	if ed.buf.s != "first\ncaf" || !s.Document().equal(d) {
		t.Fatal("IME replacement range")
	}
	tt.Type("é")
	if s.Document().String() != "first\ncafé" || s.Document().StyleAt(9).Weight != 700 {
		t.Fatal("IME range replacement lost formatting")
	}
	tt.Command("undo")
	if !s.Document().equal(d) {
		t.Fatal("replacement did not undo")
	}
	tt.Compose("temporary", 4)
	s.SetDocument(NewRichDocument("external"))
	tt.Frame()
	if ed.compose != "" || ed.richCompose != nil || ed.buf.s != "external" || s.CanUndo() {
		t.Fatal("external document update retained a stale composition/history")
	}
}

func TestRichEditorGTKSurroundingDeletionTransaction(t *testing.T) {
	d := NewRichDocument("cafe!").WithStyle(TextRange{3, 4}, RichStyle{Italic: true})
	s := NewRichEditorState(d)
	s.Select(TextSelection{4, 4})
	tt, ed := richTester(s)
	defer tt.rt.close()
	// GTK's delete-surrounding and preedit can straddle frames.
	tt.send(platform.SurfaceEvent{Kind: platform.TextComposition, CompositionStart: true})
	tt.send(platform.SurfaceEvent{Kind: platform.TextInput, Replace: true, From: 3, To: 4})
	if !s.Document().equal(d) || s.CanUndo() || ed.buf.s != "caf!" {
		t.Fatal("surrounding deletion escaped the composition transaction")
	}
	tt.Compose("e", 1)
	tt.Compose("", 0)
	if !s.Document().equal(d) || ed.buf.s != "cafe!" {
		t.Fatal("cancelling GTK preedit lost deleted content")
	}
	// End-preedit followed by commit uses the staged text and one undo step.
	tt.Type("é")
	if s.Document().String() != "café!" || !s.Document().StyleAt(3).Italic {
		t.Fatal("GTK commit lost text or formatting")
	}
	tt.Command("undo")
	if !s.Document().equal(d) || s.CanUndo() {
		t.Fatal("GTK deletion/commit had separate undo steps")
	}
	// A second surrounding deletion while composing is also transactional.
	s.Select(TextSelection{4, 4})
	tt.Frame()
	tt.Compose("候補", 2)
	tt.send(platform.SurfaceEvent{Kind: platform.TextInput, Replace: true, From: 3, To: 4})
	tt.Type("é")
	if s.Document().String() != "café!" {
		t.Fatal("delete-surrounding during preedit", s.Document().String())
	}
	tt.Command("undo")
	if !s.Document().equal(d) {
		t.Fatal("undo of surrounding deletion during preedit")
	}
	// Without a preedit-start, a deletion is committed immediately. A
	// following native commit coalesces with it and inherits the removed style.
	s.Select(TextSelection{4, 4})
	tt.Frame()
	tt.send(platform.SurfaceEvent{Kind: platform.TextInput, Replace: true, From: 3, To: 4})
	if s.Document().String() != "caf!" || !s.CanUndo() {
		t.Fatal("standalone deletion was deferred")
	}
	tt.Type("é")
	if s.Document().String() != "café!" || !s.Document().StyleAt(3).Italic {
		t.Fatal("delete/commit lost the removed style")
	}
	tt.Command("undo")
	if !s.Document().equal(d) || s.CanUndo() {
		t.Fatal("delete/commit were separate undo steps")
	}
}

func TestRichEditorClipboardRepresentationsAndFallback(t *testing.T) {
	d := NewRichDocument("Hello שלום 😀").WithStyle(TextRange{0, 5}, RichStyle{Weight: 700, Link: "https://example.com/"})
	s := NewRichEditorState(d)
	tt, _ := richTester(s)
	defer tt.rt.close()
	tt.Command("selectAll")
	tt.Command("copy")
	plain, html, rtf := tt.RichClipboard()
	if plain != d.String() || html == "" || rtf == "" {
		t.Fatal("copy did not offer all formats")
	}
	tt.Command("cut")
	if s.Document().String() != "" {
		t.Fatal("cut")
	}
	tt.Command("undo")
	if !s.Document().equal(d) {
		t.Fatal("cut undo lost styled selection")
	}
	s.Select(TextSelection{d.n, d.n})
	tt.Frame()
	tt.Command("paste")
	if s.Document().String() != d.String()+d.String() || s.Document().StyleAt(d.n).Link != d.StyleAt(0).Link {
		t.Fatal("HTML paste lost formatting")
	}
	tt.Command("undo")
	tt.SetRichClipboard(plain, "", rtf)
	tt.Command("paste")
	if s.Document().StyleAt(d.n).Weight != 700 {
		t.Fatal("RTF fallback lost formatting")
	}
	tt.Command("undo")
	tt.SetRichClipboard("fallback", "", "{broken}")
	tt.Command("paste")
	if s.Document().String() != d.String()+"fallback" {
		t.Fatal("malformed rich content did not fall back to plain text")
	}
	tt.SetClipboard("plain")
	_, html, rtf = tt.RichClipboard()
	if html != "" || rtf != "" {
		t.Fatal("plain clipboard retained stale rich formats")
	}
}

func TestRichEditorGraphemeSelectionAndVisualRTL(t *testing.T) {
	s := NewRichEditorState(NewRichDocument("é👍🏽"))
	s.Select(TextSelection{1, 3})
	if s.Selection() != (TextSelection{0, 4}) {
		t.Fatal(s.Selection())
	}
	tt, _ := richTester(s)
	defer tt.rt.close()
	tt.Key(0, KeyBackspace)
	if s.Document().String() != "" {
		t.Fatal("deletion split selected graphemes")
	}
	tt.Command("undo")
	s.SetDocument(NewRichDocument("אבג"))
	s.Select(TextSelection{1, 1})
	tt.Frame()
	tt.Key(0, KeyLeft)
	if s.Selection().Caret != 2 {
		t.Fatalf("left in RTL moved to %d", s.Selection().Caret)
	}
	tt.Key(Shift, KeyRight)
	if s.Selection() != (TextSelection{2, 1}) {
		t.Fatal("RTL shift selection lost direction", s.Selection())
	}
	tt.Key(0, KeyRight)
	if s.Selection().Caret != 1 {
		t.Fatal("RTL selection collapsed to the wrong visual edge")
	}
}

func TestRichEditorParagraphLayoutAndReadOnly(t *testing.T) {
	d := NewRichDocument("small\nLarge").WithStyle(TextRange{6, 11}, RichStyle{Size: 28, Weight: 700})
	d = d.WithParagraphStyle(TextRange{6, 11}, ParagraphStyle{Alignment: Center, LineHeight: 1.8})
	s := NewRichEditorState(d)
	tt, ed := richTester(s)
	defer tt.rt.close()
	l := ed.area.paraLayout(ed, 1)
	if l.Params.Align != 1 || l.Params.Style.LineHeight != 1.8 || l.Lines[0].Height <= ed.area.paraLayout(ed, 0).Lines[0].Height {
		t.Fatal("paragraph style/character size did not reach shaping")
	}
	readOnly := true
	tt.rt.close()
	rt := NewTester(func(c *Context) { RichTextEditor(c, s).ReadOnly(readOnly).Fill().AutoFocus() }, 360, 180)
	defer rt.rt.close()
	rt.Type("changed")
	rt.Compose("compose", 3)
	rt.Command("paste")
	if !s.Document().equal(d) {
		t.Fatal("ReadOnly accepted input")
	}
	rt.Command("selectAll")
	rt.Command("copy")
	if rt.Clipboard() != d.String() {
		t.Fatal("ReadOnly prevented copying")
	}
	readOnly = false
	rt.Frame()
	rt.Type("new")
	if s.Document().String() != "new" {
		t.Fatal("read-only state persisted after disabling")
	}
}

func TestRichEditorStateConcurrencyAndUnmount(t *testing.T) {
	s := NewRichEditorState(NewRichDocument("text"))
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				s.Select(TextSelection{0, 2})
				s.Format(FormatItalic)
				_ = s.Document().Runs()
				_ = s.Selection()
				s.Undo()
				s.Redo()
			}
		}()
	}
	wg.Wait()
	checkRichDocument(t, s.Document())
	show := true
	tt := NewTester(func(c *Context) {
		if show {
			RichTextEditor(c, s).Fill()
		}
	}, 200, 80)
	show = false
	tt.Frame()
	if s.wake != nil || s.owner != nil {
		t.Fatal("unmounted editor retained its host")
	}
	tt.rt.close()
	s.SetDocument(NewRichDocument("detached"))
	if !slices.Equal(s.Document().Runs(), []StyledRange{{TextRange{0, 8}, RichStyle{}}}) {
		t.Fatal("detached state could not be used")
	}
}

func TestRichEditorSwitchToPlainInput(t *testing.T) {
	state := NewRichEditorState(NewRichDocument("rich"))
	rich, plain := true, "plain"
	tt := NewTester(func(c *Context) {
		if rich {
			RichTextEditor(c, state).Fill().AutoFocus()
		} else {
			TextArea(c, &plain).Fill().Focus()
		}
	}, 240, 80)
	defer tt.rt.close()
	tt.Type("!")
	rich = false
	tt.Frame()
	tt.Type("!")
	if plain != "plain!" || state.Document().String() != "rich!" || state.wake != nil {
		t.Fatal("switching to plain input retained rich editing state")
	}
}

func TestRichEditorLinkActivationAndSelection(t *testing.T) {
	url := "https://example.com/guide"
	d := NewRichDocument("a link z").WithStyle(TextRange{2, 6}, RichStyle{Link: url})
	s := NewRichEditorState(d)
	tt, ed := richTester(s)
	defer tt.rt.close()
	x, y, h := ed.area.caretAt(ed, 3, 0)
	x, cy := ed.originX+x+1, ed.originY+float32(y)+h/2
	tt.ClickAt(x, cy)
	if len(tt.h.opened) != 0 || s.Selection().Anchor != s.Selection().Caret {
		t.Fatal("ordinary link click did not select text")
	}
	before := s.Selection()
	tt.ClickAtWith(Cmd, x, cy)
	if len(tt.h.opened) != 1 || tt.h.opened[0] != url || s.Selection() != before {
		t.Fatal("modified link click did not open the URL while preserving selection")
	}
}
