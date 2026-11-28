package richtext

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestBoundsForRangePlain(t *testing.T) {
	doc := NewDocument(NewText("hello world"))
	richTester(t, doc)
	v := doc.shownIn.Value()

	one, _, ok := v.BoundsForRange(ui.TextInputRange{Start: 0, End: 1})
	if !ok {
		t.Fatal("single-rune bounds not visible")
	}
	endCaret, _, _ := v.BoundsForRange(ui.TextInputRange{Start: 5, End: 5})
	multi, actual, ok := v.BoundsForRange(ui.TextInputRange{Start: 0, End: 5})
	if !ok || actual != (ui.TextInputRange{Start: 0, End: 5}) {
		t.Fatalf("multi-rune bounds: rect=%+v actual=%+v ok=%v", multi, actual, ok)
	}
	if wantW := endCaret.X - one.X; multi.W != wantW || multi.W <= one.W {
		t.Fatalf("multi-rune rect=%+v, want W=%v (> %v)", multi, wantW, one.W)
	}
	rev, revActual, ok := v.BoundsForRange(ui.TextInputRange{Start: 5, End: 0})
	if !ok || revActual != actual || rev != multi {
		t.Fatalf("reversed range: rect=%+v actual=%+v, want rect=%+v actual=%+v", rev, revActual, multi, actual)
	}
}

func TestBoundsForRangeComposition(t *testing.T) {
	doc := NewDocument(NewText("ab"))
	tt, _ := richTester(t, doc)
	doc.SetSelection(1, 1)
	tt.Frame()

	v := doc.shownIn.Value()
	v.SetMarkedText(nil, "nihao", ui.TextInputRange{Start: 5, End: 5})

	// Virtual text is "anihaob"; query subrange "iha" inside the composition.
	startCaret, _, _ := v.BoundsForRange(ui.TextInputRange{Start: 2, End: 2})
	endCaret, _, _ := v.BoundsForRange(ui.TextInputRange{Start: 5, End: 5})
	b, actual, ok := v.BoundsForRange(ui.TextInputRange{Start: 2, End: 5})
	if !ok || actual != (ui.TextInputRange{Start: 2, End: 5}) {
		t.Fatalf("composition bounds: rect=%+v actual=%+v ok=%v", b, actual, ok)
	}
	if wantW := endCaret.X - startCaret.X; b.X != startCaret.X || b.W != wantW || b.W <= 1 {
		t.Fatalf("composition rect=%+v, want X=%v W=%v", b, startCaret.X, wantW)
	}
	full, fullActual, ok := v.BoundsForRange(ui.TextInputRange{Start: 0, End: 7})
	if !ok || fullActual != (ui.TextInputRange{Start: 0, End: 7}) || full.W <= b.W {
		t.Fatalf("full range with composition: rect=%+v actual=%+v ok=%v (innerW=%v)", full, fullActual, ok, b.W)
	}
}

func TestBoundsForRangeLineBreak(t *testing.T) {
	doc := NewDocument(NewText("hi\nhello world"))
	richTester(t, doc)
	v := doc.shownIn.Value()

	startCaret, _, _ := v.BoundsForRange(ui.TextInputRange{Start: 1, End: 1})
	cross, actual, ok := v.BoundsForRange(ui.TextInputRange{Start: 1, End: 10})
	if !ok || actual != (ui.TextInputRange{Start: 1, End: 1}) || cross != startCaret {
		t.Fatalf("newline cross: rect=%+v actual=%+v ok=%v, want rect=%+v", cross, actual, ok, startCaret)
	}

	const wrapped = "aa bbbbbb"
	wrapDoc := NewDocument(NewText(wrapped))
	tt := ui.NewTester(func(c *ui.Context) {
		Editor(c, wrapDoc).Width(80).Height(200)
	}, 100, 220)
	tt.Press(10, 10)
	tt.Release(10, 10)
	wv := wrapDoc.shownIn.Value()

	line0, _, _ := wv.BoundsForRange(ui.TextInputRange{Start: 0, End: 0})
	wrapAt := -1
	for i := 1; i <= len(wrapped); i++ {
		if c, _, _ := wv.BoundsForRange(ui.TextInputRange{Start: i, End: i}); c.Y > line0.Y {
			wrapAt = i
			break
		}
	}
	if wrapAt <= 0 {
		t.Fatal("text did not soft-wrap")
	}

	firstLine, firstActual, ok := wv.BoundsForRange(ui.TextInputRange{Start: 0, End: wrapAt})
	if !ok || firstActual != (ui.TextInputRange{Start: 0, End: wrapAt}) || firstLine.Y != line0.Y || firstLine.W <= 1 {
		t.Fatalf("first wrapped line: rect=%+v actual=%+v ok=%v (line0Y=%v)", firstLine, firstActual, ok, line0.Y)
	}
	crossWrap, crossActual, ok := wv.BoundsForRange(ui.TextInputRange{Start: 0, End: len(wrapped)})
	if !ok || crossActual != (ui.TextInputRange{Start: 0, End: 0}) || crossWrap != line0 {
		t.Fatalf("cross wrap: rect=%+v actual=%+v ok=%v, want rect=%+v", crossWrap, crossActual, ok, line0)
	}

	wv.caret = wrapAt
	wv.affinity = ui.TextUpstream
	secondLine, secondActual, ok := wv.BoundsForRange(ui.TextInputRange{Start: wrapAt, End: len(wrapped)})
	if !ok || secondActual != (ui.TextInputRange{Start: wrapAt, End: len(wrapped)}) || secondLine.Y <= line0.Y || secondLine.W <= 1 {
		t.Fatalf("second wrapped line: rect=%+v actual=%+v ok=%v (line0Y=%v)", secondLine, secondActual, ok, line0.Y)
	}
	atWrapCaret, _, _ := wv.BoundsForRange(ui.TextInputRange{Start: wrapAt, End: wrapAt})
	if atWrapCaret.Y != line0.Y || atWrapCaret.W != 1 {
		t.Fatalf("collapsed caret at wrap with upstream affinity: rect=%+v, want on line 0 (Y=%v)", atWrapCaret, line0.Y)
	}
}

func TestSetMarkedTextSelectionCancel(t *testing.T) {
	doc := NewDocument(NewText("hello"))
	tt, _ := richTester(t, doc)
	doc.SetSelection(1, 4)
	tt.Frame()
	v := doc.shownIn.Value()

	v.SetMarkedText(nil, "ni", ui.TextInputRange{Start: 2, End: 2})
	if got := doc.Text().String(); got != "hello" {
		t.Fatalf("composition changed the document to %q", got)
	}
	if text, actual := v.TextForRange(ui.TextInputRange{Start: 0, End: 4}); text != "hnio" || actual != (ui.TextInputRange{Start: 0, End: 4}) {
		t.Fatalf("TextForRange = (%q, %+v), want (%q, %+v)", text, actual, "hnio", ui.TextInputRange{Start: 0, End: 4})
	}
	if got := v.Selection().Range; got != (ui.TextInputRange{Start: 3, End: 3}) {
		t.Fatalf("Selection = %+v, want (3, 3)", got)
	}
	if got, ok := v.MarkedRange(); !ok || got != (ui.TextInputRange{Start: 1, End: 3}) {
		t.Fatalf("MarkedRange = (%+v, %v), want ((1, 3), true)", got, ok)
	}
	if _, _, ok := v.BoundsForRange(ui.TextInputRange{Start: 1, End: 3}); !ok {
		t.Fatal("marked range has no bounds")
	}
	if got := v.composedText().String(); got != "hnio" {
		t.Fatalf("composed text = %q, want %q", got, "hnio")
	}

	v.SetMarkedText(nil, "", ui.TextInputRange{})
	if got, ok := v.MarkedRange(); ok {
		t.Fatalf("cancel left marked range %+v", got)
	}
	if got := doc.Text().String(); got != "hello" {
		t.Fatalf("cancel changed the document to %q", got)
	}
	if doc.CanUndo() {
		t.Fatal("cancel made a step of undo")
	}
	if a, c := doc.Selection(); a != 1 || c != 4 {
		t.Fatalf("selection = (%d, %d), want (1, 4)", a, c)
	}
}

func TestSetMarkedTextSelectionCommit(t *testing.T) {
	doc := NewDocument(NewText("hello"))
	tt, _ := richTester(t, doc)
	doc.SetSelection(1, 4)
	tt.Frame()
	v := doc.shownIn.Value()

	v.SetMarkedText(nil, "ni", ui.TextInputRange{Start: 2, End: 2})
	v.ReplaceText(nil, "你")
	if !doc.CanUndo() {
		t.Fatal("commit left no step of undo")
	}
	if got := doc.Text().String(); got != "h你o" {
		t.Fatalf("committed %q, want %q", got, "h你o")
	}
	doc.Undo()
	if got := doc.Text().String(); got != "hello" {
		t.Fatalf("after one undo %q, want %q", got, "hello")
	}
	if a, c := doc.Selection(); a != 1 || c != 4 {
		t.Fatalf("after one undo selection = (%d, %d), want (1, 4)", a, c)
	}
}

func TestSetMarkedTextExplicitRange(t *testing.T) {
	doc := NewDocument(NewText("hello"))
	tt, _ := richTester(t, doc)
	doc.SetSelection(0, 0)
	tt.Frame()
	v := doc.shownIn.Value()

	r := ui.TextInputRange{Start: 1, End: 4}
	v.SetMarkedText(&r, "ni", ui.TextInputRange{Start: 2, End: 2})
	if got := doc.Text().String(); got != "hello" {
		t.Fatalf("composition changed the document to %q", got)
	}
	if got := v.composedText().String(); got != "hnio" {
		t.Fatalf("composed text = %q, want %q", got, "hnio")
	}
	v.ReplaceText(nil, "你")
	if got := doc.Text().String(); got != "h你o" {
		t.Fatalf("committed %q, want %q", got, "h你o")
	}
	doc.Undo()
	if got := doc.Text().String(); got != "hello" {
		t.Fatalf("after one undo %q, want %q", got, "hello")
	}
}

func TestSetMarkedTextUTF16Range(t *testing.T) {
	doc := NewDocument(NewText("a😀b"))
	tt, _ := richTester(t, doc)
	doc.SetSelection(1, 2)
	tt.Frame()
	v := doc.shownIn.Value()

	v.SetMarkedText(nil, "ni", ui.TextInputRange{Start: 2, End: 2})
	if text, actual := v.TextForRange(ui.TextInputRange{Start: 0, End: 4}); text != "anib" || actual != (ui.TextInputRange{Start: 0, End: 4}) {
		t.Fatalf("TextForRange = (%q, %+v), want (%q, %+v)", text, actual, "anib", ui.TextInputRange{Start: 0, End: 4})
	}
	if got, ok := v.MarkedRange(); !ok || got != (ui.TextInputRange{Start: 1, End: 3}) {
		t.Fatalf("MarkedRange = (%+v, %v), want ((1, 3), true)", got, ok)
	}
	v.ReplaceText(nil, "你")
	if got := doc.Text().String(); got != "a你b" {
		t.Fatalf("committed %q, want %q", got, "a你b")
	}
}

func TestSetMarkedTextSyncClearsPendingRange(t *testing.T) {
	for _, change := range []struct {
		name string
		fn   func(*Document)
	}{
		{name: "selection", fn: func(doc *Document) { doc.SetSelection(0, 0) }},
		{name: "text", fn: func(doc *Document) { doc.SetText(NewText("other")) }},
		{name: "shorter text", fn: func(doc *Document) { doc.SetText(NewText("x")) }},
	} {
		t.Run(change.name, func(t *testing.T) {
			doc := NewDocument(NewText("hello"))
			tt, _ := richTester(t, doc)
			doc.SetSelection(1, 4)
			tt.Frame()
			v := doc.shownIn.Value()

			v.SetMarkedText(nil, "ni", ui.TextInputRange{Start: 2, End: 2})
			change.fn(doc)
			tt.Frame()
			if v.pendingSet || v.pendingRange != [2]int{} || v.compose != "" || v.composing {
				t.Fatalf("sync left composition state: pending=%v range=%+v compose=%q composing=%v", v.pendingSet, v.pendingRange, v.compose, v.composing)
			}
			if text, _ := v.TextForRange(ui.TextInputRange{Start: 0, End: 100}); text != doc.Text().String() {
				t.Fatalf("TextForRange = %q, want %q", text, doc.Text().String())
			}
			if a, c := doc.Selection(); a < 0 || c < 0 || a > doc.Text().Len() || c > doc.Text().Len() {
				t.Fatalf("selection (%d, %d) out of bounds for len %d", a, c, doc.Text().Len())
			}
		})
	}
}

func TestSetMarkedTextDirectCommitKeepsTypingAttributes(t *testing.T) {
	doc := NewDocument(NewText("ab"))
	tt, _ := richTester(t, doc)
	tt.Key(ui.Cmd, ui.KeyB)
	tt.Compose("ni", 2)
	tt.Type("你")
	if got := runsOf(doc.Text()); got != "ab|*你*" {
		t.Fatalf("runs = %q, want %q", got, "ab|*你*")
	}
}

func TestSetMarkedTextBackwardSelectionCancel(t *testing.T) {
	doc := NewDocument(NewText("hello"))
	tt, _ := richTester(t, doc)
	doc.SetSelection(4, 1)
	tt.Frame()
	v := doc.shownIn.Value()
	v.SetMarkedText(nil, "ni", ui.TextInputRange{Start: 2, End: 2})
	v.SetMarkedText(nil, "", ui.TextInputRange{})
	if a, c := doc.Selection(); a != 4 || c != 1 {
		t.Fatalf("selection after cancel = (%d, %d), want (4, 1)", a, c)
	}
}

func TestSetMarkedTextCrossParagraphImmediateBounds(t *testing.T) {
	doc := NewDocument(NewText("one\ntwo"))
	tt, _ := richTester(t, doc)
	doc.SetSelection(1, 6)
	tt.Frame()
	v := doc.shownIn.Value()
	v.SetMarkedText(nil, "ni", ui.TextInputRange{Start: 2, End: 2})
	tt.Frame()
	// Cancel without an intervening Frame, then query bounds in paragraph 1.
	v.SetMarkedText(nil, "", ui.TextInputRange{})
	if _, _, ok := v.BoundsForRange(ui.TextInputRange{Start: 4, End: 7}); !ok {
		t.Fatal("paragraph 1 bounds not available immediately after cross-paragraph cancel")
	}
}

func TestSetMarkedTextExplicitRangeWhileComposing(t *testing.T) {
	doc := NewDocument(NewText("hello world"))
	tt, _ := richTester(t, doc)
	doc.SetSelection(0, 5)
	tt.Frame()
	v := doc.shownIn.Value()
	v.SetMarkedText(nil, "ni", ui.TextInputRange{Start: 2, End: 2})
	// Replacing an unrelated explicit range commits the active composition first.
	r := ui.TextInputRange{Start: 3, End: 8} // "world" in "ni world"
	v.SetMarkedText(&r, "hao", ui.TextInputRange{Start: 3, End: 3})
	if got := doc.Text().String(); got != "ni world" {
		t.Fatalf("document after explicit range switch = %q, want %q", got, "ni world")
	}
	if got := v.composedText().String(); got != "ni hao" {
		t.Fatalf("composed text = %q, want %q", got, "ni hao")
	}
	v.ReplaceText(nil, "好")
	if got := doc.Text().String(); got != "ni 好" {
		t.Fatalf("committed text = %q, want %q", got, "ni 好")
	}
}

func TestSetMarkedTextCrossParagraph(t *testing.T) {
	const original = "one\ntwo"
	for _, tc := range []struct {
		name   string
		cancel bool
	}{
		{name: "cancel", cancel: true},
		{name: "commit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := NewDocument(NewText(original))
			tt, _ := richTester(t, doc)
			doc.SetSelection(1, 6)
			tt.Frame()
			v := doc.shownIn.Value()

			v.SetMarkedText(nil, "ni", ui.TextInputRange{Start: 2, End: 2})
			tt.Frame()
			if got := doc.Text().String(); got != original {
				t.Fatalf("composition changed the document to %q", got)
			}
			if text, actual := v.TextForRange(ui.TextInputRange{Start: 0, End: 4}); text != "onio" || actual != (ui.TextInputRange{Start: 0, End: 4}) {
				t.Fatalf("TextForRange = (%q, %+v), want (%q, %+v)", text, actual, "onio", ui.TextInputRange{Start: 0, End: 4})
			}
			if got, ok := v.MarkedRange(); !ok || got != (ui.TextInputRange{Start: 1, End: 3}) {
				t.Fatalf("MarkedRange = (%+v, %v), want ((1, 3), true)", got, ok)
			}
			if _, _, ok := v.BoundsForRange(ui.TextInputRange{Start: 1, End: 3}); !ok {
				t.Fatal("marked range has no bounds")
			}
			if _, caret := doc.Selection(); caret < 0 || caret > len(original) {
				t.Fatalf("selection caret %d out of range", caret)
			}
			if got, want := v.composedText().String(), "onio"; got != want {
				t.Fatalf("composed text = %q, want %q", got, want)
			}
			if got := v.composedText().ParagraphCount(); got != 1 {
				t.Fatalf("composed text has %d paragraphs, want 1", got)
			}
			if got := len(v.paras); got != 1 {
				t.Fatalf("layout has %d paragraphs, want 1", got)
			}
			if box := doc.VisibleParagraphs(); len(box) != 1 || box[0].Start != 0 {
				t.Fatalf("visible paragraphs = %+v, want one paragraph starting at 0", box)
			}

			if tc.cancel {
				v.SetMarkedText(nil, "", ui.TextInputRange{})
				tt.Frame()
				if got := doc.Text().String(); got != original {
					t.Fatalf("cancel changed the document to %q", got)
				}
				if doc.CanUndo() {
					t.Fatal("cancel made a step of undo")
				}
				if got := doc.Text().ParagraphCount(); got != 2 {
					t.Fatalf("cancel left %d paragraphs, want 2", got)
				}
				if got := len(doc.VisibleParagraphs()); got != 2 {
					t.Fatalf("cancel left %d visible paragraphs, want 2", got)
				}
				return
			}
			v.ReplaceText(nil, "你")
			tt.Frame()
			if got := doc.Text().String(); got != "o你o" {
				t.Fatalf("committed %q, want %q", got, "o你o")
			}
			doc.Undo()
			if got := doc.Text().String(); got != original {
				t.Fatalf("after one undo %q, want %q", got, original)
			}
			if got := doc.Text().ParagraphCount(); got != 2 {
				t.Fatalf("after one undo %d paragraphs, want 2", got)
			}
			if a, c := doc.Selection(); a != 1 || c != 6 {
				t.Fatalf("after one undo selection = (%d, %d), want (1, 6)", a, c)
			}
		})
	}
}

func longIMEDoc(n int) *Document {
	paras := make([]Paragraph, n)
	for i := range paras {
		s := fmt.Sprintf("Paragraph %03d", i)
		if i%3 == 0 {
			s += ", long enough to wrap in a window this narrow at times."
		}
		paras[i] = para(s)
	}
	return NewDocument(TextOf(paras...))
}

func TestCrossParagraphCompositionKeepsViewport(t *testing.T) {
	for _, end := range []string{"cancel", "commit"} {
		t.Run(end, func(t *testing.T) {
			doc := longIMEDoc(300)
			var boxes []ParagraphBox
			tt := openTester(t, func(c *ui.Context) {
				boxes = doc.VisibleParagraphs()
				EditorBase(c, doc, EditorOptions{Font: ui.Font{Size: 10}}).Fill()
			}, 300, 400)
			tt.Press(150, 5)
			tt.Release(150, 5)
			doc.ScrollToParagraph(25, ui.ScrollStart)
			tt.Frame()
			tt.Scroll(150, 200, 0, -0.2)
			tt.Frame()

			firstBefore := boxes[0]
			caretPara := boxes[len(boxes)/2].Index
			from := boxes[len(boxes)/2].Start
			to := boxOf(t, boxes, caretPara+1).Start

			v := doc.shownIn.Value()
			v.selAnchor, v.caret = from+2, to+2
			v.publish()
			v.hasDesired = false
			v.reveal = false
			tt.Frame()

			firstBefore = boxes[0]
			_, caretYBefore, _ := v.composedCaretAt(v.composedLocal(to+2), ui.TextDownstream)
			caretRelBefore := caretYBefore - v.scroll
			layoutsBefore := make([]*ui.TextLayout, len(v.paras))
			heightsBefore := make([]float32, len(v.paras))
			for i := range v.paras {
				layoutsBefore[i] = v.paras[i].layout
				heightsBefore[i] = v.paras[i].h
			}

			v.SetMarkedText(nil, "ni", ui.TextInputRange{Start: 2, End: 2})
			tt.Frame()
			if boxes[0].Index != firstBefore.Index || math.Abs(float64(boxes[0].Top-firstBefore.Top)) > 0.01 {
				t.Fatalf("composition jumped view from (%d, %v) to (%d, %v)",
					firstBefore.Index, firstBefore.Top, boxes[0].Index, boxes[0].Top)
			}
			for i := range v.paras {
				if i == caretPara {
					continue
				}
				j := i
				if i > caretPara {
					j = i + 1
				}
				if heightsBefore[j] > 0 && (v.paras[i].layout != layoutsBefore[j] || v.paras[i].h != heightsBefore[j]) {
					t.Fatalf("composition reset unaffected paragraph %d", j)
				}
			}

			if end == "cancel" {
				v.SetMarkedText(nil, "", ui.TextInputRange{})
			} else {
				v.ReplaceText(nil, "你")
			}
			tt.Frame()
			if boxes[0].Index != firstBefore.Index || math.Abs(float64(boxes[0].Top-firstBefore.Top)) > 0.01 {
				t.Fatalf("%s jumped view from (%d, %v) to (%d, %v)",
					end, firstBefore.Index, firstBefore.Top, boxes[0].Index, boxes[0].Top)
			}
			if end == "cancel" {
				_, caretYAfter, _ := v.composedCaretAt(v.composedLocal(to+2), ui.TextDownstream)
				if got := caretYAfter - v.scroll; math.Abs(got-caretRelBefore) > 0.01 {
					t.Fatalf("cancel moved caret Y from %v to %v", caretRelBefore, got)
				}
			}
			shift := len(layoutsBefore) - len(v.paras)
			for i := range v.paras {
				if i == caretPara || (end == "cancel" && i == caretPara+1) {
					continue
				}
				j := i
				if i > caretPara {
					j = i + shift
				}
				if heightsBefore[j] > 0 && (v.paras[i].layout != layoutsBefore[j] || v.paras[i].h != heightsBefore[j]) {
					t.Fatalf("%s reset unaffected paragraph %d", end, j)
				}
			}
		})
	}
}

func TestCompositionBlinkDoesNotRelayout(t *testing.T) {
	doc := NewDocument(NewText("hello world"))
	tt, _ := richTester(t, doc)
	doc.SetSelection(1, 4)
	tt.Frame()
	v := doc.shownIn.Value()
	v.SetMarkedText(nil, "ni", ui.TextInputRange{Start: 2, End: 2})
	tt.Frame()
	composedBefore := v.composedText()
	layoutBefore := v.paras[0].layout
	if layoutBefore == nil {
		t.Fatal("composing paragraph was not laid out")
	}
	for range 10 {
		tt.Advance(600 * time.Millisecond)
		tt.Frame()
	}
	if !v.composedText().Same(composedBefore) {
		t.Fatal("composedText was rebuilt across caret-blink frames")
	}
	if v.paras[0].layout != layoutBefore {
		t.Fatal("composing paragraph was relaid out across caret-blink frames")
	}
}

func TestExternalEditKeepsComposition(t *testing.T) {
	for _, tc := range []struct {
		name   string
		sel    [2]int
		edit   func(*Document)
		marked ui.TextInputRange
		text   string
		commit string
		keep   bool
	}{
		{
			name:   "caret_after",
			sel:    [2]int{2, 2},
			edit:   func(d *Document) { d.Edit(func(e *Edit) { e.ReplaceString(11, 11, "!", Attributes{}) }) },
			marked: ui.TextInputRange{Start: 2, End: 4},
			text:   "henillo\nworld!",
			commit: "he你llo\nworld!",
			keep:   true,
		},
		{
			name:   "caret_before",
			sel:    [2]int{2, 2},
			edit:   func(d *Document) { d.Edit(func(e *Edit) { e.ReplaceString(0, 0, "!", Attributes{}) }) },
			marked: ui.TextInputRange{Start: 3, End: 5},
			text:   "!henillo\nworld",
			commit: "!he你llo\nworld",
			keep:   true,
		},
		{
			name: "caret_before_newline_and_after",
			sel:  [2]int{2, 2},
			edit: func(d *Document) {
				d.Edit(func(e *Edit) {
					e.ReplaceString(0, 0, "a\n", Attributes{})
					e.ReplaceString(13, 13, "!", Attributes{})
				})
			},
			marked: ui.TextInputRange{Start: 4, End: 6},
			text:   "a\nhenillo\nworld!",
			commit: "a\nhe你llo\nworld!",
			keep:   true,
		},
		{
			name:   "selection_after",
			sel:    [2]int{1, 4},
			edit:   func(d *Document) { d.Edit(func(e *Edit) { e.ReplaceString(11, 11, "!", Attributes{}) }) },
			marked: ui.TextInputRange{Start: 1, End: 3},
			text:   "hnio\nworld!",
			commit: "h你o\nworld!",
			keep:   true,
		},
		{
			name: "caret_overlapping_delete",
			sel:  [2]int{2, 2},
			edit: func(d *Document) { d.Edit(func(e *Edit) { e.ReplaceString(0, 3, "!", Attributes{}) }) },
			keep: false,
		},
		{
			name: "selection_overlapping_insert",
			sel:  [2]int{1, 4},
			edit: func(d *Document) { d.Edit(func(e *Edit) { e.ReplaceString(2, 2, "!", Attributes{}) }) },
			keep: false,
		},
		{
			name: "selection_set",
			sel:  [2]int{2, 2},
			edit: func(d *Document) { d.SetSelection(0, 0) },
			keep: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := NewDocument(NewText("hello\nworld"))
			tt, _ := richTester(t, doc)
			doc.SetSelection(tc.sel[0], tc.sel[1])
			tt.Frame()
			v := doc.shownIn.Value()
			v.SetMarkedText(nil, "ni", ui.TextInputRange{Start: 2, End: 2})
			tt.Frame()
			tc.edit(doc)
			tt.Frame()
			if tc.keep {
				if got, ok := v.MarkedRange(); !ok || got != tc.marked {
					t.Fatalf("marked range = (%+v, %v), want (%+v, true)", got, ok, tc.marked)
				}
				if !v.pendingSet || !v.composing || v.compose != "ni" {
					t.Fatalf("composition lost: pending=%v composing=%v compose=%q", v.pendingSet, v.composing, v.compose)
				}
				if got := v.composedText().String(); got != tc.text {
					t.Fatalf("composed text = %q, want %q", got, tc.text)
				}
				if len(v.paras) != v.composedText().ParagraphCount() {
					t.Fatalf("paras len = %d, want %d", len(v.paras), v.composedText().ParagraphCount())
				}
				v.ReplaceText(nil, "你")
				tt.Frame()
				if got := doc.Text().String(); got != tc.commit {
					t.Fatalf("after commit = %q, want %q", got, tc.commit)
				}
				return
			}
			if _, ok := v.MarkedRange(); ok || v.pendingSet || v.compose != "" || v.composing {
				t.Fatalf("stale composition: compose=%q pending=%v composing=%v", v.compose, v.pendingSet, v.composing)
			}
		})
	}
}

func TestExplicitMarkedRangeSwitchIsOneUndoStep(t *testing.T) {
	doc := NewDocument(NewText("hello world"))
	tt, _ := richTester(t, doc)
	doc.SetSelection(0, 5)
	tt.Frame()
	v := doc.shownIn.Value()
	v.SetMarkedText(nil, "ni", ui.TextInputRange{Start: 2, End: 2})
	r := ui.TextInputRange{Start: 3, End: 8}
	v.SetMarkedText(&r, "hao", ui.TextInputRange{Start: 3, End: 3})
	tt.Type("好")
	if got := doc.Text().String(); got != "ni 好" {
		t.Fatalf("committed %q, want %q", got, "ni 好")
	}
	doc.Undo()
	if got := doc.Text().String(); got != "hello world" {
		t.Fatalf("one undo left %q, want %q", got, "hello world")
	}
	if doc.CanUndo() {
		t.Fatal("explicit range switch made a second undo step")
	}
}

func TestGTKPreeditChangedEmptyThenCommit(t *testing.T) {
	for _, tc := range []struct {
		name      string
		committed string
		want      string
	}{
		{name: "single rune", committed: "你", want: "h你o"},
		{name: "multi-rune commit", committed: "你好", want: "h你好o"},
		{name: "empty commit", committed: "", want: "ho"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := NewDocument(NewText("hello"))
			tt, _ := richTester(t, doc)
			doc.SetSelection(1, 4)
			tt.Frame()
			v := doc.shownIn.Value()
			v.SetMarkedText(nil, "ni", ui.TextInputRange{Start: 2, End: 2})
			v.SetMarkedText(nil, "", ui.TextInputRange{})
			if _, ok := v.MarkedRange(); ok || v.compose != "" {
				t.Fatal("empty preedit left a marked range")
			}
			v.UnmarkText()
			v.ReplaceText(nil, tc.committed)
			tt.Frame()
			if got := doc.Text().String(); got != tc.want {
				t.Fatalf("committed %q, want %q", got, tc.want)
			}
			doc.Undo()
			if got := doc.Text().String(); got != "hello" {
				t.Fatalf("after undo = %q, want %q", got, "hello")
			}
			if doc.CanUndo() {
				t.Fatal("GTK commit made more than one undo step")
			}
		})
	}

	t.Run("caret_after_typing_does_not_coalesce", func(t *testing.T) {
		doc := NewDocument(Text{})
		tt, _ := richTester(t, doc)
		typeRunes(tt, "hello")
		v := doc.shownIn.Value()
		v.SetMarkedText(nil, "ni", ui.TextInputRange{Start: 2, End: 2})
		v.SetMarkedText(nil, "", ui.TextInputRange{})
		v.UnmarkText()
		v.ReplaceText(nil, "你")
		tt.Frame()
		if got := doc.Text().String(); got != "hello你" {
			t.Fatalf("committed %q, want %q", got, "hello你")
		}
		tt.Key(ui.Cmd, ui.KeyZ)
		if got := doc.Text().String(); got != "hello" {
			t.Fatalf("after one undo = %q, want %q", got, "hello")
		}
	})
}

func TestCrossParagraphCompositionMapsDocumentParagraphIndexes(t *testing.T) {
	doc := NewDocument(NewText("one\ntwo\nthree"))
	var spaceIdx, spanIdx []int
	var boxes []ParagraphBox
	tt := openTester(t, func(c *ui.Context) {
		boxes = doc.VisibleParagraphs()
		EditorBase(c, doc, EditorOptions{
			Font: ui.Font{Size: 10},
			Space: func(p int) float32 {
				spaceIdx = append(spaceIdx, p)
				return 0
			},
			Spans: func(p int, _ ParagraphStyle, spans []ui.Span) []ui.Span {
				spanIdx = append(spanIdx, p)
				return spans
			},
		}).Fill()
	}, 300, 200)
	doc.SetSelection(1, 6)
	tt.Frame()
	v := doc.shownIn.Value()

	spaceIdx, spanIdx = nil, nil
	v.SetMarkedText(nil, "ni", ui.TextInputRange{Start: 2, End: 2})
	// Force paragraph 1 ("three") to relayout via Spans/Space while composing.
	v.paras[1] = paraView{}
	v.resetHeights()
	tt.Frame()

	if len(boxes) != 2 || boxes[0].Index != 0 || boxes[1].Index != 2 || boxes[1].Start != 8 {
		t.Fatalf("VisibleParagraphs = %+v, want indexes [0, 2] with second Start=8", boxes)
	}
	hasIndex := func(xs []int, want int) bool {
		for _, x := range xs {
			if x == want {
				return true
			}
		}
		return false
	}
	if !hasIndex(spanIdx, 2) || hasIndex(spanIdx, 1) {
		t.Fatalf("Spans indexes during cross-paragraph composition = %v, want 2 (not 1)", spanIdx)
	}
	if !hasIndex(spaceIdx, 2) || hasIndex(spaceIdx, 1) {
		t.Fatalf("Space indexes during cross-paragraph composition = %v, want 2 (not 1)", spaceIdx)
	}
}

func TestInputTextClearsPendingComposition(t *testing.T) {
	doc := NewDocument(NewText("hello"))
	tt, _ := richTester(t, doc)
	doc.SetSelection(1, 4)
	tt.Frame()
	v := doc.shownIn.Value()
	v.SetMarkedText(nil, "ni", ui.TextInputRange{Start: 2, End: 2})
	tt.Frame()

	if !v.input(ui.InputEvent{Kind: ui.InputText, Text: "x"}) {
		t.Fatal("InputText was not handled")
	}
	if v.compose != "" || v.pendingSet || v.composedActive || v.composing {
		t.Fatalf("InputText left composition state: compose=%q pending=%v active=%v composing=%v",
			v.compose, v.pendingSet, v.composedActive, v.composing)
	}
	if got := doc.Text().String(); got != "hxo" {
		t.Fatalf("text after InputText = %q, want %q", got, "hxo")
	}
	doc.Undo()
	if got := doc.Text().String(); got != "hello" {
		t.Fatalf("after one undo = %q, want %q", got, "hello")
	}
}
