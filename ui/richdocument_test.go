package ui

import (
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/internal/text"
)

func checkRichDocument(t *testing.T, d RichDocument) {
	t.Helper()
	var bounds text.Boundaries
	bounds.Reset([]rune(d.String()))
	valid := bounds.GraphemeOffsets()
	end := 0
	for i, run := range d.Runs() {
		if run.Range.Start != end || run.Range.End <= end || !slices.Contains(valid, run.Range.End) {
			t.Fatalf("noncanonical run %d: %+v in %q", i, run, d.String())
		}
		if i > 0 && run.Style == d.runs[i-1].Style {
			t.Fatalf("adjacent equal runs: %+v", d.Runs())
		}
		end = run.Range.End
	}
	if end != d.Len() || len(d.Paragraphs()) != strings.Count(d.String(), "\n")+1 {
		t.Fatalf("invalid document: %+v", d)
	}
}

func TestRichDocumentGraphemesAndImmutableEdits(t *testing.T) {
	d := NewRichDocument("é 👩‍👩‍👧‍👦\r\nשלום")
	if d.String() != "é 👩‍👩‍👧‍👦\nשלום" {
		t.Fatal(d.String())
	}
	for _, r := range []TextRange{{1, 2}, {2, 1}, {-10, 1}} {
		if got := d.NormalizeRange(r); got != (TextRange{0, 2}) {
			t.Fatalf("normalize %v = %v", r, got)
		}
	}
	if got := d.NormalizeRange(TextRange{1, 1}); got != (TextRange{0, 0}) {
		t.Fatal(got)
	}
	bold := RichStyle{Weight: 700, Link: "https://example.com/"}
	styled := d.WithStyle(TextRange{4, 5}, bold)
	if styled.StyleAt(3) != bold || d.StyleAt(3) != (RichStyle{}) {
		t.Fatal("formatting split a ZWJ cluster or mutated the source")
	}
	fragment := styled.Slice(TextRange{4, 5})
	if fragment.String() != "👩‍👩‍👧‍👦" || fragment.StyleAt(0) != bold {
		t.Fatalf("fragment: %+v", fragment)
	}
	joined := NewRichDocument("ex").WithStyle(TextRange{0, 1}, bold).Replace(TextRange{1, 1}, NewRichDocument("́"))
	if joined.StyleAt(1) != bold {
		t.Fatal("inserted accent did not inherit the first rune's style")
	}
	runs := styled.Runs()
	runs[0].Style.Italic = true
	if styled.StyleAt(0).Italic {
		t.Fatal("Runs exposed mutable storage")
	}
	checkRichDocument(t, styled)
	checkRichDocument(t, joined)
}

func TestRichDocumentParagraphSplitJoin(t *testing.T) {
	d := NewRichDocument("first\nsecond\nthird")
	d = d.WithParagraphStyle(TextRange{0, 6}, ParagraphStyle{Alignment: Center, LineHeight: 1.5})
	if d.paragraph(1).Alignment != Start {
		t.Fatal("selection ending at a paragraph start styled the next paragraph")
	}
	d = d.WithParagraphStyle(TextRange{6, 6}, ParagraphStyle{Alignment: End})
	f := NewRichDocument("new\nother").WithParagraphStyle(TextRange{0, 9}, ParagraphStyle{Alignment: End, LineHeight: 2})
	out := d.Replace(TextRange{2, 8}, f)
	if out.String() != "finew\nothercond\nthird" || out.paragraph(0).Alignment != Center || out.paragraph(1).LineHeight != 2 || out.paragraph(2).Alignment != Start {
		t.Fatalf("replacement = %q, paragraphs %+v", out.String(), out.Paragraphs())
	}
	joined := d.Replace(TextRange{5, 6}, RichDocument{})
	if joined.String() != "firstsecond\nthird" || joined.paragraph(0).Alignment != Center {
		t.Fatal("joining did not retain leading paragraph style")
	}
	checkRichDocument(t, out)
	checkRichDocument(t, joined)
}

func TestRichDocumentRandomRangeEdits(t *testing.T) {
	rng := rand.New(rand.NewPCG(31, 14))
	d := NewRichDocument("abc\nשלום\n👩‍👩‍👧‍👦 é")
	for range 400 {
		r := d.NormalizeRange(TextRange{rng.IntN(d.n + 1), rng.IntN(d.n + 1)})
		before := d
		if rng.IntN(3) == 0 {
			d = d.WithStyle(r, RichStyle{Weight: 700, Italic: rng.IntN(2) == 0})
			if d.String() != before.String() {
				t.Fatal("format changed text")
			}
		} else {
			f := NewRichDocument([]string{"", "x", "\n", "é", "👍🏽", "ab\nc"}[rng.IntN(6)])
			f = f.WithStyle(TextRange{0, f.n}, RichStyle{Underline: true})
			d = d.Replace(r, f)
			want := []rune(before.String())
			want = append(want[:r.Start:r.Start], append([]rune(f.String()), want[r.End:]...)...)
			if d.String() != string(want) {
				t.Fatalf("range edit = %q, want %q", d.String(), string(want))
			}
		}
		checkRichDocument(t, d)
		checkRichDocument(t, before)
	}
}
