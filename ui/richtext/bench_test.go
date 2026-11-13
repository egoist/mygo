package richtext

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
)

// longText returns n paragraphs of formatted text: a heading every
// fiftieth, items of lists, and runs of bold, italic and links.
func longText(n int) Text {
	paras := make([]Paragraph, n)
	for i := range paras {
		st := ParagraphStyle{}
		switch {
		case i%50 == 0:
			st.Heading = 2
		case i%7 == 3:
			st.List = Bullets
		}
		paras[i] = Paragraph{Style: st, Runs: []Run{
			{Text: fmt.Sprintf("Paragraph %d has ", i)},
			{Text: "bold words", Attributes: Attributes{Weight: 700}},
			{Text: ", an "},
			{Text: "italic one", Attributes: Attributes{Italic: true}},
			{Text: " and "},
			{Text: "a link", Attributes: Attributes{Link: "https://example.com"}},
			{Text: " in a line long enough to wrap in a narrow window."},
		}}
	}
	return TextOf(paras...)
}

// benchParagraphs are the sizes of the documents of the benchmarks.
var benchParagraphs = []int{1000, 10000, 100000}

// BenchmarkTypeFrame types a character in the middle of a document and
// draws the frame that shows it, as typing does.
func BenchmarkTypeFrame(b *testing.B) {
	for _, n := range benchParagraphs {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			doc := NewDocument(longText(n))
			tt := ui.NewTester(func(c *ui.Context) { Editor(c, doc).Fill() }, 1000, 800)
			tt.Press(500, 400)
			tt.Release(500, 400)
			mid := doc.Text().Len() / 2
			doc.SetSelection(mid, mid)
			tt.Frame()
			b.ReportAllocs()
			for b.Loop() {
				tt.Type("x")
			}
		})
	}
}

// BenchmarkReplace replaces a rune in the middle of a document's text,
// the edit a keystroke makes, without drawing.
func BenchmarkReplace(b *testing.B) {
	for _, n := range benchParagraphs {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			t := longText(n)
			mid := t.Len() / 2
			b.ReportAllocs()
			for b.Loop() {
				t = t.ReplaceString(mid, mid+1, "y", Attributes{Weight: 700})
			}
		})
	}
}

// BenchmarkFormat makes a thousand runes bold in the middle of a document.
func BenchmarkFormat(b *testing.B) {
	for _, n := range benchParagraphs {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			t := longText(n)
			mid := t.Len() / 2
			on := false
			b.ReportAllocs()
			for b.Loop() {
				on = !on
				t = t.Format(mid, mid+1000, func(a *Attributes) { a.Italic = on })
			}
		})
	}
}

// BenchmarkScrollFrame scrolls a long document by a wheel's step a frame.
func BenchmarkScrollFrame(b *testing.B) {
	doc := NewDocument(longText(100000))
	tt := ui.NewTester(func(c *ui.Context) { Editor(c, doc).Fill() }, 1000, 800)
	tt.Frame()
	b.ReportAllocs()
	for b.Loop() {
		tt.Scroll(500, 400, 0, 120)
	}
}

// shortParagraphs returns n short paragraphs, as a note's lines or a
// list's items are, many to a chunk of the text: their words run plain,
// bold and plain, with a multi-byte rune in each.
func shortParagraphs(n int) Text {
	paras := make([]Paragraph, n)
	for i := range paras {
		paras[i] = Paragraph{Runs: []Run{
			{Text: fmt.Sprintf("Line %d — ", i)},
			{Text: "bold", Attributes: Attributes{Weight: 700}},
			{Text: "."},
		}}
	}
	return TextOf(paras...)
}

// benchShort are the sizes of the documents of short paragraphs.
var benchShort = []int{100, 300, 1000, 3000}

// BenchmarkParagraphs walks the paragraphs of a document of short ones,
// as saving or searching it does.
func BenchmarkParagraphs(b *testing.B) {
	for _, n := range benchShort {
		t := shortParagraphs(n)
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				for range t.Paragraphs() {
				}
			}
		})
	}
}

// BenchmarkRuns walks the runs of the whole of a document of short
// paragraphs, as copying it does.
func BenchmarkRuns(b *testing.B) {
	for _, n := range benchShort {
		t := shortParagraphs(n)
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				for range t.Runs(0, t.Len()) {
				}
			}
		})
	}
}

// tableText returns a table of n rows of three cells, or the same text as
// plain paragraphs.
func tableText(n int, table bool) Text {
	head := ParagraphStyle{Row: HeaderRow, Columns: "r--"}
	if !table {
		head = ParagraphStyle{}
	}
	paras := []Paragraph{{Style: head, Runs: []Run{{Text: "Time\tTalk\tNotes"}}}}
	for i := range n {
		st := ParagraphStyle{Row: BodyRow, Columns: "r--"}
		if !table {
			st = ParagraphStyle{}
		}
		paras = append(paras, Paragraph{Style: st, Runs: []Run{{Text: fmt.Sprintf("%d:%02d\tTalk number %d\tNotes on the talk, long enough to wrap in its column of the table", 9+i/60%12, i%60, i)}}})
	}
	return TextOf(paras...)
}

// BenchmarkTypeInTable types a character in the middle of a table and
// draws the frame that shows it, against the same text as paragraphs.
func BenchmarkTypeInTable(b *testing.B) {
	for _, n := range []int{20, 200, 2000} {
		for _, table := range []bool{false, true} {
			b.Run(fmt.Sprintf("rows=%d/table=%v", n, table), func(b *testing.B) {
				doc := NewDocument(tableText(n, table))
				tt := ui.NewTester(func(c *ui.Context) { Editor(c, doc).Fill() }, 1000, 800)
				tt.Press(500, 400)
				tt.Release(500, 400)
				// A row its table's columns are measured from (measuredRows),
				// as typing grows its cell without end.
				mid := doc.Text().paraStart(min(n/2, 10)) + 3
				doc.SetSelection(mid, mid)
				tt.Frame()
				b.ReportAllocs()
				for b.Loop() {
					tt.Type("x")
				}
			})
		}
	}
}
