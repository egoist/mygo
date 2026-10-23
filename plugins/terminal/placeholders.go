package terminal

import (
	"math"
	"slices"

	"github.com/egoist/mygo/plugins/terminal/internal/vt"
	"github.com/egoist/mygo/ui"
)

// Unicode placeholders of the Kitty graphics protocol: a program places
// an image virtually (U=1), then prints U+10EEEE in the cells the image
// covers, as text, which multiplexers like tmux keep, scroll and redraw,
// and which agents' interfaces use for images in what they lay out. A
// cell's foreground color is the image's ID, its underline color the
// placement's, and the combining diacritics after it the row and column
// of the placement's grid it shows, then the ID's high byte. A cell that
// leaves them out continues the cell before it. The image fits the grid
// as Ghostty and kitty fit it.

const placeholder = 0x10EEEE

// placeholderRun is a run of placeholder cells of a row showing tiles of
// a row of an image's grid, side by side.
type placeholderRun struct {
	x, width int // the first column, and the cells
	// image is the image's ID; id the placement's, 0 for the image's
	// first virtual placement.
	image, id uint32
	// row and col are the tile of the grid the first cell shows.
	row, col int
}

// placeholderCell is what a placeholder cell says: -1 for what it leaves
// out.
type placeholderCell struct {
	low      uint32 // the image ID's low 24 bits
	high     int
	id       uint32
	row, col int
}

// parsePlaceholder reads a placeholder cell of a style, with the
// codepoints after U+10EEEE.
func parsePlaceholder(s vt.Style, marks []rune) placeholderCell {
	c := placeholderCell{low: colorID(s.FG), id: colorID(s.UnderlineColor), high: -1, row: -1, col: -1}
	// A diacritic not of the table counts as left out, as in kitty.
	if len(marks) > 0 {
		c.row = diacriticIndex(marks[0])
	}
	if len(marks) > 1 {
		c.col = diacriticIndex(marks[1])
	}
	if len(marks) > 2 {
		if h := diacriticIndex(marks[2]); h <= 255 {
			c.high = h
		}
	}
	return c
}

// colorID is the ID a color says: a palette index, or the 24 bits of an
// RGB color.
func colorID(c vt.Color) uint32 {
	switch c.Kind {
	case 1:
		return uint32(c.Index)
	case 2:
		return uint32(c.RGB.R)<<16 | uint32(c.RGB.G)<<8 | uint32(c.RGB.B)
	}
	return 0
}

// placeholderRuns makes the runs of a row from its cells, left to right.
type placeholderRuns struct {
	open     bool
	first    placeholderCell // with row and col 0 when left out
	x, width int
}

// add adds the placeholder cell at column x, which continues the open
// run or starts one.
func (p *placeholderRuns) add(out []placeholderRun, x int, c placeholderCell) []placeholderRun {
	f := &p.first
	if p.open && c.low == f.low && c.id == f.id &&
		(c.row < 0 || c.row == f.row) && (c.col < 0 || c.col == f.col+p.width) &&
		(c.high < 0 || c.high == f.high) {
		p.width++
		return out
	}
	out = p.end(out)
	c.row, c.col = max(c.row, 0), max(c.col, 0)
	p.open, p.first, p.x, p.width = true, c, x, 1
	return out
}

// end ends the open run, as a cell that is not a placeholder does.
func (p *placeholderRuns) end(out []placeholderRun) []placeholderRun {
	if !p.open {
		return out
	}
	p.open = false
	id := p.first.low
	if p.first.high > 0 {
		id |= uint32(p.first.high) << 24
	}
	return append(out, placeholderRun{x: p.x, width: p.width, image: id, id: p.first.id, row: p.first.row, col: p.first.col})
}

// virtualPlacement is a placement of an image that Unicode placeholders
// show: cols×rows is the grid the image fits in, 0 for the cells the
// image covers at its size.
type virtualPlacement struct {
	image, id  uint32
	cols, rows int
}

// placeholderTile is where a run of placeholders draws its part of an
// image: x, y, w and h in device pixels from the grid's origin, src in
// the image's pixels.
type placeholderTile struct {
	x, y, w, h int
	src        ui.Rect
}

// placeholderPlacement is where a run on row y of the viewport draws its
// part of an image of imgW×imgH pixels, placed over a grid of cols×rows
// cells of cellW×cellH pixels (0 for the cells the image covers): the
// image fits the grid keeping its aspect ratio, centered, and the run
// shows its cells' tiles of that. False when they show none of the image.
// This follows Ghostty's renderPlacement (graphics_unicode.zig).
func placeholderPlacement(r placeholderRun, y, cols, rows, imgW, imgH, cellW, cellH int) (placeholderTile, bool) {
	if cellW <= 0 || cellH <= 0 || imgW <= 0 || imgH <= 0 {
		return placeholderTile{}, false
	}
	if rows == 0 {
		rows = (imgH + cellH - 1) / cellH
	}
	if cols == 0 {
		cols = (imgW + cellW - 1) / cellW
	}
	if cols <= 0 || rows <= 0 || cols > math.MaxUint16 || rows > math.MaxUint16 {
		return placeholderTile{}, false
	}
	iw, ih := float64(imgW), float64(imgH)
	gw, gh := float64(cols*cellW), float64(rows*cellH)

	// The scale fitting the image in the grid, and the margins centering
	// it, in the grid's pixels.
	var scale, marginX, marginY float64
	if iw*gh > ih*gw {
		scale = gw / iw
		marginY = (gh - ih*scale) / 2
	} else {
		scale = gh / ih
		marginX = (gw - iw*scale) / 2
	}
	// The image with its margins, in the image's pixels.
	mx, my := marginX/scale, marginY/scale
	sw, sh := iw+2*mx, ih+2*my

	// The run's tiles of that, then less the margins, which the image
	// does not have.
	srcX := sw * float64(r.col) / float64(cols)
	srcY := sh * float64(r.row) / float64(rows)
	srcW := sw * float64(r.width) / float64(cols)
	srcH := sh / float64(rows)
	var dx, dy float64
	dw, dh := float64(r.width*cellW), float64(cellH)

	switch {
	case srcY < my:
		off := my - srcY
		srcH -= off
		dy = off
		dh -= off * scale
		srcY = 0
		if srcH > ih {
			srcH = ih
			dh = ih * scale
		}
	case srcY+srcH > sh-my:
		srcY -= my
		srcH = sh - my - srcY - my
		dh = srcH * scale
	default:
		srcY -= my
	}
	switch {
	case srcX < mx:
		off := mx - srcX
		srcW -= off
		dx = off
		dw -= off * scale
		srcX = 0
		if srcW > iw {
			srcW = iw
			dw = iw * scale
		}
	case srcX+srcW > sw-mx:
		srcX -= mx
		srcW = sw - mx - srcX - mx
		dw = srcW * scale
	default:
		srcX -= mx
	}
	if srcW <= 0 || srcH <= 0 {
		return placeholderTile{}, false
	}
	round := func(f float64) int { return int(math.Round(f)) }
	p := placeholderTile{
		x: r.x*cellW + round(dx*scale), y: y*cellH + round(dy*scale),
		w: round(dw), h: round(dh),
		src: ui.Rect{X: float32(round(srcX)), Y: float32(round(srcY)), W: float32(round(srcW)), H: float32(round(srcH))},
	}
	return p, p.w > 0 && p.h > 0
}

// target is the virtual placement a placeholder of an image and a
// placement ID shows: the one of that ID, or with none, the image's of
// the lowest ID. (Ghostty prefers a placement given an ID to one the
// library numbered, which the library does not tell apart.)
func target(virtual []virtualPlacement, img, id uint32) (virtualPlacement, bool) {
	var best virtualPlacement
	found := false
	for _, p := range virtual {
		if p.image != img || (id != 0 && p.id != id) {
			continue
		}
		if !found || p.id < best.id {
			best, found = p, true
		}
	}
	return best, found
}

// diacriticIndex is the number a diacritic says, or -1 for one not of the
// table.
func diacriticIndex(r rune) int {
	i, ok := slices.BinarySearch(diacritics, r)
	if !ok {
		return -1
	}
	return i
}

// diacritics are the combining characters that say the numbers 0 to 296,
// in order: kitty's gen/rowcolumn-diacritics.txt.
var diacritics = []rune{
	0x0305, 0x030D, 0x030E, 0x0310, 0x0312, 0x033D, 0x033E, 0x033F,
	0x0346, 0x034A, 0x034B, 0x034C, 0x0350, 0x0351, 0x0352, 0x0357,
	0x035B, 0x0363, 0x0364, 0x0365, 0x0366, 0x0367, 0x0368, 0x0369,
	0x036A, 0x036B, 0x036C, 0x036D, 0x036E, 0x036F, 0x0483, 0x0484,
	0x0485, 0x0486, 0x0487, 0x0592, 0x0593, 0x0594, 0x0595, 0x0597,
	0x0598, 0x0599, 0x059C, 0x059D, 0x059E, 0x059F, 0x05A0, 0x05A1,
	0x05A8, 0x05A9, 0x05AB, 0x05AC, 0x05AF, 0x05C4, 0x0610, 0x0611,
	0x0612, 0x0613, 0x0614, 0x0615, 0x0616, 0x0617, 0x0657, 0x0658,
	0x0659, 0x065A, 0x065B, 0x065D, 0x065E, 0x06D6, 0x06D7, 0x06D8,
	0x06D9, 0x06DA, 0x06DB, 0x06DC, 0x06DF, 0x06E0, 0x06E1, 0x06E2,
	0x06E4, 0x06E7, 0x06E8, 0x06EB, 0x06EC, 0x0730, 0x0732, 0x0733,
	0x0735, 0x0736, 0x073A, 0x073D, 0x073F, 0x0740, 0x0741, 0x0743,
	0x0745, 0x0747, 0x0749, 0x074A, 0x07EB, 0x07EC, 0x07ED, 0x07EE,
	0x07EF, 0x07F0, 0x07F1, 0x07F3, 0x0816, 0x0817, 0x0818, 0x0819,
	0x081B, 0x081C, 0x081D, 0x081E, 0x081F, 0x0820, 0x0821, 0x0822,
	0x0823, 0x0825, 0x0826, 0x0827, 0x0829, 0x082A, 0x082B, 0x082C,
	0x082D, 0x0951, 0x0953, 0x0954, 0x0F82, 0x0F83, 0x0F86, 0x0F87,
	0x135D, 0x135E, 0x135F, 0x17DD, 0x193A, 0x1A17, 0x1A75, 0x1A76,
	0x1A77, 0x1A78, 0x1A79, 0x1A7A, 0x1A7B, 0x1A7C, 0x1B6B, 0x1B6D,
	0x1B6E, 0x1B6F, 0x1B70, 0x1B71, 0x1B72, 0x1B73, 0x1CD0, 0x1CD1,
	0x1CD2, 0x1CDA, 0x1CDB, 0x1CE0, 0x1DC0, 0x1DC1, 0x1DC3, 0x1DC4,
	0x1DC5, 0x1DC6, 0x1DC7, 0x1DC8, 0x1DC9, 0x1DCB, 0x1DCC, 0x1DD1,
	0x1DD2, 0x1DD3, 0x1DD4, 0x1DD5, 0x1DD6, 0x1DD7, 0x1DD8, 0x1DD9,
	0x1DDA, 0x1DDB, 0x1DDC, 0x1DDD, 0x1DDE, 0x1DDF, 0x1DE0, 0x1DE1,
	0x1DE2, 0x1DE3, 0x1DE4, 0x1DE5, 0x1DE6, 0x1DFE, 0x20D0, 0x20D1,
	0x20D4, 0x20D5, 0x20D6, 0x20D7, 0x20DB, 0x20DC, 0x20E1, 0x20E7,
	0x20E9, 0x20F0, 0x2CEF, 0x2CF0, 0x2CF1, 0x2DE0, 0x2DE1, 0x2DE2,
	0x2DE3, 0x2DE4, 0x2DE5, 0x2DE6, 0x2DE7, 0x2DE8, 0x2DE9, 0x2DEA,
	0x2DEB, 0x2DEC, 0x2DED, 0x2DEE, 0x2DEF, 0x2DF0, 0x2DF1, 0x2DF2,
	0x2DF3, 0x2DF4, 0x2DF5, 0x2DF6, 0x2DF7, 0x2DF8, 0x2DF9, 0x2DFA,
	0x2DFB, 0x2DFC, 0x2DFD, 0x2DFE, 0x2DFF, 0xA66F, 0xA67C, 0xA67D,
	0xA6F0, 0xA6F1, 0xA8E0, 0xA8E1, 0xA8E2, 0xA8E3, 0xA8E4, 0xA8E5,
	0xA8E6, 0xA8E7, 0xA8E8, 0xA8E9, 0xA8EA, 0xA8EB, 0xA8EC, 0xA8ED,
	0xA8EE, 0xA8EF, 0xA8F0, 0xA8F1, 0xAAB0, 0xAAB2, 0xAAB3, 0xAAB7,
	0xAAB8, 0xAABE, 0xAABF, 0xAAC1, 0xFE20, 0xFE21, 0xFE22, 0xFE23,
	0xFE24, 0xFE25, 0xFE26, 0x10A0F, 0x10A38, 0x1D185, 0x1D186, 0x1D187,
	0x1D188, 0x1D189, 0x1D1AA, 0x1D1AB, 0x1D1AC, 0x1D1AD, 0x1D242, 0x1D243,
	0x1D244,
}
