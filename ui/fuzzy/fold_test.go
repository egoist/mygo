package fuzzy

import (
	"testing"
	"unicode"
)

// An ASCII query folds the item's runes only as far as ASCII, which tells
// the same matches: of the other runes, only the long s, the Kelvin sign
// and İ fold to ASCII ones.
func TestFoldingToASCIIIsEnoughForASCIIQueries(t *testing.T) {
	for r := rune(0); r <= unicode.MaxRune; r++ {
		full, ascii := fold(r, false), fold(r, true)
		if full < 0x80 && ascii != full || full >= 0x80 && ascii < 0x80 {
			t.Fatalf("%U: folds to %U, to %U as far as ASCII", r, full, ascii)
		}
	}
}

// Runes with the same small letter fold together, so a rune matches all
// it matched when runes were compared by their small letters, as the
// editor's completions once were. Folding matches more: those Unicode
// folds together whose small letters differ, as ſ and s, ς and σ, ϐ and
// β.
func TestFoldingMatchesWhatSmallLettersMatched(t *testing.T) {
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if fold(r, false) != fold(unicode.ToLower(r), false) {
			t.Fatalf("%U folds to %U, its small letter %U to %U", r, fold(r, false), unicode.ToLower(r), fold(unicode.ToLower(r), false))
		}
	}
	for _, p := range [][2]rune{{'ſ', 's'}, {'ς', 'σ'}, {'ϐ', 'β'}, {'K', 'k'}} {
		if fold(p[0], false) != fold(p[1], false) {
			t.Errorf("%q and %q fold apart", p[0], p[1])
		}
	}
}
