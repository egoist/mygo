package richtext

import (
	"strings"

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
