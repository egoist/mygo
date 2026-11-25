package markdown

import (
	"image"
	"math"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown/parser"
)

// findView builds *src, which can grow, in a view of 700 by 300 DIPs that
// scrolls, wide enough for the whole find bar, with the bar open as o
// asks, and returns it, the scroll state and whether it shows a text
// whole.
func findView(src *string, o *Options) (*ui.Tester, *ui.ScrollState, func(string) bool) {
	var scroll ui.ScrollState
	tt := ui.NewTester(func(c *ui.Context) {
		c.DisableAnimations()
		ui.Scroll(c).Fill().TrackScroll(&scroll).Children(func() { View(c, *src, o) })
	}, 700, 300)
	shows := func(s string) bool {
		r, ok := tt.Find(s)
		return ok && r.Y >= 0 && r.Y+r.H <= 300
	}
	return tt, &scroll, shows
}

// findSays fails unless the view's find bar says s, as "1 of 2".
func findSays(t *testing.T, tt *ui.Tester, s string) {
	t.Helper()
	if !tt.HasText(s) {
		t.Fatalf("the bar does not say %q: %q", s, strings.Join(tt.Texts(), "|"))
	}
}

// click presses the bar's control named label and draws the frame after.
func click(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
}

// dragSelect drags a selection from the start of the text showing from to
// the end of the text showing to, and draws the frame after.
func dragSelect(t *testing.T, tt *ui.Tester, from, to string) {
	t.Helper()
	a, ok := tt.Find(from)
	if !ok {
		t.Fatalf("no %q to select from", from)
	}
	b, ok := tt.Find(to)
	if !ok {
		t.Fatalf("no %q to select to", to)
	}
	tt.Press(a.X+0.5, a.Y+a.H/2)
	tt.Move(b.X+b.W-1, b.Y+b.H/2)
	tt.Release(b.X+b.W-1, b.Y+b.H/2)
	tt.Frame()
}

// rect fails unless the view shows a text s whole, and returns where.
func rect(t *testing.T, tt *ui.Tester, s string) ui.Rect {
	t.Helper()
	r, ok := tt.Find(s)
	if !ok {
		t.Fatalf("the view does not show %q", s)
	}
	return r
}

// wash returns the color most of the pixels of r have in img: what shows
// behind the text there.
func wash(img *image.RGBA, r ui.Rect) [3]int {
	counts := map[[3]int]int{}
	var best [3]int
	for y := int(r.Y); y < int(r.Y+r.H); y++ {
		for x := int(r.X); x < int(r.X+r.W); x++ {
			c := img.RGBAAt(x, y)
			k := [3]int{int(c.R), int(c.G), int(c.B)}
			if counts[k]++; counts[k] > counts[best] {
				best = k
			}
		}
	}
	return best
}

// over returns fg as it shows over bg, a color of a tester's image.
func over(fg ui.Color, bg [3]int) [3]int {
	a := float64(fg.A) / 255
	return [3]int{
		int(math.Round(a*float64(fg.R) + (1-a)*float64(bg[0]))),
		int(math.Round(a*float64(fg.G) + (1-a)*float64(bg[1]))),
		int(math.Round(a*float64(fg.B) + (1-a)*float64(bg[2]))),
	}
}

// pixels counts the pixels of r in img that are c, and those that are
// another color.
func pixels(img *image.RGBA, r ui.Rect, c, other [3]int) (int, int) {
	var a, b int
	for y := int(r.Y); y < int(r.Y+r.H); y++ {
		for x := int(r.X); x < int(r.X+r.W); x++ {
			p := img.RGBAAt(x, y)
			k := [3]int{int(p.R), int(p.G), int(p.B)}
			switch {
			case near(k, c):
				a++
			case near(k, other):
				b++
			}
		}
	}
	return a, b
}

// near reports whether a and b are a color of a tester's image apart.
func near(a, b [3]int) bool {
	for i := range a {
		if math.Abs(float64(a[i]-b[i])) > 3 {
			return false
		}
	}
	return true
}

// tintPixels counts the pixels of r in img tinted with c over bg, the color
// behind the text there, as the bar paints a match: the colors ClearType's
// fringes make of plain text are others.
func tintPixels(img *image.RGBA, r ui.Rect, c ui.Color, bg [3]int) int {
	want := over(c, bg)
	n := 0
	for y := int(r.Y); y < int(r.Y+r.H); y++ {
		for x := int(r.X); x < int(r.X+r.W); x++ {
			p := img.RGBAAt(x, y)
			if near([3]int{int(p.R), int(p.G), int(p.B)}, want) {
				n++
			}
		}
	}
	return n
}

// TestViewFind checks that the view's find bar is the code editor's, and
// that it opens with its options, finds two matches, goes through them
// with Enter round the ends, closes with Escape, and opens again to what
// was found, with the matches highlighting while the bar is open and not
// once it is closed.
func TestViewFind(t *testing.T) {
	src := "cache one\n\nfiller text\n\nfiller text\n\ncache two\n"
	o := &Options{Find: true}
	tt, _, _ := findView(&src, o)
	tt.Type("cache")
	tt.Frame()
	for _, of := range []string{"Match case", "Match whole word", "Use regular expression", "Find in selection", "Next match", "Close"} {
		if !tt.HasText(of) {
			t.Fatalf("the bar is not the code editor's, which has %q: %q", of, strings.Join(tt.Texts(), "|"))
		}
	}
	findSays(t, tt, "1 of 2")
	// The theme's tints, over the page: the match the bar is on in
	// CurrentMatch, the other in Match.
	th := editor.DefaultTheme(ui.LightTheme())
	bg := wash(tt.Image(), rect(t, tt, "filler text"))
	if n := tintPixels(tt.Image(), rect(t, tt, "cache one"), th.CurrentMatch, bg); n < 20 {
		t.Errorf("the match the bar is on highlights with %d pixels, want its tint", n)
	}
	if n := tintPixels(tt.Image(), rect(t, tt, "cache two"), th.Match, bg); n < 20 {
		t.Errorf("the other match highlights with %d pixels, want its tint", n)
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	findSays(t, tt, "2 of 2")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	findSays(t, tt, "1 of 2")
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if tt.HasText("of 2") {
		t.Error("Escape did not close the bar")
	}
	for _, s := range []string{"cache one", "cache two"} {
		for _, c := range []ui.Color{th.Match, th.CurrentMatch} {
			if n := tintPixels(tt.Image(), rect(t, tt, s), c, bg); n != 0 {
				t.Errorf("%s highlights with %d pixels with the bar closed", s, n)
			}
		}
	}
	// Open again, as a Find menu item does: the same query finds again.
	o.FindVersion++
	tt.Frame()
	findSays(t, tt, "1 of 2")
	if n := tintPixels(tt.Image(), rect(t, tt, "cache one"), th.CurrentMatch, bg); n < 20 {
		t.Errorf("opening the bar again highlights with %d pixels, want its tint", n)
	}
}

// TestViewFindHighlightsCurrentMatch checks that the matches highlight in
// Theme.Match and the one the bar is on in Theme.CurrentMatch, a color of
// its own, which Enter takes to the next match.
func TestViewFindHighlightsCurrentMatch(t *testing.T) {
	src := "cat\n\nfiller text\n\nCAT\n"
	o := &Options{Find: true}
	tt, _, _ := findView(&src, o)
	tt.Type("cat")
	tt.Frame()
	findSays(t, tt, "1 of 2")
	first, second, plain := rect(t, tt, "cat"), rect(t, tt, "CAT"), rect(t, tt, "filler text")
	base := wash(tt.Image(), plain)
	ect := editor.DefaultTheme(ui.LightTheme())
	match, current := over(ect.Match, base), over(ect.CurrentMatch, base)
	if near(match, current) {
		t.Fatalf("Theme.Match and Theme.CurrentMatch show the same over %v: %v", base, match)
	}
	// A match is washed in c alone: none of its pixels in the other's, as
	// the bar goes to it and leaves it.
	washed := func(r ui.Rect, c [3]int) bool {
		other := current
		if c == current {
			other = match
		}
		a, b := pixels(tt.Image(), r, c, other)
		return a > 20 && b == 0
	}
	if !washed(first, current) || !washed(second, match) {
		t.Error("the match the bar is on is not washed in Theme.CurrentMatch alone, or the next not in Theme.Match alone")
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	findSays(t, tt, "2 of 2")
	if !washed(first, match) || !washed(second, current) {
		t.Error("after Enter the first match is not washed in Theme.Match alone, or the one the bar is on not in Theme.CurrentMatch alone")
	}
}

// TestViewFindCmdF checks that Cmd+F opens the bar while the view has the
// focus, as clicking its text gives it, and gives its field the focus
// again where the bar is open.
func TestViewFindCmdF(t *testing.T) {
	src := "findable text\n"
	o := &Options{}
	tt, _, _ := findView(&src, o)
	tt.Frame()
	if _, ok := tt.Find("Close"); ok {
		t.Fatal("the find bar shows without being asked for")
	}
	if err := tt.Click("findable text"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Frame()
	if _, ok := tt.Find("Close"); !ok {
		t.Fatal("Cmd+F did not open the find bar")
	}
	tt.Type("findable")
	tt.Frame()
	findSays(t, tt, "1 of 1")
	// A click on the view's text moves the focus from the field: at the
	// start of the text, which the bar over the top right does not cover.
	r, _ := tt.Find("findable text")
	tt.ClickAt(r.X+3, r.Y+r.H/2)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Frame()
	if !tt.Focused("Find") {
		t.Fatal("Cmd+F did not give the field the focus again")
	}
}

// TestViewFindCloseRefocuses checks that closing the bar, with Escape or
// its close button, gives the view the focus back where the scroll is, so
// that Cmd+F opens the bar again without a click.
func TestViewFindCloseRefocuses(t *testing.T) {
	for _, how := range []string{"Escape", "the close button"} {
		t.Run(how, func(t *testing.T) {
			src := "findable text\n\n" + strings.Repeat("filler text\n\n", 60)
			o := &Options{Find: true}
			tt, scroll, _ := findView(&src, o)
			tt.Type("findable")
			tt.Frame()
			scroll.Y = 400
			tt.Frame()
			if how == "Escape" {
				tt.Key(0, ui.KeyEscape)
			} else if err := tt.Click("Close"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if tt.HasText("of 1") {
				t.Fatalf("closing with %s left the bar open", how)
			}
			if !tt.Focused("filler text") {
				t.Errorf("closing with %s did not give the view the focus", how)
			}
			if scroll.Y != 400 {
				t.Errorf("closing with %s scrolled the view to %g, from 400", how, scroll.Y)
			}
			tt.Key(ui.Cmd, ui.KeyF)
			tt.Frame()
			if _, ok := tt.Find("Close"); !ok {
				t.Errorf("Cmd+F does not open the bar again after closing with %s", how)
			}
		})
	}
}

// TestViewFindScrollsToMatch checks that the match the bar goes to comes
// into view, and that wrapping around brings the first match back.
func TestViewFindScrollsToMatch(t *testing.T) {
	src := "cache one\n\n" + strings.Repeat("filler text\n\n", 40) + "cache two\n"
	o := &Options{Find: true}
	tt, scroll, shows := findView(&src, o)
	tt.Type("cache")
	tt.Frame()
	findSays(t, tt, "1 of 2")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if scroll.Y <= 0 || !shows("cache two") {
		t.Error("the match below the fold did not come into view")
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if !shows("cache one") {
		t.Error("wrapping around did not bring the first match into view")
	}
}

// TestViewFindRevealsTallParagraph checks that a match near the end of a
// paragraph taller than the view comes into it, not only the paragraph's
// middle.
func TestViewFindRevealsTallParagraph(t *testing.T) {
	src := strings.Repeat("filler words ", 300) + "needle\n"
	o := &Options{Find: true}
	tt, _, _ := findView(&src, o)
	tt.Type("needle")
	tt.Frame()
	findSays(t, tt, "1 of 1")
	view := ui.Rect{X: 0, Y: 0, W: 700, H: 300}
	img := tt.Image()
	th := editor.DefaultTheme(ui.LightTheme())
	if n := tintPixels(img, view, th.CurrentMatch, wash(img, view)); n < 20 {
		t.Errorf("the match in the tall paragraph is not in the view: %d pixels", n)
	}
}

// TestViewFindCentersMatch checks that the match the bar goes to comes to
// the middle of the view, where the scroll reaches it.
func TestViewFindCentersMatch(t *testing.T) {
	src := "cache one\n\n" + strings.Repeat("filler text\n\n", 40) + "cache two\n\n" + strings.Repeat("filler text\n\n", 40)
	o := &Options{Find: true}
	tt, _, _ := findView(&src, o)
	tt.Type("cache")
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	findSays(t, tt, "2 of 2")
	if r := rect(t, tt, "cache two"); math.Abs(float64(r.Y+r.H/2-150)) > 1 {
		t.Errorf("the match the bar is on is at %g, the middle of the view's 300 being 150", r.Y+r.H/2)
	}
}

// TestViewFindMatchesCaseAsAsked checks that the query matches any case,
// and the bar's Match case option makes it match case.
func TestViewFindMatchesCaseAsAsked(t *testing.T) {
	src := "Cache cache\n"
	o := &Options{Find: true}
	tt, _, _ := findView(&src, o)
	tt.Type("Cache")
	tt.Frame()
	findSays(t, tt, "1 of 2")
	click(t, tt, "Match case")
	findSays(t, tt, "1 of 1")
}

// TestViewFindWholeWords checks that the bar's Match whole word option
// keeps the matches that are whole words, and that turning it off finds
// the others again.
func TestViewFindWholeWords(t *testing.T) {
	src := "a cat, a catalog and CAT scans\n"
	o := &Options{Find: true}
	tt, _, _ := findView(&src, o)
	tt.Type("cat")
	tt.Frame()
	findSays(t, tt, "1 of 3")
	click(t, tt, "Match whole word")
	findSays(t, tt, "1 of 2") // the cat and the CAT, not the catalog
	click(t, tt, "Match whole word")
	findSays(t, tt, "1 of 3")
}

// TestViewFindStaysOnMatch checks that the match the bar is on stays where
// it is as the query and an option change, where matches before it come
// and go: where it is tells, not how many came before it.
func TestViewFindStaysOnMatch(t *testing.T) {
	src := "cat catalog cat cat\n"
	o := &Options{Find: true}
	tt, _, _ := findView(&src, o)
	tt.Type("at")
	tt.Frame()
	findSays(t, tt, "1 of 4")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	findSays(t, tt, "3 of 4")
	// Typed back to "a": one comes before, in the catalog.
	tt.Key(0, ui.KeyBackspace)
	tt.Frame()
	findSays(t, tt, "4 of 5")
	// Typed on to "at" again: it goes.
	tt.Type("t")
	tt.Frame()
	findSays(t, tt, "3 of 4")
	for range 2 {
		tt.Key(0, ui.KeyBackspace)
	}
	tt.Type("cat")
	tt.Frame()
	findSays(t, tt, "4 of 4")
	tt.Key(ui.Shift, ui.KeyEnter)
	tt.Frame()
	findSays(t, tt, "3 of 4")
	// Whole words: the catalog's goes, before the match the bar is on,
	// which is the second of the rest.
	click(t, tt, "Match whole word")
	findSays(t, tt, "2 of 3")
	click(t, tt, "Match whole word")
	findSays(t, tt, "3 of 4")
}

// TestViewFindRegexp checks that the bar's Use regular expression option
// takes the query as one, and that one that is not says Invalid.
func TestViewFindRegexp(t *testing.T) {
	src := "cat cot cut\n"
	o := &Options{Find: true}
	tt, _, _ := findView(&src, o)
	tt.Type("c[ao]t")
	tt.Frame()
	findSays(t, tt, "No results")
	click(t, tt, "Use regular expression")
	findSays(t, tt, "1 of 2")
	for range len("c[ao]t") {
		tt.Key(0, ui.KeyBackspace)
	}
	tt.Type("c(")
	tt.Frame()
	findSays(t, tt, "Invalid")
	if tt.HasText("of 2") {
		t.Error("the bar counts matches of a query that is not a regular expression")
	}
	for range 2 {
		tt.Key(0, ui.KeyBackspace)
	}
	tt.Type("c.t$")
	tt.Frame()
	findSays(t, tt, "1 of 1")
}

// TestViewFindInSelection checks that the bar's Find in selection option
// finds within the text selected as it was turned on, two paragraphs of
// the view's three here, and in all of the text again once it is off.
func TestViewFindInSelection(t *testing.T) {
	src := "alpha beta\n\nalpha gamma\n\nalpha delta\n"
	o := &Options{Find: true}
	tt, _, _ := findView(&src, o)
	dragSelect(t, tt, "alpha beta", "alpha gamma")
	click(t, tt, "Find in selection")
	// The drag left the focus on the view's text: the field takes typing.
	click(t, tt, "Find")
	tt.Type("alpha")
	tt.Frame()
	findSays(t, tt, "1 of 2")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	findSays(t, tt, "2 of 2")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	findSays(t, tt, "1 of 2")
	click(t, tt, "Find in selection")
	findSays(t, tt, "1 of 3")
}

// TestViewFindInSelectionNothingSelected checks that the option finds in
// all of the text where nothing was selected as it turned on, as the
// editor's does.
func TestViewFindInSelectionNothingSelected(t *testing.T) {
	src := "alpha beta\n\nalpha gamma\n"
	o := &Options{Find: true}
	tt, _, _ := findView(&src, o)
	click(t, tt, "Find in selection")
	tt.Type("alpha")
	tt.Frame()
	findSays(t, tt, "1 of 2")
}

// TestViewFindInSharedSelection checks that a view leaving its selection
// to the view around it (Options.ShareSelection) finds in that selection.
func TestViewFindInSharedSelection(t *testing.T) {
	var innerO Options
	innerO.ShareSelection, innerO.Find = true, true
	var inner *viewState
	outerO := Options{Block: func(c *ui.Context, d *parser.Document, i int) bool {
		if d.Block(i).Kind != parser.Quote {
			return false
		}
		el := View(c, "alpha beta\n\nalpha gamma\n", &innerO)
		inner = *ui.Local(el, stateKey{}, newState)
		return true
	}}
	src := "Intro\n\n> quoted\n\nOutro"
	tt := ui.NewTester(func(c *ui.Context) {
		c.DisableAnimations()
		ui.Scroll(c).Fill().Children(func() { View(c, src, &outerO) })
	}, 700, 300)
	tt.Frame()
	dragSelect(t, tt, "alpha beta", "alpha beta")
	click(t, tt, "Find in selection")
	if inner == nil {
		t.Fatal("the quote was not built by the inner view")
	}
	if len(inner.find.scope) != 1 {
		t.Fatalf("the inner view's find has %d parts in its scope, want the paragraph selected", len(inner.find.scope))
	}
	click(t, tt, "Find")
	tt.Type("alpha")
	tt.Frame()
	findSays(t, tt, "1 of 1")
	click(t, tt, "Find in selection")
	findSays(t, tt, "1 of 2")
}

// TestViewFindKeepsUnmatchedText checks that what the query does not
// match still shows while the view finds, a table's cells included.
func TestViewFindKeepsUnmatchedText(t *testing.T) {
	src := "keep this\n\n| a | b |\n|---|---|\n| cache | plain |\n"
	o := &Options{Find: true}
	tt, _, _ := findView(&src, o)
	tt.Type("cache")
	tt.Frame()
	findSays(t, tt, "1 of 1")
	for _, gone := range []string{"keep this", "plain", "a", "b"} {
		if _, ok := tt.Find(gone); !ok {
			t.Errorf("the unmatched text %q vanished", gone)
		}
	}
}

// TestViewFindSkipsImageDescriptions checks that an image's description
// within text is not found in, while the text around it is, and the
// paragraph shows whole.
func TestViewFindSkipsImageDescriptions(t *testing.T) {
	src := "see ![photo of a cat](https://example.com/cat.png) and a cat\n\nphoto\n"
	o := &Options{Find: true}
	for _, tc := range []struct{ query, says string }{
		{"photo", "1 of 1"},          // the paragraph below, not the description
		{"cat", "1 of 1"},            // the cat after the image, not the description's
		{"of a", "No results"},       // the description's alone
		{"see [photo", "No results"}, // from the text into the description
		{"and a cat", "1 of 1"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			tt, _, _ := findView(&src, o)
			tt.Type(tc.query)
			tt.Frame()
			findSays(t, tt, tc.says)
			rect(t, tt, "see [photo of a cat] and a cat")
		})
	}
}

// TestViewFindInCodeBlocks checks that code blocks match, one chunk of
// lines at a time, and that the match in a block's second chunk comes
// into view.
func TestViewFindInCodeBlocks(t *testing.T) {
	src := "Intro.\n\n```\nmutex here\n```\n\n```go\n" +
		strings.Repeat("line of code\n", 60) + "deeply buried mutex\n```\n\n" +
		strings.Repeat("tail text\n\n", 30)
	o := &Options{Find: true}
	tt, scroll, _ := findView(&src, o)
	tt.Type("mutex")
	tt.Frame()
	findSays(t, tt, "1 of 2")
	// A code block's matches tint over its own background.
	th := DefaultTheme(ui.LightTheme())
	bg := over(th.CodeBlockBackground, wash(tt.Image(), rect(t, tt, "Intro.")))
	if n := tintPixels(tt.Image(), rect(t, tt, "mutex here"), th.CurrentMatch, bg); n < 20 {
		t.Errorf("a match in a code block highlights with %d pixels, want its tint", n)
	}
	// The second match, in the second chunk of the long block, below the
	// fold.
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	findSays(t, tt, "2 of 2")
	if scroll.Y <= 0 {
		t.Error("the match in the block's second chunk did not come into view")
	}
	if n := tintPixels(tt.Image(), ui.Rect{X: 0, Y: 0, W: 700, H: 300}, th.CurrentMatch, bg); n < 20 {
		t.Errorf("the match in the block's second chunk is not in the view: %d pixels", n)
	}
}

// TestViewFindNoMatches checks what the bar says without a query and
// without matches.
func TestViewFindNoMatches(t *testing.T) {
	src := "some text\n"
	o := &Options{Find: true}
	tt, _, _ := findView(&src, o)
	tt.Frame()
	if tt.HasText("No results") {
		t.Error("the bar says no results without a query")
	}
	tt.Type("zzz")
	tt.Frame()
	findSays(t, tt, "No results")
}

// TestViewFindBarSticksToView checks that the bar rides at the top of
// whatever the scroll around the view shows.
func TestViewFindBarSticksToView(t *testing.T) {
	src := "cache one\n\n" + strings.Repeat("filler text\n\n", 60)
	o := &Options{Find: true}
	tt, scroll, _ := findView(&src, o)
	tt.Type("cache")
	tt.Frame()
	scroll.Y = scroll.MaxY
	tt.Frame()
	r, ok := tt.Find("Close")
	if !ok {
		t.Fatal("the find bar is gone")
	}
	if r.Y < 0 || r.Y+r.H > 300 {
		t.Fatalf("the bar rides at %g, outside the view", r.Y)
	}
}

// TestViewFindBarFitsNarrowView checks that the bar leaves out the count of
// matches, then its options and the buttons going through them, as the
// view narrows, staying within it, as it does in an editor.
func TestViewFindBarFitsNarrowView(t *testing.T) {
	src := "cache one\n\nfiller text\n\ncache two\n"
	o := &Options{Find: true}
	tt, _, _ := findView(&src, o)
	tt.Type("cache")
	tt.Frame()
	for _, tc := range []struct {
		w                         int
		count, optionsAndStepping bool
	}{
		{700, true, true},
		{450, false, true},
		{330, false, false},
	} {
		tt.SetSize(tc.w, 300)
		// The bar is built for the width the view had the frame before.
		tt.Frame()
		tt.Frame()
		if got := [2]bool{tt.HasText("1 of 2"), tt.HasText("Match case") && tt.HasText("Next match")}; got != [2]bool{tc.count, tc.optionsAndStepping} {
			t.Errorf("width %d: the count and the options with stepping shown %v, want %v: %q", tc.w, got,
				[2]bool{tc.count, tc.optionsAndStepping}, strings.Join(tt.Texts(), "|"))
		}
		r, ok := tt.Find("Close")
		if !ok || r.X < 0 || r.X+r.W > float32(tc.w) {
			t.Errorf("width %d: the close button is at %v (found %v), outside the view", tc.w, r, ok)
		}
	}
}

// TestViewFindKeys goes through the matches with the bar's keys, round the
// ends: Enter and Shift+Enter, Cmd+G and Shift+Cmd+G (F3 and Shift+F3
// elsewhere), as the editor's bar takes them.
func TestViewFindKeys(t *testing.T) {
	src := "cache one\n\n" + strings.Repeat("filler text\n\n", 40) + "cache two\n\n" +
		strings.Repeat("filler text\n\n", 20) + "cache three\n"
	o := &Options{Find: true}
	tt, _, _ := findView(&src, o)
	tt.Type("cache")
	tt.Frame()
	findSays(t, tt, "1 of 3")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	findSays(t, tt, "2 of 3")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	findSays(t, tt, "3 of 3")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	findSays(t, tt, "1 of 3")
	tt.Key(ui.Shift, ui.KeyEnter)
	tt.Frame()
	findSays(t, tt, "3 of 3")
	var next, previous func()
	if ui.Cmd == ui.Super {
		next = func() { tt.Key(ui.Cmd, ui.KeyG) }
		previous = func() { tt.Key(ui.Shift|ui.Cmd, ui.KeyG) }
	} else {
		next = func() { tt.Key(0, ui.KeyF3) }
		previous = func() { tt.Key(ui.Shift, ui.KeyF3) }
	}
	next()
	tt.Frame()
	findSays(t, tt, "1 of 3")
	previous()
	tt.Frame()
	findSays(t, tt, "3 of 3")
}

// TestViewFindButtonsKeepFocus checks that the bar's buttons take clicks
// without taking the focus from its field, so that typing goes on into
// the query.
func TestViewFindButtonsKeepFocus(t *testing.T) {
	src := "cache one\n\nfiller text\n\ncache two\n"
	o := &Options{Find: true}
	tt, _, _ := findView(&src, o)
	tt.Type("cache")
	tt.Frame()
	click(t, tt, "Next match")
	findSays(t, tt, "2 of 2")
	if !tt.Focused("Find") {
		t.Fatal("Next took the focus from the field")
	}
	tt.Type(" one")
	tt.Frame()
	findSays(t, tt, "1 of 1")
	click(t, tt, "Match case")
	if !tt.Focused("Find") {
		t.Fatal("Match case took the focus from the field")
	}
}

// TestStreamFind checks that a reply streaming in is found as it comes.
func TestStreamFind(t *testing.T) {
	text := "cache one"
	o := &Options{Find: true, Reveal: &Reveal{Unit: AtOnce}}
	tt := ui.NewTester(func(c *ui.Context) {
		c.DisableAnimations()
		Stream(c, text, o)
	}, 700, 300)
	tt.Type("cache")
	tt.Frame()
	findSays(t, tt, "1 of 1")
	text = "cache one\n\ncache two"
	tt.Frame()
	findSays(t, tt, "1 of 2")
}
