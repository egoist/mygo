package terminal

import (
	"math"
	"slices"
	"unicode/utf8"

	"github.com/egoist/mygo/plugins/terminal/internal/vt"
	"github.com/egoist/mygo/ui"
)

// Grid draws cells another terminal emulator keeps, as View draws a
// Terminal's: in the cells of the font, with its shaping, the characters
// a terminal draws itself (box drawing, blocks, Powerline's separators),
// the selection, the cursor and an input method's composition. It parses
// no output and takes no input: its owner keeps the cells, their number
// and the keyboard. Main thread only.
type Grid struct {
	// Antialiased draws the glyphs without font smoothing, as
	// ui.Font.Antialiased. Set it before SetFont.
	Antialiased bool
	// Focused shows the cursor filled and the preedit; a block cursor is
	// hollow without it.
	Focused bool
	// Cursor is the cursor, and Preedit an input method's composition,
	// shown at the cursor in place of it, as DrawCursor draws them.
	Cursor  GridCursor
	Preedit string

	v     view
	cells [][]Cell // what each row was made from
}

// Cell is a cell of a Grid, its colors resolved.
type Cell struct {
	// Text is the cell's character, a grapheme cluster; "" in the cell a
	// wide character covers. Wide tells that it takes the next cell too.
	Text string
	Wide bool
	// FG is the text's color and BG the background's, when HasBG: the
	// theme's otherwise.
	FG, BG                                 ui.Color
	HasBG                                  bool
	Bold, Italic, Underline, Strikethrough bool
}

// GridCursor is the cursor of a Grid, at column X of row Y.
type GridCursor struct {
	X, Y    int
	Visible bool
	Style   CursorStyle
}

// NewGrid returns a Grid that draws with theme's default colors, its
// selection's and its cursor's.
func NewGrid(theme *Theme) *Grid {
	g := &Grid{}
	g.v = view{theme: theme, colors: vt.Colors{Foreground: rgb(theme.Foreground), Background: rgb(theme.Background)}}
	g.v.shaped.init(4096)
	return g
}

// SetFont sets the font at a scale, the device pixels of a DIP of the
// painter Draw draws with, and returns the size of a cell in those
// pixels, which CellSize returns too.
func (g *Grid) SetFont(f Font, scale float32) (w, h int) {
	k := f.key(scale)
	k.flat = g.Antialiased
	if k != g.v.font {
		g.v.setFont(k) // which forgets the rows: Resize makes them again
	}
	return g.v.cellW, g.v.cellH
}

// Resize sets the number of rows. The columns are the cells each row has.
func (g *Grid) Resize(rows int) {
	v := &g.v
	if len(v.lines) != rows {
		v.lines = append(v.lines[:0], make([]rowCache, rows)...)
		g.cells = append(g.cells[:0], make([][]Cell, rows)...)
	}
	v.rows = rows
}

// SetRow sets the cells of row y, with columns x0 to x1, x1 excluded,
// selected. A row of the cells it had keeps what it drew.
func (g *Grid) SetRow(y int, cells []Cell, x0, x1 int) {
	v := &g.v
	line := &v.lines[y]
	if !line.ok || !slices.Equal(g.cells[y], cells) {
		g.cells[y] = append(g.cells[y][:0], cells...)
		g.buildRow(line, cells)
	}
	line.selA, line.selB, line.sel = x0, x1-1, x1 > x0
}

func (g *Grid) buildRow(line *rowCache, cells []Cell) {
	b := g.v.newRow(line)
	var buf []rune
	for x := 0; x < len(cells); x++ {
		c := cells[x]
		if c.Text == "" {
			continue
		}
		w := 1
		if c.Wide {
			w = 2
		}
		st := resolved{fg: c.FG, bg: c.BG, shapeFg: c.FG, hasBg: c.HasBG, strike: c.Strikethrough}
		if c.Bold {
			st.variant |= 1
		}
		if c.Italic {
			st.variant |= 2
		}
		if c.Underline {
			st.underline = vt.UnderlineSingle
		}
		first, size := utf8.DecodeRuneInString(c.Text)
		cp := b.cell(x, w, st, first)
		buf = buf[:0]
		if cp != ' ' && size < len(c.Text) {
			buf = append(buf, []rune(c.Text)...)
		} else {
			buf = append(buf, cp)
		}
		b.text(x, w, buf)
		x += w - 1
	}
	b.flush()
}

// Draw draws the rows in r, from its top left, without the cursor.
func (g *Grid) Draw(p *ui.Painter, r ui.Rect) {
	v := &g.v
	v.cursor, v.focused, v.preedit = vt.Cursor{}, g.Focused, ""
	v.draw(p, r)
}

// DrawCursor draws the cursor, or the preedit, of the rows Draw draws in
// r, and nothing else: over them, or over a part of them alone, as an
// image of the cursor's cell drawn apart, so that the rows need not be
// drawn again when the cursor moves or hides.
func (g *Grid) DrawCursor(p *ui.Painter, r ui.Rect) {
	v := &g.v
	c := g.Cursor
	style := vt.CursorBlock
	switch c.Style {
	case CursorBar:
		style = vt.CursorBar
	case CursorUnderline:
		style = vt.CursorUnderline
	}
	v.cursor = vt.Cursor{X: c.X, Y: c.Y, InView: c.Y >= 0 && c.Y < len(v.lines), Visible: c.Visible, Style: style}
	v.focused, v.preedit = g.Focused, g.Preedit
	s := v.scale
	cw, ch := float32(v.cellW)/s, float32(v.cellH)/s
	ox, oy := float32(math.Round(float64(r.X*s)))/s, float32(math.Round(float64(r.Y*s)))/s
	cur, curColor, curText, block := v.cursorRect(ox, oy, cw, ch)
	if block {
		p.Fill(cur, curColor, 0)
	}
	v.drawCursor(p, cur, curColor, curText, block, ox, oy, cw, ch, float32(v.baseline)/s)
}
