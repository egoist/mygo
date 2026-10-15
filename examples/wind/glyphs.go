package main

import (
	"math"

	"github.com/egoist/mygo/ui"
)

// charset is the characters an atlas holds, ASCII's printable ones and a
// middle dot, sixteen to a row.
const charset = " !\"#$%&'()*+,-./0123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_`abcdefghijklmnopqrstuvwxyz{|}~·"

// looks are the sets of characters a visit shows, each a sequence that a
// cell steps through from a place of its own.
var looks = []struct{ name, chars string }{
	{"mixed", "ab0cAde1fEgh2iGjk3lHmn4oKpq5rMst6uRvw7xSyz8nT9eW"},
	{"lettered", "abcd3efgRhijk7lmnAopqr0stuEvwxy5zaeKiou2gcS9"},
	{"figured", "0a1b2C3d4e5F6g7h8i9J0k1m2N3p4q5R6s7t8U9v0w1X2y3z"},
}

// The glyphs: glyphSize DIPs, in a light weight on screens of 1.5 pixels
// a DIP or more and a regular one on others, where a light stroke would
// break up; in the system's monospaced font.
const (
	glyphSize   = 9
	lightWeight = 300
	family      = "ui-monospace, Menlo, Consolas, monospace"
	// inkFrom is the coverage from which a pixel counts in a glyph's box.
	inkFrom = 8
)

// inked is a pixel of a glyph's ink: where it lies in its cell, its
// coverage, which way its stroke's edge faces (a unit vector, when it
// lies on one), and the samples of the wind it reads, as a texture's
// linear filtering does: the column before or the cell's own (dx), the
// row (dy, from the cell's first), and how far toward the next of each.
type inked struct {
	x, y   int
	ink    float32
	edge   bool
	nx, ny float32
	dx, dy int
	tx, ty float32
}

// atlas is the charset rasterized for cells of cellW×cellH pixels: each
// character's ink, empty for those the font does not draw a cell wide.
type atlas struct {
	cellW, cellH int
	glyphs       [][]inked
	missing      []bool
}

// newAtlas rasterizes the charset for cells of cellW pixels at scale
// pixels a DIP, as large as glyphSize allows in a cell with a pixel to
// spare either side, its baseline placed to center the font's ascent and
// descent, and each glyph centered as the zero's advance. It draws a frame
// of its own (ui.Render), which must not happen within a window's.
func newAtlas(cellW int, scale float32) *atlas {
	cellH := 2 * cellW
	weight := lightWeight
	if scale < 1.5 {
		weight += 100
	}
	ratio := float32(0.6)
	if g := ui.Shape("0", ui.Font{Family: family, Size: 100, Weight: weight}); len(g) == 1 {
		ratio = g[0].Advance / 100
	}
	px := min(float32(math.Round(float64(glyphSize*scale))), floor(float32(cellW-2)/ratio))
	f := ui.Font{Family: family, Size: px / scale, Weight: weight, Antialiased: true}
	m := f.Metrics()
	base := float32(math.Round(float64((float32(cellH)-(m.Ascent+m.Descent)*scale)/2 + m.Ascent*scale)))
	var zero float32
	if g := ui.Shape("0", f); len(g) == 1 {
		zero = g[0].Advance * scale
	}
	left := floor((float32(cellW) - zero) / 2)

	chars := []rune(charset)
	a := &atlas{cellW: cellW, cellH: cellH, glyphs: make([][]inked, len(chars)), missing: make([]bool, len(chars))}
	shaped := make([][]ui.Glyph, len(chars))
	for i, r := range chars {
		g := ui.Shape(string(r), f)
		if len(g) != 1 || abs(g[0].Advance*scale-zero) > 0.5 {
			a.missing[i] = true
			continue
		}
		shaped[i] = g
	}
	w, h := 16*cellW, (len(chars)+15)/16*cellH
	sheet := ui.Render(func(c *ui.Context) {
		c.Root().Background(ui.Transparent)
		ui.Box(c).Fill().Draw(func(p *ui.Painter, _ ui.Rect) {
			for i, g := range shaped {
				if g == nil {
					continue
				}
				x, y := float32(i%16*cellW), float32(i/16*cellH)
				p.Clip(ui.Rect{X: x / scale, Y: y / scale, W: float32(cellW) / scale, H: float32(cellH) / scale}, 0, func() {
					p.Glyphs(g, (x+left)/scale, (y+base)/scale, ui.Hex("#ffffff"))
				})
			}
		})
	}, int(math.Ceil(float64(float32(w)/scale))), int(math.Ceil(float64(float32(h)/scale))), scale)

	for i := range chars {
		if a.missing[i] {
			continue
		}
		ox, oy := i%16*cellW, i/16*cellH
		cov := func(x, y int) float32 {
			x, y = min(max(x, 0), cellW-1), min(max(y, 0), cellH-1)
			sx, sy := ox+x, oy+y
			if sx >= sheet.Rect.Dx() || sy >= sheet.Rect.Dy() {
				return 0
			}
			return float32(sheet.Pix[sy*sheet.Stride+4*sx+3]) / 255
		}
		// The glyph's box: where its coverage reaches inkFrom.
		x0, y0, x1, y1 := cellW, cellH, -1, -1
		for y := range cellH {
			for x := range cellW {
				if cov(x, y)*255 >= inkFrom {
					x0, y0, x1, y1 = min(x0, x), min(y0, y), max(x1, x), max(y1, y)
				}
			}
		}
		for y := y0; y <= y1; y++ {
			for x := x0; x <= x1; x++ {
				ink := cov(x, y)
				if ink <= 0 {
					continue
				}
				p := inked{x: x, y: y, ink: ink}
				// Which way the stroke's edge faces, down its coverage's slope.
				rx, ry := cov(x+1, y)-cov(x-1, y), cov(x, y+1)-cov(x, y-1)
				if s := hypot(rx, ry); s > 0.02 {
					p.edge, p.nx, p.ny = true, rx/s, ry/s
				}
				// The wind has a sample a cell wide and two a cell high.
				u := (float32(x)+0.5)/float32(cellW) - 0.5
				v := (float32(y)+0.5)/float32(cellH/2) - 0.5
				p.dx, p.dy = int(floor(u)), int(floor(v))
				p.tx, p.ty = u-floor(u), v-floor(v)
				a.glyphs[i] = append(a.glyphs[i], p)
			}
		}
	}
	return a
}

// sequence returns the characters of look that the atlas draws, as
// indexes of charset.
func (a *atlas) sequence(look string) []uint8 {
	chars := looks[0].chars
	for _, l := range looks {
		if l.name == look {
			chars = l.chars
		}
	}
	var seq []uint8
	for _, r := range chars {
		i := indexOf(r)
		if i > 0 && !a.missing[i] {
			seq = append(seq, uint8(i))
		}
	}
	if len(seq) == 0 {
		seq = append(seq, uint8(indexOf('.')))
	}
	return seq
}

func indexOf(r rune) int {
	for i, c := range []rune(charset) {
		if c == r {
			return i
		}
	}
	return -1
}

// paint is a frame of the glyphs: the colors they mix between, the
// opacity of the whole, and where they go.
type paint struct {
	ink, glow [3]float32
	inkAlpha  float32
	opacity   float32
	pix       []uint8
	stride    int
}

// The light: what a cell shows at rest, where the wind is calm; how
// strongly the wind catches the edges facing it; how much stronger a
// ripple's color shows than the plain ink, which the ink's light leaves
// room for; and how strong the ripples are.
const (
	rest, friction, headroom, glowAmount = 0.03, 0.6, 3, 1
	// flick is how many characters a second a cell steps through, about.
	flick = 1.2
)

// glyphRows paints the cells of rows from to to of grid g, as the wind's
// field f lights them, with the glyphs of a and its sequence seq.
func glyphRows(g grid, a *atlas, seq []uint8, f *field, pt *paint, from, to int) {
	seed := f.w.seed
	for cy := from; cy < to; cy++ {
		for cx := range g.cols {
			cell := pcg(uint32(cx)*0x9E3779B9 ^ pcg(uint32(cy)^seed))
			// Each cell steps through the sequence at its own pace, from its
			// own place in it, and a roused one runs on through it, and back
			// as it settles.
			pace := pcg(cell)
			steps := int(floor(f.time*flick*(0.6+0.8*unit(pace)) + unit(pcg(pace))))
			var glow [2]float32
			if len(f.hand.taps) > 0 {
				var hand [4]float32
				hand, glow = f.hand.push(g.cellX(cx), g.cellY(cy))
				steps += int(floor(6*hand[3] + unit(pcg(pace^0x33))))
				k := 0.65 + 0.35*unit(pcg(pace^0x51))
				glow[0], glow[1] = glow[0]*k, glow[1]*k
			}
			if glow[0] < 0.04 && glow[1] < 0.015 && !f.windNear(cx, cy) {
				// Only its resting light would be left, which cannot be seen.
				continue
			}
			c := int(seq[(int(cell>>8)+steps)%len(seq)])
			tint := smoothstep(0.04, 0.3, glowAmount*glow[0])
			color := [3]float32{mix(pt.ink[0], pt.glow[0], tint), mix(pt.ink[1], pt.glow[1], tint), mix(pt.ink[2], pt.glow[2], tint)}
			strong := mix(pt.inkAlpha, headroom, tint)
			ox, oy := cx*g.cellW, cy*g.cellH
			for _, p := range a.glyphs[c] {
				w := f.lerp(cx+p.dx, 2*cy+p.dy, p.tx, p.ty)
				facing := float32(0.35)
				if p.edge {
					facing = max(0, p.nx*(w[1]*2-1)+p.ny*(w[2]*2-1))
				}
				caught := mix(1, 0.45+1.1*facing, friction)
				light := min(1, w[0]*caught) + rest*w[3]
				bright := min(1, light) * strong
				left := glowAmount * 0.3 * glow[1] * w[3]
				alpha := p.ink * min(1, (bright+left)/headroom)
				k := alpha / (bright + left + 0.0001) * pt.opacity
				o := pt.pix[(oy+p.y)*pt.stride+4*(ox+p.x):][:4]
				o[0] = byteOf((color[0]*bright + pt.glow[0]*left) * k)
				o[1] = byteOf((color[1]*bright + pt.glow[1]*left) * k)
				o[2] = byteOf((color[2]*bright + pt.glow[2]*left) * k)
				o[3] = byteOf(alpha * pt.opacity)
			}
		}
	}
}

// windNear reports whether the wind lights any sample of cell (cx, cy)
// or those around them.
func (f *field) windNear(cx, cy int) bool {
	for y := 2*cy - 1; y <= 2*cy+2; y++ {
		for x := cx - 1; x <= cx+1; x++ {
			if f.at(x, y)[0] != 0 {
				return true
			}
		}
	}
	return false
}

// lerp returns the field between texels (i, j) and (i+1, j+1), tx and ty
// of the way, from 0 to 1 a channel, as a texture's linear filtering.
func (f *field) lerp(i, j int, tx, ty float32) [4]float32 {
	a, b, c, d := f.at(i, j), f.at(i+1, j), f.at(i, j+1), f.at(i+1, j+1)
	var out [4]float32
	for k := range out {
		top := mix(float32(a[k]), float32(b[k]), tx)
		bottom := mix(float32(c[k]), float32(d[k]), tx)
		out[k] = mix(top, bottom, ty) / 255
	}
	return out
}
