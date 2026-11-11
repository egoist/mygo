package richtext

import (
	"fmt"
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

// longDoc returns a document of n paragraphs, each long enough to wrap in
// a narrow editor.
func longDoc(n int) *Document {
	paras := make([]Paragraph, n)
	for i := range paras {
		paras[i] = para(fmt.Sprintf("Paragraph %d, long enough to wrap in a window this narrow at times.", i))
	}
	return NewDocument(TextOf(paras...))
}

// boxOf returns the box of paragraph i among boxes.
func boxOf(t *testing.T, boxes []ParagraphBox, i int) ParagraphBox {
	t.Helper()
	for _, b := range boxes {
		if b.Index == i {
			return b
		}
	}
	t.Fatalf("paragraph %d is not in view: %v", i, boxes)
	return ParagraphBox{}
}

// near reports whether a and b are the same to a hundredth of a DIP.
func near(a, b float32) bool { return math.Abs(float64(a-b)) < 0.01 }

// ScrollBy scrolls the editor by as much as asked, as far as its text
// goes, and reports whether it moved; before an editor showed the
// document, it does nothing.
func TestScrollBy(t *testing.T) {
	doc := longDoc(200)
	if doc.ScrollBy(100) {
		t.Fatal("no editor shows the document, and it scrolled")
	}
	tt, boxes := proseTester(t, doc, &Theme{ParagraphSpacing: 0.5}, 400)
	doc.SetSelection(0, 0)
	tt.Frame()
	if (*boxes)[0].Index != 0 || (*boxes)[0].Top != 0 {
		t.Fatalf("the first frame shows %v", (*boxes)[0])
	}
	// A paragraph that stays in view moves up by as much.
	b := (*boxes)[len(*boxes)/2]
	if !doc.ScrollBy(b.Top / 2) {
		t.Fatal("ScrollBy did not scroll")
	}
	if !tt.FramePending() {
		t.Fatal("ScrollBy asked for no frame")
	}
	tt.Frame()
	if got := boxOf(t, *boxes, b.Index); !near(got.Top, b.Top/2) {
		t.Fatalf("scrolled by %v, paragraph %d went from %v to %v", b.Top/2, b.Index, b.Top, got.Top)
	}
	// The wheel goes as far as the text does, and so does ScrollBy.
	if !doc.ScrollBy(1e9) {
		t.Fatal("ScrollBy to the end did not scroll")
	}
	tt.Frame()
	last := (*boxes)[len(*boxes)-1]
	if last.Index != 199 || !near(last.Top+last.Height, 400) {
		t.Fatalf("at the end, the last box is %+v", last)
	}
	if doc.ScrollBy(10) {
		t.Fatal("ScrollBy scrolled past the end")
	}
	if !doc.ScrollBy(-1e9) {
		t.Fatal("ScrollBy to the start did not scroll")
	}
	tt.Frame()
	if first := (*boxes)[0]; first.Index != 0 || first.Top != 0 {
		t.Fatalf("at the start, the first box is %+v", first)
	}
	if doc.ScrollBy(-10) {
		t.Fatal("ScrollBy scrolled past the start")
	}
	// Frames that change nothing ask for none more.
	tt.Frame()
	if tt.FramePending() {
		t.Fatal("the frames do not settle")
	}
}

// ScrollToParagraph lines a paragraph's lines up with the editor's top,
// middle or bottom, or scrolls as little as shows them, as far as the
// text goes.
func TestScrollToParagraph(t *testing.T) {
	doc := longDoc(200)
	doc.ScrollToParagraph(100, ui.ScrollStart) // no editor yet: nothing
	tt, boxes := proseTester(t, doc, &Theme{ParagraphSpacing: 0.5}, 400)
	doc.SetSelection(0, 0)
	tt.Frame()
	to := func(i int, align ui.ScrollAlign) ParagraphBox {
		t.Helper()
		doc.ScrollToParagraph(i, align)
		tt.Frame()
		return boxOf(t, *boxes, i)
	}
	if b := to(100, ui.ScrollStart); !near(b.Top, 0) {
		t.Fatalf("paragraph 100 at the start: %+v", b)
	}
	if b := to(150, ui.ScrollCenter); !near(b.Top+b.Height/2, 200) {
		t.Fatalf("paragraph 150 in the middle: %+v", b)
	}
	if b := to(60, ui.ScrollEnd); !near(b.Top+b.Height, 400) {
		t.Fatalf("paragraph 60 at the end: %+v", b)
	}
	// Nearest: in view, nothing moves; below, it comes up to the bottom;
	// above, down to the top.
	in := (*boxes)[len(*boxes)/2]
	if b := to(in.Index, ui.ScrollNearest); b != in {
		t.Fatalf("paragraph %d in view went from %+v to %+v", in.Index, in, b)
	}
	if b := to(90, ui.ScrollNearest); !near(b.Top+b.Height, 400) {
		t.Fatalf("paragraph 90 below, nearest: %+v", b)
	}
	if b := to(20, ui.ScrollNearest); !near(b.Top, 0) {
		t.Fatalf("paragraph 20 above, nearest: %+v", b)
	}
	// Past the ends: the first and the last paragraphs, as far as the text
	// goes.
	doc.ScrollToParagraph(-5, ui.ScrollStart)
	tt.Frame()
	if b := (*boxes)[0]; b.Index != 0 || b.Top != 0 {
		t.Fatalf("paragraph -5: %+v", b)
	}
	doc.ScrollToParagraph(1000, ui.ScrollStart)
	tt.Frame()
	if b := (*boxes)[len(*boxes)-1]; b.Index != 199 || !near(b.Top+b.Height, 400) {
		t.Fatalf("paragraph 1000: %+v", b)
	}
	defer func() {
		if recover() == nil {
			t.Error("an alignment out of range did not panic")
		}
	}()
	doc.ScrollToParagraph(0, ui.ScrollAlign(9))
}

// anchorTester builds an EditorBase of doc 300×400 that leaves room
// above paragraph *at of *room DIPs, the boxes it reads as it builds, and
// whether to focus it. It scrolls to paragraph 50 and puts the caret at
// the start of the paragraph in the middle of the view.
func anchorTester(t *testing.T, doc *Document, focus bool) (tt *ui.Tester, boxes *[]ParagraphBox, at *int, room *float32) {
	t.Helper()
	boxes, at, room = new([]ParagraphBox), new(int), new(float32)
	tt = ui.NewTester(func(c *ui.Context) {
		*boxes = doc.VisibleParagraphs()
		EditorBase(c, doc, EditorOptions{Font: ui.Font{Size: 10}, Space: func(p int) float32 {
			if p == *at {
				return *room
			}
			return 0
		}}).Fill()
	}, 300, 400)
	if focus {
		tt.ClickAt(150, 5)
		if !tt.Focused("Rich text") {
			t.Fatal("the editor did not take the focus")
		}
	}
	doc.ScrollToParagraph(50, ui.ScrollStart)
	tt.Frame()
	mid := (*boxes)[len(*boxes)/2]
	doc.SetSelection(mid.Start, mid.Start)
	tt.Frame()
	if (*boxes)[0].Index != 50 {
		t.Fatalf("the view starts at paragraph %d", (*boxes)[0].Index)
	}
	return tt, boxes, at, room
}

// caretRect returns where the editor shows the caret, which is in view.
func caretRect(t *testing.T, doc *Document) ui.Rect {
	t.Helper()
	r, ok := doc.CaretRect()
	if !ok {
		t.Fatal("the caret is out of view")
	}
	return r
}

// While the editor has the keyboard, what grows between the top of the
// view and the caret moves the text above it up, and the line being typed
// stays, as browsers keep the focused editable element in place.
func TestScrollAnchorCaret(t *testing.T) {
	doc := longDoc(200)
	tt, boxes, at, room := anchorTester(t, doc, true)
	caret := caretRect(t, doc)
	first, second := (*boxes)[0], (*boxes)[1]
	_, at0 := doc.Selection()
	cp := doc.Text().paraAt(at0)

	// Room opens above a paragraph between the top of the view and the
	// caret's.
	*at, *room = first.Index+2, 40
	tt.Frame()
	if got := caretRect(t, doc); got != caret {
		t.Fatalf("room above paragraph %d moved the caret from %v to %v", *at, caret, got)
	}
	if got := boxOf(t, *boxes, second.Index); !near(got.Top, second.Top-40) {
		t.Fatalf("room above paragraph %d: paragraph %d went from %v to %v, want 40 up", *at, second.Index, second.Top, got.Top)
	}
	// It grows, and goes.
	*room = 90
	tt.Frame()
	if got := caretRect(t, doc); got != caret {
		t.Fatalf("more room moved the caret from %v to %v", caret, got)
	}
	*room = 0
	tt.Frame()
	if got := caretRect(t, doc); got != caret {
		t.Fatalf("the room gone moved the caret from %v to %v", caret, got)
	}
	// Room above the caret's own paragraph keeps its line too.
	*at, *room = cp, 30
	tt.Frame()
	if got := caretRect(t, doc); got != caret {
		t.Fatalf("room above the caret's paragraph moved the caret from %v to %v", caret, got)
	}
	// Room below the caret moves nothing above it.
	*at = cp + 2
	tt.Frame()
	if got := caretRect(t, doc); got != caret {
		t.Fatalf("room below the caret moved it from %v to %v", caret, got)
	}
	// Typing keeps its line, and so does an edit above it in view.
	tt.Type("abc")
	typed := caretRect(t, doc)
	if typed.Y != caret.Y || typed.X <= caret.X {
		t.Fatalf("typed from %v, the caret is at %v", caret, typed)
	}
	start := (*boxes)[1].Start
	doc.Edit(func(e *Edit) { e.ReplaceString(start, start, "Inserted\n", Attributes{}) })
	tt.Frame()
	if got := caretRect(t, doc); got.Y != typed.Y {
		t.Fatalf("a paragraph inserted above moved the caret from %v to %v", typed, got)
	}
	// Joining the caret's paragraph to the one above keeps the view.
	before := (*boxes)[0]
	tt.Key(ui.Cmd, ui.KeyLeft)
	tt.Key(0, ui.KeyBackspace)
	if got := boxOf(t, *boxes, before.Index); got.Top != before.Top {
		t.Fatalf("joining paragraphs moved the first in view from %+v to %+v", before, got)
	}
	// Frames that change nothing ask for none more.
	tt.Frame()
	if tt.FramePending() {
		t.Fatal("the frames do not settle")
	}
}

// Without the keyboard, the editor keeps its place by the first paragraph
// in view, as before: what grows above the caret moves the caret down.
func TestScrollAnchorUnfocused(t *testing.T) {
	doc := longDoc(200)
	tt, boxes, at, room := anchorTester(t, doc, false)
	if tt.Focused("Rich text") {
		t.Fatal("the editor has the focus")
	}
	caret := caretRect(t, doc)
	first := (*boxes)[0]
	*at, *room = first.Index+2, 40
	tt.Frame()
	if got := caretRect(t, doc); !near(got.Y, caret.Y+40) {
		t.Fatalf("room above paragraph %d moved the caret from %v to %v, want 40 down", *at, caret.Y, got.Y)
	}
	if got := (*boxes)[0]; got != first {
		t.Fatalf("the first paragraph in view went from %+v to %+v", first, got)
	}
}

// An editor scrolled to its top stays there while focused, as one that
// shows all of its text does: room above the caret moves it down.
func TestScrollAnchorAtTop(t *testing.T) {
	doc := longDoc(200)
	var boxes []ParagraphBox
	var room float32
	tt := ui.NewTester(func(c *ui.Context) {
		boxes = doc.VisibleParagraphs()
		EditorBase(c, doc, EditorOptions{Font: ui.Font{Size: 10}, Space: func(p int) float32 {
			if p == 1 {
				return room
			}
			return 0
		}}).Fill()
	}, 300, 400)
	doc.SetSelection(0, 0)
	tt.Frame()
	tt.ClickAt(150, 200)
	if !tt.Focused("Rich text") {
		t.Fatal("the editor did not take the focus")
	}
	caret := caretRect(t, doc)
	room = 40
	tt.Frame()
	if got := caretRect(t, doc); !near(got.Y, caret.Y+40) {
		t.Fatalf("at the top, room above the caret moved it from %v to %v, want 40 down", caret.Y, got.Y)
	}
	if first := boxes[0]; first.Index != 0 || first.Top != 0 {
		t.Fatalf("at the top, the first box is %+v", first)
	}
}
