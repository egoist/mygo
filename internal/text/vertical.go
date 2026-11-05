package text

//go:generate go run gen_orientation.go

import (
	"math"
	"slices"

	"github.com/go-text/typesetting/segmenter"
)

// WritingMode is the direction the lines of a text run in, and the one
// they stack in (Params.Mode), as CSS's writing-mode.
type WritingMode uint8

const (
	// Horizontal sets lines across, stacked from the top: horizontal-tb.
	Horizontal WritingMode = iota
	// VerticalRL sets lines down, in columns stacked from the right:
	// vertical-rl, as books in Chinese and Japanese are set.
	VerticalRL
	// VerticalLR sets columns stacked from the left: vertical-lr.
	VerticalLR
)

// Orientation is how the characters of vertical text stand
// (Params.Orientation, Span.Orientation), as CSS's text-orientation.
type Orientation uint8

const (
	// Mixed stands up the characters Unicode sets upright in vertical
	// text (UAX #50), as Han, kana, Hangul and fullwidth forms, with
	// their vertical forms (the font's vert feature), and turns the
	// others a quarter clockwise, as Latin and digits.
	Mixed Orientation = iota + 1
	// Upright stands up every character.
	Upright
	// Sideways turns every character a quarter clockwise, as a line
	// across turned.
	Sideways
)

// A vertical layout (Layout.Vertical) is laid out as one across, in
// logical coordinates: x goes down its columns, y across them, and its
// lines stack as lines across do, so that wrapping, alignment,
// justification, truncation, carets and selection work alike. Where it
// shows, a point (x, y) of line li is at (Across(li) - y, x): turned a
// quarter clockwise, the line's top at the right. Its sideways glyphs are
// drawn turned with it; its upright ones (Glyph.Upright) stand in a cell
// of an em down the column (VerticalOrigin), centered across it.

// emTop is how far below the top of its cell an upright glyph's baseline
// is, as a part of its size: the top of the ideographic em box, where CJK
// fonts put their glyphs' vertical origin.
const emTop = 0.88

// VerticalOrigin returns how far below X its baseline is, for an upright
// glyph (Upright): its cell's top is at X, and its horizontal origin at Y.
func (g Glyph) VerticalOrigin() float32 { return g.Size * emTop }

// Vertical reports whether the layout sets its lines in columns
// (Params.Mode): its coordinates are then logical, as Physical maps them.
func (l *Layout) Vertical() bool { return l.vertical }

// Size returns the width and height the layout takes where it shows:
// Width and Height, swapped for a vertical layout.
func (l *Layout) Size() (w, h float32) {
	if l.vertical {
		return l.Height, l.Width
	}
	return l.Width, l.Height
}

// Across returns where the y of line li of a vertical layout maps
// across: a point (x, y) of the line shows at (Across(li) - y, x).
func (l *Layout) Across(li int) float32 {
	if l.Params.Mode == VerticalLR && li >= 0 && li < len(l.Lines) {
		line := &l.Lines[li]
		return 2*line.Y + line.Height
	}
	return l.Height
}

// lineAcross returns the line of a vertical layout that a logical y is
// in, or the nearest.
func (l *Layout) lineAcross(y float32) int {
	for i := range l.Lines {
		if y < l.Lines[i].Y+l.Lines[i].Height {
			return i
		}
	}
	return len(l.Lines) - 1
}

// PhysicalRect returns where a rectangle of the layout shows, one within
// a line, as those of Selection and Caret are.
func (l *Layout) PhysicalRect(r Rect) Rect {
	if !l.vertical {
		return r
	}
	c := l.Height
	if l.Params.Mode == VerticalLR {
		c = l.Across(l.lineAcross(r.Y + r.H/2))
	}
	return Rect{c - r.Y - r.H, r.X, r.H, r.W}
}

// Logical returns the point of the layout that shows at (x, y).
func (l *Layout) Logical(x, y float32) (float32, float32) {
	if !l.vertical {
		return x, y
	}
	c := l.Height
	if l.Params.Mode == VerticalLR {
		// The columns go from the left: the one x is in.
		li := len(l.Lines) - 1
		for i := range l.Lines {
			if x < l.Lines[i].Y+l.Lines[i].Height {
				li = i
				break
			}
		}
		c = l.Across(li)
	}
	return y, c - x
}

// UseVertical lets layouts set their lines in columns (Params.Mode),
// which package ui calls as the first element sets a writing mode, so
// that apps that never do not link it.
func (s *System) UseVertical() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.vert = &verticalHooks
}

// verticalFuncs are what lays out and decorates vertical text, once
// UseVertical was called.
type verticalFuncs struct {
	shape   func(s *System, text []rune, style Style, spans []Span, width float32, rtl, wholeWords bool) []shapedLine
	strokes func(l *Layout, li, i, j int, x, y, scale float32, d Decoration, ls LineStyle) []Stroke
}

var verticalHooks = verticalFuncs{shape: verticalShape, strokes: verticalStrokes}

// shape shapes a paragraph as the engine does, into lines of at most
// width DIPs, or in columns of at most width DIPs for a style of vertical
// text (Style.orient).
func (s *System) shape(text []rune, style Style, spans []Span, width float32, rtl, wholeWords bool) []shapedLine {
	if style.orient != 0 && s.vert != nil {
		return s.vert.shape(s, text, style, spans, width, rtl, wholeWords)
	}
	return s.engine().shape(text, style, spans, width, rtl, wholeWords)
}

// featureless is an engine that cannot apply a font's OpenType features,
// as the browsers' canvas: vertical text takes Unicode's vertical
// presentation forms for its punctuation instead of the vert feature.
type featureless interface{ noFeatures() }

// voClass is a Vertical_Orientation of UAX #50.
type voClass uint8

const (
	voR  voClass = iota // rotated
	voU                 // upright
	voTu                // transformed typographically, else upright
	voTr                // transformed typographically, else rotated
)

type orientationRange struct {
	lo, hi rune
	vo     voClass
}

// orientationOf returns the Vertical_Orientation of r.
func orientationOf(r rune) voClass {
	i, ok := slices.BinarySearchFunc(orientations[:], r, func(o orientationRange, r rune) int {
		switch {
		case o.hi < r:
			return -1
		case o.lo > r:
			return 1
		}
		return 0
	})
	if !ok {
		return voR
	}
	return orientations[i].vo
}

// standsUp reports whether r stands upright in vertical text oriented
// o; forms tells that the engine applies no features, so that the
// characters to transform stand up only with a vertical form.
func standsUp(r rune, o Orientation, forms bool) bool {
	switch o {
	case Upright:
		return true
	case Sideways:
		return false
	}
	switch orientationOf(r) {
	case voU, voTu:
		return true
	case voTr:
		return !forms || verticalForm(r) != 0
	}
	return false
}

// verticalForm returns the vertical presentation form of r (U+FE10 to
// U+FE19 and U+FE30 to U+FE4F), or 0 if it has none.
func verticalForm(r rune) rune {
	switch r {
	case '，':
		return '︐'
	case '、':
		return '︑'
	case '。':
		return '︒'
	case '：':
		return '︓'
	case '；':
		return '︔'
	case '！':
		return '︕'
	case '？':
		return '︖'
	case '〖':
		return '︗'
	case '〗':
		return '︘'
	case '…':
		return '︙'
	case '‥':
		return '︰'
	case '—':
		return '︱'
	case '–':
		return '︲'
	case '＿':
		return '︳'
	case '（':
		return '︵'
	case '）':
		return '︶'
	case '｛':
		return '︷'
	case '｝':
		return '︸'
	case '〔':
		return '︹'
	case '〕':
		return '︺'
	case '【':
		return '︻'
	case '】':
		return '︼'
	case '《':
		return '︽'
	case '》':
		return '︾'
	case '〈':
		return '︿'
	case '〉':
		return '﹀'
	case '「':
		return '﹁'
	case '」':
		return '﹂'
	case '『':
		return '﹃'
	case '』':
		return '﹄'
	case '［':
		return '﹇'
	case '］':
		return '﹈'
	}
	return 0
}

// verticalShape shapes text, a paragraph, in columns of at most width
// DIPs (one for 0): the engine shapes it across, the characters that
// stand up with their vertical forms, and each glyph then takes its room
// down the column, an upright one a cell an em long, a sideways one its
// advance. The lines break where Unicode's line breaking algorithm lets
// them, at the advances down the column, and are shaped again each.
func verticalShape(s *System, text []rune, style Style, spans []Span, width float32, rtl, wholeWords bool) []shapedLine {
	e := s.engine()
	orient := style.orient
	style.orient = 0
	_, forms := e.(featureless)
	up := make([]bool, len(text))
	src, upright := text, false
	for i, r := range text {
		o := orient
		if k := spanIndex(spans, i); k < len(spans) {
			if spans[k].ObjectWidth > 0 {
				continue // an inline object takes its own room
			}
			if spans[k].Orientation != 0 {
				o = spans[k].Orientation
			}
		}
		if up[i] = standsUp(r, o, forms); !up[i] {
			continue
		}
		upright = true
		if f := verticalForm(r); forms && f != 0 {
			if &src[0] == &text[0] {
				src = slices.Clone(text)
			}
			src[i] = f
		}
	}
	if upright && !forms {
		spans = uprightSpans(spans, style, up)
	}
	whole := shapeColumn(e, src, style, spans, up, rtl, wholeWords)
	if width <= 0 || advance(whole) <= width+1e-3 {
		return []shapedLine{whole}
	}
	ends := breakColumn(text, whole, width, wholeWords)
	out := make([]shapedLine, 0, len(ends))
	start := 0
	for _, end := range ends {
		out = append(out, shapeColumn(e, src[start:end], style, spansIn(spans, start, end), up[start:end], rtl, wholeWords))
		offsetLines(out[len(out)-1:], start)
		start = end
	}
	return out
}

// uprightSpans returns spans split where the runes of the text stand up
// or not (up), those that stand up taking the font's vertical forms.
func uprightSpans(spans []Span, style Style, up []bool) []Span {
	var out []Span
	for i := 0; i < len(up); {
		k := spanIndex(spans, i)
		var sp Span
		end := len(up)
		if k < len(spans) {
			sp, end = spans[k], min(spans[k].End, len(up))
		}
		j := i + 1
		for j < end && up[j] == up[i] {
			j++
		}
		if up[i] {
			f := sp.Features
			if f == "" {
				f = style.Features
			}
			if f == "" {
				sp.Features = "vert"
			} else {
				sp.Features = f + ",vert"
			}
		}
		sp.End = j
		out = append(out, sp)
		i = j
	}
	return out
}

// shapeColumn shapes text on one line down a column: the engine shapes
// it across, and its glyphs are placed down the column, those of the
// runes that stand up (up) each cluster in a cell of an em, the others as
// they are, on the line's central baseline.
func shapeColumn(e engine, text []rune, style Style, spans []Span, up []bool, rtl, wholeWords bool) shapedLine {
	lines := e.shape(text, style, spans, 0, rtl, wholeWords)
	out := shapedLine{end: len(text)}
	if len(lines) == 1 {
		out.runs = lines[0].runs
	} else {
		// One line, as engines lay text without a width.
		var dx float32
		for _, l := range lines {
			for _, r := range l.runs {
				for k := range r.glyphs {
					r.glyphs[k].X += dx
				}
				out.runs = append(out.runs, r)
			}
			dx += advance(l)
		}
	}
	var pen, orig float32
	for ri := range out.runs {
		r := &out.runs[ri]
		size, a, d := style.FontSize(), style.FontSize()*0.8, style.FontSize()*0.2
		if f := r.font; f != nil {
			if f.Size > 0 {
				size = f.Size
			}
			if f.Ascent > 0 {
				a, d = f.Ascent, f.Descent
			}
		}
		gs := r.glyphs
		for gi := 0; gi < len(gs); {
			g := &gs[gi]
			c := g.Cluster
			if c < 0 || c >= len(up) || !up[c] {
				// Sideways, its em box centered on the baseline.
				off := g.X - orig
				orig += g.Advance
				g.X = pen + off
				g.Y += (a - d) / 2
				pen += g.Advance
				gi++
				continue
			}
			// A cluster standing up: its glyphs share its cell, centered
			// across the line by their advance across.
			j, h, x0 := gi, float32(0), g.X
			for j < len(gs) && gs[j].Cluster == c {
				h += gs[j].Advance
				j++
			}
			st := style
			if k := spanIndex(spans, c); k < len(spans) {
				st = spans[k].style(style)
			}
			ls := st.LetterSpacing
			orig += h
			if h > ls {
				h -= ls // the spacing the engine added after it
			}
			for k := gi; k < j; k++ {
				gk := &gs[k]
				gk.Y = h/2 - (gk.X - x0)
				gk.X, gk.Advance, gk.Upright = pen, 0, true
			}
			gs[gi].Advance = size + ls
			pen += size + ls
			gi = j
		}
	}
	return out
}

// breakColumn returns the ends of the lines that text, shaped on one line
// down a column (shapeColumn), breaks into at width: as many clusters as
// fit, up to the last place a line may break before the one that does
// not, whitespace hanging; within a word when nothing fits before it,
// unless wholeWords.
func breakColumn(text []rune, whole shapedLine, width float32, wholeWords bool) []int {
	n := len(text)
	adv := make([]float32, n+1)
	starts := make([]bool, n+1)
	starts[0], starts[n] = true, true
	for _, r := range whole.runs {
		for _, g := range r.glyphs {
			if g.Cluster >= 0 && g.Cluster < n {
				adv[g.Cluster] += g.Advance
				starts[g.Cluster] = true
			}
		}
	}
	var seg segmenter.Segmenter
	opps := lineBreaks(&seg, nil, text)
	var ends []int
	for start := 0; start < n; {
		w, opp, end := float32(0), -1, n
	clusters:
		for i := start; i < n; {
			j := i + 1
			for j < n && !starts[j] {
				j++
			}
			if i > start && opps[i] {
				opp = i
			}
			if i > start && !hangs(text[i]) && w+adv[i] > width+1e-3 {
				switch {
				case opp > start:
					end = opp
				case !wholeWords:
					end = i
				default:
					for k := i + 1; k < n; k++ {
						if opps[k] {
							end = k
							break
						}
					}
				}
				break clusters
			}
			w += adv[i]
			i = j
		}
		ends = append(ends, end)
		start = end
	}
	return ends
}

// verticalStrokes returns the strokes of a decoration of glyphs i to j
// of line li of a vertical layout, in its logical coordinates from (x, y)
// in device pixels: an underline along the line-under edge of their em
// boxes, the left where the text shows, a strikethrough through the
// middle of the line.
func verticalStrokes(l *Layout, li, i, j int, x, y, scale float32, d Decoration, ls LineStyle) []Stroke {
	line := &l.Lines[li]
	gs := line.Glyphs[i:j]
	x0, x1 := float32(math.MaxFloat32), float32(-math.MaxFloat32)
	size := l.Params.Style.FontSize()
	for _, g := range gs {
		x0, x1 = min(x0, g.X), max(x1, g.X+g.Advance)
		if g.Size > 0 {
			size = g.Size
		}
	}
	if x1 <= x0 {
		return nil
	}
	thick := max(float32(math.Round(float64(size/16*scale))), 1)
	if ls.Thick > 0 {
		thick = max(float32(math.Round(float64(ls.Thick*scale))), 1)
	}
	mid := (y + line.Baseline) * scale
	top := float32(math.Round(float64(mid - thick/2)))
	if d != Strikethrough {
		edge := (y+line.Baseline+size/2)*scale + max(scale, 1)
		if ls.HasOffset {
			edge = (y + line.Baseline + size/2 + ls.Offset) * scale
		}
		top = float32(math.Round(float64(edge)))
	}
	return []Stroke{{X0: (x + x0) * scale, X1: (x + x1) * scale, Top: top, Bottom: top + thick}}
}
