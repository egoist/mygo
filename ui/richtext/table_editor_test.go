package richtext

import (
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

// shots is where the tests leave their pictures, when set.
var shots = os.Getenv("TABLE_SHOTS")

func shoot(t *testing.T, tt *ui.Tester, name string) {
	if shots == "" {
		return
	}
	f, err := os.Create(filepath.Join(shots, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}

const scheduleMD = "## Schedule\n\nBefore the table.\n\n" +
	"| Time | Talk | Notes |\n| ---: | --- | --- |\n" +
	"| 9:00 | Welcome & coffee | Badges at the **front desk**<br>Wi-Fi on the back |\n" +
	"| 9:30 | Keynote | Slides ![slides.pdf](app:file/slides.pdf) |\n\n" +
	"![Map](app:embed/map)\n\nAfter the table."

// sized gives the attachments sizes, as an app would: an embed a box as
// wide as the text, a file a chip.
func sized(t Text) Text {
	var paras []Paragraph
	for _, p := range t.Paragraphs() {
		for k, r := range p.Runs {
			if at := r.Attributes.Attachment; at != nil {
				a := *at
				if strings.HasPrefix(a.URL, "app:embed/") {
					a.Width, a.Height = 4000, 56
				} else {
					a.Width, a.Height = 92, 20
				}
				p.Runs[k].Attributes.Attachment = &a
			}
		}
		paras = append(paras, p)
	}
	return TextOf(paras...)
}

// rowCells returns the cells of each row, as the text says them.
func rowCells(t Text) [][]string {
	var out [][]string
	for _, p := range t.Paragraphs() {
		if p.Style.Row != NoRow {
			out = append(out, strings.Split(plainOf(p), "\t"))
		}
	}
	return out
}

func TestATableIsEditedAsCells(t *testing.T) {
	doc := NewDocument(sized(ParseMarkdown(scheduleMD)))
	tt := ui.NewTester(func(c *ui.Context) { Editor(c, doc).Fill() }, 900, 560)
	tt.Frame()
	shoot(t, tt, "table-light")

	// The rows' boxes, to click in their cells.
	var boxes []ParagraphBox
	for _, b := range doc.VisibleParagraphs() {
		if p := seqAt(doc.Text().seq(), b.Index); p.style.Row != NoRow {
			boxes = append(boxes, b)
		}
	}
	if len(boxes) != 3 {
		t.Fatalf("rows in view: %d, want 3", len(boxes))
	}
	// A click in the first body row's middle cell, right of its text in
	// its padding: the caret at the end of "Welcome & coffee".
	at := doc.Text().paraStart(boxes[1].Index) + len([]rune("9:00\tWelcome & coffee"))
	doc.SetSelection(at, at)
	tt.Frame()
	r, _ := doc.CaretRect()
	doc.SetSelection(0, 0)
	tt.Frame()
	tt.ClickAt(r.X+4, r.Y+r.H/2)
	tt.Type("!")
	if got := rowCells(doc.Text())[1][1]; got != "Welcome & coffee!" {
		t.Fatalf("typed in the cell clicked: %q", got)
	}

	// Tab: the end of the next cell.
	tt.Key(0, ui.KeyTab)
	tt.Type(" (ask)")
	if got := rowCells(doc.Text())[1][2]; !strings.HasSuffix(got, "Wi-Fi on the back (ask)") {
		t.Fatalf("Tab to the next cell: %q", got)
	}
	// Shift-Tab twice: the first cell.
	tt.Key(ui.Shift, ui.KeyTab)
	tt.Key(ui.Shift, ui.KeyTab)
	tt.Type("~")
	if got := rowCells(doc.Text())[1][0]; got != "9:00~" {
		t.Fatalf("Shift-Tab back: %q", got)
	}

	// Backspace at a cell's start and Delete at its end do nothing: the
	// cells never merge.
	before := doc.Text().Markdown()
	ps := doc.Text().paraStart(boxes[1].Index)
	start := ps + len([]rune("9:00~")) + 1 // after the first tab
	doc.SetSelection(start, start)
	tt.Frame()
	tt.Key(0, ui.KeyBackspace)
	if got := doc.Text().Markdown(); got != before {
		t.Fatalf("Backspace at a cell's start changed the table:\n%s", got)
	}
	if a, z := doc.Selection(); a != start || z != start {
		t.Fatalf("Backspace at a cell's start moved the caret to %d-%d, from %d", a, z, start)
	}
	doc.SetSelection(start-1, start-1)
	tt.Frame()
	tt.Key(0, ui.KeyDelete)
	if got := doc.Text().Markdown(); got != before {
		t.Fatalf("Delete at a cell's end changed the table:\n%s", got)
	}

	// Down from the header's first cell: the first body row's.
	hs := doc.Text().paraStart(boxes[0].Index)
	doc.SetSelection(hs+2, hs+2)
	tt.Frame()
	tt.Key(0, ui.KeyDown)
	tt.Type("^")
	if got := rowCells(doc.Text())[1][0]; !strings.Contains(got, "^") {
		t.Fatalf("Down from the header: %q", got)
	}
	// Down from a cell of one line in a row two lines tall (its Notes
	// cell breaks a line): the row below, not the room under the cell's
	// text; Up comes back.
	ts := doc.Text().paraStart(boxes[1].Index) + len([]rune("9:00^\tWel"))
	doc.SetSelection(ts, ts)
	tt.Frame()
	tt.Key(0, ui.KeyDown)
	tt.Type("~")
	if got := rowCells(doc.Text())[2][1]; !strings.Contains(got, "~") {
		t.Fatalf("Down from a short cell in a tall row: %q", rowCells(doc.Text()))
	}
	tt.Key(0, ui.KeyBackspace)
	tt.Key(0, ui.KeyUp)
	tt.Type("@")
	if got := rowCells(doc.Text())[1][1]; !strings.Contains(got, "@") {
		t.Fatalf("Up back to the tall row: %q", rowCells(doc.Text()))
	}
	tt.Key(0, ui.KeyBackspace)

	// A selection across cells, deleted: their text goes, the cells stay.
	rs := doc.Text().paraStart(boxes[2].Index)
	doc.SetSelection(rs+2, rs+len([]rune("9:30\tKey")))
	tt.Frame()
	tt.Key(0, ui.KeyBackspace)
	cells := rowCells(doc.Text())
	if len(cells[2]) != 3 || cells[2][0] != "9:" || cells[2][1] != "note" {
		t.Fatalf("deleted across cells: %q", cells[2])
	}

	// Tab in the last cell adds a row; Enter another; Enter on an empty
	// last row leaves the table.
	end := doc.Text().paraStart(boxes[2].Index) + seqAt(doc.Text().seq(), boxes[2].Index).n
	doc.SetSelection(end, end)
	tt.Frame()
	tt.Key(0, ui.KeyTab)
	tt.Type("10:30")
	tt.Key(0, ui.KeyEnter)
	tt.Key(0, ui.KeyEnter)
	tt.Type("Notes under the table")
	cells = rowCells(doc.Text())
	if len(cells) != 4 || cells[3][0] != "10:30" || len(cells[3]) != 3 {
		t.Fatalf("rows after Tab and Enter: %q", cells)
	}

	// An input method composing in a cell.
	doc.SetSelection(doc.Text().paraStart(boxes[2].Index)+2, doc.Text().paraStart(boxes[2].Index)+2)
	tt.Frame()
	tt.Compose("会", 1)
	tt.Frame()
	shoot(t, tt, "table-composing")
	tt.Compose("", 0)
	tt.Type("会议")
	if got := rowCells(doc.Text())[2][0]; got != "9:会议" {
		t.Fatalf("composed in a cell: %q", got)
	}
	tt.Frame()
	shoot(t, tt, "table-edited")
	md := doc.Text().Markdown()
	t.Logf("Markdown after the edits:\n%s", md)
	if !strings.Contains(md, "| 10:30 |  |  |\n\nNotes under the table") {
		t.Fatalf("written:\n%s", md)
	}

	// Undo takes each step back, to the table read.
	for doc.CanUndo() {
		doc.Undo()
	}
	if got := doc.Text().Markdown(); got != sized(ParseMarkdown(scheduleMD)).Markdown() {
		t.Fatalf("undone:\n%s", got)
	}
	tt.SetDark(true)
	tt.Frame()
	shoot(t, tt, "table-dark")
}

// TestATableKeepsItsShape pastes into a table and deletes the empty
// paragraphs around it: the rows stay rows, as many cells as they were.
func TestATableKeepsItsShape(t *testing.T) {
	row := func(r TableRow, s string) Paragraph {
		return Paragraph{Style: ParagraphStyle{Row: r, Columns: "--"}, Runs: []Run{{Text: s}}}
	}
	empty := Paragraph{}
	doc := NewDocument(TextOf(empty, row(HeaderRow, "a\tb"), row(BodyRow, "1\t2"), empty))
	tt := ui.NewTester(func(c *ui.Context) { Editor(c, doc).Fill() }, 600, 300)
	tt.Frame()
	tt.Press(300, 150)
	tt.Release(300, 150)
	styles := func() (out []TableRow) {
		for _, p := range doc.Text().Paragraphs() {
			out = append(out, p.Style.Row)
		}
		return out
	}

	// Rich text pasted in a cell is its text: its paragraphs the cell's
	// lines, its tabs spaces, its formats kept.
	end := doc.Text().paraStart(2) + 1 // after "1"
	doc.SetSelection(end, end)
	tt.Frame()
	tt.SetClipboardData(transfer.New(transfer.NewItem(transfer.Bytes(transfer.HTML, []byte("<p><b>x</b></p><p>y\tz</p>")), transfer.Bytes(transfer.Text, []byte("x\ny\tz")))))
	tt.Key(ui.Cmd, ui.KeyV)
	if got := rowCells(doc.Text())[1]; len(got) != 2 || got[0] != "1x\u2028y z" || got[1] != "2" {
		t.Fatalf("pasted in a cell: %q", got)
	}
	if a := doc.Text().AttributesAt(end); a.Weight != 700 {
		t.Fatalf("the paste's bold: %+v", a)
	}
	// At a row's start, the row keeps its style.
	start := doc.Text().paraStart(2)
	doc.SetSelection(start, start)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyV)
	if got := styles(); len(got) != 4 || got[2] != BodyRow {
		t.Fatalf("pasted at a row's start: %v", got)
	}

	// Backspace in the empty paragraph after the table takes it away.
	n := doc.Text().Len()
	doc.SetSelection(n, n)
	tt.Frame()
	tt.Key(0, ui.KeyBackspace)
	if got := styles(); len(got) != 3 || got[1] != HeaderRow || got[2] != BodyRow {
		t.Fatalf("Backspace after the table: %v", got)
	}
	// Backspace at the header's start takes the empty paragraph above.
	doc.SetSelection(1, 1)
	tt.Frame()
	tt.Key(0, ui.KeyBackspace)
	if got := styles(); len(got) != 2 || got[0] != HeaderRow || got[1] != BodyRow {
		t.Fatalf("Backspace at the header's start: %v", got)
	}
	if got := rowCells(doc.Text()); len(got[0]) != 2 || len(got[1]) != 2 {
		t.Fatalf("cells: %q", got)
	}
}

// A table looks as the theme says: the room above and below it, its
// cells' padding, its grid and its header's fill.
func TestATableLooksAsTheThemeSays(t *testing.T) {
	text := ParseMarkdown("Before.\n\n| a | b |\n| --- | --- |\n| 1 | 2 |\n\nAfter.")
	type drawn struct {
		above, rows, below, header float32
		fill, grid                 color.RGBA
	}
	draw := func(th *Theme) drawn {
		doc := NewDocument(text)
		var boxes []ParagraphBox
		tt := ui.NewTester(func(c *ui.Context) {
			boxes = doc.VisibleParagraphs()
			EditorBase(c, doc, EditorOptions{Font: ui.Font{Size: 10}, Theme: th}).Fill()
		}, 300, 300)
		tt.Frame()
		tt.Frame()
		if len(boxes) != 4 {
			t.Fatalf("%d paragraphs in view: %+v", len(boxes), boxes)
		}
		gap := func(i int) float32 { return boxes[i+1].Top - (boxes[i].Top + boxes[i].Height) }
		img := tt.Image()
		at := func(x, y float32) color.RGBA {
			s := float32(img.Bounds().Dx()) / 300
			return color.RGBAModel.Convert(img.At(int(x*s), int(y*s))).(color.RGBA)
		}
		h := boxes[1]
		return drawn{gap(0), gap(1), gap(2), h.Height, at(4, h.Top+h.Height/2), at(0.5, h.Top+h.Height/2)}
	}
	def := draw(nil)
	red, blue := ui.RGB(220, 0, 0), ui.RGB(0, 0, 220)
	got := draw(&Theme{TableSpacing: 2, TableCellPadding: [2]float32{0.6, 1.4}, TableHeaderBackground: red, TableBorder: blue})
	if !near(got.above-def.above, 14) || !near(got.below-def.below, 14) {
		t.Errorf("a TableSpacing of 2 ems against 0.6 at 10 DIPs: the room above went from %v to %v, below from %v to %v, want 14 more", def.above, got.above, def.below, got.below)
	}
	if got.rows != def.rows {
		t.Errorf("the room between the rows went from %v to %v", def.rows, got.rows)
	}
	if !near(got.header-def.header, 20) {
		t.Errorf("1.4 ems of padding down against 0.4: the header went from %v to %v tall, want 20 more", def.header, got.header)
	}
	if want := (color.RGBA{220, 0, 0, 255}); got.fill != want {
		t.Errorf("the header's fill is %v, want the theme's %v", got.fill, want)
	}
	if want := (color.RGBA{0, 0, 220, 255}); got.grid != want {
		t.Errorf("the grid is %v, want the theme's %v", got.grid, want)
	}
}

// tableDoc returns a document of a paragraph, a table of a header and two
// rows, and a paragraph, in an editor; the table's rows are paragraphs 1
// to 3.
func tableDoc(t *testing.T) (*Document, *ui.Tester) {
	t.Helper()
	doc := NewDocument(ParseMarkdown("Before.\n\n| a | b |\n| --- | --- |\n| 1 | 2 |\n| 3 | 4 |\n\nAfter."))
	tt := ui.NewTester(func(c *ui.Context) { Editor(c, doc).Fill() }, 600, 400)
	tt.Frame()
	if got := len(rowCells(doc.Text())); got != 3 {
		t.Fatalf("%d rows", got)
	}
	tt.ClickAt(300, 380) // the keyboard's, below the text
	return doc, tt
}

// noRows fails if the text holds a row.
func noRows(t *testing.T, what string, text Text) {
	t.Helper()
	for _, p := range text.Paragraphs() {
		if p.Style.Row != NoRow {
			t.Fatalf("%s: %q is still a row: %q", what, plainOf(p), text.String())
		}
	}
}

// A table goes from the keyboard: select all and Delete, or Cut, or typing
// over it; its rows selected whole and deleted; or a header left alone
// and empty, which Enter makes a paragraph again. A selection within the
// table still keeps its cells.
func TestATableGoesFromTheKeyboard(t *testing.T) {
	for _, c := range []struct {
		name string
		keys func(tt *ui.Tester)
		want string
	}{
		{"select all, Delete", func(tt *ui.Tester) { tt.Key(ui.Cmd, ui.KeyA); tt.Key(0, ui.KeyBackspace) }, ""},
		{"select all, Cut", func(tt *ui.Tester) { tt.Key(ui.Cmd, ui.KeyA); tt.Key(ui.Cmd, ui.KeyX) }, ""},
		{"select all, type", func(tt *ui.Tester) { tt.Key(ui.Cmd, ui.KeyA); tt.Type("x") }, "x"},
	} {
		t.Run(c.name, func(t *testing.T) {
			doc, tt := tableDoc(t)
			c.keys(tt)
			if got := doc.Text().String(); got != c.want {
				t.Fatalf("left %q, want %q", got, c.want)
			}
			noRows(t, c.name, doc.Text())
		})
	}
	t.Run("a table alone, select all, Delete", func(t *testing.T) {
		doc := NewDocument(ParseMarkdown("| a | b |\n| --- | --- |\n| 1 | 2 |"))
		tt := ui.NewTester(func(c *ui.Context) { Editor(c, doc).Fill() }, 600, 400)
		tt.Frame()
		tt.ClickAt(300, 380)
		tt.Key(ui.Cmd, ui.KeyA)
		tt.Key(0, ui.KeyBackspace)
		if got := doc.Text().String(); got != "" {
			t.Fatalf("left %q", got)
		}
		noRows(t, "deleted", doc.Text())
	})
	t.Run("its rows selected whole, Delete", func(t *testing.T) {
		doc, tt := tableDoc(t)
		text := doc.Text()
		doc.SetSelection(text.paraStart(1), text.paraStart(4)) // the header's start to After's
		tt.Frame()
		tt.Key(0, ui.KeyBackspace)
		if got := doc.Text().String(); got != "Before.\nAfter." {
			t.Fatalf("left %q", got)
		}
		noRows(t, "deleted", doc.Text())
		doc.Undo()
		if got := len(rowCells(doc.Text())); got != 3 {
			t.Fatalf("undone: %d rows", got)
		}
	})
	t.Run("from within Before to within After, typing", func(t *testing.T) {
		doc, tt := tableDoc(t)
		text := doc.Text()
		doc.SetSelection(3, text.paraStart(4)+3)
		tt.Frame()
		tt.Type("x")
		if got := doc.Text().String(); got != "Befxer." {
			t.Fatalf("left %q", got)
		}
		noRows(t, "typed over", doc.Text())
	})
	t.Run("within it, Delete keeps the cells", func(t *testing.T) {
		doc, tt := tableDoc(t)
		text := doc.Text()
		doc.SetSelection(text.paraStart(2), text.paraStart(3)+1) // "1\t2\n3"
		tt.Frame()
		tt.Key(0, ui.KeyBackspace)
		if got := rowCells(doc.Text()); len(got) != 3 || strings.Join(got[1], "|") != "|" || strings.Join(got[2], "|") != "|4" {
			t.Fatalf("rows %q", got)
		}
	})
	t.Run("an empty header alone, Enter", func(t *testing.T) {
		doc := NewDocument(TextOf(para("Before."), rowPara(ParagraphStyle{Row: HeaderRow, Columns: "--"}, Run{Text: "\t"}), para("After.")))
		tt := ui.NewTester(func(c *ui.Context) { Editor(c, doc).Fill() }, 600, 400)
		tt.Frame()
		tt.ClickAt(300, 380)
		at := doc.Text().paraStart(1)
		doc.SetSelection(at, at)
		tt.Frame()
		tt.Key(0, ui.KeyEnter)
		if got := doc.Text().String(); got != "Before.\n\nAfter." {
			t.Fatalf("left %q", got)
		}
		noRows(t, "left", doc.Text())
	})
}

// A table that fits is as wide as its cells, not the editor: typing in a
// cell widens its column, not the others. Its columns are worked out once
// until a change touches the table, not at each keystroke elsewhere, and
// a long table's from its first rows only.
func TestATablesColumns(t *testing.T) {
	doc, tt := tableDoc(t)
	v := doc.shownIn.Value()
	if len(v.tables) != 1 {
		t.Fatalf("tables laid out: %+v", v.tables)
	}
	widths := v.tables[0].widths
	if sum := widths[0] + widths[1]; sum >= 500 {
		t.Errorf("a table of one-letter cells is %v wide", sum)
	}
	// Typing outside it keeps its columns, as worked out.
	at := doc.Text().Len()
	doc.SetSelection(at, at)
	tt.Frame()
	tt.Type(" More.")
	if len(v.tables) != 1 || &v.tables[0].widths[0] != &widths[0] {
		t.Errorf("typing after the table worked its columns out again")
	}
	// Typing in its first column widens that one.
	at = doc.Text().paraStart(2) + 1 // after "1"
	doc.SetSelection(at, at)
	tt.Frame()
	tt.Type(" and a longer cell")
	now := v.tables[0].widths
	if now[0] <= widths[0] || now[1] != widths[1] {
		t.Errorf("typing in the first column: widths %v, then %v", widths, now)
	}

	// The paragraph after it made a row joins it, and its columns are
	// worked out again with it.
	doc.Edit(func(e *Edit) {
		at := e.Text().paraStart(4)
		e.FormatParagraphs(at, at, func(s *ParagraphStyle) { s.Row, s.Columns = BodyRow, "--" })
	})
	tt.Frame()
	if len(v.tables) != 1 || v.tables[0].a != 1 || v.tables[0].z != 4 {
		t.Errorf("after a paragraph joined it: tables %+v, want rows 1-4", v.tables)
	}

	// Rows at another depth of quotation are another table.
	quoted := ParseMarkdown("| a | b |\n| --- | --- |\n| 1 | 2 |\n\n> | c |\n> | --- |\n> | 3 |")
	doc = NewDocument(quoted)
	tt = ui.NewTester(func(c *ui.Context) { Editor(c, doc).Fill() }, 600, 400)
	tt.Frame()
	if v := doc.shownIn.Value(); len(v.tables) != 2 || v.tables[0].z != 1 || v.tables[1].a != 2 {
		t.Errorf("tables %+v, want rows 0-1 and 2-3", v.tables)
	}

	// A long table measures its first measuredRows rows.
	var md strings.Builder
	md.WriteString("| n | name |\n| --- | --- |\n")
	for i := range 300 {
		md.WriteString("| " + strings.Repeat("9", i%7+1) + " | row |\n")
	}
	doc = NewDocument(ParseMarkdown(md.String()))
	tt = ui.NewTester(func(c *ui.Context) { Editor(c, doc).Fill() }, 600, 400)
	tt.Frame()
	if v := doc.shownIn.Value(); len(v.natural) != measuredRows {
		t.Errorf("a table of 301 rows measured %d of them, want %d", len(v.natural), measuredRows)
	}
}
