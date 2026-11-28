package editor

import (
	"image/color"
	"slices"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"golang.org/x/text/unicode/norm"
)

// invisibleTest is an editor of text in a white theme whose Warning, the
// color of the boxes, is pure red.
func invisibleTest(t *testing.T, text string, opts func(*State)) *editorTest {
	t.Helper()
	ct := newEditorTest(t, "", text)
	ct.st.Theme = &Theme{Background: ui.RGB(255, 255, 255), Text: ui.RGB(0, 0, 0), Warning: ui.RGB(255, 0, 0)}
	ct.st.NoActiveLine, ct.st.NoIndentGuides = true, true
	if opts != nil {
		opts(ct.st)
	}
	ct.st.Select(0, 0)
	ct.tt.Frame()
	return ct
}

// outlined reports whether the last frame drew red between offsets a and
// b, which show on one row.
func (ct *editorTest) outlined(a, b int) bool {
	ct.t.Helper()
	ra, ok1 := ct.st.Rect(a)
	rb, ok2 := ct.st.Rect(b)
	if !ok1 || !ok2 {
		ct.t.Fatalf("offsets %d and %d don't show", a, b)
	}
	return hasColor(ct.tt, red, min(ra.X, rb.X), max(ra.X, rb.X), ra.Y, ra.Y+ra.H)
}

func red(c color.RGBA) bool  { return c.R > 200 && c.G < 90 && c.B < 90 }
func blue(c color.RGBA) bool { return c.B > 200 && c.R < 90 && c.G < 90 }

// hasColor reports whether the last frame of tt drew a color is reports
// in the DIPs from x0 to x1 and y0 to y1, all of it with x1 negative.
func hasColor(tt *ui.Tester, is func(color.RGBA) bool, x0, x1, y0, y1 float32) bool {
	img := tt.Image()
	s := float32(img.Bounds().Dx()) / 600
	if x1 < 0 {
		x1, y1 = float32(img.Bounds().Dx())/s, float32(img.Bounds().Dy())/s
	}
	for y := int(y0 * s); y < int(y1*s); y++ {
		for x := int(x0 * s); x <= int(x1*s); x++ {
			if is(img.RGBAAt(x, y)) {
				return true
			}
		}
	}
	return false
}

// xs are the left edges of the offsets of the first line of the text.
func (ct *editorTest) xs() []float32 {
	text, _, _ := strings.Cut(ct.st.Buffer.String(), "\n")
	var out []float32
	for i := range text + "." {
		r, _ := ct.st.Rect(i)
		out = append(out, r.X)
	}
	return out
}

// A right-to-left override shows as a space in a box and reorders nothing,
// where it used to show the rest of its line backwards (Trojan Source).
func TestBidiControlsReorderNothing(t *testing.T) {
	text := "if s == \"abc\u202edef\" { x() }"
	at := strings.Index(text, "\u202e")
	ct := invisibleTest(t, text, nil)
	xs := ct.xs()
	for i := 1; i < len(xs); i++ {
		if xs[i] < xs[i-1] {
			t.Fatalf("offset %d shows left of the one before it: %v", i, xs)
		}
	}
	r0, _ := ct.st.Rect(at)
	r1, _ := ct.st.Rect(at + len("\u202e"))
	if cw := ct.st.ed.view.charWidth; r1.X-r0.X != cw {
		t.Fatalf("the override is %v DIPs wide, want a column, %v", r1.X-r0.X, cw)
	}
	if !ct.outlined(at, at+len("\u202e")) {
		t.Fatal("the override is not outlined")
	}
	if ct.outlined(0, at-1) {
		t.Fatal("text before it is outlined")
	}

	// Turned off, the font shows it, and the line reads backwards from
	// it; turned on again, it is boxed again.
	ct.st.NoInvisibleCharacters = true
	ct.tt.Frame()
	d, _ := ct.st.Rect(at + 3)
	e, _ := ct.st.Rect(at + 4)
	if e.X >= d.X {
		t.Fatal("with NoInvisibleCharacters, def shows left to right")
	}
	if ct.outlined(at, at+len("\u202e")) {
		t.Fatal("with NoInvisibleCharacters, the override is outlined")
	}
	ct.st.NoInvisibleCharacters = false
	ct.tt.Frame()
	if !ct.outlined(at, at+len("\u202e")) {
		t.Fatal("turned on again, the override is not outlined")
	}
}

// Zero-width spaces, controls and odd spaces are outlined; what shapes or
// varies the text around it is not.
func TestInvisibleCharactersAreOutlined(t *testing.T) {
	for _, c := range []struct {
		text, char string
		want       bool
	}{
		{"a\u200bb", "\u200b", true},
		{"a\x1bb", "\x1b", true},
		{"a\ufeffb", "\ufeff", true},
		{"a\u2066b\u2069c", "\u2066", true},
		{"a\u2028b", "\u2028", true},
		{"x \U0001F469\u200d\U0001F467 y", "\u200d", false},
		{"x \U0001F3F4\U000E0067\U000E0062\U000E0073\U000E0063\U000E0074\U000E007F y", "\U000E0067", false},
		{"a\U000E0067b", "\U000E0067", true},
		{"\u0645\u06cc\u200c\u062e\u0648\u0627\u0647\u0645", "\u200c", false}, // Persian: the non-joiner shapes the word
		{"a\u200cb", "\u200c", true},
		{"let \u0430\u200c = 1", "\u200c", true},      // after a Cyrillic letter
		{"const caf\u00e9\u200d = 1", "\u200d", true}, // after a Latin letter
		{"\u03b1\u200d\u03b2", "\u200d", true},        // Greek
		{"\u0561\u200c\u0562", "\u200c", true},        // Armenian
		{"\u0645\u200c", "\u200c", true},              // no letter after it to join
		{"\u0915\u094d\u200c\u0937", "\u200c", false}, // after a virama
		{"x \U0001F469\u200dy", "\u200d", true},       // no emoji after it
		{"s = \"\u00e9\ufe0f\"", "\ufe0f", true},      // after a letter
		{"x1\ufe0f = 2", "\ufe0f", true},              // a digit, not a keycap's
		{"\u845b\U000E0100", "\U000E0100", false},     // a Han ideograph's variant
		{"a\U000E0100b", "\U000E0100", true},          // not after Han
		{"\u2229\ufe00", "\ufe00", false},             // a symbol's standardized variant
		{"\u65e5\u672c\u200b\u8a9e", "\u200b", true},  // among CJK
		{"\tx\u00a0y", "\u00a0", true},                // after a tab's segment
		{"a\u200bb\u00a0c", "\u00a0", true},           // after a box
		{"isAdmin\u200d = 1", "\u200d", true},
		{"a\u200db", "\u200d", true},
		{"\u0915\u094d\u200d\u0937", "\u200d", false}, // Devanagari: the joiner shapes the conjunct
		{"\u09b0\u200d\u09cd\u09af", "\u200d", false}, // Bengali ya-phala: a letter, the joiner, a virama
		{"a\u200d\u094d", "\u200d", true},             // a Latin letter before another script's virama
		{"x \U0001F469\U0001F3FD\u200d\U0001F4BB y", "\u200d", false},
		{"a\u180eb", "\u180e", true},
		{"\u1820\u180e\u1821", "\u180e", false}, // Mongolian
		{"x \u263a\ufe0f y", "\ufe0f", false},
		{"x \u263a\ufe0f\ufe01 y", "\ufe01", true}, // a run of variation selectors
		{"a\ufe0fb", "\ufe0f", true},
		{"key 1\ufe0f\u20e3", "\ufe0f", false},
		{"a\u034fb", "\u034f", true},
		{"a\u115fb", "\u115f", true},
		{"a\u17b4b", "\u17b4", true},
		{"a\u00a0b", "\u00a0", true},
		{"a\u2003b", "\u2003", true},
		{"a\u202fb", "\u202f", true},
		{"\u65e5\u672c\u3000\u8a9e", "\u3000", false}, // the ideographic space of CJK text
		// Tags only as a flag's: a black flag, two to six of a-z and 0-9, and the cancel tag.
		{"x \U0001F3F4\U000E0061\U000E0061\U000E0061\U000E0061\U000E0061\U000E0061\U000E0061\U000E007F y", "\U000E0061", true},
		{"x \U0001F3F4\U000E0068\U000E0069\U000E0064\U000E0065 y", "\U000E0068", true},
		{"x \U0001F3F4\U000E0047\U000E0042\U000E007F y", "\U000E0047", true},
		{"x \U0001F3F4\U000E0067\U000E0062\U000E007F\U000E0061 y", "\U000E0061", true},
	} {
		ct := invisibleTest(t, c.text, nil)
		at := strings.Index(c.text, c.char)
		if c.char == "\U000E0061" && strings.Contains(c.text, "\U000E007F\U000E0061") {
			at = strings.LastIndex(c.text, c.char)
		}
		if got := ct.outlined(at, at+len(c.char)); got != c.want {
			t.Errorf("%+q: %+q outlined %v, want %v", c.text, c.char, got, c.want)
		}
	}
}

// A byte order mark opening the text is not outlined; one elsewhere is.
func TestByteOrderMarkAtTheStart(t *testing.T) {
	ct := invisibleTest(t, "\ufeffpackage main\nx\ufeffy", nil)
	if ct.outlined(0, len("\ufeff")) {
		t.Fatal("the byte order mark opening the text is outlined")
	}
	if !ct.st.ed.view.line(1).hasSeg(segInvisible) {
		t.Fatal("a byte order mark inside the text is not shown")
	}
}

// A line in another font boxes them too.
func TestStyledLineOutlinesInvisibleCharacters(t *testing.T) {
	ct := invisibleTest(t, "a\u200bb", nil)
	ct.st.SetLineStyles([]LineStyle{{Line: 0, Font: ui.Font{Size: 20}}})
	ct.tt.Frame()
	if !ct.outlined(1, 1+len("\u200b")) {
		t.Fatal("the zero-width space of a styled line is not outlined")
	}
}

// A diff outlines the invisible characters of its old side, and a unified
// one those of its deleted lines, which reorder nothing there either;
// NoInvisibleCharacters turns it off.
func TestDiffOutlinesInvisibleCharacters(t *testing.T) {
	for _, layout := range []DiffLayout{DiffSplit, DiffUnified} {
		for _, off := range []bool{false, true} {
			d := &DiffState{Old: NewBuffer("x := \"abc\u202edef\"\n"), New: NewBuffer("y\n"), Layout: layout,
				NoBackground: true, Inline: DiffInlineNone, NoInvisibleCharacters: off,
				Theme: &Theme{Background: ui.RGB(255, 255, 255), Text: ui.RGB(0, 0, 0), Warning: ui.RGB(255, 0, 0),
					Deleted: ui.RGB(0, 0, 255), Added: ui.RGB(0, 0, 255), Modified: ui.RGB(0, 0, 255)}}
			tt := ui.NewTester(func(c *ui.Context) { DiffView(c, d).Fill() }, 600, 200)
			tt.Frame()
			if red := hasColor(tt, red, 0, -1, 0, 0); red == off {
				t.Fatalf("%v, NoInvisibleCharacters %v: red drawn %v", layout, off, red)
			}
			v := &d.eds[0].view
			if layout == DiffUnified {
				v = &d.oldView
			}
			if l := v.line(0); l.hasSeg(segInvisible) == off {
				t.Fatalf("%v, NoInvisibleCharacters %v: the old line shows the override as %q", layout, off, l.disp)
			}
		}
	}
}

func (l *lineLayout) hasSeg(kind uint8) bool {
	return slices.ContainsFunc(l.segs, func(s dispSeg) bool { return s.kind == kind })
}

// The scans' shortcuts miss no character: each that shows as invisible
// between letters, at any of eight bytes' places, or after a byte that
// starts no well-formed character, makes its line show otherwise, and
// each odd space is found.
func TestScansMissNoCharacter(t *testing.T) {
	v := &view{invisibles: true}
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if !utf8.ValidRune(r) || !invisible(r) && !oddSpace(r) {
			continue
		}
		for _, before := range []string{"a", "\u65e5", "\xe6", "\xe6\x97", "\xf0\x9f", "\xd0", "abcdefgh", "abcdefghijk"} {
			s := before + string(r) + "bcdefghij"
			if want := invisibleAt(s, len(before), false) > 0; v.showsAsIs(s, false) == want {
				t.Fatalf("U+%04X after %+q: showsAsIs %v, want %v", r, before, !want, want)
			}
		}
		if got := oddSpacesIn("a"+string(r)+"b", nil); oddSpace(r) != (len(got) == 1) {
			t.Fatalf("U+%04X: odd spaces %v", r, got)
		}
	}
}

// invisible's ranges told at once hide no control but tab and no format
// character but a prepended concatenation mark.
func TestInvisibleMissesNoFormatCharacter(t *testing.T) {
	for r := rune(0); r <= unicode.MaxRune; r++ {
		format := unicode.Is(unicode.Cf, r) && !unicode.Is(unicode.Prepended_Concatenation_Mark, r)
		if (format || unicode.IsControl(r) && r != '\t') && !invisible(r) {
			t.Fatalf("U+%04X is not invisible", r)
		}
	}
}

// Painting a long line of boxes goes through the segments and spaces of
// the part of it in view once, not through the line again for each box.
func TestPaintingBoxesTakesWorkInView(t *testing.T) {
	ct := invisibleTest(t, strings.Repeat("ab\x1b\u00a0", 6000), nil)
	v := &ct.st.ed.view
	v.markSteps = 0
	ct.st.Select(1, 1)
	ct.tt.Frame()
	if v.markSteps == 0 || v.markSteps > 2*pieceBytes {
		t.Fatalf("a frame went through %d segments and spaces", v.markSteps)
	}
}

// The controls of bidirectional text in an inlay or a replacement, as a
// language server's hints, reorder nothing of the line around them.
func TestInlaysAndReplacementsReorderNothing(t *testing.T) {
	text := "x := f(a, b) // ok"
	for _, set := range []func(st *State){
		func(st *State) { st.SetInlays([]Inlay{{Offset: 5, Text: "\u202e: int"}}) },
		func(st *State) { st.SetReplacements([]Replacement{{Start: 5, End: 6, Text: "\u202eg"}}) },
	} {
		ct := invisibleTest(t, text, nil)
		set(ct.st)
		ct.tt.Frame()
		xs := ct.xs()
		for i := 7; i < len(xs); i++ {
			if xs[i] < xs[i-1] {
				t.Fatalf("offset %d shows left of the one before it: %v", i, xs)
			}
		}
	}
}

// An offset inside a boxed character maps to its box, as a diagnostic's
// start there shows from the box.
func TestDiagnosticFromInsideABox(t *testing.T) {
	text := "ab\u200bcd"
	ct := invisibleTest(t, text, func(st *State) { st.Theme.Error = ui.RGB(0, 0, 255) })
	ct.st.SetDiagnostics(ct.st.Buffer.Version(), []Diagnostic{{Start: 3, End: 5, Severity: SeverityError}})
	ct.tt.Frame()
	a, _ := ct.st.Rect(2)
	b, _ := ct.st.Rect(5)
	if !hasColor(ct.tt, blue, a.X, b.X, a.Y, a.Y+a.H) {
		t.Fatal("the diagnostic does not show under the box")
	}
}

// A click on a box's right half places the caret after its character, and
// the arrows step over it whole.
func TestCaretAcrossABox(t *testing.T) {
	ct := invisibleTest(t, "a\u200bbc", nil)
	r0, _ := ct.st.Rect(0)
	cw := ct.st.ed.view.charWidth
	ct.tt.ClickAt(r0.X+1.75*cw, r0.Y+r0.H/2)
	ct.tt.Frame()
	if got := ct.st.Selection(); got.Head != 4 || got.Anchor != 4 {
		t.Fatalf("a click on the box's right half selects %+v, want 4", got)
	}
	ct.tt.Key(0, ui.KeyLeft)
	ct.tt.Frame()
	if got := ct.st.Selection().Head; got != 1 {
		t.Fatalf("left from after the box goes to %d, want 1", got)
	}
}

// A selection and a find match of a boxed character cover its box.
func TestSelectionAndMatchCoverTheBox(t *testing.T) {
	text := "x\nab\u200bcd"
	at := strings.Index(text, "\u200b")
	covered := func(ct *editorTest) bool {
		a, _ := ct.st.Rect(at)
		b, _ := ct.st.Rect(at + len("\u200b"))
		return hasColor(ct.tt, blue, a.X, b.X, a.Y, a.Y+a.H)
	}
	ct := invisibleTest(t, text, func(st *State) { st.Theme.Selection = ui.RGB(0, 0, 255) })
	ct.st.Select(at, at+len("\u200b"))
	ct.tt.Frame()
	if !covered(ct) {
		t.Fatal("the selection does not cover the box")
	}
	ct = invisibleTest(t, text, func(st *State) {
		st.Theme.Match, st.Theme.CurrentMatch = ui.RGB(0, 0, 255), ui.RGB(0, 0, 255)
		// The match is selected too: a selection all but clear.
		st.Theme.Selection = ui.RGBA(0, 255, 0, 0.01)
		st.Theme.InactiveSelection = st.Theme.Selection
	})
	ct.st.Find("\u200b")
	ct.tt.Frame()
	if n, _ := ct.st.FindMatches(); n != 1 || !covered(ct) {
		t.Fatalf("the find match (of %d) does not cover the box", n)
	}
}

// Boxes take room in soft-wrapped lines, those in view and those measured
// away from it.
func TestSoftWrapCountsBoxes(t *testing.T) {
	line := "abcd" + strings.Repeat("\u200b", 8) + "xyz"
	ct := invisibleTest(t, line, func(st *State) { st.SoftWrap, st.WrapColumn = true, 10 })
	first, _ := ct.st.Rect(0)
	last, _ := ct.st.Rect(len(line))
	if last.Y <= first.Y {
		t.Fatal("a line of 15 columns does not wrap at 10")
	}
	far := strings.Repeat("x\n", 300) + line
	ct = invisibleTest(t, far, func(st *State) { st.SoftWrap, st.WrapColumn = true, 10 })
	for i := 0; i < 100 && ct.tt.FramePending(); i++ {
		ct.tt.Frame()
	}
	lh := float64(ct.st.ed.view.lineHeight)
	if got := ct.st.LineTop(301) - ct.st.LineTop(300); got != 2*lh {
		t.Fatalf("the last line is %v high, want two rows, %v", got, 2*lh)
	}
}

// The viramas are those of canonical combining class 9.
func TestViramas(t *testing.T) {
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if want := norm.NFC.PropertiesString(string(r)).CCC() == 9; isVirama(r) != want {
			t.Fatalf("U+%04X: isVirama %v, want %v", r, !want, want)
		}
	}
}
