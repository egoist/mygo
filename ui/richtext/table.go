package richtext

import (
	"slices"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown/parser"
)

// TableRow is what row of a table a paragraph is (ParagraphStyle.Row).
type TableRow uint8

const (
	// NoRow is a paragraph that is not a row of a table.
	NoRow TableRow = iota
	// HeaderRow is a table's first row, which starts it.
	HeaderRow
	// BodyRow is a row after it.
	BodyRow
)

const (
	// CellSeparator parts a row's cells.
	CellSeparator = '\t'
	// CellLineBreak breaks a cell's line, as <br> in Markdown.
	CellLineBreak = ' '
)

// columnsOf returns a table's alignments as ParagraphStyle.Columns.
func columnsOf(aligns []parser.Align) string {
	var b strings.Builder
	for _, a := range aligns {
		switch a {
		case parser.AlignLeft:
			b.WriteByte('l')
		case parser.AlignCenter:
			b.WriteByte('c')
		case parser.AlignRight:
			b.WriteByte('r')
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

// columnAlign returns how a column of columns aligns its cells' lines.
func columnAlign(columns string, k int) ui.Align {
	if k < len(columns) {
		switch columns[k] {
		case 'c':
			return ui.Center
		case 'r':
			return ui.End
		}
	}
	return ui.Start
}

// splitCells returns a row's runs as its cells' runs, at the tabs.
func splitCells(runs []Run) [][]Run { return splitRuns(runs, CellSeparator) }

// splitRuns returns runs cut at each sep, which goes.
func splitRuns(runs []Run, sep rune) [][]Run {
	parts := [][]Run{nil}
	for _, r := range runs {
		for {
			part, rest, cut := strings.Cut(r.Text, string(sep))
			if part != "" {
				parts[len(parts)-1] = append(parts[len(parts)-1], Run{Text: part, Attributes: r.Attributes})
			}
			if !cut {
				break
			}
			parts = append(parts, nil)
			r.Text = rest
		}
	}
	return parts
}

// cellText returns s as a cell's text: no tabs, its lines broken by line
// separators.
func cellText(s string) string {
	return strings.NewReplacer("\t", " ", "\r\n", string(CellLineBreak), "\n", string(CellLineBreak)).Replace(s)
}

// sameTable reports whether a body row of style b, right after a row of
// style a, is a row of a's table: at the same depth of quotation and
// indentation. The editor lays tables out by it, and Markdown writes them.
func sameTable(a, b ParagraphStyle) bool {
	return a.Row != NoRow && b.Row == BodyRow && a.quoteDepth() == b.quoteDepth() && a.Indent == b.Indent
}

// tableEnd returns the paragraph after the table whose first row is
// paras[i].
func tableEnd(paras []Paragraph, i int) int {
	j := i + 1
	for j < len(paras) && sameTable(paras[i].Style, paras[j].Style) {
		j++
	}
	return j
}

// writeMarkdownTable writes the rows as a GFM table: the first as its
// header, the delimiter row with the first row's alignments, every row as
// many cells as the table's widest. Markdown has no table without a
// header: rows with none before them go under an empty one, and stay
// body rows.
func writeMarkdownTable(b *strings.Builder, rows []Paragraph, quote string) {
	cols := rows[0].Style.Columns
	n := len(cols)
	cells := make([][][]Run, len(rows))
	for i, r := range rows {
		cells[i] = splitCells(r.Runs)
		n = max(n, len(cells[i]))
	}
	line := func(cs [][]Run) {
		b.WriteString(quote + "|")
		for k := range n {
			s := ""
			if k < len(cs) {
				s = cellMarkdown(cs[k])
			}
			b.WriteString(" " + s + " |")
		}
	}
	if rows[0].Style.Row != HeaderRow {
		cells = append([][][]Run{nil}, cells...)
	}
	for i := range cells {
		if i > 0 {
			b.WriteByte('\n')
		}
		line(cells[i])
		if i == 0 {
			b.WriteString("\n" + quote + "|")
			for k := range n {
				switch {
				case k < len(cols) && cols[k] == 'l':
					b.WriteString(" :--- |")
				case k < len(cols) && cols[k] == 'c':
					b.WriteString(" :---: |")
				case k < len(cols) && cols[k] == 'r':
					b.WriteString(" ---: |")
				default:
					b.WriteString(" --- |")
				}
			}
		}
	}
}

// cellMarkdown returns a cell's runs as Markdown in a table: each of its
// lines written on its own and joined by <br>, as a code span or a mark
// can't hold one, and the pipes escaped, in code spans too, as GFM's
// tables have them.
func cellMarkdown(runs []Run) string {
	lines := splitRuns(runs, CellLineBreak)
	out := make([]string, len(lines))
	for i, l := range lines {
		var c strings.Builder
		writeMarkdownRuns(&c, l)
		out[i] = strings.ReplaceAll(c.String(), "|", "\\|")
	}
	return strings.Join(out, "<br>")
}

// paraText is a paragraph laid out: one text layout, or a row's cells
// side by side, each a layout of its own. Indices are the paragraph's
// UTF-16 units, its tabs included.
type paraText interface {
	CaretAt(p ui.TextCaretPosition) ui.Rect
	PositionAt(pt ui.Point) ui.TextCaretPosition
	MoveCaret(p ui.TextCaretPosition, dir int) ui.TextCaretPosition
	Objects() []ui.TextObject
	Size() (float32, float32)
	PreviousBoundary(u int) int
	NextBoundary(u int) int
	SelectionRects(r ...ui.TextInputRange) []ui.Rect
	Draw(p *ui.Painter, x, y float32, c ui.Color)
}

// plainText is a paragraph that is one layout.
type plainText struct{ *ui.TextLayout }

func (t plainText) Draw(p *ui.Painter, x, y float32, c ui.Color) { p.TextLayout(t.TextLayout, x, y, c) }

// rowCell is a cell of a row laid out: its layout, where its text starts
// in the row, and its units in the row's, from its first to the tab after
// it (or the row's end).
type rowCell struct {
	l      *ui.TextLayout
	x, y   float32
	u0, u1 int
}

// rowText is a row laid out: its cells in its columns, from edges[k] to
// edges[k+1], as tall as its tallest cell and its padding.
type rowText struct {
	cells []rowCell
	edges []float32
	w, h  float32
}

func (t *rowText) cellAtUnit(u int) int {
	for k, c := range t.cells {
		if u <= c.u1 {
			return k
		}
	}
	return len(t.cells) - 1
}

func (t *rowText) cellAtX(x float32) int {
	k := 0
	for k+1 < len(t.cells) && x >= t.edges[k+1] {
		k++
	}
	return k
}

func (t *rowText) CaretAt(p ui.TextCaretPosition) ui.Rect {
	c := t.cells[t.cellAtUnit(p.Index)]
	r := c.l.CaretAt(ui.TextCaretPosition{Index: p.Index - c.u0, Affinity: p.Affinity})
	r.X, r.Y = r.X+c.x, r.Y+c.y
	return r
}

func (t *rowText) PositionAt(pt ui.Point) ui.TextCaretPosition {
	c := t.cells[t.cellAtX(pt.X)]
	p := c.l.PositionAt(ui.Point{X: pt.X - c.x, Y: pt.Y - c.y})
	p.Index += c.u0
	return p
}

// MoveCaret moves within a cell, and over the tab at its edge to the next
// or the one before.
func (t *rowText) MoveCaret(p ui.TextCaretPosition, dir int) ui.TextCaretPosition {
	k := t.cellAtUnit(p.Index)
	c := t.cells[k]
	local := ui.TextCaretPosition{Index: p.Index - c.u0, Affinity: p.Affinity}
	n := c.l.MoveCaret(local, dir)
	if n.Index != local.Index {
		n.Index += c.u0
		return n
	}
	switch {
	case dir > 0 && k+1 < len(t.cells):
		return ui.TextCaretPosition{Index: t.cells[k+1].u0, Affinity: ui.TextDownstream}
	case dir < 0 && k > 0:
		return ui.TextCaretPosition{Index: t.cells[k-1].u1, Affinity: ui.TextDownstream}
	}
	return p
}

func (t *rowText) Objects() []ui.TextObject {
	var out []ui.TextObject
	for _, c := range t.cells {
		for _, o := range c.l.Objects() {
			o.Index += c.u0
			o.Rect.X, o.Rect.Y = o.Rect.X+c.x, o.Rect.Y+c.y
			out = append(out, o)
		}
	}
	return out
}

func (t *rowText) Size() (float32, float32) { return t.w, t.h }

func (t *rowText) PreviousBoundary(u int) int {
	k := t.cellAtUnit(u)
	c := t.cells[k]
	if u <= c.u0 {
		return max(u-1, 0)
	}
	return c.l.PreviousBoundary(u-c.u0) + c.u0
}

func (t *rowText) NextBoundary(u int) int {
	k := t.cellAtUnit(u)
	c := t.cells[k]
	if u >= c.u1 {
		return u + 1
	}
	return c.l.NextBoundary(u-c.u0) + c.u0
}

func (t *rowText) SelectionRects(ranges ...ui.TextInputRange) []ui.Rect {
	var out []ui.Rect
	for _, r := range ranges {
		for _, c := range t.cells {
			s, e := max(r.Start, c.u0), min(r.End, c.u1)
			if s >= e {
				continue
			}
			for _, x := range c.l.SelectionRects(ui.TextInputRange{Start: s - c.u0, End: e - c.u0}) {
				x.X, x.Y = x.X+c.x, x.Y+c.y
				out = append(out, x)
			}
		}
	}
	return out
}

func (t *rowText) Draw(p *ui.Painter, x, y float32, col ui.Color) {
	for _, c := range t.cells {
		p.TextLayout(c.l, x+c.x, y+c.y, col)
	}
}

// cellPadding is the room around a cell's text, in DIPs.
func (look *look) cellPadding() (x, y float32) {
	return look.size * look.TableCellPadding[0], look.size * look.TableCellPadding[1]
}

// minColumnWidth is how narrow a column gets, in ems, its padding in it:
// an empty column stays wide enough to click into.
const minColumnWidth = 3

// rowColumns returns how many columns the table of a row of style st with
// cells cells has.
func rowColumns(st ParagraphStyle, cells int) int { return max(len(st.Columns), cells, 1) }

// layRow lays a row's spans out in its columns.
func (v *view) layRow(spans []ui.Span, st ParagraphStyle, font ui.Font, widths []float32) *rowText {
	// The spans by cell, a cell's lines broken where it has line
	// separators, and the units each starts and ends at.
	var cells [][]ui.Span
	var bounds [][2]int
	cur, u, u0 := []ui.Span(nil), 0, 0
	for _, s := range spans {
		text := s.Text
		for {
			part, rest, cut := strings.Cut(text, string(CellSeparator))
			if part != "" {
				x := s
				x.Text = strings.ReplaceAll(part, string(CellLineBreak), "\n")
				cur = append(cur, x)
				u += len(utf16.Encode([]rune(part)))
			}
			if !cut {
				break
			}
			cells, bounds = append(cells, cur), append(bounds, [2]int{u0, u})
			cur = nil
			u++
			u0 = u
			text = rest
		}
	}
	cells, bounds = append(cells, cur), append(bounds, [2]int{u0, u})
	n := len(widths)
	padX, padY := v.look.cellPadding()
	t := &rowText{edges: []float32{0}}
	for _, w := range widths {
		t.edges = append(t.edges, t.edges[len(t.edges)-1]+w)
	}
	t.w = t.edges[n]
	var tallest float32
	for k := range cells {
		colW := widths[min(k, n-1)]
		l := ui.ShapeRichTextWith(cells[k], font, ui.TextShape{Width: max(colW-2*padX, 1), Align: columnAlign(st.Columns, k)})
		_, h := l.Size()
		tallest = max(tallest, h)
		x := t.edges[min(k, n-1)] + padX
		t.cells = append(t.cells, rowCell{l: l, x: x, y: padY, u0: bounds[k][0], u1: bounds[k][1]})
	}
	t.h = tallest + 2*padY
	return t
}

// paintRow paints a row's grid behind its text: the header's fill, its
// lines between cells and around the table.
func (v *view) paintRow(p *ui.Painter, i int, st ParagraphStyle, t *rowText, x, y float32) {
	look := &v.look
	line := look.TableBorder
	if st.Row == HeaderRow {
		p.Fill(ui.Rect{X: x, Y: y, W: t.w, H: t.h}, look.TableHeaderBackground, 0)
	}
	if i == 0 || !sameTable(v.para(i-1).style, st) {
		p.Fill(ui.Rect{X: x, Y: y, W: t.w, H: 1}, line, 0)
	}
	p.Fill(ui.Rect{X: x, Y: y + t.h - 1, W: t.w, H: 1}, line, 0)
	for _, e := range t.edges {
		ex := min(x+e, x+t.w-1)
		p.Fill(ui.Rect{X: ex, Y: y, W: 1, H: t.h}, line, 0)
	}
}

// tableTab moves the caret to the end of the next cell (the one before
// with shift), across rows; Tab in a table's last cell adds a row.
func (v *view) tableTab(shift bool) bool {
	t := v.shown
	p := t.paraAt(v.caret)
	ps := t.paraStart(p)
	line := []rune(v.lineOf(p))
	c := v.caret - ps
	cellEnd := func(from int) int { // the end of the cell starting at from
		for k := from; k < len(line); k++ {
			if line[k] == CellSeparator {
				return k
			}
		}
		return len(line)
	}
	if !shift {
		end := cellEnd(c)
		if end < len(line) {
			v.moveTo(ps+cellEnd(end+1), ui.TextDownstream, false)
			return true
		}
		if p+1 < t.ParagraphCount() && sameTable(v.para(p).style, v.para(p+1).style) {
			next := t.paraStart(p + 1)
			v.moveTo(next+firstCellEnd(v.lineOf(p+1)), ui.TextDownstream, false)
			return true
		}
		v.addRow(p)
		return true
	}
	start := c
	for start > 0 && line[start-1] != CellSeparator {
		start--
	}
	if start > 0 {
		v.moveTo(ps+start-1, ui.TextDownstream, false)
		return true
	}
	if p > 0 && sameTable(v.para(p-1).style, v.para(p).style) {
		v.moveTo(ps-1, ui.TextDownstream, false)
	}
	return true
}

// firstCellEnd returns where a row's first cell ends, in runes.
func firstCellEnd(line string) int {
	if k := strings.IndexRune(line, CellSeparator); k >= 0 {
		return len([]rune(line[:k]))
	}
	return len([]rune(line))
}

// addRow adds an empty row after row p, of as many cells as its table
// has columns, and puts the caret in its first cell.
func (v *view) addRow(p int) {
	st := v.para(p).style
	n := rowColumns(st, strings.Count(v.lineOf(p), string(CellSeparator))+1)
	pe := v.shown.paraStart(p) + v.para(p).n
	v.record(false)
	v.edit(func(e *Edit) {
		e.ReplaceString(pe, pe, "\n"+strings.Repeat(string(CellSeparator), n-1), Attributes{})
		e.FormatParagraphs(pe+1, pe+1, func(s *ParagraphStyle) {
			s.Row, s.Columns, s.Heading, s.List = BodyRow, st.Columns, 0, NoList
		})
		e.Select(pe+1, pe+1)
	})
}

// tableEnter adds a row under the caret's; on an empty last row, it
// leaves the table, as Enter on an empty item leaves its list: a header
// alone too, so a table emptied row by row goes.
func (v *view) tableEnter() {
	t := v.shown
	p := t.paraAt(v.caret)
	st := v.para(p).style
	last := p+1 >= t.ParagraphCount() || !sameTable(st, v.para(p+1).style)
	if last && strings.Trim(v.lineOf(p), string(CellSeparator)) == "" {
		ps := t.paraStart(p)
		v.record(false)
		v.edit(func(e *Edit) {
			e.ReplaceString(ps, ps+v.para(p).n, "", Attributes{})
			e.FormatParagraphs(ps, ps, func(s *ParagraphStyle) { s.Row, s.Columns = NoRow, "" })
			e.Select(ps, ps)
		})
		return
	}
	v.addRow(p)
}

// keepCells returns how to replace the runes from start to end with s and
// keep a table's shape: the spans of the range to delete, between the tabs
// of its rows and the newlines at its rows, which stay, and what goes at
// start, s made a cell's text when start is in a row that stays. ok is
// false where the range touches no row, for the plain replace. A table the
// range holds whole, from its first row's start to its last row's end,
// goes with its rows' tabs and newlines, as select all and Delete take it
// away; plain is set when the row at start goes so, for its paragraph to
// be one no more. The newline of an empty paragraph by a table goes, as
// does the one before it after a row, so Backspace and Delete take an
// empty paragraph away and keep the row.
func keepCells(t Text, start, end int, s string) (spans [][2]int, ins string, plain, ok bool) {
	seq := t.seq()
	n := t.ParagraphCount()
	pa, pz := t.paraAt(start), t.paraAt(end)
	isRow := func(p int) bool { return seqAt(seq, p).style.Row != NoRow }
	row := false
	for p := pa; p <= pz && !row; p++ {
		row = isRow(p)
	}
	if !row {
		return nil, s, false, false
	}
	// kept reports whether row p stays: a paragraph that is a row of a
	// table the range doesn't hold whole.
	whole := map[int]bool{}
	kept := func(p int) bool {
		if p >= n || !isRow(p) {
			return false
		}
		if w, ok := whole[p]; ok {
			return !w
		}
		a, z := p, p
		for a > 0 && sameTable(seqAt(seq, a-1).style, seqAt(seq, a).style) {
			a--
		}
		for z+1 < n && sameTable(seqAt(seq, z).style, seqAt(seq, z+1).style) {
			z++
		}
		w := start <= t.paraStart(a) && end >= t.paraStart(z)+seqAt(seq, z).n
		for q := a; q <= z; q++ {
			whole[q] = w
		}
		return !w
	}
	plain = isRow(pa) && !kept(pa)
	if kept(pa) {
		s = cellText(s)
	}
	// stays reports whether the newline after paragraph p stays.
	stays := func(p int) bool {
		here, next := seqAt(seq, p), seqAt(seq, min(p+1, n-1))
		switch {
		case !isRow(p) && here.n == 0:
			return false // the empty paragraph goes, its newline with it
		case kept(p) && !isRow(p+1) && next.n == 0:
			return false // the row takes the empty paragraph after it
		}
		return kept(p) || kept(p+1)
	}
	from, p := start, pa
	for k, r := range []rune(t.Slice(start, end).String()) {
		at, stay := start+k, false
		switch r {
		case CellSeparator:
			stay = kept(p)
		case '\n':
			stay = stays(p)
			p++
		}
		if stay {
			if at > from {
				spans = append(spans, [2]int{from, at})
			}
			from = at + 1
		}
	}
	if end > from {
		spans = append(spans, [2]int{from, end})
	}
	return spans, s, plain, true
}

// unrow makes the paragraph at i no row, as what's left where a row went
// whole.
func unrow(e *Edit, i int) {
	e.FormatParagraphs(i, i, func(s *ParagraphStyle) { s.Row, s.Columns = NoRow, "" })
}

// cellOf returns t as the text of one cell of a row of style st, as a
// paste puts it there: its paragraphs the cell's lines, its tabs spaces.
func cellOf(t Text, st ParagraphStyle) Text {
	st.ID = ""
	p := Paragraph{Style: st}
	first := true
	for _, q := range t.Paragraphs() {
		if !first {
			p.Runs = append(p.Runs, Run{Text: string(CellLineBreak)})
		}
		first = false
		for _, r := range q.Runs {
			r.Text = cellText(r.Text)
			p.Runs = append(p.Runs, r)
		}
	}
	return TextOf(p)
}

// tableWidths returns the widths of the columns of row p's table, as a
// browser lays a table out: each column as wide as its widest cell
// unwrapped, as wide as that when it fits; when that's wider than width,
// the columns narrower than an equal share keep their width and the others
// share what's left, wrapping. The cells measured are those of the table's
// first measuredRows rows.
func (v *view) tableWidths(p int, width float32) []float32 {
	// Once for each table and width, until a change touches its rows or
	// the paragraphs around it join it: its rows ask in turn.
	for i, t := range v.tables {
		if p >= t.a && p <= t.z {
			if t.width == width && (t.a == 0 || !sameTable(v.para(t.a-1).style, v.para(t.a).style)) &&
				(t.z+1 == len(v.paras) || !sameTable(v.para(t.z).style, v.para(t.z+1).style)) {
				return t.widths
			}
			v.tables = slices.Delete(v.tables, i, i+1)
			break
		}
	}
	if len(v.natural) > 2*len(v.paras)+64 {
		v.natural = nil // the rows edited away, forgotten now and then
	}
	a, z := p, p
	for a > 0 && sameTable(v.para(a-1).style, v.para(a).style) {
		a--
	}
	for z+1 < len(v.paras) && sameTable(v.para(z).style, v.para(z+1).style) {
		z++
	}
	var want []float32
	for q := a; q <= min(z, a+measuredRows-1); q++ {
		for k, w := range v.naturalWidths(q) {
			if k >= len(want) {
				want = append(want, 0)
			}
			want[k] = max(want[k], w)
		}
	}
	n := max(len(want), len(v.para(p).style.Columns), 1)
	for len(want) < n {
		want = append(want, 0)
	}
	padX, _ := v.look.cellPadding()
	var sum float32
	for k := range want {
		want[k] = max(want[k]+2*padX, v.look.size*minColumnWidth)
		sum += want[k]
	}
	out := make([]float32, n)
	defer func() { v.tables = append(v.tables, tableSpan{a, z, width, out}) }()
	if sum <= width {
		// At its width: typing widens the column typed in, not the others.
		copy(out, want)
		return out
	}
	fixed := make([]bool, n)
	left, free := width, n
	for changed := true; changed && free > 0; {
		changed = false
		share := left / float32(free)
		for k := range want {
			if !fixed[k] && want[k] <= share {
				fixed[k], out[k] = true, want[k]
				left -= want[k]
				free--
				changed = true
			}
		}
	}
	for k := range want {
		if !fixed[k] {
			out[k] = max(left/float32(free), 1)
		}
	}
	return out
}

// naturalWidths returns the width of each of row q's cells on one line,
// its widest line where it breaks, kept until the row's runs change.
func (v *view) naturalWidths(q int) []float32 {
	para := v.para(q)
	var first *attrRun
	if len(para.runs) > 0 {
		first = unsafe.SliceData(para.runs)
	}
	if c, ok := v.natural[para.id]; ok && c.runs == first && c.nruns == len(para.runs) && c.style == para.style {
		return c.widths
	}
	line := v.lineOf(q)
	saved := v.look.width
	v.look.width = 1e6
	var cells [][]ui.Span
	cur := []ui.Span(nil)
	off, done := 0, 0
	for _, run := range para.runs {
		k := min(run.n, para.n-done)
		end := off
		for i := 0; i < k && end < len(line); i++ {
			_, size := utf8.DecodeRuneInString(line[end:])
			end += size
		}
		text := line[off:end]
		for {
			part, rest, cut := strings.Cut(text, string(CellSeparator))
			if part != "" {
				cur = append(cur, v.look.span(strings.ReplaceAll(part, string(CellLineBreak), "\n"), run.a, para.style))
			}
			if !cut {
				break
			}
			cells, cur = append(cells, cur), nil
			text = rest
		}
		off = end
		done += k
		if done >= para.n {
			break
		}
	}
	cells = append(cells, cur)
	v.look.width = saved
	ws := make([]float32, len(cells))
	font := v.paraFont(para.style)
	for k, c := range cells {
		ws[k], _ = ui.ShapeRichTextWith(c, font, ui.TextShape{}).Size()
	}
	if v.natural == nil {
		v.natural = map[uint64]naturalCells{}
	}
	v.natural[para.id] = naturalCells{runs: first, nruns: len(para.runs), style: para.style, widths: ws}
	return ws
}

// naturalCells are a row's cells' widths on one line, for the runs and
// style they were measured for.
type naturalCells struct {
	runs   *attrRun
	nruns  int
	style  ParagraphStyle
	widths []float32
}

// tableSpan is a table's rows, from a to z, and its columns' widths in
// width.
type tableSpan struct {
	a, z   int
	width  float32
	widths []float32
}

// measuredRows is how many of a table's rows its columns take their widths
// from, its first: a long table lays out as fast as a short one, and its
// columns stay as it scrolls, the rows after them wrapping at their widths.
const measuredRows = 100

// tablesChanged drops the columns of the tables a change of the paragraphs
// from pa to pz, now pa to npz, touched; those after it move with their
// rows. One it made a paragraph next to a row of is found out as its rows
// ask (tableWidths).
func (v *view) tablesChanged(pa, pz, npz int) {
	kept := v.tables[:0]
	for _, t := range v.tables {
		switch {
		case t.z < pa:
			kept = append(kept, t)
		case t.a > pz:
			t.a, t.z = t.a+npz-pz, t.z+npz-pz
			kept = append(kept, t)
		}
	}
	v.tables = kept
}
