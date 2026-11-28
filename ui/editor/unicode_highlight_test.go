package editor

import (
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

// unicodeTest is an editor of text in a white theme whose Warning, the
// color of the outlines, is pure red.
func unicodeTest(t *testing.T, text string, opts func(*State)) *editorTest {
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
// b, which show on the first line.
func (ct *editorTest) outlined(a, b int) bool {
	ct.t.Helper()
	ra, ok1 := ct.st.Rect(a)
	rb, ok2 := ct.st.Rect(b)
	if !ok1 || !ok2 {
		ct.t.Fatalf("offsets %d and %d don't show", a, b)
	}
	img := ct.tt.Image()
	s := float32(img.Bounds().Dx()) / 600
	for y := int(ra.Y * s); y < int((ra.Y+ra.H)*s); y++ {
		for x := int(ra.X * s); x <= int(rb.X*s); x++ {
			if c := img.RGBAAt(x, y); c.R > 200 && c.G < 90 && c.B < 90 {
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
	ct := unicodeTest(t, text, nil)
	xs := ct.xs()
	for i := 1; i < len(xs); i++ {
		if xs[i] < xs[i-1] {
			t.Fatalf("offset %d shows left of the one before it: %v", i, xs)
		}
	}
	cw := ct.st.ed.view.charWidth
	if r0, _ := ct.st.Rect(at); true {
		r1, _ := ct.st.Rect(at + len("\u202e"))
		if r1.X-r0.X != cw {
			t.Fatalf("the override is %v DIPs wide, want a column, %v", r1.X-r0.X, cw)
		}
	}
	if !ct.outlined(at, at+len("\u202e")) {
		t.Fatal("the override is not outlined")
	}
	if ct.outlined(0, at-1) {
		t.Fatal("text before it is outlined")
	}

	// Off, the font shows it, and the line reads backwards from it.
	ct = unicodeTest(t, text, func(st *State) { st.NoInvisibleCharacters = true })
	d, _ := ct.st.Rect(at + 3)
	e, _ := ct.st.Rect(at + 4)
	if e.X >= d.X {
		t.Fatal("with NoInvisibleCharacters, def shows left to right")
	}
	if ct.outlined(at, at+len("\u202e")) {
		t.Fatal("with NoInvisibleCharacters, the override is outlined")
	}
}

// Zero-width spaces and controls are outlined; the joiners of emoji and
// the tags of a flag are not.
func TestInvisibleCharactersAreOutlined(t *testing.T) {
	for _, c := range []struct {
		text, char string
		want       bool
	}{
		{"a\u200bb", "\u200b", true},
		{"a\x1bb", "\x1b", true},
		{"a\ufeffb", "\ufeff", true},
		{"a\u2066b\u2069c", "\u2066", true},
		{"x \U0001F469\u200d\U0001F467 y", "\u200d", false},
		{"x \U0001F3F4\U000E0067\U000E0062\U000E0073\U000E0063\U000E0074\U000E007F y", "\U000E0067", false},
		{"a\U000E0067b", "\U000E0067", true},
		{"\u0645\u06cc\u200c\u062e\u0648\u0627\u0647\u0645", "\u200c", false}, // Persian: the non-joiner shapes the word
		{"a\u200cb", "\u200c", true},
		{"isAdmin\u200d = 1", "\u200d", true},
		{"a\u200db", "\u200d", true},
		{"\u0915\u094d\u200d\u0937", "\u200d", false}, // Devanagari: the joiner shapes the conjunct
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
		ct := unicodeTest(t, c.text, nil)
		at := strings.Index(c.text, c.char)
		if c.char == "\U000E0061" && strings.Contains(c.text, "\U000E007F\U000E0061") {
			at = strings.LastIndex(c.text, c.char)
		}
		if got := ct.outlined(at, at+len(c.char)); got != c.want {
			t.Errorf("%+q: %+q outlined %v, want %v", c.text, c.char, got, c.want)
		}
	}
}

// AmbiguousCharacters outlines look-alikes in words with ASCII letters,
// not words of their own script; off, the default, it outlines none.
func TestAmbiguousCharacters(t *testing.T) {
	on := func(st *State) { st.AmbiguousCharacters = true }
	text := "go to p\u0430ypal.com"
	at := strings.Index(text, "\u0430")
	if ct := unicodeTest(t, text, nil); ct.outlined(0, len(text)) {
		t.Fatal("outlined by default")
	}
	ct := unicodeTest(t, text, on)
	if !ct.outlined(at, at+2) {
		t.Fatal("the Cyrillic \u0430 of p\u0430ypal is not outlined")
	}
	if ct.outlined(0, at-1) || ct.outlined(at+3, len(text)) {
		t.Fatal("ASCII is outlined")
	}
	russian := "// \u043f\u0440\u0438\u0432\u0435\u0442 \u043c\u0438\u0440, \u0430 2\u00d73"
	if ct := unicodeTest(t, russian, on); ct.outlined(0, len(russian)) {
		t.Fatal("Russian is outlined")
	}
	// A word all of look-alikes, or with ASCII digits only, has none
	// outlined; Turkish's dotless i beside ASCII letters is.
	for _, c := range []struct {
		text string
		want bool
	}{{"x = \u0441\u043e\u0440", false}, {"x = \u04400", false}, {"kap\u0131", true}} {
		if got := unicodeTest(t, c.text, on).outlined(0, len(c.text)); got != c.want {
			t.Errorf("%+q outlined %v, want %v", c.text, got, c.want)
		}
	}
}

// The hover over an outlined character says what it is.
func TestHoverNamesTheCharacter(t *testing.T) {
	for _, c := range []struct {
		text, char, want string
	}{
		{"x\u202ey", "\u202e", "The character U+202E is invisible and can reorder the text around it."},
		{"x\u200by", "\u200b", "The character U+200B is invisible."},
		{"p\u0430y", "\u0430", "The character U+0430 \u201c\u0430\u201d can be confused with \u201ca\u201d."},
	} {
		var got []Diagnostic
		ct := unicodeTest(t, c.text, func(st *State) {
			st.AmbiguousCharacters, st.HoverDelay = true, time.Second
			st.RenderHover = func(c *ui.Context, panel ui.Element, h *HoverCard) {
				got = h.Diagnostics()
				DefaultHover(c, panel, h)
			}
		})
		at := strings.Index(c.text, c.char)
		r, _ := ct.st.Rect(at)
		next, _ := ct.st.Rect(at + len(c.char))
		ct.tt.Move((r.X+next.X)/2, r.Y+r.H/2)
		ct.tt.Advance(1500 * time.Millisecond)
		ct.tt.Frame()
		if len(got) != 1 || got[0].Message != c.want || got[0].Source != "editor" || got[0].Start != at || got[0].End != at+len(c.char) {
			t.Errorf("%+q: the hover shows %+v, want %q", c.text, got, c.want)
		}
	}
}

// A byte order mark opening the text is not outlined; one elsewhere is.
func TestByteOrderMarkAtTheStart(t *testing.T) {
	ct := unicodeTest(t, "\ufeffpackage main\nx\ufeffy", nil)
	if ct.outlined(0, len("\ufeff")) {
		t.Fatal("the byte order mark opening the text is outlined")
	}
	if !ct.st.ed.view.line(1).hasSeg(segInvisible) {
		t.Fatal("a byte order mark inside the text is not shown")
	}
}

// NoInvisibleCharacters leaves odd spaces alone too.
func TestNoInvisibleCharactersOutlinesNoSpaces(t *testing.T) {
	ct := unicodeTest(t, "a\u00a0b", func(st *State) { st.NoInvisibleCharacters = true })
	if ct.outlined(0, len("a\u00a0b")) {
		t.Fatal("a no-break space is outlined")
	}
}

// A unified diff outlines the invisible characters of its deleted lines,
// which reorder nothing there either; NoInvisibleCharacters turns it off.
func TestDiffDeletedLinesOutlineInvisibleCharacters(t *testing.T) {
	for _, off := range []bool{false, true} {
		d := &DiffState{Old: NewBuffer("x := \"abc\u202edef\"\n"), New: NewBuffer("y\n"), Layout: DiffUnified,
			NoBackground: true, Inline: DiffInlineNone, NoInvisibleCharacters: off,
			Theme: &Theme{Background: ui.RGB(255, 255, 255), Text: ui.RGB(0, 0, 0), Warning: ui.RGB(255, 0, 0),
				Deleted: ui.RGB(0, 0, 255), Added: ui.RGB(0, 0, 255), Modified: ui.RGB(0, 0, 255)}}
		tt := ui.NewTester(func(c *ui.Context) { DiffView(c, d).Fill() }, 600, 200)
		tt.Frame()
		img := tt.Image()
		red := 0
		for y := range img.Bounds().Dy() {
			for x := range img.Bounds().Dx() {
				if c := img.RGBAAt(x, y); c.R > 200 && c.G < 90 && c.B < 90 {
					red++
				}
			}
		}
		if (red > 0) == off {
			t.Fatalf("NoInvisibleCharacters %v: %d red pixels", off, red)
		}
		if l := d.oldView.line(0); l.hasSeg(segInvisible) == off {
			t.Fatalf("NoInvisibleCharacters %v: the old line shows the override as %q", off, l.disp)
		}
	}
}

// The hover shows over the box, not with the pointer over the half of the
// next character that the box's edge is nearer.
func TestHoverOnlyOverTheBox(t *testing.T) {
	var got []Diagnostic
	ct := unicodeTest(t, "x\u200byz", func(st *State) {
		st.HoverDelay = time.Second
		st.RenderHover = func(c *ui.Context, panel ui.Element, h *HoverCard) {
			got = h.Diagnostics()
			DefaultHover(c, panel, h)
		}
	})
	r, _ := ct.st.Rect(len("x\u200b"))
	ct.tt.Move(r.X+ct.st.ed.view.charWidth/4, r.Y+r.H/2)
	ct.tt.Advance(1500 * time.Millisecond)
	ct.tt.Frame()
	if len(got) != 0 {
		t.Fatalf("the hover beside the box shows %+v", got)
	}
}

func (l *lineLayout) hasSeg(kind uint8) bool {
	return slices.ContainsFunc(l.segs, func(s dispSeg) bool { return s.kind == kind })
}

// The scans' shortcuts miss no character: each that shows as invisible
// between two letters makes its line show otherwise, and each odd space
// is outlined.
func TestScansMissNoCharacter(t *testing.T) {
	v := &view{unicodeMarks: unicodeMarks{invisible: true}}
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if !utf8.ValidRune(r) || !invisible(r) && !oddSpace(r) {
			continue
		}
		s := "a" + string(r) + "b"
		if want := invisibleAt(s, 1, false) > 0; v.showsAsIs(s, false) == want {
			t.Fatalf("U+%04X: showsAsIs %v, want %v", r, !want, want)
		}
		if got := outlinedIn(s, v.unicodeMarks, nil); oddSpace(r) != (len(got) == 1) {
			t.Fatalf("U+%04X: outlined %v", r, got)
		}
	}
}
