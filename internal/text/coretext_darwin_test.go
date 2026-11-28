//go:build darwin && !ios

package text

import (
	"bytes"
	"math"
	"math/rand/v2"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestCoreTextSyntheticItalic(t *testing.T) {
	for _, tc := range []struct{ family, text string }{
		{"PingFang SC", "口中文斜体"}, {"system-ui", "口中文斜体"}, {"Helvetica, PingFang SC", "口中文斜体"}, {"PingFang SC", "gy"},
	} {
		t.Run(tc.family+tc.text, func(t *testing.T) {
			s := newSystem()
			e := s.engine().(*coreText)
			style := Style{Family: tc.family, Size: 64}
			plain := s.Layout(Params{Text: tc.text, Style: style})
			style.Italic = true
			italic := s.Layout(Params{Text: tc.text, Style: style})
			if len(plain.Lines[0].Glyphs) != len(italic.Lines[0].Glyphs) {
				t.Fatal("italic changed glyph count")
			}
			for i, g := range italic.Lines[0].Glyphs {
				p := plain.Lines[0].Glyphs[i]
				if p.Font == g.Font || ct.fontGetMatrix(g.Font.native).c != obliqueSkew {
					t.Fatalf("glyph %d did not get a separate synthetic font", i)
				}
				if tc.text == "gy" {
					id := uint16(g.ID)
					var bounds cgRect
					ct.fontGetBoundingRects(g.Font.native, ctFontOrientationDefault, &id, &bounds, 1)
					if bounds.y >= 0 {
						t.Fatal("descender case has no negative-y ink")
					}
				}
				a := e.glyph(p.Font, p.ID, 2, 0, Flat, false)
				b := e.glyph(g.Font, g.ID, 2, 0, Flat, false)
				if bytes.Equal(a.pix, b.pix) {
					t.Fatal("upright and italic pixels are identical")
				}
				for y := range b.h {
					for x := range b.w {
						if (x == 0 || x == b.w-1 || y == 0 || y == b.h-1) && b.pix[y*b.w+x] != 0 {
							t.Fatal("synthetic ink touches bitmap boundary")
						}
					}
				}
				// Exercise the atlas cache in both orders as well, small enough
				// for every glyph to fit the test system's 256×256 atlas.
				pi := s.Glyph(p.Font, p.ID, 0.5, 0, Flat, false)
				ii := s.Glyph(g.Font, g.ID, 0.5, 0, Flat, false)
				if !pi.OK || !ii.OK || (pi.X == ii.X && pi.Y == ii.Y) {
					t.Fatalf("upright and italic share an atlas entry: %+v %+v", pi, ii)
				}
				if again := s.Glyph(p.Font, p.ID, 0.5, 0, Flat, false); again != pi {
					t.Fatal("italic replaced upright cache entry")
				}
				if i == 0 && tc.text != "gy" {
					// 口 has matching vertical sides, so compare their
					// centroids near the top and bottom of the ink.
					top, bottom := inkCentroid(b, 0.2), inkCentroid(b, 0.8)
					if math.IsNaN(top) || math.IsNaN(bottom) || top-bottom < 10 {
						t.Fatalf("glyph top is not tilted right: top %v bottom %v", top, bottom)
					}
				}
			}
		})
	}
}

func inkCentroid(b bitmap, fraction float64) float64 {
	lo, hi := b.h, 0
	for y := range b.h {
		for x := range b.w {
			if b.pix[y*b.w+x] > 128 {
				lo, hi = min(lo, y), max(hi, y)
			}
		}
	}
	y := lo + int(float64(hi-lo)*fraction)
	var sum, weight float64
	for x := range b.w {
		v := float64(b.pix[y*b.w+x])
		sum += float64(x+b.left) * v
		weight += v
	}
	if weight == 0 {
		return math.NaN()
	}
	return sum / weight
}

func TestCoreTextTrueItalic(t *testing.T) {
	s := newSystem()
	l := s.Layout(Params{Text: "italic", Style: Style{Family: "system-ui", Size: 48, Italic: true}})
	identity := cgAffineTransform{a: 1, d: 1}
	for _, g := range l.Lines[0].Glyphs {
		if ct.fontGetSymbolicTraits(g.Font.native)&ctFontItalicTrait == 0 {
			t.Fatal("system Latin italic did not select a true italic face")
		}
		if m := ct.fontGetMatrix(g.Font.native); m != identity {
			t.Fatalf("true italic was transformed: %+v", m)
		}
	}
}

func TestCoreTextItalicFallbackSpans(t *testing.T) {
	s := newSystem()
	// In PingFang SC, both spans are in one font, which Core Text would
	// shape as one run.
	for _, family := range []string{"Helvetica", "PingFang SC"} {
		for _, text := range []string{"中文中文", "😀文中文"} {
			l := s.Layout(Params{Text: text, Style: Style{Family: family, Size: 32},
				Spans: EncodeSpans([]Span{{End: 2}, {End: 4, Italic: true}})})
			for _, g := range l.Lines[0].Glyphs {
				want := float64(0)
				if g.Cluster >= 2 {
					want = obliqueSkew
				}
				if got := ct.fontGetMatrix(g.Font.native).c; got != want {
					t.Fatalf("%s %q cluster %d: skew %v, want %v", family, text, g.Cluster, got, want)
				}
			}
		}
	}
}

// Core Text draws color glyphs upright whatever the font's matrix, so
// emoji in italic text keep the upright font, as in WebKit.
func TestCoreTextItalicColorGlyphs(t *testing.T) {
	s := newSystem()
	for _, family := range []string{"PingFang SC", "Helvetica"} {
		l := s.Layout(Params{Text: "中😀", Style: Style{Family: family, Size: 32, Italic: true}})
		for _, g := range l.Lines[0].Glyphs {
			want := obliqueSkew
			if g.Cluster == 1 {
				want = 0
			}
			if got := ct.fontGetMatrix(g.Font.native).c; got != want {
				t.Fatalf("%s cluster %d: skew %v, want %v", family, g.Cluster, got, want)
			}
		}
	}
}

// TestCoreTextLanguage holds the glyphs of Han characters to the language
// of the text: 直 and 骨 are drawn apart in Japanese and in Chinese, by
// the fonts Core Text falls back to and by a font's own forms for the
// language. A language that writes no Han characters leaves them as text
// without a language has them, and Latin letters keep their glyphs.
func TestCoreTextLanguage(t *testing.T) {
	s := newSystem()
	ids := func(family, lang string, spans []Span) []uint32 {
		l := s.Layout(Params{Text: "直骨 Ag", Style: Style{Family: family, Size: 32, Lang: lang}, Spans: EncodeSpans(spans)})
		var out []uint32
		for _, g := range l.Lines[0].Glyphs {
			out = append(out, g.ID)
		}
		return out
	}
	for _, family := range []string{"system-ui", "Helvetica"} {
		none, en, ja, hans := ids(family, "", nil), ids(family, "en", nil), ids(family, "ja", nil), ids(family, "zh-Hans", nil)
		if !slices.Equal(none, en) {
			t.Errorf("%s: English %v, without a language %v", family, en, none)
		}
		if slices.Equal(ja[:2], hans[:2]) {
			t.Errorf("%s: Japanese and Chinese Han glyphs are the same: %v", family, ja)
		}
		if !slices.Equal(ja[2:], hans[2:]) || !slices.Equal(ja[2:], none[2:]) {
			t.Errorf("%s: Latin glyphs changed with the language: %v %v %v", family, none, ja, hans)
		}
		// A span in Japanese in a Chinese text, and the other way round.
		if got := ids(family, "zh-Hans", []Span{{End: 2, Lang: "ja"}}); !slices.Equal(got, ja) {
			t.Errorf("%s: a Japanese span in Chinese text: %v, want %v", family, got, ja)
		}
		if got := ids(family, "ja", []Span{{End: 1}, {End: 2, Lang: "zh-Hans"}}); got[0] != ja[0] || got[1] != hans[1] {
			t.Errorf("%s: a Chinese span in Japanese text: %v", family, got)
		}
	}
	// The system's language for text without one.
	s.SetLanguage("ja")
	if got := ids("system-ui", "", nil); !slices.Equal(got, ids("system-ui", "ja", nil)) {
		t.Errorf("text without a language in a Japanese system: %v", got)
	}
}

// TestCoreTextLanguageSpacing checks that the room Core Text leaves
// between Han and Latin letters in Chinese and Japanese text counts in the
// width of a line, so that text laid out as wide as it measured stays on
// one line.
func TestCoreTextLanguageSpacing(t *testing.T) {
	s := newSystem()
	for _, lang := range []string{"", "zh-Hans", "ja"} {
		p := Params{Text: "2026年1月5日星期一", Style: Style{Size: 13, Lang: lang}}
		l := s.Layout(p)
		gs := l.Lines[0].Glyphs
		last := gs[len(gs)-1]
		if end := last.X + last.Advance - gs[0].X; end > l.Width+1e-3 {
			t.Errorf("%q: the pen ends at %v, past the line's width %v", lang, end, l.Width)
		}
		p.Width = l.Width
		if n := len(s.Layout(p).Lines); n != 1 {
			t.Errorf("%q: laid out as wide as it measured, the text takes %d lines", lang, n)
		}
	}
}

// A text wrapped again at another width takes its typesetter from before,
// and lays out as the same text does when shaped anew, in every script and
// direction and with what a style gives its attributes.
func TestCoreTextKeptTypesetter(t *testing.T) {
	texts := []string{
		"The quick brown fox jumps over the lazy dog, and then some more words follow to wrap the paragraph.",
		"你好世界，这段中文会换行。The fonts fall back within a line 日本語のテキストも含む。",
		"مرحبا بالعالم، هذا نص طويل يلتف على عدة أسطر من اليمين إلى اليسار في الفقرة",
		"Emoji 😀🎉👩‍👩‍👧‍👦 and a verylongwordthatdoesnotfitalineandmustbreakorhang somewhere",
	}
	styles := []Style{{Size: 14}, {Size: 14, Italic: true}, {Size: 14, Weight: 700}, {Size: 14, LetterSpacing: 2}, {Size: 14, Features: "kern=0"}, {Size: 14, Lang: "ja"}}
	widths := []float32{300, 120, 450, 120, 77}
	kept := newSystem()
	for _, text := range texts {
		for _, style := range styles {
			for _, width := range widths {
				p := Params{Text: text, Width: width, Style: style}
				// Without the layouts' cache, which would answer the second
				// time a width comes.
				kept.mu.Lock()
				got := &kept.layout(p).layout
				kept.mu.Unlock()
				fresh := newSystem()
				fresh.mu.Lock()
				want := &fresh.layout(p).layout
				fresh.mu.Unlock()
				if len(got.Lines) != len(want.Lines) || got.Width != want.Width || got.Height != want.Height {
					t.Fatalf("%q in %+v at %v: %d lines, %v×%v, shaped anew %d lines, %v×%v", text, style, width, len(got.Lines), got.Width, got.Height, len(want.Lines), want.Width, want.Height)
				}
				for i := range got.Lines {
					a, b := got.Lines[i].Glyphs, want.Lines[i].Glyphs
					if len(a) != len(b) {
						t.Fatalf("%q in %+v at %v: line %d has %d glyphs, shaped anew %d", text, style, width, i, len(a), len(b))
					}
					for k := range a {
						if a[k].ID != b[k].ID || a[k].X != b[k].X || a[k].Y != b[k].Y || a[k].Advance != b[k].Advance || a[k].Cluster != b[k].Cluster {
							t.Fatalf("%q in %+v at %v: line %d glyph %d is %+v, shaped anew %+v", text, style, width, i, k, a[k], b[k])
						}
					}
				}
			}
		}
	}
	e := kept.engine().(*coreText)
	// The first text is all ASCII: in Japanese, it shapes as in no language.
	if want := len(texts)*len(styles) - 1; len(e.typesetters) != want {
		t.Errorf("%d typesetters kept, want one for each of the %d shapings", len(e.typesetters), want)
	}
}

// Typesetters go once their text stopped wrapping again, and all of them as
// windows go idle, and what does not wrap keeps none.
func TestCoreTextTypesettersGo(t *testing.T) {
	s := newSystem()
	e := s.engine().(*coreText)
	wrapped := Params{Text: "A paragraph that wraps", Width: 60, Style: Style{Size: 14}}
	s.Layout(wrapped)
	s.Layout(Params{Text: "A label on one line", Style: Style{Size: 14}})
	if len(e.typesetters) != 1 {
		t.Fatalf("%d typesetters kept, want that of the text that wraps", len(e.typesetters))
	}
	for range keptTypesetterFrames {
		s.EndFrame()
	}
	if len(e.typesetters) != 1 {
		t.Fatalf("gone after %d frames, which another window's may be", keptTypesetterFrames)
	}
	s.EndFrame()
	if len(e.typesetters) != 0 {
		t.Fatalf("%d typesetters kept after their text stopped wrapping", len(e.typesetters))
	}
	s.Layout(Params{Text: wrapped.Text, Width: 70, Style: wrapped.Style})
	s.ReleaseShaping()
	if len(e.typesetters) != 0 {
		t.Fatalf("%d typesetters kept once idle", len(e.typesetters))
	}
}

// Text whose language the engine is not told, for it changes nothing
// (languageShapes), lays out as it does when the engine is told, in the fonts
// of the system, so that leaving it out costs no one a glyph or a line break.
func TestCoreTextLanguageLeftOut(t *testing.T) {
	cases := []struct {
		text  string
		langs []string
	}{
		{"The quick brown fox -- jumps/over the lazy dog's back, e.g. at 10:30 (or 22:30); fi fl ffi 1,234.50 well-known \"quotes\" and a verylongwordthatoverflowsitsline.", []string{"en", "en-GB", "zh-Hans", "zh-Hant", "ja", "ko", "de", "fr", "ru", "ar", "fi", "sv"}},
		{"Über die Straße, déjà vu à la crème: señor, naïve, fjörd, Ærø, garçon, ¿qué? ¡sí! 50 % × 2 ÷ 4 ± 1 °C", []string{"en", "de", "fr", "es", "pt", "it", "sv", "fi", "da", "nb"}},
	}
	for _, c := range cases {
		text := []rune(c.text)
		for _, lang := range c.langs {
			if languageShapes(text, lang) {
				t.Errorf("%q in %s tells the engine", c.text, lang)
				continue
			}
			for _, width := range []float32{0, 90, 233, 400} {
				told := shapedWithLanguage(t, text, lang, width)
				s := newSystem()
				s.mu.Lock()
				left := s.engine().shape(text, Style{Size: 14, Lang: lang}, nil, width, false, false)
				if !sameShape(told, left) {
					t.Errorf("%q in %s at %v shapes apart from when the engine is told", c.text, lang, width)
				}
				s.mu.Unlock()
			}
		}
	}
	for text, lang := range map[string]string{"直骨": "ja", "“quoted”": "zh-Hans", "kind fi": "tr", "ijs": "nl", "Привет": "ru", "شكرا": "ar"} {
		if !languageShapes([]rune(text), lang) {
			t.Errorf("%q in %s does not tell the engine", text, lang)
		}
	}
}

// shapedWithLanguage shapes text with the language in its attributes, as
// shape does for a text whose language changes it.
func shapedWithLanguage(t *testing.T, text []rune, lang string, width float32) []shapedLine {
	t.Helper()
	s := newSystem()
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.engine().(*coreText)
	style := Style{Size: 14, Lang: lang}
	font := e.ctFont(style)
	e.u16, e.index = appendUTF16(e.u16[:0], e.index[:0], text)
	u16, index := e.u16, e.index
	typesetter := e.newTypesetter(font, u16, text, style, nil, false, lang)
	if typesetter == 0 {
		t.Fatal("no typesetter")
	}
	defer ct.release(typesetter)
	var out []shapedLine
	n := len(u16)
	for start := 0; start < n; {
		count := n - start
		if width > 0 {
			count = max(ct.suggestLineBreak(typesetter, start, float64(width)+1.0/1024), 1)
		}
		line := ct.typesetterCreateLine(typesetter, cfRange{start, count})
		out = append(out, cloneLine(e.line(line, index, n, false)))
		ct.release(line)
		start += count
	}
	return out
}

// cloneLine copies a line out of the engine's scratch.
func cloneLine(l shapedLine) shapedLine {
	l.runs = slices.Clone(l.runs)
	for i := range l.runs {
		l.runs[i].glyphs = slices.Clone(l.runs[i].glyphs)
	}
	return l
}

func sameShape(a, b []shapedLine) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].start != b[i].start || a[i].end != b[i].end || len(a[i].runs) != len(b[i].runs) {
			return false
		}
		for j := range a[i].runs {
			x, y := a[i].runs[j].glyphs, b[i].runs[j].glyphs
			if len(x) != len(y) {
				return false
			}
			for k := range x {
				if x[k].ID != y[k].ID || x[k].X != y[k].X || x[k].Y != y[k].Y || x[k].Advance != y[k].Advance || x[k].Cluster != y[k].Cluster || x[k].Font.Ascent != y[k].Font.Ascent {
					return false
				}
			}
		}
	}
	return true
}

// A number put together glyph by glyph is what Core Text shapes, in every
// font and style, whatever the kerning between its characters, as each of
// them is checked here rather than only the first of a font.
func TestCoreTextNumbers(t *testing.T) {
	numbers := []string{"0", "7", "11", "1,234,567.89", "-42.5%", "+3.14", "$1,000", "1.1.1.1", "$-0.50", "%%", "--", "..", "1+1-1", "00000000000000000000000000000000", "12.345", "98765.4321"}
	styles := []Style{{Size: 11}, {Size: 13}, {Size: 13, Weight: 700}, {Size: 14, Italic: true}, {Size: 14, Family: "Menlo"}, {Size: 16, Family: "Helvetica Neue"}, {Size: 15, Family: "Georgia"}, {Size: 13, Lang: "zh-Hans"}, {Size: 13, Lang: "en"}}
	s := newSystem()
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.engine().(*coreText)
	for _, style := range styles {
		for _, number := range numbers {
			text := []rune(number)
			e.resetScratch()
			lines := e.shapeNumber(text, style, nil, 0, false)
			if lines == nil {
				t.Fatalf("%q in %+v was not put together", number, style)
			}
			if !sameNumber(lines, e.shapeText(text, style, nil, 0, false, false)) {
				t.Errorf("%q in %+v put together is not what Core Text shapes", number, style)
			}
		}
	}
	for _, f := range e.numbers {
		if f.disagreed {
			t.Error("a font disagreed with Core Text")
		}
	}
}

// What is not a number, wraps, or takes more than its characters' glyphs is
// left to Core Text, and a font it disagrees with stays with it.
func TestCoreTextNumbersLeftOut(t *testing.T) {
	s := newSystem()
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.engine().(*coreText)
	style := Style{Size: 13}
	for _, c := range []struct {
		text  string
		style Style
		spans []Span
		width float32
		rtl   bool
	}{
		{text: "12a", style: style},
		{text: "12:30", style: style},
		{text: "١٢٣", style: style},
		{text: "123", style: style, rtl: true},
		{text: "123", style: style, spans: []Span{{End: 1, Weight: 700}}},
		{text: "123", style: Style{Size: 13, LetterSpacing: 1}},
		{text: "123", style: Style{Size: 13, Features: "tnum"}},
		{text: "123", style: Style{Size: 13, Lang: "tr"}},
		{text: "123456", style: style, width: 5},
		{text: strings.Repeat("1", maxNumber+1), style: style},
	} {
		e.resetScratch()
		if e.shapeNumber([]rune(c.text), c.style, c.spans, c.width, c.rtl) != nil {
			t.Errorf("%q in %+v (width %v, RTL %v) was put together", c.text, c.style, c.width, c.rtl)
		}
	}

	text := []rune("42")
	e.resetScratch()
	want := e.shapeText(text, style, nil, 0, false, false)
	wantX := want[0].runs[0].glyphs[1].X
	if e.shapeNumber(text, style, nil, 0, false) == nil {
		t.Fatal("42 was not put together")
	}
	f := e.numbers[numberKey{e.ctFont(style), false}]
	f.pairs[numberSlots['4']-1][numberSlots['2']-1].advance += 5
	e.resetScratch()
	got := e.shapeNumber(text, style, nil, 0, false)
	if !f.disagreed {
		t.Error("a number wrong by 5 did not count as a disagreement")
	}
	if x := got[0].runs[0].glyphs[1].X; x != wantX {
		t.Errorf("the second digit of a number that disagrees is at %v, Core Text puts it at %v", x, wantX)
	}
	e.resetScratch()
	if e.shapeNumber(text, style, nil, 0, false) != nil {
		t.Error("a font that disagreed still has numbers put together")
	}
}

// Latin text wrapped from one line is what Core Text wraps, for any text made
// of what wrapping takes, at any width, in a font's first checks and beyond.
func TestCoreTextWrapFuzz(t *testing.T) {
	iterations := 400
	if n, err := strconv.Atoi(os.Getenv("MYGO_WRAP_FUZZ")); err == nil {
		iterations = n
	}
	words := []string{"a", "I", "to", "the", "window", "layout", "AVATAR", "Type", "fi", "ffl", "office", "don't", "well-known", "e.g.", "U.S.A.", "1,234.56", "3.14", "$5", "50%", "a/b", "http://example.com/path?q=1&r=2", "(paren)", "[box]", "{curly}", "\"quoted\"", "'single'", "end.", "yes,", "stop;", "wait:", "what?", "no!", "x+y", "a=b", "#tag", "@me", "under_score", "<tag>", "~", "*star*", "pipe|", "back\\slash", "café", "Über", "señor", "naïve", "Ærø", "garçon", "verylongwordthatdoesnotfitalineandhastobrokenbycoretext", "--", "-", "...", "A-1", "1-2", "T.", "V,", "Y.", "L'", "f)", "ß"}
	seps := []string{" ", " ", " ", " ", "  ", "   ", "", "-", "/"}
	styles := []Style{{Size: 13}, {Size: 14, Weight: 700}, {Size: 13, Italic: true}, {Size: 15, Family: "Georgia"}, {Size: 12, Family: "Helvetica Neue"}, {Size: 13, Family: "Menlo"}, {Size: 16, Family: "Times New Roman"}}
	rng := rand.New(rand.NewPCG(1, 2))
	s := newSystem()
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.engine().(*coreText)
	wrapped, leftOut := 0, 0
	defer func() { t.Logf("%d wrappings compared, %d left to Core Text", wrapped, leftOut) }()
	for it := range iterations {
		var b strings.Builder
		for range 1 + rng.IntN(80) {
			b.WriteString(words[rng.IntN(len(words))])
			b.WriteString(seps[rng.IntN(len(seps))])
		}
		text := []rune(b.String())
		style := styles[it%len(styles)]
		e.resetScratch()
		font := e.ctFont(style)
		e.u16, e.index = appendUTF16(e.u16[:0], e.index[:0], text)
		typesetter := e.newTypesetter(font, e.u16, text, style, nil, false, "")
		kept := &keptTypesetter{typesetter: typesetter, font: font}
		for _, width := range []float32{float32(20 + rng.IntN(100)), float32(100 + rng.IntN(400)), float32(300+rng.IntN(500)) + rng.Float32()} {
			e.resetScratch()
			// Every wrapping checked by the test, not only some.
			f := e.wrapFonts[font]
			if f != nil {
				f.checked = wrapCheckedFirst
				f.lines = 1
			}
			got := e.wrapKept(kept, text, style, width)
			if got == nil {
				// What does not shape as a glyph for each rune stays with
				// Core Text.
				leftOut++
				continue
			}
			wrapped++
			e.u16, e.index = appendUTF16(e.u16[:0], e.index[:0], text)
			want := e.typesetLines(typesetter, 0, len(e.u16), width, nil, false, false, e.index, style.Italic)
			if !sameLines(got, want) {
				report(t, text, style, width, got, want)
				return
			}
		}
		ct.release(typesetter)
	}
}

// report tells the first line where wrapped text differs from Core Text's.
func report(t *testing.T, text []rune, style Style, width float32, got, want []shapedLine) {
	t.Helper()
	for i := range min(len(got), len(want)) {
		if got[i].start != want[i].start || got[i].end != want[i].end {
			t.Fatalf("in %+v at %v, line %d is %q, Core Text's %q", style, width, i, string(text[got[i].start:got[i].end]), string(text[want[i].start:want[i].end]))
		}
		if !sameLines(got[i:i+1], want[i:i+1]) {
			x, y := got[i].runs, want[i].runs
			t.Errorf("in %+v at %v, line %d %q has %d runs, Core Text's %d", style, width, i, string(text[got[i].start:got[i].end]), len(x), len(y))
			if len(x) == 1 && len(y) == 1 {
				for k := range min(len(x[0].glyphs), len(y[0].glyphs)) {
					g, h := x[0].glyphs[k], y[0].glyphs[k]
					if g.ID != h.ID || g.X != h.X || g.Advance != h.Advance {
						t.Errorf("  glyph %d (%q): id %d/%d x %v/%v advance %v/%v", k, string(text[g.Cluster]), g.ID, h.ID, g.X, h.X, g.Advance, h.Advance)
					}
				}
			}
			t.FailNow()
		}
	}
	t.Fatalf("in %+v at %v, %d lines, Core Text's %d", style, width, len(got), len(want))
}

// TestCoreTextWindowedTypeset holds lines broken over bounded
// typesetters to those of one typesetter over the whole paragraph, which
// past typesetterLimit allows unbounded layout and lays a long text out as
// a short one: Core Text's default lays it out otherwise. A span changing
// nothing (plain) takes a text off the kept typesetter onto windows, and
// with keptLimit at 0 every text but plain Latin is.
func TestCoreTextWindowedTypeset(t *testing.T) {
	oldThreshold, oldKept := windowThreshold, keptLimit
	defer func() { windowThreshold, keptLimit = oldThreshold, oldKept }()
	repeat := func(s string, n int) []rune {
		r := []rune(s)
		return []rune(strings.Repeat(s, (n+len(r)-1)/len(r)))[:n]
	}
	english := "The quick brown fox jumps over the lazy dog, while 42 typists in 1987 wrote flowing prose about kerning, ligatures (fi, fl, ffi) and office affairs. "
	chinese := "长途综合对比卡片需要在一个超长段落里排版，中文没有空格，标点“如此”，数字123和English混排。"
	mixed := english + chinese + "emoji 😀👍🏽 e\u0301 — “quotes” 1,234.56 "
	plain := []Span{{End: 1 << 30}}
	// rich styles runs of the text in turn bold, italic, monospaced and
	// plain, as a Markdown paragraph's.
	rich := func(n int) []Span {
		looks := []Span{{Weight: 700}, {Italic: true}, {Family: "Menlo"}, {}}
		var spans []Span
		for end, i := 23, 0; end < n+23; end, i = end+23, i+1 {
			sp := looks[i%len(looks)]
			sp.End = min(end, n)
			spans = append(spans, sp)
		}
		return spans
	}
	cases := []struct {
		name       string
		text       []rune
		style      Style
		spans      []Span
		window     int
		width      float32
		windowed   bool
		wholeWords bool
	}{
		{"english", repeat(english, 14000), Style{Size: 14}, plain, 4096, 400, true, false},
		{"english_small_window", repeat(english, 14000), Style{Size: 14}, plain, 128, 250, true, false},
		{"english_kept", repeat(english, 14000), Style{Size: 14}, nil, 4096, 400, false, false},
		{"chinese", repeat(chinese, 14000), Style{Size: 14}, nil, 4096, 400, true, false},
		{"chinese_small_window", repeat(chinese, 14000), Style{Size: 14}, nil, 128, 300, true, false},
		{"narrow", repeat(mixed, 14000), Style{Size: 14}, nil, 4096, 120, true, false},
		{"bold", repeat(mixed, 14000), Style{Size: 14, Weight: 700}, nil, 4096, 400, true, false},
		{"italic", repeat(mixed, 14000), Style{Size: 14, Italic: true}, nil, 4096, 400, true, false},
		{"family", repeat(mixed, 14000), Style{Size: 15, Family: "Georgia"}, nil, 4096, 400, true, false},
		{"lang", repeat(mixed, 14000), Style{Size: 13, Lang: "zh-Hans"}, nil, 4096, 400, true, false},
		{"spacing", repeat(mixed, 14000), Style{Size: 14, LetterSpacing: 0.5}, nil, 4096, 400, true, false},
		{"rich", repeat(mixed, 14000), Style{Size: 14}, rich(14000), 4096, 400, true, false},
		{"rich_small_window", repeat(mixed, 14000), Style{Size: 14}, rich(14000), 128, 300, true, false},
		{"long_token", append([]rune(strings.Repeat("x", 5000)), repeat(english, 9000)...), Style{Size: 14}, plain, 128, 250, true, false},
		{"attachments", repeat("a\ufffc b ", 12000), Style{Size: 14}, []Span{{End: 1 << 30, ObjectWidth: 28, ObjectHeight: 20}}, 128, 350, true, false},
		{"tabs", repeat("a\tb c ", 12000), Style{Size: 14, TabSize: 8, tabStop: 30}, nil, 128, 350, true, false},
		{"emoji", repeat("a😀e\u0301 b ", 12000), Style{Size: 14}, nil, 128, 350, true, false},
		{"whole_words", repeat("verylongwordthatwillnotfitanywhere and words ", 12000), Style{Size: 14}, nil, 128, 250, true, true},
		{"keep_words", repeat("한국어 단어가 줄을 넘지 않도록 ", 12000), Style{Size: 14, Lang: "ko"}, nil, 128, 250, true, false},
		{"breaks", repeat("hy\u00adphenated words and lines ", 12000), Style{Size: 14}, plain, 128, 250, true, false},
		{"rtl", append(repeat("plain prefix ", 1000), repeat("مرحبا بالعالم ", 4000)...), Style{Size: 14}, plain, 128, 300, false, false},
		{"bidi_control", append(repeat(english, 6000), []rune("\u202E")...), Style{Size: 14}, plain, 128, 300, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			windowThreshold, keptLimit = 1<<30, 1<<30
			want := shapedWithWindows(t, c.text, c.style, c.spans, c.width, c.wholeWords)
			windowThreshold, keptLimit = c.window, 0
			got := shapedWithWindows(t, c.text, c.style, c.spans, c.width, c.wholeWords)
			if !sameShape(want, got) {
				t.Fatalf("%d lines shaped over windows differ from %d lines shaped whole", len(got), len(want))
			}
			u16, _ := appendUTF16(nil, nil, c.text)
			windowed := !keeps(c.text, len(u16), c.style, c.spans, c.width, false, c.wholeWords, c.style.Lang) && windowEligible(c.text, false)
			if windowed != c.windowed {
				t.Fatalf("windowed %v, want %v", windowed, c.windowed)
			}
		})
	}
}

// TestCoreTextKeptRewrap wraps a long text kept across widths, as a window
// being resized does, and holds each width's lines to those over windows,
// which a span changing nothing takes the text to: the kept typesetter
// past typesetterLimit lays the text out as short ones do.
func TestCoreTextKeptRewrap(t *testing.T) {
	english := "The quick brown fox jumps over the lazy dog, while 42 typists in 1987 wrote flowing prose about kerning and type. "
	chinese := "长途综合对比卡片需要在一个超长段落里排版，中文没有空格，标点“如此”，数字123和English混排。"
	for _, c := range []struct {
		name string
		text string
		n    int
	}{{"english", english, 14000}, {"english_long", english, 40000}, {"chinese", chinese, 14000}, {"mixed", english + chinese, 14000}} {
		t.Run(c.name, func(t *testing.T) {
			r := []rune(strings.Repeat(c.text, c.n/len([]rune(c.text))+1))[:c.n]
			s := newSystem()
			s.mu.Lock()
			defer s.mu.Unlock()
			e := s.engine().(*coreText)
			for _, width := range []float32{400, 300, 517, 400} {
				e.resetScratch()
				got := cloneLines(e.shape(r, Style{Size: 14}, nil, width, false, false))
				if len(e.typesetters) != 1 {
					t.Fatalf("%d typesetters kept, want 1", len(e.typesetters))
				}
				want := shapedWithWindows(t, r, Style{Size: 14}, []Span{{End: 1 << 30}}, width, false)
				if len(got) != len(want) {
					t.Fatalf("at %v, %d lines kept, %d over windows", width, len(got), len(want))
				}
				for i := range got {
					if !sameLine(got[i], want[i]) {
						t.Fatalf("at %v, line %d [%d,%d) kept differs from [%d,%d) over windows", width, i, got[i].start, got[i].end, want[i].start, want[i].end)
					}
				}
			}
		})
	}
}

// TestCoreTextKeptRTL checks that a right-to-left text past keptLimit,
// which windows do not take, keeps its typesetter across widths, and that
// each width lays it out as a typesetter made for it alone.
func TestCoreTextKeptRTL(t *testing.T) {
	old := keptLimit
	keptLimit = 4096
	defer func() { keptLimit = old }()
	arabic := "النص العربي الطويل يكتب من اليمين إلى اليسار، ويحتوي على أرقام 123 وكلمات English أحيانا. "
	r := []rune(strings.Repeat(arabic, 14000/len([]rune(arabic))+1))[:14000]
	s := newSystem()
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.engine().(*coreText)
	for _, width := range []float32{400, 300, 517, 400} {
		e.resetScratch()
		got := cloneLines(e.shape(r, Style{Size: 14}, nil, width, true, false))
		if len(e.typesetters) != 1 {
			t.Fatalf("%d typesetters kept, want 1", len(e.typesetters))
		}
		fresh := newSystem()
		fresh.mu.Lock()
		want := cloneLines(fresh.engine().(*coreText).shape(r, Style{Size: 14}, nil, width, true, false))
		fresh.mu.Unlock()
		if len(got) != len(want) {
			t.Fatalf("at %v, %d lines kept, %d fresh", width, len(got), len(want))
		}
		for i := range got {
			if !sameLine(got[i], want[i]) {
				t.Fatalf("at %v, line %d [%d,%d) kept differs from [%d,%d) fresh", width, i, got[i].start, got[i].end, want[i].start, want[i].end)
			}
		}
	}
}

// sameLine reports whether two lines, of engines of their own, have the
// same runes and glyphs, placed within wrapTolerance as wrapKept places
// them.
func sameLine(a, b shapedLine) bool {
	if a.start != b.start || a.end != b.end {
		return false
	}
	var x, y []Glyph
	for _, r := range a.runs {
		x = append(x, r.glyphs...)
	}
	for _, r := range b.runs {
		y = append(y, r.glyphs...)
	}
	if len(x) != len(y) {
		return false
	}
	for k := range x {
		if x[k].ID != y[k].ID || x[k].Cluster != y[k].Cluster || math.Abs(float64(x[k].X-y[k].X)) > wrapTolerance ||
			k < len(x)-1 && math.Abs(float64(x[k].Advance-y[k].Advance)) > wrapTolerance {
			return false
		}
	}
	return true
}

func cloneLines(lines []shapedLine) []shapedLine {
	out := make([]shapedLine, len(lines))
	for i := range lines {
		out[i] = cloneLine(lines[i])
	}
	return out
}

// shapedWithWindows shapes text with a fresh engine, whose window
// threshold the test sets, and copies its lines out of its scratch.
func shapedWithWindows(t *testing.T, text []rune, style Style, spans []Span, width float32, wholeWords bool) []shapedLine {
	t.Helper()
	s := newSystem()
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.engine().(*coreText)
	e.resetScratch()
	return cloneLines(e.shape(text, style, spans, width, false, wholeWords))
}
