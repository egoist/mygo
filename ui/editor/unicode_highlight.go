package editor

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

//go:generate go run gen_confusables.go

// Characters the editor outlines, as VS Code's editor.unicodeHighlight:
// invisible ones show as a space in a box, so that the controls of
// bidirectional text reorder nothing (Trojan Source, CVE-2021-42574);
// spaces other than U+0020 are outlined where they are; and with
// State.AmbiguousCharacters, letters and digits that look like ASCII
// ones are outlined too, in words that hold ASCII letters, as in
// "pаypal" (with a Cyrillic а).

// unicodeMarks are which characters a view outlines.
type unicodeMarks struct {
	invisible, ambiguous bool
}

func (o options) unicodeMarks() unicodeMarks {
	return unicodeMarks{invisible: !o.noInvisibles, ambiguous: o.ambiguous}
}

// setUnicodeMarks lays the lines out anew when the characters outlined
// change, as invisible ones take room.
func (v *view) setUnicodeMarks(m unicodeMarks) {
	if v.unicodeMarks == m {
		return
	}
	v.unicodeMarks = m
	v.lines.m = nil
	v.measured.reset()
	v.stylesDirty = true
	if v.wrap {
		v.heights.wraps.reset(v.text.LineCount(), true)
		v.wrapAt = 0
	}
}

// showsAsIs reports whether a line's text s, at the text's start when
// first, shows as it is: without tabs, which expand, nor invisible
// characters the view shows.
func (v *view) showsAsIs(s string, first bool) bool {
	if !v.unicodeMarks.invisible {
		return strings.IndexByte(s, '\t') < 0
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !invisibleLead[c] {
			continue
		}
		// Kana and CJK punctuation, and fullwidth forms: of what starts
		// so, only U+3164 and U+FE00 to U+FFFC's few are invisible.
		if i+1 < len(s) && (c == 0xe3 && s[i+1] != 0x85 || c == 0xef && (s[i+1] < 0xb8 || s[i+1] == 0xbc || s[i+1] == 0xbd)) {
			continue
		}
		if c == '\t' {
			return false
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if invisible(r) && invisibleAt(s, i, first) > 0 {
			return false
		}
		i += size - 1
	}
	return true
}

// invisibleLead tells the bytes of UTF-8 that can start a character
// invisible or oddSpace reports, with tab and the 0xe3 of U+3164, so that
// a scan decodes no others, as most of CJK text's (TestScansMissNoCharacter).
var invisibleLead = [256]bool{
	0x00: true, 0x01: true, 0x02: true, 0x03: true, 0x04: true, 0x05: true, 0x06: true, 0x07: true,
	0x08: true, 0x09: true, 0x0a: true, 0x0b: true, 0x0c: true, 0x0d: true, 0x0e: true, 0x0f: true,
	0x10: true, 0x11: true, 0x12: true, 0x13: true, 0x14: true, 0x15: true, 0x16: true, 0x17: true,
	0x18: true, 0x19: true, 0x1a: true, 0x1b: true, 0x1c: true, 0x1d: true, 0x1e: true, 0x1f: true,
	0x7f: true, 0xc2: true, 0xcd: true, 0xd8: true, 0xe1: true, 0xe2: true, 0xe3: true, 0xef: true,
	0xf0: true, 0xf3: true,
}

// invisibleAt returns the size of the character at byte i of a line's
// text s, at the text's start when first, when it shows as invisible, or
// 0 (invisible).
func invisibleAt(s string, i int, first bool) int {
	r, size := utf8.DecodeRuneInString(s[i:])
	if !invisible(r) {
		return 0
	}
	prev, _ := utf8.DecodeLastRuneInString(s[:i])
	if i == 0 {
		prev = -1
	}
	switch {
	case r == 0xfeff:
		// A byte order mark opening the text marks it UTF-8.
		if first && i == 0 {
			return 0
		}
	case r == 0x200d:
		// A joiner in an emoji sequence, or in the letters of a script
		// it shapes, as in Devanagari.
		if isEmoji(prev) || prev >= 0x80 && (unicode.IsLetter(prev) || unicode.IsMark(prev)) {
			return 0
		}
	case r == 0x200c:
		// A non-joiner between the letters of a script it shapes, as in
		// Persian.
		if prev >= 0x80 && (unicode.IsLetter(prev) || unicode.IsMark(prev)) {
			return 0
		}
	case r >= 0x180b && r <= 0x180f:
		// Mongolian's variation selectors and vowel separator.
		if unicode.Is(unicode.Mongolian, prev) {
			return 0
		}
	case r >= 0xfe00 && r <= 0xfe0f, r >= 0xe0100 && r <= 0xe01ef:
		// One variation selector after a character it can vary, as an
		// emoji's, a CJK ideograph's or a keycap's digit; a run of them
		// carries hidden data.
		if prev >= 0x80 && !isVariationSelector(prev) || r == 0xfe0f && (prev >= '0' && prev <= '9' || prev == '#' || prev == '*') {
			return 0
		}
	case r >= 0xe0020 && r <= 0xe007f:
		if inFlag(s, i) {
			return 0
		}
	}
	return size
}

// invisible reports whether r can show as invisible: a control character
// but tab, a format character but a prepended concatenation mark (which
// shows), and what VS Code's invisibleCharacters lists besides, as
// blank letters and variation selectors. invisibleAt leaves those that
// shape or vary the characters around them.
func invisible(r rune) bool {
	switch {
	case r < 0x80:
		return r < 0x20 && r != '\t' || r == 0x7f
	case r < 0xa0:
		return true
	case r == 0xad, r == 0x34f, r == 0x115f, r == 0x1160, r == 0x17b4, r == 0x17b5,
		r == 0x2028, r == 0x2029, r == 0x2800, r == 0x3164, r == 0xffa0, r == 0xfffc:
		return true
	case r >= 0x180b && r <= 0x180f, r >= 0xfe00 && r <= 0xfe0f, r >= 0xe0100 && r <= 0xe01ef:
		return true
	case r < 0x600, r >= 0x900 && r < 0x180b, r >= 0x2070 && r < 0xfe00, r >= 0x1d17b && r < 0xe0000:
		// No format characters here: most text is told at once.
		return false
	}
	return unicode.Is(unicode.Cf, r) && !unicode.Is(unicode.Prepended_Concatenation_Mark, r)
}

// oddSpace reports whether r is a space other than U+0020, which shows
// as a space but is not one to a compiler: no-break and typographic
// spaces, as VS Code lists them. The ideographic space, common in CJK
// text, is left out, as Zed does.
func oddSpace(r rune) bool {
	return r == 0xa0 || r >= 0x2000 && r <= 0x200a || r == 0x202f || r == 0x205f
}

func isVariationSelector(r rune) bool {
	return r >= 0xfe00 && r <= 0xfe0f || r >= 0xe0100 && r <= 0xe01ef || r >= 0x180b && r <= 0x180d || r == 0x180f
}

// isEmoji approximates Unicode's Extended_Pictographic, which Go's
// unicode package lacks, with the emoji variation selector and skin
// tones that end many characters of sequences.
func isEmoji(r rune) bool {
	return r >= 0x1f000 && r <= 0x1faff || r >= 0x2600 && r <= 0x27bf || r >= 0x2300 && r <= 0x23ff ||
		r >= 0x2b00 && r <= 0x2bff || r == 0xfe0f || r == 0xa9 || r == 0xae
}

// inFlag reports whether the tag character at byte i of s is in a flag's
// tag sequence: a black flag, two to six tags of letters a-z or digits,
// and the cancel tag, as the flag of Scotland is.
func inFlag(s string, i int) bool {
	start := i
	for n := 0; start > 0 && n < 7; n++ {
		r, size := utf8.DecodeLastRuneInString(s[:start])
		if r < 0xe0020 || r > 0xe007f {
			break
		}
		start -= size
	}
	if r, _ := utf8.DecodeLastRuneInString(s[:start]); r != 0x1f3f4 {
		return false
	}
	n := 0
	for j := start; j < len(s); {
		r, size := utf8.DecodeRuneInString(s[j:])
		switch {
		case r == 0xe007f:
			return n >= 2 && n <= 6 && j >= i
		case r >= 0xe0061 && r <= 0xe007a, r >= 0xe0030 && r <= 0xe0039:
			n++
		default:
			return false
		}
		j += size
	}
	return false
}

// outlinedIn appends the offsets in s of the characters outlined where
// they are: spaces other than U+0020 with m.invisible, and with
// m.ambiguous the letters and digits that look like an ASCII letter or
// digit, in words that hold an ASCII letter.
func outlinedIn(s string, m unicodeMarks, dst []int) []int {
	if !m.ambiguous {
		for i := 0; i < len(s); i++ {
			if c := s[i]; c == 0xc2 || c == 0xe2 {
				if r, _ := utf8.DecodeRuneInString(s[i:]); oddSpace(r) {
					dst = append(dst, i)
				}
			}
		}
		return dst
	}
	word, ascii := len(dst), false
	endWord := func() {
		if !ascii {
			dst = dst[:word]
		}
		word, ascii = len(dst), false
	}
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r < utf8.RuneSelf && ('a' <= r && r <= 'z' || 'A' <= r && r <= 'Z'):
			ascii = true
		case r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r):
			if _, ok := lookalike(r); ok {
				dst = append(dst, i)
			}
		default:
			endWord()
			if m.invisible && oddSpace(r) {
				dst = append(dst, i)
				word = len(dst)
			}
		}
		i += size
	}
	endWord()
	return dst
}

// lookalike returns the ASCII letter or digit r looks like.
func lookalike(r rune) (byte, bool) {
	if r < utf8.RuneSelf {
		return 0, false
	}
	i, _ := slices.BinarySearch(confusables[:], uint32(r)<<8)
	if i < len(confusables) && rune(confusables[i]>>8) == r {
		return byte(confusables[i]), true
	}
	return 0, false
}

// paintUnicodeMarks outlines in c the characters of a row the view marks,
// with rects as scratch, which it returns.
func (v *view) paintUnicodeMarks(p *ui.Painter, rects []ui.Rect, l *lineLayout, row *rowLayout, ox, y float32, c ui.Color) []ui.Rect {
	stroke := func(a, b int) {
		rects = v.rangeRects(l, row, l.toDisp(a, false), l.toDisp(b, false), rects[:0])
		for _, rc := range rects {
			p.Stroke(ui.Rect{X: ox + rc.X, Y: y + 1, W: rc.W, H: l.rowHeight - 2}, c, 2, 1)
		}
	}
	for _, s := range l.segs {
		if s.kind == segInvisible {
			stroke(s.from, s.to)
		}
	}
	for _, at := range l.outlined {
		if !l.replaced(at) {
			_, size := utf8.DecodeRuneInString(l.text[at:])
			stroke(at, at+size)
		}
	}
	return rects
}

// unicodeMarkAt returns the line, and the bytes in it, of the character
// outlined at off, and whether it shows as invisible. With byPointer, the
// pointer is to be over its box, rather than off at either of its edges.
func (ed *editor) unicodeMarkAt(off int, byPointer bool) (l *lineLayout, from, to int, hidden, ok bool) {
	v := &ed.view
	if v.unicodeMarks == (unicodeMarks{}) || off < 0 || off > v.text.Len() {
		return nil, 0, 0, false, false
	}
	l = v.line(v.text.LineOf(off))
	col := off - l.start
	over := func(a, b int) bool {
		if col < a || col > b {
			return false
		}
		if !byPointer {
			return true
		}
		ra, ok1 := ed.caretRect(l.start + a)
		rb, ok2 := ed.caretRect(l.start + b)
		x, y := ed.toContent(ed.pointer.X, ed.pointer.Y)
		return ok1 && ok2 && x >= ra.X && x < rb.X && y >= float64(ra.Y) && y < float64(ra.Y+ra.H)
	}
	for _, s := range l.segs {
		if s.kind == segInvisible && over(s.from, s.to) {
			return l, s.from, s.to, true, true
		}
	}
	for _, at := range l.outlined {
		if _, size := utf8.DecodeRuneInString(l.text[at:]); !l.replaced(at) && over(at, at+size) {
			return l, at, at + size, false, true
		}
	}
	return nil, 0, 0, false, false
}

// unicodeNote returns what the hover says of the character outlined at
// off, as a diagnostic of it.
func (ed *editor) unicodeNote(off int, byPointer bool) (Diagnostic, bool) {
	l, from, to, hidden, ok := ed.unicodeMarkAt(off, byPointer)
	if !ok {
		return Diagnostic{}, false
	}
	r, _ := utf8.DecodeRuneInString(l.text[from:])
	code := fmt.Sprintf("U+%04X", r)
	d := Diagnostic{Start: l.start + from, End: l.start + to, Severity: SeverityWarning, Source: "editor"}
	a, ambiguous := lookalike(r)
	switch {
	case !hidden && ambiguous:
		d.Message = ed.texts.AmbiguousCharacter(code, string(r), string(rune(a)))
	case unicode.Is(unicode.Bidi_Control, r):
		d.Message = ed.texts.BidiCharacter(code)
	default:
		d.Message = ed.texts.InvisibleCharacter(code)
	}
	return d, true
}

// marksUnicode reports whether the pointer is over a character outlined
// at off.
func (ed *editor) marksUnicode(off int) bool {
	_, _, _, _, ok := ed.unicodeMarkAt(off, true)
	return ok
}
