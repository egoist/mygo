package richtext

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

// about reports whether two lengths in DIPs are the same within a pixel's
// rounding.
func about(a, b float32) bool { return math.Abs(float64(a-b)) < 0.6 }

// A heading's LineHeight sets the height of its lines in ems of its size,
// on each line it wraps to; without it a heading takes the text's.
func TestHeadingLineHeight(t *testing.T) {
	doc := NewDocument(TextOf(
		Paragraph{Style: ParagraphStyle{Heading: 1}, Runs: []Run{{Text: "Title"}}},
		Paragraph{Style: ParagraphStyle{Heading: 2}, Runs: []Run{{Text: "wraps wraps wraps wraps wraps wraps wraps wraps wraps"}}},
		Paragraph{Style: ParagraphStyle{Heading: 3}, Runs: []Run{{Text: "Text's"}}},
		Paragraph{Runs: []Run{{Text: "body"}}},
	))
	th := &Theme{Headings: [6]HeadingStyle{
		{Size: 24.0 / 15, LineHeight: 30.0 / 24},
		{Size: 19.0 / 15, LineHeight: 26.0 / 19},
		{Size: 16.0 / 15},
	}}
	tt := ui.NewTester(func(c *ui.Context) {
		EditorBase(c, doc, EditorOptions{Font: ui.Font{Size: 15, LineHeight: 22.0 / 15}, Theme: th}).Fill()
	}, 300, 400)
	tt.Frame()
	boxes := doc.VisibleParagraphs()
	if len(boxes) != 4 {
		t.Fatalf("paragraphs in view: %+v", boxes)
	}
	if h := boxes[0].Height; !about(h, 30) {
		t.Errorf("a heading of 24 with lines of 30: %v high", h)
	}
	if h := boxes[1].Height; !about(h, 52) {
		t.Errorf("a heading of 19 with lines of 26, on two lines: %v high", h)
	}
	if h := boxes[2].Height; !about(h, 16*22.0/15) {
		t.Errorf("a heading without LineHeight takes the text's, %v: %v high", 16*22.0/15, h)
	}
	if h := boxes[3].Height; !about(h, 22) {
		t.Errorf("body text %v high", h)
	}
}

// Space less than zero takes room away from the theme's, down to none:
// a paragraph never runs into the one above.
func TestSpaceTakesAway(t *testing.T) {
	doc := NewDocument(NewText("one\ntwo\nthree"))
	var room float32
	tt := ui.NewTester(func(c *ui.Context) {
		EditorBase(c, doc, EditorOptions{Font: ui.Font{Size: 15, LineHeight: 22.0 / 15}, Theme: &Theme{ParagraphSpacing: 8.0 / 15},
			Space: func(p int) float32 {
				if p == 1 {
					return room
				}
				return 0
			}}).Fill()
	}, 300, 200)
	gap := func() float32 {
		tt.Frame()
		tt.Frame()
		b := doc.VisibleParagraphs()
		return b[1].Top - (b[0].Top + b[0].Height)
	}
	for _, c := range []struct{ room, want float32 }{{0, 8}, {12, 20}, {-3, 5}, {-8, 0}, {-40, 0}} {
		room = c.room
		if g := gap(); !about(g, c.want) {
			t.Errorf("Space %v: %v between the paragraphs, want %v", c.room, g, c.want)
		}
	}
	b := doc.VisibleParagraphs()
	if g := b[2].Top - (b[1].Top + b[1].Height); !about(g, 8) {
		t.Errorf("the paragraph after: %v above it", g)
	}
}

// Above a block of code, Space less than zero takes room away from the
// block's padding at its top, inside its background, down to none.
func TestSpaceTakesAwayCodePadding(t *testing.T) {
	doc := NewDocument(TextOf(
		para("before"),
		Paragraph{Style: ParagraphStyle{CodeBlock: true}, Runs: []Run{{Text: "code"}}},
	))
	var room float32
	tt := ui.NewTester(func(c *ui.Context) {
		EditorBase(c, doc, EditorOptions{Font: ui.Font{Size: 15, LineHeight: 22.0 / 15}, Theme: &Theme{CodeBlockPadding: [2]float32{12.0 / 15, 10.0 / 15}},
			Space: func(p int) float32 {
				if p == 1 {
					return room
				}
				return 0
			}}).Fill()
	}, 300, 200)
	gap := func() float32 {
		tt.Frame()
		tt.Frame()
		b := doc.VisibleParagraphs()
		return b[1].Top - (b[0].Top + b[0].Height)
	}
	for _, c := range []struct{ room, want float32 }{{0, 10}, {-4, 6}, {-10, 0}, {-30, 0}} {
		room = c.room
		if g := gap(); !about(g, c.want) {
			t.Errorf("Space %v above a block of code: %v between the paragraphs, want %v", c.room, g, c.want)
		}
	}
}
