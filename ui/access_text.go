package ui

import (
	"sort"
	"time"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/text"
)

// accessText keeps range queries separate from the frame arena. It owns
// no native resources and never retains an Element from an earlier frame.
type accessText struct {
	rt        *engine
	st        *state
	id, frame uint64
	buf       buffer // static text; editors already have a paragraph index
	ed        *editor
	layout    *text.Layout
	ox, oy    float64
	clip      platform.RectF
	g         graphemes
}

func (rt *engine) describeText(e *Element, n *platform.AccessNode) {
	// Inline children belong to their paragraph's text provider. Their
	// node records its range there, rather than an independent layout.
	if e.isInline() {
		return
	}
	ed := e.st.editor
	if e.flags&(flagEditable|flagSelectable) == 0 {
		ed = nil
	}
	if ed != nil && ed.password {
		return
	}
	if e.kind != kindText && (ed == nil || e.flags&flagEditable == 0) {
		return
	}
	d := e.st.accessText
	if d == nil {
		d = &accessText{rt: rt, st: e.st, id: e.id}
		e.st.accessText = d
	}
	if d.ed != ed {
		d.g = graphemes{}
	}
	d.frame, d.ed, d.layout = rt.frame, ed, e.tl
	d.ox, d.oy = float64(e.x+e.contentX()), float64(e.y+e.contentY())
	d.clip = platform.RectF{X: float64(e.st.vx), Y: float64(e.st.vy), W: float64(e.st.vw), H: float64(e.st.vh)}
	if ed != nil {
		d.layout = ed.layout
		d.ox, d.oy = float64(e.x+ed.originX-ed.scrollX), float64(e.y+ed.originY)
		if ed.area != nil {
			d.ox += float64(ed.scrollX)
		}
	} else if d.buf.s != e.text || len(d.buf.paras) == 0 {
		d.buf.set(e.text)
	}
	b := d.buffer()
	n.Text = &platform.AccessText{Content: b.s, Length: b.n, Selectable: !e.IsDisabled() && (e.flags&(flagEditable|flagSelectable) != 0), Query: d.query}
	if ed != nil {
		n.Text.Caret = ed.caret
		n.SelStart, n.SelEnd = ed.selection()
		if ed.readOnly {
			n.States |= platform.AccessReadOnly
		}
	} else {
		n.States |= platform.AccessReadOnly
	}
}

func (d *accessText) buffer() *buffer {
	if d.ed != nil {
		return &d.ed.buf
	}
	return &d.buf
}

func (d *accessText) live() bool {
	return d.rt != nil && d.st != nil && d.rt.states[d.id] == d.st && d.st.seen == d.rt.frame && d.st.pass == d.rt.pass && d.frame == d.rt.frame &&
		d.st.flags&(flagInvisible|flagInert) == 0 && (d.ed == nil || !d.ed.password)
}

func (d *accessText) invalidate() { *d = accessText{} }

func (d *accessText) snap(i int) int {
	b := d.buffer()
	i = max(0, min(i, b.n))
	if i == 0 || i == b.n {
		return i
	}
	lo := d.g.prev(b, i)
	if d.g.next(b, lo) == i {
		return i
	}
	return lo
}

func (d *accessText) query(q platform.AccessTextQuery) platform.AccessTextResult {
	if !d.live() {
		return platform.AccessTextResult{}
	}
	b := d.buffer()
	i := max(0, min(q.Start, b.n))
	o := platform.AccessTextResult{OK: true, Start: i}
	switch q.Kind {
	case platform.TextSlice:
		end := q.End
		if end < 0 {
			end = b.n
		}
		end = max(i, min(end, b.n))
		o.Text, o.End = b.slice(i, end), end
	case platform.TextToUTF16:
		o.Start = b.unitOf(i)
	case platform.TextFromUTF16:
		o.Start = b.runeOf(q.Start, true)
	case platform.TextToByte:
		o.Start = b.byteOf(i)
	case platform.TextFromByte:
		o.Start = b.runeOf(q.Start, false)
	case platform.TextUnitRange:
		o.Start, o.End = d.unit(i, q.Unit)
	case platform.TextMoveOffset:
		for count := q.Count; count != 0; {
			lo, hi := d.unit(i, q.Unit)
			next := hi
			step := 1
			if count < 0 {
				step = -1
				next = lo
				if next == i && i > 0 {
					next, _ = d.unit(i-1, q.Unit)
				}
			}
			if next == i {
				break
			}
			i = next
			count -= step
			o.Count += step
		}
		o.Start = i
	case platform.TextRangeBounds:
		o.Rects = d.bounds(i, max(i, min(q.End, b.n)))
	case platform.TextVisibleRanges:
		o.Ranges = d.visible()
	case platform.TextOffsetAtPoint:
		o.Start = d.snap(d.at(q.X, q.Y))
	case platform.TextLineNumber:
		o.Start = d.lineNumber(i)
	case platform.TextLineRange:
		o.Start, o.End, o.OK = d.lineRange(q.Start)
	default:
		o.OK = false
	}
	return o
}

// paragraphLayout uses the displayed layout where available. Queries
// outside the viewport shape a temporary paragraph: they do not change
// heights, the scroll anchor, or the bounded cache of the text area.
func (d *accessText) paragraphLayout(p int) *text.Layout {
	b := d.buffer()
	if d.ed == nil || d.ed.area == nil {
		return d.layout
	}
	if l := b.paras[p].layout; l != nil {
		return l
	}
	params := d.ed.area.params
	params.Text = b.text(p)
	return textSystem().Shape(params)
}

func (d *accessText) local(p, i int) int {
	if d.ed != nil {
		if a := d.ed.area; a != nil {
			return a.local(d.ed, p, i)
		}
		return d.ed.displayIndex(i)
	}
	return i
}

func (d *accessText) global(p, i int) int {
	if d.ed != nil {
		if a := d.ed.area; a != nil {
			return a.global(d.ed, p, i)
		}
		return d.ed.textIndex(i)
	}
	return i
}

func (d *accessText) unit(i int, unit platform.AccessTextUnit) (int, int) {
	b := d.buffer()
	if b.n == 0 {
		return 0, 0
	}
	i = max(0, min(i, b.n))
	switch unit {
	case platform.TextDocument:
		return 0, b.n
	case platform.TextCharacter:
		if i == b.n {
			return b.n, b.n
		}
		lo := d.snap(i)
		return lo, d.g.next(b, lo)
	case platform.TextParagraph:
		p := b.para(min(i, b.n-1))
		end, _ := b.next(p)
		return b.paras[p].rune, end
	case platform.TextWord:
		p := b.para(min(i, b.n-1))
		start := d.g.of(b, p)
		lo, hi := d.g.b.WordUnit(i - start)
		return start + lo, start + hi
	case platform.TextLine:
		p := 0
		if d.ed != nil && d.ed.area != nil {
			p = b.para(i)
		}
		l := d.paragraphLayout(p)
		if l == nil || len(l.Lines) == 0 {
			return 0, b.n
		}
		li := l.LineAt(d.local(p, i))
		start, end := l.Lines[li].Start, l.Lines[li].End
		if li+1 < len(l.Lines) {
			end = l.Lines[li+1].Start
		} else if d.ed != nil && d.ed.area != nil {
			end = len(l.Runes) + b2i(p+1 < len(b.paras))
		}
		lo, hi := d.global(p, start), d.global(p, end)
		if d.ed != nil && d.ed.area != nil && li+1 == len(l.Lines) {
			hi, _ = b.next(p)
		}
		return lo, hi
	}
	return 0, b.n
}

func intersectText(a, b platform.RectF) platform.RectF {
	x, y := max(a.X, b.X), max(a.Y, b.Y)
	return platform.RectF{X: x, Y: y, W: max(0, min(a.X+a.W, b.X+b.W)-x), H: max(0, min(a.Y+a.H, b.Y+b.H)-y)}
}

func (d *accessText) top(p int) float64 {
	if d.ed != nil && d.ed.area != nil {
		return d.oy + d.ed.area.hs.top(p) - d.ed.area.scroll
	}
	return d.oy
}

// visibleParagraphs bounds geometry work to the paragraphs already in
// view, even when the requested range is the entire document.
func (d *accessText) visibleParagraphs() (int, int) {
	if d.ed != nil && d.ed.area != nil {
		return d.ed.area.first, d.ed.area.last
	}
	return 0, 0
}

func (d *accessText) bounds(start, end int) []platform.RectF {
	var out []platform.RectF
	lo, hi := d.visibleParagraphs()
	b := d.buffer()
	for p := lo; p <= hi; p++ {
		ps, pe := 0, b.n
		if d.ed != nil && d.ed.area != nil {
			ps = b.paras[p].rune
			pe, _ = b.next(p)
		}
		if start > pe || end < ps || end == ps && start < end {
			continue
		}
		l := d.paragraphLayout(p)
		if l == nil {
			continue
		}
		a, z := d.local(p, max(start, ps)), d.local(p, min(end, pe))
		var rs []text.Rect
		if start == end {
			if d.ed != nil && d.ed.area != nil && b.para(start) != p {
				continue
			}
			x, y, h := l.Caret(a)
			rs = []text.Rect{{X: x, Y: y, W: 1, H: h}}
		} else {
			rs = l.RangeRects(a, z)
			if d.ed != nil && d.ed.area != nil && p+1 < len(b.paras) && start <= b.end(p) && end > b.end(p) {
				x, y, h := l.Caret(len(l.Runes))
				rs = append(rs, text.Rect{X: x, Y: y, W: l.Params.Style.FontSize() / 3, H: h})
			}
		}
		for _, r := range rs {
			v := intersectText(platform.RectF{X: d.ox + float64(r.X), Y: d.top(p) + float64(r.Y), W: float64(r.W), H: float64(r.H)}, d.clip)
			if v.W > 0 && v.H > 0 {
				out = append(out, v)
			}
		}
	}
	return out
}

func (d *accessText) visible() []platform.AccessTextRange {
	var ranges []platform.AccessTextRange
	lo, hi := d.visibleParagraphs()
	for p := lo; p <= hi; p++ {
		l := d.paragraphLayout(p)
		if l == nil {
			continue
		}
		for li, line := range l.Lines {
			y := d.top(p) + float64(line.Y)
			if y+float64(line.Height) <= d.clip.Y || y >= d.clip.Y+d.clip.H || d.clip.W <= 0 {
				continue
			}
			// A horizontally scrolled line may show disjoint logical
			// ranges in bidi text. Work on glyphs, rather than the caret
			// endpoints of the whole visual line.
			var pieces []platform.AccessTextRange
			for _, g := range line.Glyphs {
				if g.Runes == 0 || d.ox+float64(g.X+g.Advance) <= d.clip.X || d.ox+float64(g.X) >= d.clip.X+d.clip.W {
					continue
				}
				pieces = append(pieces, platform.AccessTextRange{Start: d.global(p, g.Cluster), End: d.global(p, g.Cluster+g.Runes)})
			}
			if len(pieces) == 0 && line.Start == line.End {
				pieces = append(pieces, platform.AccessTextRange{Start: d.global(p, line.Start), End: d.global(p, line.End)})
			}
			sort.Slice(pieces, func(i, j int) bool { return pieces[i].Start < pieces[j].Start })
			for _, r := range pieces {
				if n := len(ranges); n > 0 && r.Start <= ranges[n-1].End {
					ranges[n-1].End = max(ranges[n-1].End, r.End)
				} else {
					ranges = append(ranges, r)
				}
			}
			if n := len(ranges); n > 0 && li == len(l.Lines)-1 && d.ed != nil && d.ed.area != nil && ranges[n-1].End == d.buffer().end(p) {
				ranges[n-1].End, _ = d.buffer().next(p)
			}
		}
	}
	return ranges
}

func (d *accessText) at(x, y float64) int {
	lo, hi := d.visibleParagraphs()
	best := lo
	for p := lo; p <= hi; p++ {
		l := d.paragraphLayout(p)
		if l == nil {
			continue
		}
		best = p
		if y < d.top(p)+float64(l.Height) {
			break
		}
	}
	l := d.paragraphLayout(best)
	if l == nil {
		return 0
	}
	return d.global(best, l.IndexAt(float32(x-d.ox), float32(y-d.top(best))))
}

// Exact global line numbers inherently require measuring the prefix.
// Only these explicit ordinal queries do so; no layouts are retained.
func (d *accessText) lineNumber(i int) int {
	p := 0
	if d.ed != nil && d.ed.area != nil {
		p = d.buffer().para(i)
	}
	n := 0
	for j := 0; j < p; j++ {
		n += len(d.paragraphLayout(j).Lines)
	}
	if l := d.paragraphLayout(p); l != nil {
		n += l.LineAt(d.local(p, i))
	}
	return n
}

func (d *accessText) lineRange(n int) (int, int, bool) {
	if n < 0 {
		return 0, 0, false
	}
	paras := 1
	if d.ed != nil && d.ed.area != nil {
		paras = len(d.buffer().paras)
	}
	for p := 0; p < paras; p++ {
		l := d.paragraphLayout(p)
		if l == nil {
			return 0, 0, false
		}
		if n < len(l.Lines) {
			i := d.global(p, l.Lines[n].Start)
			lo, hi := d.unit(i, platform.TextLine)
			return lo, hi, true
		}
		n -= len(l.Lines)
	}
	return 0, 0, false
}

// textSelection normalizes requests to grapheme boundaries. Selection
// changes do not commit input-method text or create an undo step.
func (rt *engine) textSelection(s *state, from, to int) {
	d := s.accessText
	if d == nil || !d.live() || d.ed == nil {
		return
	}
	ed := d.ed
	ed.accessScroll = false
	from, to = d.snap(from), d.snap(to)
	ed.anchor, ed.caret = from, to
	ed.hasDesired, ed.coalesce = false, false
	rt.focusOn(s)
	if ed.area != nil {
		ed.area.reveal = true
	}
	rt.blinkStart = time.Now()
}

// revealText shows the requested line of static/selectable text in its
// scroll ancestors. It uses the committed boxes, just as pointer scrolls
// do, and the next frame places the content at the new offsets.
func (d *accessText) revealText(i int) {
	if d.layout == nil {
		return
	}
	x, y, h := d.layout.Caret(max(0, min(i, d.buffer().n)))
	r := platform.RectF{X: d.ox + float64(x), Y: d.oy + float64(y), W: 1, H: float64(h)}
	for ch, p := d.st, d.rt.states[d.st.parent]; p != nil; ch, p = p, d.rt.states[p.parent] {
		if ch.flags&flagAbsolute != 0 {
			break
		}
		sx, sy := p.scrollX, p.scrollY
		if p.flags&flagScrollX != 0 {
			sx = nearest(sx, r.X-float64(p.cx)+sx, 1, 0, p.cw)
			sx = max(0, min(sx, p.contentW-float64(p.w)))
		}
		if p.flags&flagScrollY != 0 {
			sy = nearest(sy, r.Y-float64(p.cy)+sy, h, 0, p.ch)
			sy = max(0, min(sy, p.contentH-float64(p.h)))
		}
		r.X += p.scrollX - sx
		r.Y += p.scrollY - sy
		p.scrollTo(sx, sy)
	}
}
