package ui

import (
	"math/bits"
	"slices"
	"sort"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/internal/text"
)

// buffer is the text of a text input: the string the app's value holds,
// which it shares, and the paragraphs of the text, the runes between
// newlines. Edits make a new string, a copy of the bytes, and move the
// paragraphs after them: no edit converts the whole text, which takes
// milliseconds for a few megabytes.
type buffer struct {
	s     string
	n     int // runes
	u     int // UTF-16 units
	paras []paragraph
	// version counts the edits.
	version uint64
}

// paragraph is a paragraph of a buffer: where it starts, in runes and in
// bytes, and what a text area made of it (textarea.go): its layout, at
// the width and in the style of the text area's last layout, with the
// composition of an input method it showed, and its height, 0 until it was
// laid out.
type paragraph struct {
	rune, byte int
	unit       int // UTF-16 offset, for native text range queries
	layout     *text.Layout
	compose    string
	h          float32
}

func (b *buffer) set(s string) {
	b.s = s
	clear(b.paras) // the layouts of the old text
	b.paras = append(b.paras[:0], paragraph{})
	r := 0
	u := 0
	for i, c := range s {
		r++
		u++
		if c > 0xffff {
			u++
		}
		if c == '\n' {
			b.paras = append(b.paras, paragraph{rune: r, byte: i + 1, unit: u})
		}
	}
	b.n = r
	b.u = u
	b.version++
}

// para returns the paragraph holding rune i; the newline ending a
// paragraph is its.
func (b *buffer) para(i int) int {
	return sort.Search(len(b.paras), func(k int) bool { return b.paras[k].rune > i }) - 1
}

// next returns where the paragraph after p starts, after its newline, or
// the end of the text.
func (b *buffer) next(p int) (rune, byte int) {
	if p+1 < len(b.paras) {
		return b.paras[p+1].rune, b.paras[p+1].byte
	}
	return b.n, len(b.s)
}

// end returns the rune ending paragraph p, its newline or the end of the
// text.
func (b *buffer) end(p int) int {
	if p+1 < len(b.paras) {
		return b.paras[p+1].rune - 1
	}
	return b.n
}

// byteOf returns the byte of the text rune i starts at.
func (b *buffer) byteOf(i int) int {
	i = max(0, min(i, b.n))
	if i == b.n {
		return len(b.s)
	}
	p := b.para(i)
	pr := &b.paras[p]
	nr, nb := b.next(p)
	if nb-pr.byte == nr-pr.rune {
		return pr.byte + i - pr.rune // ASCII
	}
	off := pr.byte
	for k := pr.rune; k < i; k++ {
		_, size := utf8.DecodeRuneInString(b.s[off:])
		off += size
	}
	return off
}

// slice returns the runes from a to z.
func (b *buffer) slice(a, z int) string { return b.s[b.byteOf(a):b.byteOf(z)] }

// unitOf converts a rune offset to UTF-16, scanning only its paragraph.
func (b *buffer) unitOf(i int) int {
	i = max(0, min(i, b.n))
	if i == b.n {
		return b.u
	}
	p := b.paras[b.para(i)]
	u := p.unit
	for _, r := range b.s[p.byte:b.byteOf(i)] {
		u++
		if r > 0xffff {
			u++
		}
	}
	return u
}

// runeOf converts a byte or UTF-16 offset, rounding an interior byte or
// surrogate to the start of its rune. Native ranges cannot split a rune.
func (b *buffer) runeOf(i int, utf16 bool) int {
	limit := len(b.s)
	if utf16 {
		limit = b.u
	}
	i = max(0, min(i, limit))
	if i == limit {
		return b.n
	}
	p := sort.Search(len(b.paras), func(p int) bool {
		if utf16 {
			return b.paras[p].unit > i
		}
		return b.paras[p].byte > i
	}) - 1
	pr := b.paras[max(p, 0)]
	off := pr.byte
	if utf16 {
		off = pr.unit
	}
	rn := pr.rune
	for _, r := range b.s[pr.byte:] {
		size := utf8.RuneLen(r)
		if size < 0 {
			size = 1
		}
		if utf16 {
			size = 1
			if r > 0xffff {
				size++
			}
		}
		if off+size > i {
			break
		}
		off += size
		rn++
		if off == i {
			break
		}
	}
	return rn
}

// runeOffset returns the byte of s rune i starts at.
func runeOffset(s string, i int) int {
	for at := range s {
		if i == 0 {
			return at
		}
		i--
	}
	return len(s)
}

// text returns paragraph p, without its newline.
func (b *buffer) text(p int) string {
	_, nb := b.next(p)
	if p+1 < len(b.paras) {
		nb-- // the newline
	}
	return b.s[b.paras[p].byte:nb]
}

// replace replaces the runes from a to z with s. The paragraphs from the
// one holding a to the one holding z give way to those of the new text,
// which have no layout yet: it returns the first, those there were, and
// how many there are.
func (b *buffer) replace(a, z int, s string) (first int, old []paragraph, after int) {
	ba, bz := b.byteOf(a), b.byteOf(z)
	pa, pz := b.para(a), b.para(z)
	old = slices.Clone(b.paras[pa : pz+1])
	runes := utf8.RuneCountInString(s)
	ua, uz := b.unitOf(a), b.unitOf(z)
	var added []paragraph
	r := a
	u := ua
	for i, c := range s {
		r++
		u++
		if c > 0xffff {
			u++
		}
		if c == '\n' {
			added = append(added, paragraph{rune: r, byte: ba + i + 1, unit: u})
		}
	}
	dr, db := runes-(z-a), len(s)-(bz-ba)
	du := u - uz
	b.s = b.s[:ba] + s + b.s[bz:]
	b.n += dr
	b.u += du
	b.paras = slices.Replace(b.paras, pa+1, pz+1, added...)
	b.paras[pa] = paragraph{rune: b.paras[pa].rune, byte: b.paras[pa].byte, unit: b.paras[pa].unit}
	for k := pa + 1 + len(added); k < len(b.paras); k++ {
		b.paras[k].rune += dr
		b.paras[k].byte += db
		b.paras[k].unit += du
	}
	b.version++
	return pa, old, len(added) + 1
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }

// nextWord returns the end of the word after i, skipping what is between,
// as text.Boundaries does.
func (b *buffer) nextWord(i int) int {
	off := b.byteOf(i)
	word := false
	for i < b.n {
		r, size := utf8.DecodeRuneInString(b.s[off:])
		if isWordRune(r) {
			word = true
		} else if word {
			break
		}
		i, off = i+1, off+size
	}
	return i
}

// prevWord returns the start of the word before i.
func (b *buffer) prevWord(i int) int {
	off := b.byteOf(i)
	word := false
	for i > 0 {
		r, size := utf8.DecodeLastRuneInString(b.s[:off])
		if isWordRune(r) {
			word = true
		} else if word {
			break
		}
		i, off = i-1, off-size
	}
	return i
}

// wordAt returns the word, or the run of other runes, around i, within
// its line.
func (b *buffer) wordAt(i int) (start, end int) {
	if b.n == 0 {
		return 0, 0
	}
	i = min(i, b.n-1)
	off := b.byteOf(i)
	r, _ := utf8.DecodeRuneInString(b.s[off:])
	word := isWordRune(r)
	start, end = i, i
	for o := off; start > 0; {
		r, size := utf8.DecodeLastRuneInString(b.s[:o])
		if isWordRune(r) != word || r == '\n' {
			break
		}
		start, o = start-1, o-size
	}
	for o := off; end < b.n; {
		r, size := utf8.DecodeRuneInString(b.s[o:])
		if isWordRune(r) != word || r == '\n' {
			break
		}
		end, o = end+1, o+size
	}
	return start, end
}

// graphemes finds the grapheme boundaries of a buffer a paragraph at a
// time: they never cross a newline, but for the carriage return before
// one, which the paragraph holds with its newline.
type graphemes struct {
	b       text.Boundaries
	para    int
	version uint64
	valid   bool
}

// of prepares g for paragraph p of b, with its newline, and returns where
// the paragraph starts.
func (g *graphemes) of(b *buffer, p int) int {
	if !g.valid || g.para != p || g.version != b.version {
		_, nb := b.next(p)
		g.b.Reset([]rune(b.s[b.paras[p].byte:nb]))
		g.para, g.version, g.valid = p, b.version, true
	}
	return b.paras[p].rune
}

// next returns the end of the grapheme starting at or containing i.
func (g *graphemes) next(b *buffer, i int) int {
	if i >= b.n {
		return b.n
	}
	start := g.of(b, b.para(i))
	return start + g.b.NextGrapheme(i-start)
}

// prev returns the start of the grapheme before i.
func (g *graphemes) prev(b *buffer, i int) int {
	if i <= 0 {
		return 0
	}
	p := b.para(i)
	if b.paras[p].rune == i {
		p-- // the newline ending the paragraph before
	}
	start := g.of(b, p)
	return start + g.b.PrevGrapheme(i-start)
}

// heights sums the heights of the paragraphs of a text area, measured as
// they are laid out and estimated for the others, in two Fenwick trees:
// the heights measured, and how many are not, which an estimate of their
// height multiplies. The top of a paragraph, and the paragraph at a
// height, take O(log n) for any estimate.
type heights struct {
	measured []float64 // 1-based
	unknown  []int32
	est      float64
}

// reset sums the heights of paras.
func (h *heights) reset(paras []paragraph) {
	n := len(paras)
	h.measured = slices.Grow(h.measured[:0], n+1)[:n+1]
	h.unknown = slices.Grow(h.unknown[:0], n+1)[:n+1]
	h.measured[0], h.unknown[0] = 0, 0
	for i, p := range paras {
		if p.h > 0 {
			h.measured[i+1], h.unknown[i+1] = float64(p.h), 0
		} else {
			h.measured[i+1], h.unknown[i+1] = 0, 1
		}
	}
	for i := 1; i <= n; i++ {
		if j := i + i&-i; j <= n {
			h.measured[j] += h.measured[i]
			h.unknown[j] += h.unknown[i]
		}
	}
}

// add changes the height measured of paragraph p by dm, and the count of
// those not measured by du.
func (h *heights) add(p int, dm float64, du int32) {
	for i := p + 1; i < len(h.measured); i += i & -i {
		h.measured[i] += dm
		h.unknown[i] += du
	}
}

// top returns the top of paragraph p: the height of those before it.
func (h *heights) top(p int) float64 {
	var m float64
	var u int32
	for i := min(p, len(h.measured)-1); i > 0; i -= i & -i {
		m += h.measured[i]
		u += h.unknown[i]
	}
	return m + float64(u)*h.est
}

// at returns the paragraph at y, the last for y past the end.
func (h *heights) at(y float64) int {
	n := len(h.measured) - 1
	if n <= 0 {
		return 0
	}
	pos := 0
	var sum float64
	for step := 1 << (bits.Len(uint(n)) - 1); step > 0; step >>= 1 {
		if next := pos + step; next <= n {
			if s := sum + h.measured[next] + float64(h.unknown[next])*h.est; s <= y {
				pos, sum = next, s
			}
		}
	}
	return min(pos, n-1)
}

// totals returns the sum of the heights measured and how many are not.
func (h *heights) totals() (float64, int) {
	var m float64
	var u int32
	for i := len(h.measured) - 1; i > 0; i -= i & -i {
		m += h.measured[i]
		u += h.unknown[i]
	}
	return m, int(u)
}
