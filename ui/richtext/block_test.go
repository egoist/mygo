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

// The Tab on the attachment gives the focus to the element of the card,
// and the Return after it makes a new paragraph instead.
func TestBlockTabAndReturnAfterTheAttachment(t *testing.T) {
	focused := false
	doc, _ := blockDoc(func(c *ui.Context, width float32) ui.Element {
		e := ui.Box(c).Height(40).Focusable().Label("Card")
		focused = e.Focused()
		return e
	})
	tt := blockTester(t, doc)
	tt.Press(20, 20)
	tt.Release(20, 20)
	// The caret is on the attachment: the Tab gives the card the focus.
	doc.SetSelection(7, 7)
	tt.Frame()
	tt.Key(0, ui.KeyTab)
	tt.Frame()
	if !focused {
		t.Fatal("the element of the card does not have the focus")
	}
}

// The caret steps over the attachment of a card, and the Return after it
// makes a new paragraph: the card does not take the focus.
func TestBlockCaretStepsOverTheAttachment(t *testing.T) {
	focused := false
	doc, _ := blockDoc(func(c *ui.Context, width float32) ui.Element {
		e := ui.Box(c).Height(40).Focusable().Label("Card")
		focused = e.Focused()
		return e
	})
	tt := blockTester(t, doc)
	tt.Press(20, 20)
	tt.Release(20, 20)
	doc.SetSelection(6, 6)
	tt.Frame()
	// Right: the caret is on the attachment.
	tt.Key(0, ui.KeyRight)
	tt.Frame()
	if _, caret := doc.Selection(); caret != 7 {
		t.Fatalf("the caret is at %d, the attachment is at 7", caret)
	}
	// Right again: the caret is after it.
	tt.Key(0, ui.KeyRight)
	tt.Frame()
	if _, caret := doc.Selection(); caret != 8 {
		t.Fatalf("the caret is at %d, after the attachment is 8", caret)
	}
	// The Return there makes a new paragraph, and the card does not take
	// the focus.
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if focused {
		t.Fatal("the element of the card has the focus")
	}
	if n := doc.Text().ParagraphCount(); n != 4 {
		t.Fatalf("the text has %d paragraphs, a new one was not made", n)
	}
}

// The Return with a selection in the card's paragraph is the text's: it
// takes the selection away.
func TestBlockReturnWithASelection(t *testing.T) {
	focused := false
	doc, _ := blockDoc(func(c *ui.Context, width float32) ui.Element {
		e := ui.Box(c).Height(40).Focusable().Label("Card")
		focused = e.Focused()
		return e
	})
	tt := blockTester(t, doc)
	tt.Press(20, 20)
	tt.Release(20, 20)
	// The selection is in the card's paragraph, over the attachment.
	doc.SetSelection(8, 7)
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if focused {
		t.Fatal("the element of the card has the focus")
	}
	if strings.ContainsRune(doc.Text().String(), ObjectReplacement) {
		t.Fatal("Return did not replace the selected attachment")
	}
}

// Undoing the edit that took the card away brings it back, and the
// element of the card is built again.
func TestBlockUndo(t *testing.T) {
	built := 0
	doc, _ := blockDoc(func(c *ui.Context, width float32) ui.Element {
		built++
		return ui.Box(c).Height(40).Label("Card")
	})
	tt := blockTester(t, doc)
	tt.Press(20, 20)
	tt.Release(20, 20)
	// The caret is on the attachment: the Backspace takes the paragraph
	// break away, and the card is not a card any more.
	doc.SetSelection(7, 7)
	tt.Frame()
	tt.Key(0, ui.KeyBackspace)
	tt.Frame()
	built = 0
	tt.Frame()
	if built != 0 {
		t.Fatalf("the element of the card is built %d times, the card is gone", built)
	}
	// Undoing brings the card back.
	tt.Key(ui.Cmd, ui.KeyZ)
	tt.Frame()
	tt.Frame()
	if built == 0 {
		t.Fatal("the element of the card is not built after the undo")
	}
	if _, ok := tt.Find("Card"); !ok {
		t.Fatal("the card is not shown after the undo")
	}
}

// A card that deletes its own paragraph leaves the text whole: the focus
// it had goes back to the text, and the keys go on reaching it.
func TestBlockDeletesItsParagraph(t *testing.T) {
	var doc *Document
	doc, _ = blockDoc(func(c *ui.Context, width float32) ui.Element {
		return ui.Box(c).Height(40).Children(func() {
			b := ui.ButtonBase(c).Fill().Label("Delete")
			if b.Clicked() {
				doc.Edit(func(e *Edit) { e.ReplaceString(7, 8, "", Attributes{}) })
			}
		})
	})
	tt := blockTester(t, doc)
	// The caret is on the attachment, and the card takes the focus.
	tt.Press(20, 20)
	tt.Release(20, 20)
	doc.SetSelection(7, 7)
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	// The card deletes its own paragraph.
	if err := tt.Click("Delete"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if doc.Text().ParagraphCount() != 3 {
		t.Fatalf("the text has %d paragraphs", doc.Text().ParagraphCount())
	}
	// The Escape does not bring the focus back to a paragraph that is
	// gone, and the text takes the keys again.
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	tt.Type("x")
	tt.Frame()
	if !strings.Contains(doc.Text().String(), "x") {
		t.Fatalf("the text is %q", doc.Text().String())
	}
}

// A card that is not laid out yet is as high as its attachment says.
func TestBlockHeightFallback(t *testing.T) {
	at := &Attachment{Name: "Card", Height: 60}
	doc, _ := blockDocAt(at, func(c *ui.Context, width float32) ui.Element {
		return ui.Box(c).Label("Card")
	}, "After")
	tt := blockTester(t, doc)
	tt.Frame()
	boxes := doc.VisibleParagraphs()
	if len(boxes) != 3 {
		t.Fatalf("the editor shows %d paragraphs", len(boxes))
	}
	if boxes[1].Height != 60 {
		t.Fatalf("the block is %v high, the attachment says 60", boxes[1].Height)
	}
}

// A card above the caret that grows while the text is scrolled keeps the
// caret's paragraph where it is.
func TestBlockAboveTheCaretResizing(t *testing.T) {
	height := float32(40)
	doc, _ := blockDocAt(&Attachment{Name: "Card", KeepBuilt: true}, func(c *ui.Context, width float32) ui.Element {
		return ui.Box(c).Height(height).Label("Card")
	}, strings.Repeat("more\n", 100))
	tt := blockTester(t, doc)
	// Put the caret in the text below the card and scroll to it.
	doc.SetSelection(60, 60)
	tt.Frame()
	doc.ScrollToParagraph(10, ui.ScrollStart)
	tt.Frame()
	tt.Frame()
	before := doc.VisibleParagraphs()
	if len(before) == 0 {
		t.Fatal("the editor shows no paragraph")
	}
	// The card grows: the text below it moves, and the caret's paragraph
	// with it.
	height = 80
	tt.Frame()
	tt.Frame()
	after := doc.VisibleParagraphs()
	if len(after) == 0 {
		t.Fatal("the editor shows no paragraph")
	}
	if after[0].Top != before[0].Top {
		t.Fatalf("the text moved from %v to %v", before[0].Top, after[0].Top)
	}
}

// The Escape in a field of a card is the field's: the editor does not
// take the focus back from it.
// A control in a card sees the Escape it pressed, and the text has the
// keys again after it: a card that holds a button, which the keys do
// nothing in, gives them back rather than keeping them.
func TestBlockEscapeInAControl(t *testing.T) {
	escaped := false
	focused := false
	doc, _ := blockDoc(func(c *ui.Context, width float32) ui.Element {
		return ui.Box(c).Height(40).Children(func() {
			e := ui.Box(c).Fill().Focusable().Label("Field").HandleInput(func(ev ui.InputEvent) bool {
				if ev.Kind == ui.InputKeyDown && ev.Key == ui.KeyEscape {
					escaped = true
					return true
				}
				return false
			})
			focused = e.Focused()
		})
	})
	tt := blockTester(t, doc)
	// The control in the card has the focus.
	r, ok := tt.Find("Field")
	if !ok {
		t.Fatal("the control is not shown")
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	if !focused {
		t.Fatal("the control does not have the focus")
	}
	// The Escape is the control's to use, and the text has the keys after
	// it.
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if !escaped {
		t.Fatal("the control did not see the Escape")
	}
	if focused {
		t.Fatal("the editor did not take the focus back from the control")
	}
	tt.Type("x")
	tt.Frame()
	if got, want := doc.Text().String(), "Before\nx\ufffc\nAfter"; got != want {
		t.Fatalf("the keys went nowhere after the Escape: %q", got)
	}
}

// The Escape pressed on a button in a card gives the keys back to the
// text: a button does nothing with them, and the text has them again.
func TestBlockEscapeOnAButton(t *testing.T) {
	focused := false
	doc, _ := blockDoc(func(c *ui.Context, width float32) ui.Element {
		return ui.Box(c).Height(40).Children(func() {
			b := ui.ButtonBase(c).Fill().Label("Press me")
			focused = b.Focused()
		})
	})
	tt := blockTester(t, doc)
	r, ok := tt.Find("Press me")
	if !ok {
		t.Fatal("the button is not shown")
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	if !focused {
		t.Fatal("the button does not have the focus")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if focused {
		t.Fatal("the button still has the focus")
	}
	_, caret := doc.Selection()
	if caret != 7 {
		t.Fatalf("the caret is at %d, the attachment is at 7", caret)
	}
	tt.Type("x")
	tt.Frame()
	if got, want := doc.Text().String(), "Before\nx\ufffc\nAfter"; got != want {
		t.Fatalf("the keys went nowhere after the Escape: %q", got)
	}
}

// A press on a card that is partly above the view does not scroll the
// text to the caret.
func TestBlockPressDoesNotScroll(t *testing.T) {
	doc, _ := blockDocOf(func(c *ui.Context, width float32) ui.Element {
		return ui.Box(c).Height(40).Children(func() {
			ui.ButtonBase(c).Fill().Label("Press me")
		})
	}, strings.Repeat("more\n", 100))
	tt := blockTester(t, doc)
	// The card is partly above the view.
	doc.ScrollBy(-10000)
	tt.Frame()
	doc.ScrollBy(40)
	tt.Frame()
	tt.Frame()
	r, ok := tt.Find("Press me")
	if !ok {
		t.Fatal("the button is not shown")
	}
	before := doc.VisibleParagraphs()
	if len(before) == 0 {
		t.Fatal("the editor shows no paragraph")
	}
	// The press on the button does not scroll the text.
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	tt.Frame()
	after := doc.VisibleParagraphs()
	if len(after) == 0 {
		t.Fatal("the editor shows no paragraph")
	}
	if after[0].Top != before[0].Top {
		t.Fatalf("the text moved from %v to %v", before[0].Top, after[0].Top)
	}
}

// The cards that are kept built out of view are built in the order of the
// text, so that Tab and the screen readers go through them as they are
// in it.
func TestBlockKeptInTextOrder(t *testing.T) {
	var order []int
	card := func(n int) Paragraph {
		return Paragraph{Runs: []Run{{Text: string(ObjectReplacement), Attributes: Attributes{Attachment: &Attachment{
			Name:      "Card",
			KeepBuilt: true,
			Element: func(c *ui.Context, width float32) ui.Element {
				order = append(order, n)
				return ui.Box(c).Height(40)
			},
		}}}}}
	}
	paras := []Paragraph{para("Before"), card(1)}
	for range 50 {
		paras = append(paras, para("more"))
	}
	paras = append(paras, card(2), para("After"))
	doc := NewDocument(TextOf(paras...))
	tt := blockTester(t, doc)
	// The text is scrolled to the second card, and the first is out of
	// view, but kept built.
	doc.ScrollToParagraph(52, ui.ScrollStart)
	tt.Frame()
	tt.Frame()
	order = nil
	tt.Frame()
	if len(order) < 2 {
		t.Fatalf("the cards were built %v", order)
	}
	if order[0] != 1 || order[1] != 2 {
		t.Fatalf("the cards were built in the order %v, the text's is 1 then 2", order)
	}
}

// An editor as tall as its text, in a scroll container, builds the cards
// the clip shows alone.
func TestBlockClippedToTheClip(t *testing.T) {
	const cards = 40
	built := 0
	doc := NewDocument(TextOf(cardParagraphs(cards, &built)...))
	var scroll ui.ScrollState
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Scroll(c).Height(200).TrackScroll(&scroll).Children(func() {
			Editor(c, doc).Height(2000)
		})
	}, 500, 400)
	tt.Frame()
	tt.Frame()
	built = 0
	tt.Frame()
	if built == 0 {
		t.Fatal("no card is built")
	}
	if built*2 > cards {
		t.Fatalf("%d cards are built of %d, the clip shows a few", built, cards)
	}
}

// cardParagraphs returns the paragraphs of the cards, and counts how many
// of them are built.
func cardParagraphs(n int, built *int) []Paragraph {
	paras := make([]Paragraph, 0, n)
	for range n {
		paras = append(paras, Paragraph{Runs: []Run{{Text: string(ObjectReplacement), Attributes: Attributes{Attachment: &Attachment{
			Name: "Card",
			Element: func(c *ui.Context, width float32) ui.Element {
				*built++
				return ui.Box(c).Height(40)
			},
		}}}}})
	}
	return paras
}
