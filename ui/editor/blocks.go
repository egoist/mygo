package editor

import (
	"slices"
	"sort"
	"strconv"

	"github.com/egoist/mygo/ui"
)

// Block is an element between the lines of the text, as Zed's blocks: an
// image under the line naming it, say. It is no part of the text, which
// stays the buffer's. On a line a replacement hides, it shows in place of
// the lines the replacement covers, as a table drawn in place of its
// source.
type Block struct {
	// Line is the line it goes with, from 0.
	Line int
	// Above puts it above the line, else it goes below.
	Above bool
	// Content builds it: e is the element the editor places there, as
	// wide as the text and as high as what Content puts in it, to style
	// and fill as the editor's parts are (RenderFindBar), and b the block
	// as the edits since it was set moved it. A press on it goes to the
	// text, unless Interactive.
	Content func(c *ui.Context, e ui.Element, b Block)
	// Height is how high it is taken to be until it has shown, as only
	// those in view are laid out: a row when 0. Its element then says.
	Height float32
	// Interactive keeps the presses on Content's element for what is in
	// it, as a button's, rather than the text's, the wheel still
	// scrolling. Give a button KeepFocus to keep the typing in the editor.
	Interactive bool
	// Label names Content's element for assistive technology.
	Label string
}

// appBlock is a block the app gave, with what the editor keeps of it.
type appBlock struct {
	Block
	at       int // where its line starts, which edits move
	id       int // names its element from frame to frame
	height   float32
	measured bool // height is what the element's layout said
}

// builtBlock is the element the last build made of view.blocks[i], whose
// layout says how high the block is.
type builtBlock struct {
	i, id int
	e     ui.Element
}

func compareBlocks(a, b *appBlock) int {
	if a.Line != b.Line {
		return a.Line - b.Line
	}
	switch {
	case a.Above == b.Above:
		return 0
	case a.Above:
		return -1
	}
	return 1
}

// setBlocks shows bs, sorted by where they go. Those at the place of one
// shown, in order, keep its element and the height it measured, so that
// setting them again as the text changes costs no layout.
func (ed *editor) setBlocks(bs []Block) {
	v := &ed.view
	n := v.text.LineCount()
	in := v.blockScratch[:0]
	for _, b := range bs {
		if b.Content != nil && b.Line >= 0 && b.Line < n {
			in = append(in, appBlock{Block: b, at: v.text.LineStart(b.Line)})
		}
	}
	slices.SortStableFunc(in, func(a, b appBlock) int { return compareBlocks(&a, &b) })
	old := v.blocks
	i := 0
	for j := range in {
		b := &in[j]
		for i < len(old) && compareBlocks(&old[i], b) < 0 {
			i++
		}
		if i < len(old) && compareBlocks(&old[i], b) == 0 {
			b.id, b.height, b.measured = old[i].id, old[i].height, old[i].measured
			i++
			continue
		}
		v.nextBlock++
		b.id = v.nextBlock
	}
	// The slice given last is the scratch of the next, its functions let go.
	clear(old)
	v.blocks, v.blockScratch = in, old[:0]
	v.blocksStale = true
}

// moveBlocks moves the blocks through changes of the text, by where
// their lines start: settleBlocks finds their lines once the text is the
// last of the changes'.
func (v *view) moveBlocks(cs []change) {
	for i := range v.blocks {
		v.blocks[i].at = mapOffset(v.blocks[i].at, cs, biasRight)
	}
}

// settleBlocks finds the lines of the blocks after edits moved them, and
// that of a block whose line was deleted, where the line was.
func (v *view) settleBlocks() {
	if len(v.blocks) == 0 {
		return
	}
	for i := range v.blocks {
		b := &v.blocks[i]
		b.Line = v.text.LineOf(b.at)
		b.at = v.text.LineStart(b.Line)
	}
	// Lines joined bring blocks above and below to one line.
	if !slices.IsSortedFunc(v.blocks, func(a, b appBlock) int { return compareBlocks(&a, &b) }) {
		slices.SortStableFunc(v.blocks, func(a, b appBlock) int { return compareBlocks(&a, &b) })
	}
	v.blocksStale = true
}

// blockRows returns the rows of the app's blocks, for the heights. A
// block stays on a line a replacement hides, and goes with one a fold
// does. It is a new slice when they changed, as the heights tell them by
// the slice.
func (ed *editor) blockRows() []block {
	v := &ed.view
	if len(v.blocks) == 0 && len(v.rows) == 0 {
		return nil
	}
	spans := ed.hiddenSpans()
	if !v.blocksStale && v.rowsHeight == v.lineHeight && slices.Equal(spans, v.rowsFolded) {
		return v.rows
	}
	v.blocksStale, v.rowsHeight, v.rowsFolded = false, v.lineHeight, spans
	folded := heights{hidden: spans}
	replaced := heights{hidden: v.replacedLines()}
	rows := make([]block, len(v.blocks))
	for i := range v.blocks {
		b := &v.blocks[i]
		h := b.height
		if !b.measured {
			if h = b.Height; h <= 0 {
				h = v.lineHeight
			}
		}
		rows[i] = block{line: b.Line, below: !b.Above, height: h, kind: blockApp, from: b.id, to: i,
			sticky: replaced.isHidden(b.Line) && !folded.isHidden(b.Line)}
	}
	v.rows = rows
	return rows
}

// standIn returns the replacement a block shows in place of, on line: one
// showing nothing, from the start of a line through a newline, whose
// lines hold a block not folded away. Carets stop at its start, on the
// block, as at the start of what any replacement shows.
func (ed *editor) standIn(line int) (Replacement, bool) {
	v := &ed.view
	t := &v.text
	if len(v.blocks) == 0 || len(v.repls) == 0 {
		return Replacement{}, false
	}
	off := t.LineStart(line)
	i := sort.Search(len(v.repls), func(i int) bool { return v.repls[i].End > off })
	if i >= len(v.repls) || v.repls[i].Start > off {
		return Replacement{}, false
	}
	r := v.repls[i]
	first, end := t.LineOf(r.Start), t.LineOf(r.End)
	if r.shown() != "" || r.Start != t.LineStart(first) || end <= line {
		return Replacement{}, false
	}
	folded := heights{hidden: ed.hiddenSpans()}
	j := sort.Search(len(v.blocks), func(j int) bool { return v.blocks[j].Line >= first })
	for ; j < len(v.blocks) && v.blocks[j].Line < end; j++ {
		if !folded.isHidden(v.blocks[j].Line) {
			return r, true
		}
	}
	return Replacement{}, false
}

// standInWithin returns the replacement of the block standing in nearest
// the start of the lines of sp going forward, else nearest their end.
func (ed *editor) standInWithin(sp lineSpan, fwd bool) (Replacement, bool) {
	v := &ed.view
	i := sort.Search(len(v.blocks), func(i int) bool { return v.blocks[i].Line >= sp.from })
	j := sort.Search(len(v.blocks), func(i int) bool { return v.blocks[i].Line > sp.to })
	for k := range j - i {
		n := i + k
		if !fwd {
			n = j - 1 - k
		}
		if r, ok := ed.standIn(v.blocks[n].Line); ok {
			return r, true
		}
	}
	return Replacement{}, false
}

// standInBox returns the top and the bottom of what stands in for r's
// lines, in the content.
func (ed *editor) standInBox(r Replacement) (top, bottom float64) {
	t := &ed.view.text
	h := &ed.view.heights
	return h.top(t.LineOf(r.Start)), h.top(t.LineOf(r.End))
}

// paintStandIns paints what shows of the selections on the blocks
// standing in for lines in view: as selected behind those a selection
// covers, and the caret of those it stands at, tall as the block, in the
// margin before it.
func (ed *editor) paintStandIns(p *ui.Painter, r ui.Rect, f *frame) {
	v := &ed.view
	if len(v.blocks) == 0 || len(v.repls) == 0 {
		return
	}
	ct := ed.theme
	selColor := ct.Selection
	if !ed.focused {
		selColor = ct.InactiveSelection
	}
	reps := v.replsIn(v.text.LineStart(f.first), v.text.LineEnd(f.last))
	for i := range reps {
		rep := reps[i]
		if _, ok := ed.standIn(v.text.LineOf(rep.Start)); !ok {
			continue
		}
		top, bottom := ed.standInBox(rep)
		y := r.Y + float32(top-ed.scrollY)
		h := float32(bottom - top)
		for _, s := range ed.sels {
			if !s.empty() && s.start() <= rep.Start && s.end() >= rep.End {
				p.Fill(ui.Rect{X: r.X + ed.gutterW, Y: y, W: r.W - ed.gutterW, H: h}, selColor, 0)
			}
			if f.caretsOn && s.head == rep.Start {
				p.Fill(ui.Rect{X: r.X + ed.textX - 3, Y: y, W: 2, H: h}, ct.Cursor, 0)
			}
		}
	}
}

// placeBlocks appends to dst the blocks of the lines from y0 to y1 of the
// content, with where they are in the view.
func (ed *editor) placeBlocks(y0, y1 float64, dst []placed) []placed {
	h := &ed.view.heights
	if len(ed.view.blocks) == 0 || h.n == 0 {
		return dst
	}
	ed.eachBlock(h.lineAt(y0), min(h.lineAt(y1), h.n-1), func(b *block, top float64) {
		if b.kind == blockApp {
			dst = append(dst, placed{start: b.to, block: true, rect: ui.Rect{Y: float32(top - ed.scrollY), H: b.height}})
		}
	})
	return dst
}

// buildBlock builds the element of the block p placed, which its layout
// measures.
func (ed *editor) buildBlock(c *ui.Context, p placed) {
	v := &ed.view
	if p.start >= len(v.blocks) {
		return
	}
	b := &v.blocks[p.start]
	e := ui.Box(c).Key("block " + strconv.Itoa(b.id)).Absolute().
		Left(ed.textX - ed.gutterW).Top(p.rect.Y).
		Width(max(0, ed.laidW-ed.textX-ed.trackWidth()-4))
	ed.describeElement(e, b.Label, b.Interactive)
	built := builtBlock{p.start, b.id, e}
	// What the last frame laid out, for a frame that paints none, as of
	// an editor as high as its content out of view.
	ed.measureBlock(built)
	ed.built = append(ed.built, built)
	b.Content(c, e, b.Block)
}

// measureBlock has a block as high as its element was laid out, in the
// frame the element belongs to.
func (ed *editor) measureBlock(built builtBlock) {
	v := &ed.view
	if built.i >= len(v.blocks) || v.blocks[built.i].id != built.id || !built.e.Valid() {
		return
	}
	r := built.e.LayoutBounds()
	if r.W <= 0 {
		return
	}
	b := &v.blocks[built.i]
	if !b.measured || b.height != r.H {
		b.height, b.measured = r.H, true
		v.blocksStale = true
	}
}

// measureBlocks has the blocks built as high as this frame laid them
// out, before their lines are placed around them.
func (ed *editor) measureBlocks() {
	for _, b := range ed.built {
		ed.measureBlock(b)
	}
}
