package editor

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/egoist/mygo/internal/diff"
	"github.com/egoist/mygo/internal/l10n"
	"github.com/egoist/mygo/ui"
)

// diffSide shows a side of a diff in an editor.
type diffSide struct {
	d       *DiffState
	old     bool
	unified bool
	hide    []lineSpan
	blks    []block
	// hideV counts how often the side's hidden lines changed (setSide):
	// what the diff's find keys its matches on, for the shown lines only.
	hideV int
}

func (s *diffSide) hidden() []lineSpan { return s.hide }

func (s *diffSide) blocks() []block { return s.blks }

// hunkOf returns the hunk holding line of the side, or -1.
func (s *diffSide) hunkOf(line int) int {
	hs := s.d.model.hunks
	if s.old {
		i := sort.Search(len(hs), func(i int) bool { return hs[i].A1 > line })
		if i < len(hs) && hs[i].A0 <= line {
			return i
		}
		return -1
	}
	i := sort.Search(len(hs), func(i int) bool { return hs[i].B1 > line })
	if i < len(hs) && hs[i].B0 <= line {
		return i
	}
	return -1
}

// empty reports whether the side's text is empty, as of a file added or
// deleted: the hunks count no lines of it, as git does, though its
// editor keeps one, which is not numbered, selected or commented on.
func (s *diffSide) empty() bool {
	if s.old {
		return s.d.eds[0].view.text.Len() == 0
	}
	return s.d.eds[1].view.text.Len() == 0
}

func (s *diffSide) colors(ed *editor) diffColors { return ed.theme.diff(isDark(ed.theme.Background)) }

func (s *diffSide) lineStyle(line int) lineLook {
	d := s.d
	var st lineLook
	ed := &d.eds[1]
	if s.old {
		ed = &d.eds[0]
	}
	if s.hunkOf(line) >= 0 {
		dc := s.colors(ed)
		if s.old {
			st.bg, st.gutter, st.number = dc.deletedLine, dc.deletedGutter, dc.deleted
			st.bar, st.stripes, st.sign = dc.deleted, true, "-"
		} else {
			st.bg, st.gutter, st.number = dc.addedLine, dc.addedGutter, dc.added
			st.bar, st.sign = dc.added, "+"
		}
		if d.NoBackground {
			st.bg, st.gutter = ui.Color{}, ui.Color{}
		}
		if d.Indicators != DiffBars {
			st.bar = ui.Color{}
		}
	}
	side := DiffNew
	if s.old {
		side = DiffOld
	}
	st = s.selectedLook(ed, side, line, st)
	if s.hunkOf(line) >= 0 && !s.empty() {
		st = s.checkLook(ed, side, line, st)
	}
	return st
}

// checkLook returns st with the check box of a changed line of side
// (LineChecks), its gutter tinted when checked, as GitHub Desktop's;
// with NoCheckMarks, the tint alone.
func (s *diffSide) checkLook(ed *editor, side DiffSide, line int, st lineLook) lineLook {
	d := s.d
	if !d.LineChecks {
		return st
	}
	checked := d.lineChecked(side, line)
	if !d.NoCheckMarks {
		st.check = 1
		if checked {
			st.check = 2
		}
	}
	if !checked {
		return st
	}
	g := st.gutter
	if g.A == 0 {
		g = ed.theme.Background
	}
	st.gutter = mixLab(g, ed.theme.Modified, 0.35)
	return st
}

// selectedLook returns st tinted as a line selected by its number
// (LineSelection) when the line of side is, or else st: a line of a
// side's text, or a deleted line of a unified view.
func (s *diffSide) selectedLook(ed *editor, side DiffSide, line int, st lineLook) lineLook {
	d := s.d
	if !d.hasSel || d.selected.Side != side || line < d.selected.Start || line > d.selected.End {
		return st
	}
	m := ed.theme.Modified
	bg := st.bg
	if bg.A == 0 {
		bg = ed.theme.Background
	}
	st.bg = mixLab(bg, m, 0.18)
	g := st.gutter
	if g.A == 0 {
		g = ed.theme.Background
	}
	st.gutter = mixLab(g, m, 0.3)
	st.number = m
	return st
}

func (s *diffSide) words(line int) []diffRange {
	d := s.d
	if d.Inline == DiffInlineNone {
		return nil
	}
	i := s.hunkOf(line)
	if i < 0 {
		return nil
	}
	w := d.model.wordsOf(i, d.Inline)
	if s.old {
		return w.oldWords[line]
	}
	return w.newWords[line]
}

func (s *diffSide) number(line int) int {
	if s.empty() {
		return -1
	}
	if pt := s.d.partial; pt != nil {
		return pt.fileLine(s.old || false, line) + 1
	}
	return line + 1
}

// paintBlock paints a block in r, whose top is at top in the content: a
// filler of rows a side lacks, hatched, or the deleted lines of a unified
// view. Separators leave their room to the elements over it
// (buildSeparators).
func (s *diffSide) paintBlock(p *ui.Painter, ed *editor, b *block, r ui.Rect, top float64) {
	ct := ed.theme
	switch b.kind {
	case blockSpacer:
		if b.from == 0 {
			return
		}
		// A filler: hatched at -45°, every 8 DIPs from its bottom-left
		// corner, as Pierre's. The corner is in float64: r's float32 is
		// a DIP off, or more, millions of DIPs down a file, which would
		// shake the stripes as the view scrolls.
		text := ui.Rect{X: r.X + ed.gutterW, Y: r.Y, W: r.W - ed.gutterW, H: r.H}
		corner := [2]float64{float64(text.X), float64(ed.r.Y) + top + float64(b.height) - ed.scrollY}
		c := ct.Text.Alpha(0.07)
		p.Clip(text, 0, func() {
			fillerStripes(p.Visible(), corner, func(from, to [2]float32) {
				p.StrokePolyline([][2]float32{from, to}, 1, c)
			})
		})
	case blockDeleted:
		s.paintDeleted(p, ed, b, r)
	}
}

// fillerStripes calls fn with the stripes of a filler whose bottom-left
// corner is at corner, from a at v's bottom to b at its top, v being what
// shows of it: lines at -45°, up to the right, every 8 DIPs along its
// bottom from the corner. They are those that cross v alone, and their
// pieces from v's bottom to its top, all of a size so that they share a
// mask without a GPU: a filler across a whole file added is millions of
// DIPs high. Stroke each alone: on a GPU, a translucent line of one
// segment takes no mask. That one would be as wide and as high as v is
// high, rasterized again as its height changes, and drawn that large for
// each stripe.
func fillerStripes(v ui.Rect, corner [2]float64, fn func(a, b [2]float32)) {
	if v.W <= 0 || v.H <= 0 {
		return
	}
	// A stripe is where x+y is the corner's, give or take 8s: s, crossing
	// v when s-y1 < x1 and s-y0 > x0.
	x0, y0 := float64(v.X), float64(v.Y)
	x1, y1 := x0+float64(v.W), y0+float64(v.H)
	phase := corner[0] + corner[1]
	lo, hi := x0+y0, x1+y1
	for s := phase + 8*(math.Floor((lo-phase)/8)+1); s < hi; s += 8 {
		fn([2]float32{float32(s - y1), float32(y1)}, [2]float32{float32(s - y0), float32(y0)})
	}
}

// paintDeleted paints the lines a unified view shows deleted: the old
// text's, tinted, with their numbers in the gutter.
func (s *diffSide) paintDeleted(p *ui.Painter, ed *editor, b *block, r ui.Rect) {
	d := s.d
	ov := &d.oldView
	ov.setTheme(ed.theme)
	dc := s.colors(ed)
	lh := ed.view.lineHeight
	ox := r.X + ed.textX - ed.scrollX
	text := ui.Rect{X: r.X + ed.gutterW, Y: r.Y, W: r.W - ed.gutterW, H: r.H}
	var cc runColors
	var rects []ui.Rect
	words := d.model.wordsOf(b.hunk, d.Inline)
	// The lines in the clip, from the first whose rows or annotations
	// reach into it, by the tops prepare kept: a run as long as a file
	// deleted is not laid out from its first line.
	type placed struct {
		line int
		y, h float32
		l    *lineLayout
	}
	var lines []placed
	tops := d.deletedTops[b.hunk]
	vis := p.Visible()
	y0, y1 := float64(vis.Y-r.Y), float64(vis.Y+vis.H-r.Y)
	first := b.from + max(0, sort.Search(len(tops), func(i int) bool { return tops[i] > y0 })-1)
	last := first
	for last < b.to && tops[last-b.from] <= y1 {
		last++
	}
	ready := ov.advanceSyntax(last, 4*time.Millisecond)
	ov.approx = false
	for line := first; line < last; line++ {
		l := ov.line(line)
		y := r.Y + float32(tops[line-b.from])
		h := float32(len(l.rows)) * lh
		if y+h >= vis.Y {
			lines = append(lines, placed{line, y, h, l})
		}
	}
	for _, pl := range lines {
		look := lineLook{number: dc.deleted}
		if !d.NoBackground {
			look.bg, look.gutter = dc.deletedLine, dc.deletedGutter
		}
		look = s.selectedLook(ed, DiffOld, pl.line, look)
		look = s.checkLook(ed, DiffOld, pl.line, look)
		if look.bg.A != 0 {
			p.Fill(ui.Rect{X: text.X, Y: pl.y, W: text.W, H: pl.h}, look.bg, 0)
		}
		if look.gutter.A != 0 {
			p.Fill(ui.Rect{X: r.X, Y: pl.y, W: ed.gutterW, H: pl.h}, look.gutter, 0)
		}
		if !d.NoLineNumbers {
			n := pl.line + 1
			if pt := d.partial; pt != nil {
				n = pt.fileLine(true, pl.line) + 1
			}
			num := strconv.Itoa(n)
			lay := ed.view.shaped.get(num, ed.view.style)
			nr := rowLayout{pieces: []rowPiece{{to: len(num), l: lay, ascii: true, width: lay.Width}}, to: len(num)}
			var nc runColors
			col := look.number
			ed.view.paintRow(p, &lineLayout{disp: num}, &nr, r.X+ed.gutterW-4-ed.view.charWidth*0.5-ed.buttonRoom()-lay.Width, pl.y, &nc, func(int, ui.Color) ui.Color { return col })
		}
		if d.Indicators == DiffBars {
			ed.paintBar(p, ui.Rect{X: r.X, Y: pl.y, W: 4, H: pl.h}, lineLook{bar: dc.deleted, stripes: true})
		}
		if look.check != 0 {
			ed.paintCheck(p, r.X, pl.y, look.check == 2)
		}
	}
	p.Clip(text, 0, func() {
		for _, pl := range lines {
			l := pl.l
			ov.colorsOf(l, &cc)
			if d.Indicators == DiffClassic {
				s.paintSign(p, ed, "-", r.X+ed.textX-ed.view.charWidth*1.5, pl.y, dc.deleted)
			}
			for ri := range l.rows {
				row := &l.rows[ri]
				ry := pl.y + float32(ri)*lh
				// Only the rows in view: a long line wraps into
				// thousands, each shaped and painted otherwise.
				if ry > p.Visible().Y+p.Visible().H || ry+lh < p.Visible().Y {
					continue
				}
				if d.Inline != DiffInlineNone {
					for _, w := range words.oldWords[pl.line] {
						rects = mergeRects(ov.rangeRects(l, row, l.toDisp(w.start, false), l.toDisp(w.end, false), rects[:0]))
						for _, rc := range rects {
							p.Fill(ui.Rect{X: ox + rc.X, Y: ry, W: rc.W, H: lh}, dc.deletedWord, 3)
						}
					}
				}
				ov.paintRow(p, l, row, ox, ry, &cc, nil)
			}
		}
	})
	// Frames go on while the highlighter works, or the lines show its
	// guesses, as the editor's own lines do.
	if !ready || ov.approx {
		p.AnimationFrame()
	}
}

// paintSign paints a classic + or - before a line.
func (s *diffSide) paintSign(p *ui.Painter, ed *editor, sign string, x, y float32, c ui.Color) {
	v := &ed.view
	lay := v.shaped.get(sign, v.style)
	row := rowLayout{pieces: []rowPiece{{to: 1, l: lay, ascii: true, width: lay.Width}}, to: 1}
	var cc runColors
	v.paintRow(p, &lineLayout{disp: sign}, &row, x, y, &cc, func(int, ui.Color) ui.Color { return c })
}

// blockInput takes presses on the numbers of lines, which select them,
// and on the deleted lines of a unified view. Separators and buttons are
// elements of their own (buildSeparators, buildBlocks).
func (s *diffSide) blockInput(ed *editor, b *block, ev ui.InputEvent, r ui.Rect) bool {
	d := s.d
	side := DiffNew
	if s.old {
		side = DiffOld
	}
	if b != nil {
		switch b.kind {
		case blockDeleted:
			// Lines of the old text: their gutters check them, or
			// their numbers select them.
			if ev.X >= ed.gutterW || ev.Kind != ui.InputPointerDown {
				return true
			}
			line := d.deletedLineAt(b, float64(ev.Y-r.Y))
			switch {
			case d.LineChecks:
				_, cy := ed.toContent(ev.X, ev.Y)
				d.pressCheck(1, DiffOld, line, cy)
			case d.LineSelection:
				s.pressLine(ed, DiffOld, line, ev.Mods)
			}
			return true
		}
		return false
	}
	// A press on the gutter of a line, in r.
	if ev.Kind != ui.InputPointerDown {
		return false
	}
	_, cy := ed.toContent(ev.X, ev.Y)
	line := ed.view.heights.lineAt(cy)
	if d.OnGutterButton != nil && !s.empty() && ed.onButton(ev.X, ev.Y) {
		s.pressButton(ed, side, line)
		return true
	}
	if d.LineChecks && !s.empty() && s.hunkOf(line) >= 0 {
		i := 1
		if s.old {
			i = 0
		}
		d.pressCheck(i, side, line, cy)
		return true
	}
	if d.LineSelection && !s.empty() {
		s.pressLine(ed, side, line, ev.Mods)
		return true
	}
	return false
}

// pressLine selects the line whose number is pressed, or extends the
// selection to it with Shift, and has the drag that follows select the
// lines it goes over (dragLine).
func (s *diffSide) pressLine(ed *editor, side DiffSide, line int, mods ui.Modifiers) {
	d := s.d
	d.selectLine(side, line, mods&ui.Shift != 0)
	if d.hasSel {
		ed.startDrag(func(_ float32, cy float64) { s.dragLine(ed, cy) }, nil)
	}
}

// pressButton selects the line whose gutter button is pressed, or keeps
// the lines selected when it is among them, and has the drag that
// follows select from it to the line under the pointer (dragLine). The
// release calls OnGutterButton with the lines selected, as a review
// comments on them.
func (s *diffSide) pressButton(ed *editor, side DiffSide, line int) {
	d := s.d
	if !d.hasSel || d.selected.Side != side || line < d.selected.Start || line > d.selected.End {
		d.selected, d.hasSel = DiffLineRange{Side: side, Start: line, End: line}, true
		if d.OnLineSelection != nil {
			d.OnLineSelection(d.selected)
		}
	}
	// The lines selected stay until the pointer leaves the line pressed.
	moved := false
	ed.startDrag(func(_ float32, cy float64) {
		if !moved {
			if sd, l, ok := s.lineAt(ed, cy); !ok || (sd == side && l == line) {
				return
			}
			moved, d.selAnchor = true, line
		}
		s.dragLine(ed, cy)
	}, func() {
		d.OnGutterButton(d.selected)
	})
}

// dragLine extends the lines selected to the line at height y of the
// content (lineAt).
func (s *diffSide) dragLine(ed *editor, y float64) {
	if side, line, ok := s.lineAt(ed, y); ok {
		s.d.dragLine(side, line)
	}
}

// lineAt returns the line at height y of the content: a deleted line of
// a unified view, or a line of the side's text. Blocks that hold no line,
// as annotations, hold none.
func (s *diffSide) lineAt(ed *editor, y float64) (DiffSide, int, bool) {
	d := s.d
	if b, r, ok := ed.blockAt(y); ok {
		if b.kind == blockDeleted {
			return DiffOld, d.deletedLineAt(b, y-ed.scrollY-float64(r.Y)), true
		}
		return 0, 0, false
	}
	if s.empty() {
		return 0, 0, false
	}
	side := DiffNew
	if s.old {
		side = DiffOld
	}
	return side, ed.view.heights.lineAt(y), true
}

// buildBlocks builds what blocks hold of elements in view: annotations,
// the actions of hunks and the gutter's button.
func (s *diffSide) buildBlocks(c *ui.Context, ed *editor) {
	d := s.d
	v := &ed.view
	h := &v.heights
	if ed.r.W <= 0 {
		return
	}
	y0, y1 := ed.buildRange()
	first, last := h.lineAt(y0), h.lineAt(y1)
	side := DiffNew
	if s.old {
		side = DiffOld
	}
	// Annotations under their lines, after one another on a line.
	if d.RenderAnnotation != nil && len(d.Annotations) > 0 {
		stacked := map[[2]int]float64{} // by where they go: 0 a line, 1 a deleted line
		for _, a := range d.Annotations {
			var top float64
			var at [2]int
			line := a.Line
			if s.unified && a.Side == DiffOld {
				if nl, ok := d.model.newLineOf(a.Line); ok {
					line = nl
				} else {
					line = -1
				}
			}
			switch {
			case a.Side == side || s.unified && a.Side == DiffOld && line >= 0:
				if line < first || line > last {
					continue
				}
				g := h.geometry(line)
				if g.hidden {
					continue
				}
				top = g.below
				// After the blocks below it that come first.
				_, below := h.blocksOf(line)
				for _, b := range below {
					if b.kind == blockAnnotation {
						break
					}
					top += float64(b.height)
				}
				at = [2]int{0, line}
			case s.unified && a.Side == DiffOld:
				// In the block of deleted lines it is in.
				found := false
				for line := first; line <= last && !found; line++ {
					above, below := h.blocksOf(line)
					g := h.geometry(line)
					for _, blks := range [][]block{above, below} {
						bt := g.top
						if len(blks) > 0 && blks[0].below {
							bt = g.below
						}
						for _, b := range blks {
							if b.kind == blockDeleted && a.Line >= b.from && a.Line < b.to {
								// Under the line's rows.
								rows := float64(float32(d.oldView.heights.wraps.rows(a.Line)) * v.lineHeight)
								top, found = bt+d.deletedTops[b.hunk][a.Line-b.from]+rows, true
								break
							}
							bt += float64(b.height)
						}
						if found {
							break
						}
					}
				}
				if !found {
					continue
				}
				at = [2]int{1, a.Line}
			default:
				continue
			}
			top += stacked[at]
			n := stacked[at]
			stacked[at] += float64(a.Height)
			a := a
			box := ui.Box(c).Key(fmt.Sprint("annotation ", a.Side, " ", a.Line, " ", a.Key, " ", n)).Absolute().
				Left(ed.gutterW).Top(float32(top - ed.scrollY)).Width(ed.r.W - ed.gutterW).Height(a.Height).HandleInput(shield)
			box.Children(func() { d.RenderAnnotation(c, a) })
		}
	}
	// The actions of a change go left of the line's menu button.
	right := ed.trackWidth() + 6
	if d.LineMenu != nil {
		right += ed.view.lineHeight + 2
	}
	// Revert and Keep on the hunk under the pointer, or the caret's.
	if d.HunkActions && !s.old {
		active := d.activeHunk()
		for i, hk := range d.model.hunks {
			if i != active {
				continue
			}
			line := hk.B0
			if line > last+1 || hk.B1 < first && hk.B0 < first {
				continue
			}
			var top float64
			if s.unified && hk.A1 > hk.A0 {
				if line >= v.text.LineCount() {
					line = v.text.LineCount() - 1
					top = h.geometry(line).below
				} else {
					top = h.geometry(line).top
					above, _ := h.blocksOf(line)
					for _, b := range above {
						if b.kind == blockDeleted {
							break
						}
						top += float64(b.height)
					}
				}
			} else {
				line = min(line, v.text.LineCount()-1)
				top = h.geometry(line).rows
			}
			if top < y0-float64(v.lineHeight) || top > y1 {
				continue
			}
			bar := ui.Row(c).Key(fmt.Sprint("hunk actions ", i)).Absolute().Right(right).Top(float32(top-ed.scrollY) + 2).
				Cursor(ui.CursorDefault).KeepFocus().HandleInput(shield)
			render := d.RenderHunkActions
			if render == nil {
				render = DefaultHunkActions
			}
			render(c, bar, &DiffHunkActions{Index: i, Hunk: DiffHunk{hk.A0, hk.A1, hk.B0, hk.B1}, d: d})
		}
	}
	// A check box of each change's lines in view, over its change bar.
	if d.LineChecks {
		for i, hk := range d.model.hunks {
			if top, bottom, ok := s.hunkSpan(ed, hk); ok && bottom >= y0 && top <= y1 {
				s.buildHunkCheck(c, ed, i, hk, top, bottom)
			}
		}
	}
	// The gutter's button, on the line under the pointer.
	if d.OnGutterButton != nil && !s.empty() && ed.pointerIn && ed.hoverLine >= 0 && ed.hoverLine < v.text.LineCount() && ed.pointer.X < ed.gutterW+40 {
		g := h.geometry(ed.hoverLine)
		if !g.hidden {
			line := DiffLineRange{Side: side, Start: ed.hoverLine, End: ed.hoverLine}
			// The editor takes its press, which a drag follows
			// (pressButton); assistive technology presses it alone.
			room := ed.buttonRoom()
			ui.Box(c).Key("gutter button").Absolute().Left(ed.gutterW - room).Top(float32(g.rows-ed.scrollY) + 2).IgnoreInput(true).Children(func() {
				btn := ui.ButtonBase(c).Size(room-4, v.lineHeight-4).KeepFocus()
				render := d.RenderGutterButton
				if render == nil {
					render = DefaultGutterButton
				}
				render(c, btn, line)
				if btn.Clicked() {
					d.OnGutterButton(line)
				}
			})
		}
	}
	if d.LineMenu != nil {
		s.buildLineMenu(c, ed, side)
	}
}

// buildLineMenu builds the menu button at the right end of the line under
// the pointer, and of the line whose menu shows in the window, which
// stays on its line while the pointer goes on to others. A system menu
// takes the pointer while it shows.
func (s *diffSide) buildLineMenu(c *ui.Context, ed *editor, side DiffSide) {
	d := s.d
	n := ed.view.text.LineCount()
	i := int(side)
	held := -1
	if d.menuHeld[i] && d.menuLine[i] < n {
		held = d.menuLine[i]
	}
	d.menuHeld[i] = false
	if held >= 0 {
		s.lineMenuButton(c, ed, side, held)
	}
	if hover := ed.hoverLine; !s.empty() && !ed.gesture.active && hover >= 0 && hover < n && hover != held {
		s.lineMenuButton(c, ed, side, hover)
	}
}

// lineMenuButton builds the menu button of a line, whose menu LineMenu
// fills for the line, or for the lines selected when it is among them.
func (s *diffSide) lineMenuButton(c *ui.Context, ed *editor, side DiffSide, line int) {
	d := s.d
	v := &ed.view
	g := v.heights.geometry(line)
	if g.hidden {
		return
	}
	lines := DiffLineRange{Side: side, Start: line, End: line}
	if d.hasSel && d.selected.Side == side && line >= d.selected.Start && line <= d.selected.End {
		lines = d.selected
	}
	lh := v.lineHeight
	t := c.Theme()
	ui.Box(c).Key(fmt.Sprint("line menu ", line)).Absolute().Right(ed.trackWidth() + 6).Top(float32(g.rows-ed.scrollY) + 2).HandleInput(shield).Children(func() {
		btn := ui.ButtonBase(c).Size(lh-4, lh-4).Radius(4).Label(l10n.In(c.Locale()).LineActions()).Cursor(ui.CursorPointer)
		btn.Draw(func(p *ui.Painter, r ui.Rect) {
			bg := t.Surface
			if btn.Hovered() || btn.Focused() {
				bg = t.Border
			}
			p.Fill(r, bg, 4)
			cx, cy := r.X+r.W/2, r.Y+r.H/2
			var chevron ui.Path
			chevron.MoveTo(cx-3.5, cy-1.75).LineTo(cx, cy+1.75).LineTo(cx+3.5, cy-1.75)
			p.StrokePath(&chevron, 1.5, t.Text)
		})
		btn.Menu(func(m *ui.Menu) {
			d.menuHeld[int(side)], d.menuLine[int(side)] = true, line
			d.LineMenu(m, lines)
			s.lineMenuItems(m, ed, lines)
		})
	})
}

// lineMenuItems adds the diff's own items to a line's menu, after the
// app's: copying the lines, selecting all the side's text, and revealing
// the unchanged lines hidden above and below them.
func (s *diffSide) lineMenuItems(m *ui.Menu, ed *editor, lines DiffLineRange) {
	tx := ed.texts
	m.Separator()
	if m.Item(tx.Copy()).Chosen() {
		r := &ed.view.text
		ed.services.WriteClipboard(r.Slice(r.LineStart(lines.Start), r.LineEnd(lines.End)))
	}
	if m.Item(tx.SelectAll()).Chosen() {
		ed.run(SelectAll)
	}
	m.Separator()
	above, below := s.gapsAround(lines)
	if m.Item(tx.ExpandUp()).Disabled(above == nil).Chosen() {
		s.d.expand(above.a, -1, false)
	}
	if m.Item(tx.ExpandDown()).Disabled(below == nil).Chosen() {
		s.d.expand(below.a, 1, false)
	}
}

// gapsAround returns the nearest gaps hiding lines of the side above and
// below the lines, or nil.
func (s *diffSide) gapsAround(lines DiffLineRange) (above, below *diffGap) {
	for i := range s.d.gapList {
		g := &s.d.gapList[i]
		if g.hidden == 0 {
			continue
		}
		start := g.hideB
		if s.old {
			start = g.hideA
		}
		switch {
		case start+g.hidden-1 < lines.Start:
			above = g
		case start > lines.End && below == nil:
			below = g
		}
	}
	return above, below
}

// hunkSpan returns the heights of the content a change's lines show
// from and to on the side, the deleted lines among them in a unified
// view, and whether it shows lines of the change.
func (s *diffSide) hunkSpan(ed *editor, hk diff.Hunk) (top, bottom float64, ok bool) {
	d := s.d
	h := &ed.view.heights
	from, to := hk.B0, hk.B1
	if s.old {
		from, to = hk.A0, hk.A1
	}
	if !s.unified {
		if to <= from || s.empty() {
			return 0, 0, false
		}
		return h.geometry(from).rows, h.geometry(to - 1).below, true
	}
	// Above the new lines, the deleted.
	if hk.A1 > hk.A0 {
		top = d.deletedTop(hk.A0)
		last := hk.A1 - 1
		bottom = d.deletedTop(last) + float64(float32(d.oldView.heights.wraps.rows(last))*ed.view.lineHeight)
	}
	if to > from && !s.empty() {
		if hk.A1 <= hk.A0 {
			top = h.geometry(from).rows
		}
		bottom = h.geometry(to - 1).below
	}
	return top, bottom, bottom > top
}

// buildHunkCheck builds the check box of change i's lines over its
// change bar, from height top to bottom of the content: a click checks
// them all, or unchecks them when they all are.
func (s *diffSide) buildHunkCheck(c *ui.Context, ed *editor, i int, hk diff.Hunk, top, bottom float64) {
	d := s.d
	var lines []DiffLineRange
	if hk.A1 > hk.A0 {
		lines = append(lines, DiffLineRange{Side: DiffOld, Start: hk.A0, End: hk.A1 - 1})
	}
	if hk.B1 > hk.B0 {
		lines = append(lines, DiffLineRange{Side: DiffNew, Start: hk.B0, End: hk.B1 - 1})
	}
	checked, all := false, true
	for _, r := range lines {
		for l := r.Start; l <= r.End; l++ {
			if d.lineChecked(r.Side, l) {
				checked = true
			} else {
				all = false
			}
		}
	}
	// A shield, as the gutter button's, keeps the press from the editor's
	// line check below it.
	ui.Box(c).Key(fmt.Sprint("hunk check ", i)).Absolute().Left(0).Top(float32(top - ed.scrollY)).HandleInput(shield).Children(func() {
		box := ui.ButtonBase(c).Size(6, float32(bottom-top)).Role(ui.RoleCheckBox).Label(l10n.In(c.Locale()).Change()).
			Cursor(ui.CursorPointer).KeepFocus()
		switch {
		case all:
			box.Checked(true)
		case checked:
			box.Mixed()
		default:
			box.Checked(false)
		}
		m := ed.theme.Modified
		box.Draw(func(p *ui.Painter, r ui.Rect) {
			if box.Hovered() {
				p.Fill(r, m, 2)
			}
		})
		if box.Clicked() && d.OnCheckLines != nil {
			d.OnCheckLines(lines, !all)
		}
	})
}
