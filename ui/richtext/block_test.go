package richtext

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// blockDoc returns a document with a card between two paragraphs, and the
// number of times the card's element was built.
func blockDoc(build func(c *ui.Context, width float32) ui.Element) (*Document, *int) {
	return blockDocOf(build, "After")
}

// blockDocOf returns a document with a card between two paragraphs, the
// last of them the given text, and the number of times the card's
// element was built.
func blockDocOf(build func(c *ui.Context, width float32) ui.Element, last string) (*Document, *int) {
	return blockDocAt(&Attachment{Name: "Card"}, build, last)
}

// blockDocAt returns a document with the attachment at between two
// paragraphs, the last of them the given text, and the number of times the
// card's element was built.
func blockDocAt(at *Attachment, build func(c *ui.Context, width float32) ui.Element, last string) (*Document, *int) {
	built := new(int)
	at.Element = func(c *ui.Context, width float32) ui.Element {
		*built++
		return build(c, width)
	}
	doc := NewDocument(TextOf(
		para("Before"),
		Paragraph{Runs: []Run{{Text: string(ObjectReplacement), Attributes: Attributes{Attachment: at}}}},
		para(last),
	))
	return doc, built
}

// blockTester builds an editor of doc, and the number of times the
// card's element was built.
func blockTester(t *testing.T, doc *Document) *ui.Tester {
	t.Helper()
	return blockTesterOpts(t, doc, EditorOptions{})
}

// blockTesterOpts builds an editor of doc with the given options.
func blockTesterOpts(t *testing.T, doc *Document, opts EditorOptions) *ui.Tester {
	t.Helper()
	tt := ui.NewTester(func(c *ui.Context) {
		Editor(c, doc, opts).Fill()
	}, 500, 400)
	tt.Frame()
	return tt
}

// The element of a block is built where its paragraph is, and the text
// below it moves as it is high.
func TestBlockElementIsPlaced(t *testing.T) {
	doc, built := blockDoc(func(c *ui.Context, width float32) ui.Element {
		return ui.Box(c).Height(40).Background(ui.RGB(200, 200, 200)).Label("Card")
	})
	tt := blockTester(t, doc)
	if *built == 0 {
		t.Fatal("the element of the block isn't built")
	}
	r, ok := tt.Find("Card")
	if !ok {
		t.Fatal("the element of the block isn't shown")
	}
	if r.W <= 0 || r.H != 40 {
		t.Fatalf("the element of the block is %v", r)
	}
	// The paragraph below it is below the element.
	boxes := doc.VisibleParagraphs()
	if len(boxes) != 3 {
		t.Fatalf("the editor shows %d paragraphs", len(boxes))
	}
	if boxes[2].Top < boxes[1].Top+boxes[1].Height {
		t.Fatalf("the paragraph below the block is at %v, the block at %v of %v", boxes[2].Top, boxes[1].Top, boxes[1].Height)
	}
	if boxes[1].Height != 40 {
		t.Fatalf("the block is %v high, the element is 40", boxes[1].Height)
	}
}

// The element of a block is as high as it is laid out at, and the text
// below it moves in the frame the height changes in.
func TestBlockHeightFollowsTheElement(t *testing.T) {
	height := float32(40)
	doc, _ := blockDoc(func(c *ui.Context, width float32) ui.Element {
		return ui.Box(c).Height(height).Label("Card")
	})
	tt := blockTester(t, doc)
	boxes := doc.VisibleParagraphs()
	if boxes[1].Height != 40 {
		t.Fatalf("the block is %v high", boxes[1].Height)
	}
	// A taller element: the text below it moves at once.
	height = 80
	tt.Frame()
	tt.Frame()
	boxes = doc.VisibleParagraphs()
	if boxes[1].Height != 80 {
		t.Fatalf("the block is %v high, the element is 80", boxes[1].Height)
	}
	if boxes[2].Top < boxes[1].Top+boxes[1].Height {
		t.Fatalf("the paragraph below the block is at %v, the block at %v of %v", boxes[2].Top, boxes[1].Top, boxes[1].Height)
	}
}

// A press on the element of a block goes to it, and the text around it
// takes the rest.
func TestBlockElementTakesPress(t *testing.T) {
	clicks := 0
	doc, _ := blockDoc(func(c *ui.Context, width float32) ui.Element {
		return ui.Box(c).Height(40).Children(func() {
			b := ui.ButtonBase(c).Fill().Label("Press me")
			if b.Clicked() {
				clicks++
			}
		})
	})
	tt := blockTester(t, doc)
	if err := tt.Click("Press me"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if clicks != 1 {
		t.Fatalf("clicked %d times", clicks)
	}
	// A press on the empty room of the block goes to the text.
	r, ok := tt.Find("Press me")
	if !ok {
		t.Fatal("the button isn't built")
	}
	tt.ClickAt(r.X-20, r.Y+r.H/2)
	tt.Frame()
	_, caret := doc.Selection()
	if caret != 7 {
		t.Fatalf("the caret is at %d, the attachment is at 7", caret)
	}
}

// The caret on the attachment and the Return key give the element of the
// block the focus, and the Escape brings the text back.
func TestBlockFocusRoundTrip(t *testing.T) {
	focused := false
	doc, _ := blockDoc(func(c *ui.Context, width float32) ui.Element {
		e := ui.Box(c).Height(40).Focusable().Label("Card")
		focused = e.Focused()
		return e
	})
	tt := blockTester(t, doc)
	// The editor has the focus, and the caret is on the attachment.
	tt.Press(20, 20)
	tt.Release(20, 20)
	doc.SetSelection(7, 7)
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if !focused {
		t.Fatal("the element of the block does not have the focus")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if focused {
		t.Fatal("the element of the block still has the focus")
	}
	_, caret := doc.Selection()
	if caret != 7 {
		t.Fatalf("the caret is at %d, the attachment is at 7", caret)
	}
}

// The app is asked about a key before the editor moves the focus into a
// block, so that it may take the key itself.
func TestBlockOnKeyFirst(t *testing.T) {
	focused := false
	doc, _ := blockDoc(func(c *ui.Context, width float32) ui.Element {
		e := ui.Box(c).Height(40).Focusable().Label("Card")
		focused = e.Focused()
		return e
	})
	tt := blockTesterOpts(t, doc, EditorOptions{OnKey: func(k ui.Key, _ ui.Modifiers) bool {
		return k == ui.KeyEnter
	}})
	// The editor has the focus, and the caret is on the attachment.
	tt.Press(20, 20)
	tt.Release(20, 20)
	doc.SetSelection(7, 7)
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if focused {
		t.Fatal("the element of the block has the focus, and the app took the key")
	}
}

// The wheel over the element of a block scrolls the text, as it does over
// it.
func TestBlockWheelScrollsTheText(t *testing.T) {
	doc, _ := blockDocOf(func(c *ui.Context, width float32) ui.Element {
		return ui.Box(c).Height(40).Label("Card")
	}, strings.Repeat("more\n", 200))
	tt := blockTester(t, doc)
	// The text is scrolled to its top, where the block is.
	doc.ScrollBy(-10000)
	tt.Frame()
	tt.Frame()
	r, ok := tt.Find("Card")
	if !ok {
		t.Fatal("the element of the block isn't shown")
	}
	before := doc.VisibleParagraphs()
	tt.Wheel(r.X+r.W/2, r.Y+r.H/2, 0, 120)
	tt.Frame()
	after := doc.VisibleParagraphs()
	if len(before) == 0 || len(after) == 0 || after[0].Index <= before[0].Index {
		t.Fatalf("the text did not scroll: %d then %d", before[0].Index, after[0].Index)
	}
}

// The wheel over the element of a block that the editor cannot scroll goes
// on to the containers around it.
func TestBlockWheelGoesOn(t *testing.T) {
	doc, _ := blockDocOf(func(c *ui.Context, width float32) ui.Element {
		return ui.Box(c).Height(40).Label("Card")
	}, strings.Repeat("more\n", 20))
	var scroll ui.ScrollState
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Scroll(c).Height(200).TrackScroll(&scroll).Children(func() {
			Editor(c, doc).Height(600)
		})
	}, 500, 400)
	tt.Frame()
	r, ok := tt.Find("Card")
	if !ok {
		t.Fatal("the element of the block isn't shown")
	}
	tt.Wheel(r.X+r.W/2, r.Y+r.H/2, 0, 120)
	tt.Advance(time.Second)
	tt.Frame()
	if scroll.Y == 0 {
		t.Fatal("the wheel over the block did not scroll the container around it")
	}
}

// Only the blocks in view are built, and the text keeps the height of a
// block that is not.
func TestBlockOnlyInView(t *testing.T) {
	built := 0
	doc, _ := blockDocOf(func(c *ui.Context, width float32) ui.Element {
		built++
		return ui.Box(c).Height(40).Label("Card")
	}, strings.Repeat("more\n", 500))
	tt := blockTester(t, doc)
	// The text is scrolled to its top, where the block is.
	doc.ScrollBy(-10000)
	tt.Frame()
	tt.Frame()
	if built == 0 {
		t.Fatal("the element of the block isn't built")
	}
	// Scrolled away, the element is not built, and the paragraph keeps
	// the height it was laid out at.
	built = 0
	doc.ScrollBy(1000)
	tt.Frame()
	tt.Frame()
	if built != 0 {
		t.Fatalf("the element of the block was built %d times out of view", built)
	}
	// Scrolled back, it is built again, and the paragraph keeps the
	// height the element was laid out at.
	doc.ScrollBy(-1000)
	tt.Frame()
	tt.Frame()
	boxes := doc.VisibleParagraphs()
	if len(boxes) < 2 || boxes[1].Height != 40 {
		t.Fatalf("the block is %v high, the element is 40", boxes)
	}
}

// A block that asks for it is built whether it is in view or not, and the
// text keeps its place when the element changes height out of view.
func TestBlockKeptOutOfView(t *testing.T) {
	built := 0
	height := float32(40)
	doc, _ := blockDocAt(&Attachment{Name: "Card", KeepBuilt: true}, func(c *ui.Context, width float32) ui.Element {
		built++
		return ui.Box(c).Height(height).Label("Card")
	}, strings.Repeat("more\n", 500))
	tt := blockTester(t, doc)
	// The text is scrolled to its top, where the block is.
	doc.ScrollBy(-10000)
	tt.Frame()
	tt.Frame()
	if built == 0 {
		t.Fatal("the element of the block isn't built")
	}
	// Scrolled away, it is built still.
	built = 0
	doc.ScrollBy(1000)
	tt.Frame()
	tt.Frame()
	if built == 0 {
		t.Fatal("the element of the block isn't built out of view")
	}
	// The element grows out of view: the text keeps its place.
	before := doc.VisibleParagraphs()
	height = 80
	tt.Frame()
	tt.Frame()
	after := doc.VisibleParagraphs()
	if len(before) == 0 || len(after) == 0 {
		t.Fatalf("the editor shows %d paragraphs, then %d", len(before), len(after))
	}
	if after[0].Top != before[0].Top {
		t.Fatalf("the element grew out of view and the text moved from %v to %v", before[0].Top, after[0].Top)
	}
}

// Assistive technology reads the text of the editor, and sees the
// element of a block in it.
func TestBlockAccessible(t *testing.T) {
	doc, _ := blockDoc(func(c *ui.Context, width float32) ui.Element {
		return ui.Box(c).Height(40).Label("Card")
	})
	tt := blockTester(t, doc)
	if _, ok := tt.AccessByRole(ui.RoleGroup, "Card"); !ok {
		t.Fatal("the element of the block isn't seen")
	}
	field, ok := tt.AccessByRole(ui.RoleTextField, "")
	if !ok || field.Value == "" {
		t.Fatalf("the field reads %+v", field)
	}
}
