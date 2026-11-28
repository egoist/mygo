package fuzzy

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// A match looks at each rune of a long item a few times, not once for
// each rune of the query, whatever the script.
func TestMatchingLooksAtALongItemAFewTimes(t *testing.T) {
	for _, c := range []struct{ query, item string }{
		{"aaaaaaaa", strings.Repeat("xa", 5000)},
		{"éééééééé", strings.Repeat("xé", 5000)},
		{strings.Repeat("é", 32), strings.Repeat("xé", 5000)},
		{"abbbbbbbbc", "a" + strings.Repeat("xb", 5000) + "c"},
		{"ΣΣΣΣ", strings.Repeat("λσ", 5000)},
		{"abcdefgh", strings.Repeat("xaxbxcxdxexfxgxh", 600)},
	} {
		p := NewPattern(c.query)
		_, ok, steps := p.match(c.item, nil)
		if !ok {
			t.Fatalf("%q does not match", c.query)
		}
		if n := utf8.RuneCountInString(c.item); steps > 6*n {
			t.Errorf("%q: %d runes looked at in an item of %d", c.query, steps, n)
		}
	}
}
