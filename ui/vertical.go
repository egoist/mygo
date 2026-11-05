package ui

import (
	"math"
	"sync"

	"github.com/egoist/mygo/internal/scene"
	"github.com/egoist/mygo/internal/text"
)

// WritingMode is the direction the lines of a text run in, and the one
// they stack in, as CSS's writing-mode.
type WritingMode uint8

const (
	// HorizontalTB sets lines across, stacked from the top, as CSS's
	// horizontal-tb: the default.
	HorizontalTB WritingMode = iota
	// VerticalRL sets lines down, in columns stacked from the right, as
	// CSS's vertical-rl: Chinese and Japanese as books set them.
	VerticalRL
	// VerticalLR sets columns stacked from the left, as CSS's
	// vertical-lr.
	VerticalLR
)

func (m WritingMode) css() string {
	switch m {
	case VerticalRL:
		return "vertical-rl"
	case VerticalLR:
		return "vertical-lr"
	}
	return "horizontal-tb"
}

// TextOrientation is how the characters of vertical text stand, as CSS's
// text-orientation. Its zero value takes the text's around it, TextMixed
// by default.
type TextOrientation uint8

const (
	// TextMixed stands up the characters Unicode sets upright in vertical
	// text (UAX #50), as Han, kana, Hangul and fullwidth forms, with the
	// font's vertical forms of punctuation, and turns the others a quarter
	// clockwise, as Latin and digits.
	TextMixed TextOrientation = iota + 1
	// TextUpright stands up every character, as a column of Latin
	// capitals on a sign.
	TextUpright
	// TextSideways turns every character a quarter clockwise, a line
	// across turned with its column, as the spine of a book in English.
	TextSideways
)

func (o TextOrientation) css() string {
	switch o {
	case TextUpright:
		return "upright"
	case TextSideways:
		return "sideways"
	}
	return "mixed"
}

// WritingMode sets the element's text, and the texts inside it, in
// columns, as CSS's writing-mode: VerticalRL sets each line down a
// column, the columns stacked from the right, as books in Chinese and
// Japanese are set, and VerticalLR stacks them from the left. A vertical
// text wraps its columns at its own Height, or MaxHeight, and without
// either sets each paragraph in one column; it is as wide as its columns.
// TextAlign, MaxLines, Ellipsis, Justify, LineHeight, the spans of a
// RichText, selection and the elements inside a text follow the
// columns: Start is the top of a column, a line is a column, and its
// height is the room across it. Boxes still lay out as they do: a Row
// of vertical texts reads from the right only with RightToLeft or a
// reversed order. HorizontalTB, the default, sets lines across. Text
// inputs keep their lines across.
func (e *node) WritingMode(m WritingMode) *node {
	if m != HorizontalTB {
		linkVertical()
	}
	e.ts.mode = m
	e.ts.set |= setMode
	return e
}

// TextOrientation sets how the characters of the element's vertical text,
// and of the texts inside it, stand (WritingMode), as CSS's
// text-orientation: TextMixed, the default, stands up Han, kana and
// Hangul and turns Latin a quarter clockwise; TextUpright stands up
// everything; TextSideways turns everything.
func (e *node) TextOrientation(o TextOrientation) *node {
	e.ts.orient = o
	e.ts.set |= setOrientation
	return e
}

// paintVertical is verticalLayout once a writing mode sets text in
// columns, so that apps that never do not link it.
var (
	paintVertical    func(p *Painter, l *text.Layout, x, y float32, color Color, ts textStyle, sp spanPaint)
	linkVerticalOnce sync.Once
)

func linkVertical() {
	linkVerticalOnce.Do(func() {
		text.Shared().UseVertical()
		paintVertical = (*Painter).verticalLayout
	})
}

// column returns the length the columns of the element's vertical text
// wrap at: its content height, laid out at ch, or measured from its
// Height or MaxHeight for -1, when it sets one, else 0, a column a
// paragraph.
func (e *node) column(ch float32) float32 {
	if e.height.u == unitAuto && e.maxH.set == 0 {
		return 0
	}
	if ch >= 0 {
		return max(ch, 1)
	}
	h, ok := e.height.resolve(-1)
	if ok {
		h = e.clampH(h, inf)
	} else if h, ok = e.maxH.upper(-1); !ok {
		return 0
	}
	return max(h-e.padY(), 1)
}

// verticalLayout paints a vertical layout as textLayout paints one
// across: each column as a line across in its logical coordinates,
// turned a quarter clockwise to where it shows (text.Layout.Across), its
// upright glyphs turned back.
func (p *Painter) verticalLayout(l *text.Layout, x, y float32, color Color, ts textStyle, spans spanPaint) {
	var sp *spanPaint
	if spans.spans != nil {
		sp = &spans
	}
	saved, opaque := p.transform, p.opaque
	// Turned glyphs take no subpixel antialiasing.
	p.opaque = false
	one := *l
	columns := func(paint func(l *text.Layout)) {
		for li := range l.Lines {
			one.Lines = l.Lines[li : li+1 : li+1]
			p.transform = saved.Mul(columnTransform(l, li, x, y, p.scale))
			paint(&one)
		}
		p.transform = saved
	}
	columns(func(one *text.Layout) { p.textBackgrounds(one, x, y, ts, sp) })
	p.textSelection(l, x, y, ts)
	columns(func(one *text.Layout) { p.textForeground(one, x, y, color, ts, sp) })
	p.opaque = opaque
}

// columnTransform maps line li of vertical layout l, laid out from (x, y)
// in its logical coordinates, to where it shows, on whole pixels at scale:
// a point (x+u, y+v) shows at (x+Across(li)-v, y+u).
func columnTransform(l *text.Layout, li int, x, y, scale float32) scene.Affine {
	snap := func(v float32) float32 { return float32(math.Round(float64(v*scale))) / scale }
	return scene.Matrix(0, 1, -1, 0, snap(x+y+l.Across(li)), snap(y-x))
}

// uprightOrigin returns where, in device pixels in the logical coordinates
// of its line, upright glyph g of a vertical layout from (x, y) has its
// origin: VerticalOrigin below the top of its cell, on a whole pixel.
func (p *Painter) uprightOrigin(g text.Glyph, x, y float32) (pen, baseline float32) {
	s := p.scale
	return float32(math.Floor(float64((x + g.X + g.VerticalOrigin()) * s))), p.rt.text.Baseline((y + g.Y) * s)
}

// standUp turns a glyph drawn with its origin at (x, y), in device
// pixels, a quarter counterclockwise around it, so that it stands up in a
// column its line's transform turns clockwise.
func standUp(x, y float32) scene.Affine { return scene.Matrix(0, -1, 1, 0, x-y, y+x) }

// columnKey returns the arrow key that moves a caret in the logical
// coordinates of a vertical text as key does where it shows: up and down
// along the column, left and right to the next column or the one before,
// whose columns stack from the left if lr.
func columnKey(key Key, lr bool) Key {
	switch key {
	case KeyUp:
		return KeyLeft
	case KeyDown:
		return KeyRight
	case KeyLeft:
		if lr {
			return KeyUp
		}
		return KeyDown
	case KeyRight:
		if lr {
			return KeyDown
		}
		return KeyUp
	}
	return key
}
