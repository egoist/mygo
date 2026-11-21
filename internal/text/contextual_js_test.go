package text

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

// contextualFake is a host that shapes pieces within their words, as
// Android's, measuring every rune 1 wide.
type contextualFake struct {
	clusterEngine
	f *Font
}

func (h *contextualFake) font(Style) *Font { return h.f }
func (h *contextualFake) measure(_ *Font, text string, _ bool) float32 {
	return float32(utf8.RuneCountInString(text))
}
func (h *contextualFake) pens(*Font, []rune, []int, bool) []float32 { return nil }
func (h *contextualFake) contextual() bool                          { return true }

// splitContext returns the text before a glyph's piece, the piece and the
// text after it, and whether the glyph has its context.
func splitContext(t string) (before, piece, after string, ok bool) {
	for _, sep := range []string{string(contextRTL), string(contextLTR)} {
		if p := strings.Split(t, sep); len(p) == 3 {
			return p[0], p[1], p[2], true
		}
	}
	return "", t, "", false
}

// checkContext shapes text with the style changing at the ends of spans,
// and reports each glyph of a complex script drawn without the whole
// word it is of.
func checkContext(t *testing.T, h *contextualFake, spans []string, width float32) (bad int) {
	t.Helper()
	var text []rune
	var sp []Span
	for _, s := range spans {
		text = append(text, []rune(s)...)
		sp = append(sp, Span{End: len(text), Features: string(rune('a' + len(sp)%2))})
	}
	// The words: runs between spaces.
	word := func(at int) string {
		i, j := at, at
		for i > 0 && text[i-1] != ' ' {
			i--
		}
		for j < len(text) && text[j] != ' ' {
			j++
		}
		return string(text[i:j])
	}
	for _, ln := range h.shape(text, Style{}, sp, width, true, false) {
		for _, r := range ln.runs {
			for _, g := range r.glyphs {
				before, piece, after, ok := splitContext(h.glyphs.texts[g.ID])
				// A mark on a space (the pause of saktah, ۜ, between
				// words) joins nothing.
				complex := strings.IndexFunc(piece, complexScript) >= 0
				if !complex || strings.HasPrefix(piece, " ") {
					continue
				}
				if w := word(g.Cluster); !ok || before+piece+after != strings.TrimSpace(w) {
					if bad < 5 {
						t.Errorf("%q: glyph %q (context %q|%q|%q), word %q", spans, piece, before, piece, after, w)
					}
					bad++
				}
			}
		}
	}
	return bad
}

// TestContextQuran shapes every verse of the Quran as Quranku colors its
// tajweed, in SPANS (a JSON list of verses, each a list of spans), and
// checks each piece of a word is shaped within it.
func TestContextQuran(t *testing.T) {
	path := os.Getenv("SPANS")
	if path == "" {
		t.Skip("SPANS names no file of verses")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var verses [][]string
	if err := json.Unmarshal(b, &verses); err != nil {
		t.Fatal(err)
	}
	h := &contextualFake{f: &Font{Size: 10, Ascent: 8, Descent: 2}}
	h.init(h)
	bad := 0
	for _, v := range verses {
		bad += checkContext(t, h, v, 0)
		bad += checkContext(t, h, v, 37) // lines that wrap
	}
	t.Logf("%d verses, %d glyphs without their word", len(verses), bad)
}

// TestContextWords checks the words that broke apart on Android: a word
// whose tajweed colors change mid-word and on a mark, an ayah's number of
// several digits, kashidas, and a pause mark on a space.
func TestContextWords(t *testing.T) {
	h := &contextualFake{f: &Font{Size: 10, Ascent: 8, Descent: 2}}
	h.init(h)
	for _, spans := range [][]string{
		{"بِسْمِ ", "ٱ", "للَّهِ ", "ٱ", "لرَّحْمَ", "ٰ", "نِ ", "ٱ", "لرَّحِ", "ي", "مِ"},
		{"مُهْتَدِينَ", " ", "۝١٦"},
		{"۝٢٥٥"},
		{"بِمُـــؤْمِـــنِـــ", "يـــ", "نَ"},
		{"يَـٰٓأَيُّهَا ٱلنَّاسُ"},
		{"مَنْ", " ۜ", " رَاقٍ"},
	} {
		if bad := checkContext(t, h, spans, 0); bad > 0 {
			t.Errorf("%q: %d glyphs without their word", spans, bad)
		}
	}
}
