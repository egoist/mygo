package fuzzy_test

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"sort"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui/fuzzy"
)

// ranked returns the items query matches, best first, ties in the order
// given, as a list filtering by what is typed shows them.
func ranked(query string, items ...string) []string {
	p := fuzzy.NewPattern(query)
	var out []string
	scores := map[string]int{}
	for _, it := range items {
		if s, ok := p.Score(it); ok {
			out = append(out, it)
			scores[it] = s
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return scores[out[a]] > scores[out[b]] })
	return out
}

func TestMatchesTheQueryInOrderWhateverTheCase(t *testing.T) {
	for _, c := range []struct {
		query, item string
		want        bool
	}{
		{"abc", "a_b_c", true},
		{"abc", "acb", false},
		{"ABC", "abc", true},
		{"abc", "ABC", true},
		{"abcd", "abc", false},
		{"", "anything", true},
		{"", "", true},
		{"a", "", false},
		{"a b", "a big file", true},
	} {
		_, ok := fuzzy.NewPattern(c.query).Score(c.item)
		if ok != c.want {
			t.Errorf("%q in %q: %v, want %v", c.query, c.item, ok, c.want)
		}
	}
}

func TestMatchesLettersOfOtherScriptsInEitherCase(t *testing.T) {
	for _, c := range []struct{ query, item string }{
		{"über", "ÜBER"},
		{"ÜBER", "über"},
		{"σοφ", "ΣΟΦΊΑ"},
		{"ΣΟΦ", "σοφία"},
		{"ς", "Σ"},
		{"straße", "STRAßE"},
		{"k", "K"}, // the Kelvin sign
		{"K", "k"},
		{"S", "ſ"},      // the long s
		{"i", "\u0130"}, // İ, whose small letter is i
		{"\u0130", "I"},
		{"中文", "中文字"},
	} {
		if _, ok := fuzzy.NewPattern(c.query).Score(c.item); !ok {
			t.Errorf("%q does not match %q", c.query, c.item)
		}
	}
	if _, ok := fuzzy.NewPattern("ü").Score("u"); ok {
		t.Error("ü matches u")
	}
}

func TestRanksTheMatchesAUserMeans(t *testing.T) {
	for _, c := range []struct {
		query string
		want  string // first
		items []string
	}{
		// The letters at the starts of words: camelCase, snake_case, paths.
		{"gebi", "getElementById", []string{"getElementsByName", "getElementById"}},
		{"sv", "serverVersion", []string{"savior", "sliver", "serverVersion"}},
		{"fb", "foo_bar", []string{"fobar", "afoobar", "foo_bar"}},
		{"nt", "New Thread", []string{"Mark as Done (agent)", "New Thread"}},
		{"hp", "HasPrefix", []string{"Hyphen", "HasPrefix"}},
		// A word typed whole beats the same letters scattered.
		{"view", "src/view.go", []string{"src/v_i_e_w.go", "src/view.go"}},
		{"set", "setValue", []string{"reset", "assets", "setValue"}},
		// The file name beats a folder's.
		{"main", "src/main.go", []string{"main/src/lib.go", "src/main.go"}},
		// The query's own case breaks a tie.
		{"Set", "SetTimeout", []string{"setTimeout", "SetTimeout"}},
		// And then the shorter item.
		{"len", "len", []string{"length", "len"}},
		{"abc", "abc", []string{"a_b_abc", "abc"}},
	} {
		if got := ranked(c.query, c.items...); len(got) == 0 || got[0] != c.want {
			t.Errorf("%q ranks %q, want %q first", c.query, got, c.want)
		}
	}
}

func TestPositionsAreTheMatchedRunes(t *testing.T) {
	for _, c := range []struct {
		query, item string
		want        []int
	}{
		{"gbi", "getElementById", []int{0, 10, 12}},
		{"geb", "getElementById", []int{0, 1, 10}}, // a run before a word start
		{"ev", "src/editor/view.go", []int{4, 11}},
		{"über", "docs/Über_Straße.md", []int{5, 7, 8, 9}},
		{"aße", "Straße", []int{3, 4, 6}},
		{"ab", "xab_a", []int{1, 2}},
		{"main", "main/src/main.go", []int{9, 10, 11, 12}},
	} {
		if got := fuzzy.NewPattern(c.query).Positions(c.item); !slices.Equal(got, c.want) {
			t.Errorf("%q in %q: %v, want %v", c.query, c.item, got, c.want)
		}
	}
	if got := fuzzy.NewPattern("xyz").Positions("abc"); got != nil {
		t.Errorf("positions without a match: %v", got)
	}
	if got := fuzzy.NewPattern("").Positions("abc"); got != nil {
		t.Errorf("positions of an empty query: %v", got)
	}
}

// Bytes that are not UTF-8 match as the replacement rune, one byte each.
func TestMatchesTextThatIsNotUTF8(t *testing.T) {
	for _, c := range []struct {
		query, item string
		want        []int
	}{
		{"ab", "a\xffb", []int{0, 2}},
		{"\xff", "x\xfe", []int{1}},
		{"\ufffd", "x\xc3", []int{1}},
		{"é", "\xc3e\u00e9", []int{2}},
	} {
		if got := fuzzy.NewPattern(c.query).Positions(c.item); !slices.Equal(got, c.want) {
			t.Errorf("%q in %q: %v, want %v", c.query, c.item, got, c.want)
		}
	}
}

// agree checks that what Score finds and what Positions gives agree, and
// that the positions are those of runes equal to the query's, whatever
// their case, in order.
func agree(t *testing.T, query, item string) {
	t.Helper()
	p := fuzzy.NewPattern(query)
	_, ok := p.Score(item)
	at := p.Positions(item)
	if query == "" {
		if !ok || at != nil {
			t.Fatalf("empty query in %q: Score %v, Positions %v", item, ok, at)
		}
		return
	}
	if ok != (at != nil) {
		t.Fatalf("%q in %q: Score %v, Positions %v", query, item, ok, at)
	}
	qr := []rune(query)
	for i, pos := range at {
		if pos < 0 || pos >= len(item) || i > 0 && pos <= at[i-1] {
			t.Fatalf("%q in %q: positions %v", query, item, at)
		}
		r, _ := utf8.DecodeRuneInString(item[pos:])
		if !sameLetter(r, qr[i]) {
			t.Fatalf("%q in %q: positions %v", query, item, at)
		}
	}
}

func FuzzScore(f *testing.F) {
	for _, c := range [][2]string{{"gbi", "getElementById"}, {"main", "src/main.go"}, {"über", "docs/Über_Straße.md"},
		{"ab", "a\xffb"}, {"σ", "ΣΟΦ"}, {"\u0130", "I"}, {"", "x"}, {"aaaa", "xaxaxa"}} {
		f.Add(c[0], c[1])
	}
	f.Fuzz(agree)
}

func TestPositionsAgreeWithScore(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	alphabet := []string{"a", "b", "B", "e", "k", "K", "s", "S", "/", "_", "-", ".", " ", "1", "ü", "Ü", "ß", "σ", "Σ", "ς", "K", "ſ", "中"}
	word := func(n int) string {
		var b strings.Builder
		for range n {
			b.WriteString(alphabet[rng.IntN(len(alphabet))])
		}
		return b.String()
	}
	for range 20_000 {
		agree(t, word(1+rng.IntN(4)), word(rng.IntN(30)))
	}
}

// sameLetter reports whether Unicode folds a and b together, or their
// small letters, as İ and I.
func sameLetter(a, b rune) bool {
	orbit := func(a, b rune) bool {
		for f := unicode.SimpleFold(a); ; f = unicode.SimpleFold(f) {
			if f == b {
				return true
			}
			if f == a {
				return false
			}
		}
	}
	return orbit(a, b) || orbit(unicode.ToLower(a), unicode.ToLower(b))
}

func TestScoreAllocatesNothing(t *testing.T) {
	items := []string{"getElementById", "src/editor/view.go", "docs/Über_Straße.md", "xyz"}
	for _, q := range []string{"gebi", "ev", "über", "Σ"} {
		p := fuzzy.NewPattern(q)
		if n := testing.AllocsPerRun(100, func() {
			for _, it := range items {
				p.Score(it)
			}
		}); n != 0 {
			t.Errorf("%q: %v allocations", q, n)
		}
	}
}

func ExamplePattern_Positions() {
	p := fuzzy.NewPattern("nrwc")
	label := "NewRequestWithContext"
	if _, ok := p.Score(label); ok {
		var b strings.Builder
		at := p.Positions(label)
		for i, r := range label {
			if len(at) > 0 && at[0] == i {
				b.WriteString("[" + string(r) + "]")
				at = at[1:]
			} else {
				b.WriteRune(r)
			}
		}
		fmt.Println(b.String())
	}
	// Output: [N]ew[R]equest[W]ith[C]ontext
}

// BenchmarkScore ranks 5,000 identifiers by what is typed, a letter more
// each time.
func BenchmarkScore(b *testing.B) {
	words := []string{"get", "set", "new", "read", "write", "file", "name", "path", "value", "index",
		"buffer", "request", "handler", "context", "error", "string", "list", "item", "node", "line"}
	var items []string
	for i := 0; len(items) < 5_000; i++ {
		a, c, d := words[i%len(words)], words[i/len(words)%len(words)], words[i/400%len(words)]
		items = append(items, a+strings.ToUpper(c[:1])+c[1:]+strings.ToUpper(d[:1])+d[1:])
	}
	typed := []string{"r", "re", "req", "reqh", "reqha", "gfn", "setv"}
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		p := fuzzy.NewPattern(typed[i%len(typed)])
		for _, it := range items {
			p.Score(it)
		}
	}
}
