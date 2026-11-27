package platform

import (
	"fmt"
	"testing"
	"unicode/utf16"
)

// textDoc is a TextInputClient over one document, which records what a
// native text service asks of it. The document holds its marked text, as
// the interface's contract has it.
type textDoc struct {
	u16      []uint16
	sel      TextSelection
	mark     TextRange
	marked   bool
	replaced []string
	markeds  []string
	unmarked int
}

func newTextDoc(s string) *textDoc {
	return &textDoc{u16: utf16.Encode([]rune(s)), sel: TextSelection{Range: TextRange{End: UTF16Len(s)}}}
}

func (d *textDoc) TextForRange(r TextRange) (string, TextRange) {
	r = NormalizeTextRange(r)
	r.Start, r.End = min(r.Start, len(d.u16)), min(r.End, len(d.u16))
	return string(utf16.Decode(d.u16[r.Start:r.End])), r
}

func (d *textDoc) Selection() TextSelection       { return d.sel }
func (d *textDoc) MarkedRange() (TextRange, bool) { return d.mark, d.marked }
func (d *textDoc) UnmarkText()                    { d.marked = false; d.unmarked++ }
func (d *textDoc) BoundsForRange(TextRange) (RectF, TextRange, bool) {
	return RectF{}, TextRange{}, false
}
func (d *textDoc) IndexForPoint(float64, float64) (int, bool) { return 0, false }

func (d *textDoc) ReplaceText(r *TextRange, s string) {
	d.replaced = append(d.replaced, replaceString(r, s))
}

func (d *textDoc) SetMarkedText(r *TextRange, _ string, sel TextRange) {
	d.markeds = append(d.markeds, replaceString(r, "")+" sel "+fmt.Sprint(sel))
}

func replaceString(r *TextRange, s string) string {
	if r == nil {
		return "nil " + s
	}
	return fmt.Sprintf("[%d,%d) %s", r.Start, r.End, s)
}

func TestUTF16Offsets(t *testing.T) {
	// 𝕏 takes two units, the rest of "ab你好𝕏" one each.
	if n := UTF16Len("ab你好𝕏"); n != 6 {
		t.Errorf("UTF16Len = %d, want 6", n)
	}
	if n := UTF16Len(""); n != 0 {
		t.Errorf("UTF16Len of nothing = %d", n)
	}
	// How many runes the first units hold: 𝕏 takes two of them.
	for _, tc := range []struct {
		s     string
		units int
		want  int
	}{
		{"a𝕏b", 4, 3},
		{"a𝕏b", 3, 2}, // the pair takes one short of the third
		{"a𝕏b", 0, 0},
		{"abc", 90, 3},
	} {
		if got := RunesInUTF16(tc.s, tc.units); got != tc.want {
			t.Errorf("RunesInUTF16(%q, %d) = %d, want %d", tc.s, tc.units, got, tc.want)
		}
	}
	// An offset inside a surrogate pair rounds down to its rune; one past
	// the end is the end.
	for _, tc := range []struct {
		s      string
		offset int
		want   int
	}{
		{"abc", 1, 1},
		{"a𝕏b", 1, 1}, // at the pair's first unit: the rune
		{"a𝕏b", 2, 1}, // inside the pair: still the rune
		{"a𝕏b", 3, 5}, // the rune after the pair
		{"a𝕏b", 90, 6},
	} {
		if got := UTF16ByteOffset(tc.s, tc.offset); got != tc.want {
			t.Errorf("UTF16ByteOffset(%q, %d) = %d, want %d", tc.s, tc.offset, got, tc.want)
		}
	}
	// A range the wrong way round, or before the start, comes out whole
	// and inside the document.
	for _, tc := range []struct{ r, want TextRange }{
		{TextRange{Start: 5, End: 2}, TextRange{Start: 2, End: 5}},
		{TextRange{Start: -3, End: -1}, TextRange{}},
		{TextRange{Start: -3, End: 4}, TextRange{End: 4}},
	} {
		if got := NormalizeTextRange(tc.r); got != tc.want {
			t.Errorf("NormalizeTextRange(%v) = %v, want %v", tc.r, got, tc.want)
		}
	}
}

func TestTextSelectionCaret(t *testing.T) {
	if got := (TextSelection{Range: TextRange{Start: 2, End: 5}}).Caret(); got != 5 {
		t.Errorf("the caret of a selection made left to right = %d, want 5", got)
	}
	if got := (TextSelection{Range: TextRange{Start: 2, End: 5}, Reversed: true}).Caret(); got != 2 {
		t.Errorf("the caret of a selection made right to left = %d, want 2", got)
	}
}

// TestInputComposition checks the ordering a native input method expects:
// the end of a composition keeps where its marked text was for a
// replacement without a range, and one that gives up takes its marked text
// with it.
func TestInputComposition(t *testing.T) {
	d := newTextDoc("abcdef")
	d.mark, d.marked = TextRange{Start: 2, End: 5}, true
	c := &InputComposition{}

	c.End(d)
	if d.unmarked != 1 || c.pending == nil || *c.pending != (TextRange{Start: 2, End: 5}) {
		t.Fatalf("End: unmarked %d times, pending %v", d.unmarked, c.pending)
	}
	// The input method that unmarked before delivering commits where the
	// marked text was.
	c.Replace(d, nil, "hey")
	if len(d.replaced) != 1 || d.replaced[0] != "[2,5) hey" || c.pending != nil {
		t.Errorf("Replace after End = %q, pending %v", d.replaced, c.pending)
	}
	// An explicit range is the one the service asked for.
	c.Replace(d, &TextRange{Start: 0, End: 1}, "x")
	if len(d.replaced) != 2 || d.replaced[1] != "[0,1) x" {
		t.Errorf("Replace with a range = %q", d.replaced)
	}

	// A composition given up without emptying takes its marked text away;
	// the rest of the document stays.
	d.mark, d.marked = TextRange{Start: 1, End: 3}, true
	c.Cancel(d)
	if len(d.markeds) != 1 || d.markeds[0] != "nil  sel {0 0}" || d.unmarked != 2 {
		t.Errorf("Cancel = %q, unmarked %d times", d.markeds, d.unmarked)
	}
	// Nothing marked, nothing to cancel.
	c.Cancel(d)
	if len(d.markeds) != 1 || d.unmarked != 2 {
		t.Errorf("Cancel without marked text did something: %q, %d", d.markeds, d.unmarked)
	}

	// Reset forgets where a composition was: the replacement goes to the
	// client's own default.
	d.mark, d.marked = TextRange{Start: 1, End: 3}, true
	c.End(d)
	c.Reset()
	c.Replace(d, nil, "y")
	if len(d.replaced) != 3 || d.replaced[2] != "nil y" {
		t.Errorf("Replace after Reset = %q", d.replaced)
	}

	// A composition ended with nothing marked leaves nothing pending.
	e := &InputComposition{}
	e.End(newTextDoc("abc"))
	e.Replace(d, nil, "z")
	if len(d.replaced) != 4 || d.replaced[3] != "nil z" {
		t.Errorf("Replace after an unmarked End = %q", d.replaced)
	}
}

// TestClientTextContext checks the window of a client document an input
// method sees: whole when it is small, around the caret when the document
// is huge, and with the marked text taken out.
func TestClientTextContext(t *testing.T) {
	d := newTextDoc("hello world")
	d.sel = TextSelection{Range: TextRange{Start: 3, End: 7}}
	c := ClientTextContext(d)
	if c.Text != "hello world" || c.Base != 0 || c.Start != 3 || c.End != 7 || c.Caret != 7 {
		t.Errorf("context of a plain selection = %+v", c)
	}
	// A selection made right to left keeps its caret at its start.
	d.sel = TextSelection{Range: TextRange{Start: 5, End: 8}, Reversed: true}
	if c = ClientTextContext(d); c.Caret != 5 || c.Start != 5 || c.End != 8 {
		t.Errorf("context of a reversed selection = %+v", c)
	}

	// A selection may cover a huge document: the window is around the
	// caret, and its positions are the window's own.
	d = newTextDoc("0123456789abcdefghij0123456789abcdefghij")
	d.sel = TextSelection{Range: TextRange{Start: 28, End: 30}}
	c = clientTextContext(d, 10, 5)
	if c.Text != "0123456789abcde" || c.Base != 20 || c.Start != 8 || c.End != 10 || c.Caret != 10 {
		t.Errorf("context of a windowed document = %+v", c)
	}

	// Marked text is the composition in the document: the context takes
	// it out, and puts the caret where the composition began.
	d = newTextDoc("abcpredef")
	d.mark, d.marked = TextRange{Start: 3, End: 6}, true
	d.sel = TextSelection{Range: TextRange{Start: 6, End: 6}}
	c = ClientTextContext(d)
	if c.Text != "abcdef" || c.Base != 0 || c.Start != 3 || c.End != 3 || c.Caret != 3 {
		t.Errorf("context with marked text = %+v", c)
	}
}

// TestTextContextRange checks how the context's positions go back to the
// document's: a collapsed position at the composition stays there, and one
// past it skips the text the composition holds.
func TestTextContextRange(t *testing.T) {
	d := newTextDoc("abcpredef")
	d.mark, d.marked = TextRange{Start: 3, End: 6}, true
	d.sel = TextSelection{Range: TextRange{Start: 6, End: 6}}
	c := ClientTextContext(d)

	// At the composition: stays, so the composition goes on where it was.
	if got := c.Range(TextRange{Start: 3, End: 3}); got != (TextRange{Start: 3, End: 3}) {
		t.Errorf("a collapsed position at the composition = %v", got)
	}
	// After it: the marked text is skipped.
	if got := c.Range(TextRange{Start: 4, End: 4}); got != (TextRange{Start: 7, End: 7}) {
		t.Errorf("a position after the composition = %v", got)
	}
	// Across it: the composition is inside, so a replacement stays one
	// client operation.
	if got := c.Range(TextRange{Start: 2, End: 4}); got != (TextRange{Start: 2, End: 7}) {
		t.Errorf("a range across the composition = %v", got)
	}
	// Past the window's end, it is the window's end, in the document.
	if got := c.Range(TextRange{Start: 90, End: 90}); got != (TextRange{Start: 9, End: 9}) {
		t.Errorf("a position past the window = %v", got)
	}

	// Without a composition the positions are the document's own.
	d2 := newTextDoc("hello")
	d2.sel = TextSelection{Range: TextRange{Start: 1, End: 4}}
	c2 := ClientTextContext(d2)
	if got := c2.Range(TextRange{Start: 1, End: 4}); got != (TextRange{Start: 1, End: 4}) {
		t.Errorf("a range without marked text = %v", got)
	}
}

// TestTextContextDeleteRange checks the deletion an input method asks for
// in characters around the caret, which stays characters of the document
// however wide the window the answer needs was.
func TestTextContextDeleteRange(t *testing.T) {
	d := newTextDoc("0123456789")
	d.sel = TextSelection{Range: TextRange{Start: 5, End: 5}}
	// Two characters before the caret, to the caret: "34".
	if got := ClientDeleteRange(d, -2, 2); got != (TextRange{Start: 3, End: 5}) {
		t.Errorf("deleting two before the caret = %v", got)
	}
	// From the caret on: "567".
	if got := ClientDeleteRange(d, 0, 3); got != (TextRange{Start: 5, End: 8}) {
		t.Errorf("deleting three at the caret = %v", got)
	}
	// Fewer than are there, and more than are there.
	if got := ClientDeleteRange(d, -1, 1); got != (TextRange{Start: 4, End: 5}) {
		t.Errorf("deleting one before the caret = %v", got)
	}
	// More than the document holds, from its start to its end.
	if got := ClientDeleteRange(d, -5, 10); got != (TextRange{Start: 0, End: 10}) {
		t.Errorf("deleting more than the document holds = %v", got)
	}

	// Astral characters are characters, not units: 𝕏 is one.
	d = newTextDoc("a𝕏b𝕏c")
	d.sel = TextSelection{Range: TextRange{Start: 3, End: 3}} // after the first 𝕏
	if got := ClientDeleteRange(d, -1, 1); got != (TextRange{Start: 1, End: 3}) {
		t.Errorf("deleting an astral character = %v", got)
	}
}
