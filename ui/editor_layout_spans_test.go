package ui

import (
	"math/rand/v2"
	"reflect"
	"testing"
	"unicode/utf8"

	"github.com/egoist/mygo/internal/text"
)

// rangeSpansEvery is rangeSpans as it walked every range: what the search
// for the first one in reach must give the same of.
func rangeSpansEvery(ed *editor, start, end int) (string, *spanPaint) {
	if len(ed.ranges) == 0 {
		return "", nil
	}
	caret, n := -1, 0
	if ed.compose != "" && start <= ed.caret && ed.caret <= end {
		caret, n = ed.caret, utf8.RuneCountInString(ed.compose)
	}
	// local is where rune i is in the layout: a run that starts at the
	// caret starts after the composition, one that ends there ends before it.
	local := func(i int, starts bool) int {
		if caret >= 0 && (i > caret || i == caret && starts) {
			i += n
		}
		return i - start
	}
	var styles []text.Span
	var spans []Span
	weighted, colored := false, false
	at := 0
	add := func(from, to int, r TextRange) {
		if from > at {
			styles = append(styles, text.Span{End: from})
			spans = append(spans, Span{})
		}
		styles = append(styles, text.Span{End: to, Weight: r.Weight})
		spans = append(spans, Span{Color: r.Color, Weight: r.Weight})
		at = to
	}
	last := start
	for _, r := range ed.ranges {
		from, to := max(r.Start, start), min(r.End, end)
		if from >= to || from < last {
			continue
		}
		if from < caret && caret < to {
			add(local(from, true), local(caret, false), r)
			add(local(caret, true), local(to, false), r)
		} else {
			add(local(from, true), local(to, false), r)
		}
		weighted = weighted || r.Weight > 0
		colored = colored || r.Color.A > 0
		last = to
	}
	key := ""
	if weighted {
		key = text.EncodeSpans(styles)
	}
	var sp *spanPaint
	if colored {
		sp = &spanPaint{spans: spans, styles: styles}
	}
	return key, sp
}

func TestRangeSpansSame(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for range 4000 {
		var ranges []TextRange
		for at := rng.IntN(5); at < 400; {
			n := 1 + rng.IntN(8)
			r := TextRange{Start: at, End: at + n}
			if rng.IntN(2) == 0 {
				r.Color = RGBA(1, 0, 0, 1)
			}
			if rng.IntN(3) == 0 {
				r.Weight = 700
			}
			ranges = append(ranges, r)
			at += n + rng.IntN(6)
		}
		ed := &editor{ranges: ranges}
		start := rng.IntN(420)
		end := start + rng.IntN(60)
		// Half the time an input method composes at a caret in or near
		// the paragraph.
		if rng.IntN(2) == 0 {
			ed.compose, ed.caret = "かな", max(0, start-2+rng.IntN(end-start+5))
		}
		k1, p1 := ed.rangeSpans(start, end)
		k2, p2 := rangeSpansEvery(ed, start, end)
		if k1 != k2 || !reflect.DeepEqual(p1, p2) {
			t.Fatalf("ranges %v, %d to %d, composing %d runes at %d: %q %+v, want %q %+v",
				ranges, start, end, utf8.RuneCountInString(ed.compose), ed.caret, k1, p1, k2, p2)
		}
	}
}

// BenchmarkRangeSpans styles every line of a 3,000-line text with ten
// ranges a line, as a highlighted script has.
func BenchmarkRangeSpans(b *testing.B) {
	const lines, perLine, width = 3000, 10, 40
	var ranges []TextRange
	for l := range lines {
		for i := range perLine {
			at := l*width + i*4
			ranges = append(ranges, TextRange{Start: at, End: at + 3, Color: RGBA(1, 0, 0, 1)})
		}
	}
	ed := &editor{ranges: ranges}
	for _, c := range []struct {
		name string
		f    func(*editor, int, int) (string, *spanPaint)
	}{{"search", (*editor).rangeSpans}, {"every", rangeSpansEvery}} {
		b.Run(c.name, func(b *testing.B) {
			for b.Loop() {
				for l := range lines {
					c.f(ed, l*width, l*width+width-1)
				}
			}
		})
	}
}
