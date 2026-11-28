package editor

import (
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/internal/text"
	"github.com/egoist/mygo/ui"
)

// Invisible characters show as a space in a box, as VS Code's
// editor.unicodeHighlight.invisibleCharacters does, so that the controls
// of bidirectional text reorder nothing (Trojan Source, CVE-2021-42574).
// Spaces other than U+0020 are outlined where they are.

// setInvisibles turns the boxes of invisible characters on or off, laying
// the lines out anew, as the boxes take room.
func (v *view) setInvisibles(on bool) {
	if v.invisibles == on {
		return
	}
	v.invisibles = on
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
// characters the view boxes.
func (v *view) showsAsIs(s string, first bool) bool {
	if !v.invisibles {
		return strings.IndexByte(s, '\t') < 0
	}
	for i := 0; i < len(s); {
		switch c := s[i]; {
		case c >= 0xe0 && c < 0xf0 && i+2 < len(s) && s[i+1]&0xc0 == 0x80 && s[i+2]&0xc0 == 0x80:
			// A character of three bytes, as most of CJK text's, passed
			// over whole: its continuation bytes start none.
			if mayBeInvisibleAt(s, i) && invisibleAt(s, i, first) > 0 {
				return false
			}
			i += 3
		case c >= 0x20 && c < 0x7f:
			i++
			for i+8 <= len(s) && printable8(s[i:]) {
				i += 8
			}
		case c < 0x80:
			// Tab, the other controls and DEL.
			return false
		case c >= 0xc0 && i+1 < len(s) && mayBeInvisibleAt(s, i) && invisibleAt(s, i, first) > 0:
			return false
		default:
			i++
		}
	}
	return true
}

// printable8 reports whether the first eight bytes of s are printable
// ASCII.
func printable8(s string) bool {
	const ones, highs = 0x0101010101010101, 0x8080808080808080
	w := uint64(s[0]) | uint64(s[1])<<8 | uint64(s[2])<<16 | uint64(s[3])<<24 |
		uint64(s[4])<<32 | uint64(s[5])<<40 | uint64(s[6])<<48 | uint64(s[7])<<56
	return (w-0x20*ones)&^w&highs == 0 && (w+ones|w)&highs == 0
}

// mayBeInvisibleAt reports whether the character past ASCII at byte i of
// s, with a byte after it, can be one invisible reports, by its first
// bytes, so that a scan decodes few others (TestScansMissNoCharacter).
func mayBeInvisibleAt(s string, i int) bool {
	c, c1 := s[i], s[i+1]
	if mayBeInvisible[c&0x3f]>>(c1&0x3f)&1 == 0 {
		return false
	}
	if c == 0xe2 && c1 == 0x80 && i+2 < len(s) {
		// Of U+2000 to U+203F, punctuation as quotes but for U+200B to
		// U+200F and U+2028 to U+202E.
		c2 := s[i+2]
		return c2 >= 0x8b && c2 <= 0x8f || c2 >= 0xa8 && c2 <= 0xae
	}
	return true
}

// mayBeInvisible tells, by the lead byte of a character of UTF-8 past
// ASCII, which of the 64 values of its next byte's low bits can start
// one invisible reports: for a lead of three or four bytes, the blocks
// of 64 or 4096 code points from those named.
var mayBeInvisible = [64]uint64{
	0xc2 & 0x3f: 0x2000ffffffff,                        // U+0080 to U+009F, U+00AD
	0xcd & 0x3f: 1 << 0xf,                              // U+034F
	0xd8 & 0x3f: 1 << 0x1c,                             // U+061C
	0xe1 & 0x3f: 1<<5 | 1<<30 | 1<<32,                  // U+1140, U+1780 and U+1800 on
	0xe2 & 0x3f: 1<<0 | 1<<1 | 1<<32,                   // U+2000, U+2040 and U+2800 on
	0xe3 & 0x3f: 1 << 5,                                // U+3140 on
	0xef & 0x3f: 1<<0x38 | 1<<0x3b | 1<<0x3e | 1<<0x3f, // U+FE00, U+FEC0, U+FF80 and U+FFC0 on
	0xf0 & 0x3f: 1<<0x13 | 1<<0x1b | 1<<0x1d,           // U+13000, U+1B000 and U+1D000 on
	0xf3 & 0x3f: 1 << 0x20,                             // U+E0000 on
}

// invisibleAt returns the size of the character at byte i of a line's
// text s, at the text's start when first, when it shows as invisible, or
// 0.
func invisibleAt(s string, i int, first bool) int {
	r, size := utf8.DecodeRuneInString(s[i:])
	if !invisible(r) {
		return 0
	}
	prev, next := rune(-1), rune(-1)
	if i > 0 {
		prev, _ = utf8.DecodeLastRuneInString(s[:i])
	}
	if i+size < len(s) {
		next, _ = utf8.DecodeRuneInString(s[i+size:])
	}
	switch {
	case r == 0xfeff:
		// A byte order mark opening the text marks it UTF-8.
		if first && i == 0 {
			return 0
		}
	case r == 0x200d:
		// After a virama, as RFC 5892's CONTEXTJ rule (A.2) allows it,
		// as in Devanagari's conjuncts; between a letter and a virama of
		// its own block, as in Bengali's ya-phala (The Unicode Standard,
		// 12.2); or between the emoji of an emoji ZWJ sequence (UTS #51).
		if isVirama(prev) || isVirama(next) && unicode.IsLetter(prev) && prev>>7 == next>>7 ||
			isEmoji(prev) && isEmoji(next) {
			return 0
		}
	case r == 0x200c:
		// After a virama, or between letters that join, as in Persian:
		// RFC 5892's CONTEXTJ rule (A.1).
		if isVirama(prev) || joinsAround(s, i, i+size) {
			return 0
		}
	case r >= 0x180b && r <= 0x180f:
		// Mongolian's variation selectors and vowel separator.
		if unicode.Is(unicode.Mongolian, prev) {
			return 0
		}
	case r == 0xfe0e || r == 0xfe0f:
		// The text or emoji form of a symbol past ASCII, which
		// emoji-variation-sequences.txt's are, or a keycap's digit, #
		// or * before U+20E3 (emoji-sequences.txt).
		if prev >= 0x80 && (unicode.IsSymbol(prev) || unicode.IsPunct(prev)) ||
			next == 0x20e3 && (prev >= '0' && prev <= '9' || prev == '#' || prev == '*') {
			return 0
		}
	case r >= 0xfe00 && r <= 0xfe0d:
		// A variant of a Han ideograph or of a symbol past ASCII, as
		// StandardizedVariants.txt's.
		if unicode.Is(unicode.Han, prev) || prev >= 0x80 && unicode.IsSymbol(prev) {
			return 0
		}
	case r >= 0xe0100 && r <= 0xe01ef:
		// A variant of a Han ideograph in the Ideographic Variation
		// Database.
		if unicode.Is(unicode.Han, prev) {
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
	case r < 0x600, r >= 0x900 && r < 0x180b, r >= 0x2010 && r < 0x202a, r > 0x202e && r < 0x2060,
		r >= 0x2070 && r < 0xfe00, r >= 0x1d17b && r < 0xe0000:
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

// oddSpacesIn appends the offsets in s of the spaces oddSpace reports, in
// order.
func oddSpacesIn(s string, dst []int) []int {
	n := len(dst)
	for _, lead := range []byte{0xc2, 0xe2} {
		for i := 0; ; i++ {
			j := strings.IndexByte(s[i:], lead)
			if j < 0 {
				break
			}
			i += j
			if r, _ := utf8.DecodeRuneInString(s[i:]); oddSpace(r) {
				dst = append(dst, i)
			}
		}
	}
	slices.Sort(dst[n:])
	return dst
}

// shownApart returns text shown in a line that is not of it, as an
// inlay's: without the controls of bidirectional text while the view
// boxes invisible characters, as they would reorder the line's own text.
func (v *view) shownApart(s string) string {
	if !v.invisibles || strings.IndexByte(s, 0xe2) < 0 && strings.IndexByte(s, 0xd8) < 0 {
		return s
	}
	return strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Bidi_Control, r) {
			return -1
		}
		return r
	}, s)
}

// isEmoji approximates Unicode's Extended_Pictographic, which Go's
// unicode package lacks, with the emoji variation selector and skin
// tones that end many characters of sequences.
func isEmoji(r rune) bool {
	return r >= 0x1f000 && r <= 0x1faff || r >= 0x2600 && r <= 0x27bf || r >= 0x2300 && r <= 0x23ff ||
		r >= 0x2b00 && r <= 0x2bff || r == 0xfe0f || r == 0xa9 || r == 0xae
}

// isVirama reports whether r is a virama: of canonical combining class
// 9, which RFC 5892 lets a joiner or a non-joiner follow (TestViramas).
func isVirama(r rune) bool {
	_, ok := slices.BinarySearch(viramas[:], r)
	return ok
}

var viramas = [...]rune{
	0x094d, 0x09cd, 0x0a4d, 0x0acd, 0x0b4d, 0x0bcd, 0x0c4d, 0x0ccd, 0x0d3b, 0x0d3c, 0x0d4d, 0x0dca,
	0x0e3a, 0x0eba, 0x0f84, 0x1039, 0x103a, 0x1714, 0x1715, 0x1734, 0x17d2, 0x1a60, 0x1b44, 0x1baa,
	0x1bab, 0x1bf2, 0x1bf3, 0x2d7f, 0xa806, 0xa82c, 0xa8c4, 0xa953, 0xa9c0, 0xaaf6, 0xabed, 0x10a3f,
	0x11046, 0x11070, 0x1107f, 0x110b9, 0x11133, 0x11134, 0x111c0, 0x11235, 0x112ea, 0x1134d, 0x113ce, 0x113cf,
	0x113d0, 0x11442, 0x114c2, 0x115bf, 0x1163f, 0x116b6, 0x1172b, 0x11839, 0x1193d, 0x1193e, 0x119e0, 0x11a34,
	0x11a47, 0x11a99, 0x11c3f, 0x11d44, 0x11d45, 0x11d97, 0x11f41, 0x11f42, 0x1612f,
}

// joinsAround reports whether the non-joiner at bytes i to j of s is
// between letters that join, past the marks around it: RFC 5892's
// joining types, which Go's unicode package lacks, told by the scripts
// whose letters join.
func joinsAround(s string, i, j int) bool {
	before, after := rune(-1), rune(-1)
	for i > 0 {
		r, size := utf8.DecodeLastRuneInString(s[:i])
		if !unicode.IsMark(r) {
			before = r
			break
		}
		i -= size
	}
	for j < len(s) {
		r, size := utf8.DecodeRuneInString(s[j:])
		if !unicode.IsMark(r) {
			after = r
			break
		}
		j += size
	}
	return joins(before) && joins(after)
}

func joins(r rune) bool {
	return unicode.IsLetter(r) && unicode.In(r, unicode.Arabic, unicode.Syriac, unicode.Nko, unicode.Mandaic,
		unicode.Mongolian, unicode.Manichaean, unicode.Psalter_Pahlavi, unicode.Phags_Pa, unicode.Adlam,
		unicode.Hanifi_Rohingya, unicode.Sogdian, unicode.Old_Uyghur)
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

// paintInvisibles outlines in c the boxes of the invisible characters
// and the odd spaces of row, in the pieces of it in view, going through
// each segment and space there once.
func (v *view) paintInvisibles(p *ui.Painter, l *lineLayout, row *rowLayout, ox, y float32, c ui.Color) {
	if l.segs == nil && len(l.spaces) == 0 {
		return
	}
	clip := p.Visible()
	for pi := range row.pieces {
		pc := &row.pieces[pi]
		px := ox + row.indent + pc.x
		if px+pc.width < clip.X-64 || px > clip.X+clip.W+64 {
			continue
		}
		var lay *text.Layout
		at, runes := pc.from, 0
		v.marksIn(l, pc.from, pc.to, func(a, b int) {
			if lay == nil {
				lay = v.piece(l, pc)
			}
			ra, rb := a-pc.from, b-pc.from
			if !pc.ascii {
				runes += utf8.RuneCountInString(l.disp[at:a])
				at = a
				ra, rb = runes, runes+utf8.RuneCountInString(l.disp[a:b])
			}
			xa, _, _ := lay.Caret(ra)
			xb, _, _ := lay.Caret(rb)
			if xb < xa {
				xa, xb = xb, xa
			}
			p.Stroke(ui.Rect{X: px + xa, Y: y + 1, W: xb - xa, H: l.rowHeight - 2}, c, 2, 1)
		})
	}
}

// marksIn calls fn with the displayed bytes, from a to b, of each box and
// odd space of l between displayed bytes from and to, in order.
func (v *view) marksIn(l *lineLayout, from, to int, fn func(a, b int)) {
	spaces := func(textFrom, textTo, disp int) {
		k, _ := slices.BinarySearch(l.spaces, textFrom)
		for ; k < len(l.spaces) && l.spaces[k] < textTo; k++ {
			v.markSteps++
			at := l.spaces[k]
			_, size := utf8.DecodeRuneInString(l.text[at:])
			fn(disp+at-textFrom, disp+at-textFrom+size)
		}
	}
	if l.segs == nil {
		spaces(from, min(to, len(l.text)), from)
		return
	}
	i := max(sort.Search(len(l.segs), func(i int) bool { return l.segs[i].disp > from })-1, 0)
	for ; i < len(l.segs) && l.segs[i].disp < to; i++ {
		v.markSteps++
		switch s := l.segs[i]; s.kind {
		case segInvisible:
			if s.disp >= from {
				fn(s.disp, s.disp+1)
			}
		case segText:
			a, b := max(s.from, s.from+from-s.disp), min(s.to, s.from+to-s.disp)
			if a < b {
				spaces(a, b, s.disp+a-s.from)
			}
		}
	}
}
