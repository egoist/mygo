package ui

import (
	"math"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/internal/text"
)

// TextRange is a half-open range of Unicode rune offsets. Rich documents
// clamp ranges to their text and expand nonempty ranges to whole graphemes.
type TextRange struct{ Start, End int }

// TextSelection keeps the direction of a selection: Anchor stays put while
// Caret moves. Both are rune offsets, snapped to grapheme boundaries.
type TextSelection struct{ Anchor, Caret int }

// Range returns the selection in document order.
func (s TextSelection) Range() TextRange {
	return TextRange{min(s.Anchor, s.Caret), max(s.Anchor, s.Caret)}
}

// RichStyle styles an editable run. Zero font, size, weight and colors inherit
// the editor's style; the boolean decorations are exact, rather than additive.
// Link is an http(s) or mailto URL. It is underlined and uses the accent color
// unless Color is set. RichTextEditor opens links on Cmd/Ctrl-click only.
type RichStyle struct {
	Font          string
	Size          float32 // DIPs
	Weight        int     // 100–900; zero is normal
	Italic        bool
	Underline     bool
	Strikethrough bool
	Color         Color
	Background    Color
	Link          string
}

// StyledRange is a run of a RichDocument. Runs cover the document without
// gaps or overlaps, and adjacent runs with equal styles are merged.
type StyledRange struct {
	Range TextRange
	Style RichStyle
}

// ParagraphStyle applies to an entire paragraph (text between newlines).
// Alignment is logical Start, Center or End. Direction follows the paragraph's
// text through the system's bidirectional shaper. LineHeight is a font-size
// multiplier; zero takes the font's natural spacing.
type ParagraphStyle struct {
	Alignment  Align
	LineHeight float32
}

// RichDocument is an immutable styled document. Its zero value is empty.
// Editing methods return a new value; old values, including undo snapshots,
// remain valid and may be read from any goroutine. Runs and Paragraphs return
// copies. Positions count runes, never UTF-8 bytes or UTF-16 code units.
type RichDocument struct {
	text       string
	runs       []StyledRange
	paragraphs []ParagraphStyle
	boundaries []int
	n          int
}

// NewRichDocument makes a plain document, normalizing CRLF and CR to LF and
// replacing NUL and invalid UTF-8. Styles can be added with WithStyle and WithParagraphStyle.
func NewRichDocument(s string) RichDocument {
	s = cleanRichText(s)
	d := RichDocument{text: s, n: utf8.RuneCountInString(s)}
	if d.n > 0 {
		d.runs = []StyledRange{{Range: TextRange{0, d.n}}}
	}
	d.paragraphs = make([]ParagraphStyle, strings.Count(s, "\n")+1)
	var boundaries text.Boundaries
	boundaries.Reset([]rune(s))
	d.boundaries = boundaries.GraphemeOffsets()
	return d
}

func cleanRichText(s string) string {
	s = strings.ToValidUTF8(s, "�")
	s = strings.ReplaceAll(s, "\x00", "�")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// String returns the plain text of the document.
func (d RichDocument) String() string { return d.text }

// Len returns the number of runes in the document.
func (d RichDocument) Len() int { return d.n }

// Runs returns a copy of the document's normalized styled ranges.
func (d RichDocument) Runs() []StyledRange { return slices.Clone(d.runs) }

// Paragraphs returns a copy of the styles, one for each paragraph, including
// the empty paragraph after a final newline. An empty document has one.
func (d RichDocument) Paragraphs() []ParagraphStyle {
	if len(d.paragraphs) == 0 {
		return []ParagraphStyle{{}}
	}
	return slices.Clone(d.paragraphs)
}

func (d RichDocument) paragraph(p int) ParagraphStyle {
	if p >= 0 && p < len(d.paragraphs) {
		return d.paragraphs[p]
	}
	return ParagraphStyle{}
}

// NormalizeRange clamps and orders a range, expanding it to whole graphemes.
// A collapsed range snaps to the preceding boundary and stays collapsed.
func (d RichDocument) NormalizeRange(r TextRange) TextRange {
	r.Start, r.End = min(r.Start, r.End), max(r.Start, r.End)
	r.Start, r.End = max(0, min(r.Start, d.n)), max(0, min(r.End, d.n))
	if r.Start == r.End {
		r.Start = d.snap(r.Start, false)
		r.End = r.Start
	} else {
		r.Start, r.End = d.snap(r.Start, false), d.snap(r.End, true)
	}
	return r
}

func (d RichDocument) snap(i int, forward bool) int {
	i = max(0, min(i, d.n))
	if i == 0 || i == d.n {
		return i
	}
	index, found := slices.BinarySearch(d.boundaries, i)
	if found {
		return i
	}
	if forward {
		return d.boundaries[index]
	}
	return d.boundaries[index-1]
}

// StyleAt returns the style of the rune at i, or the final run at the end.
func (d RichDocument) StyleAt(i int) RichStyle {
	i = max(0, min(i, d.n-1))
	for _, run := range d.runs {
		if i < run.Range.End {
			return run.Style
		}
	}
	return RichStyle{}
}

func validRichStyle(s RichStyle) RichStyle {
	if s.Size < 0 || math.IsNaN(float64(s.Size)) || math.IsInf(float64(s.Size), 0) {
		s.Size = 0
	}
	if s.Weight != 0 {
		s.Weight = max(100, min(s.Weight, 900))
	}
	if !validRichLink(s.Link) {
		s.Link = ""
	}
	return s
}

func validRichLink(s string) bool {
	if strings.ContainsAny(s, "\x00\r\n") {
		return false
	}
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	return (scheme == "https" || scheme == "http") && u.Host != "" || scheme == "mailto" && u.Opaque != ""
}

func validParagraphStyle(s ParagraphStyle) ParagraphStyle {
	if s.Alignment != Start && s.Alignment != Center && s.Alignment != End {
		s.Alignment = Start
	}
	if s.LineHeight < 0 || math.IsNaN(float64(s.LineHeight)) || math.IsInf(float64(s.LineHeight), 0) {
		s.LineHeight = 0
	}
	return s
}

func appendStyled(runs []StyledRange, r TextRange, s RichStyle) []StyledRange {
	if r.Start == r.End {
		return runs
	}
	if n := len(runs); n > 0 && runs[n-1].Range.End == r.Start && runs[n-1].Style == s {
		runs[n-1].Range.End = r.End
		return runs
	}
	return append(runs, StyledRange{r, s})
}

// WithStyle replaces the complete character style of a range. Use TransformStyle
// to change individual attributes without losing the others.
func (d RichDocument) WithStyle(r TextRange, s RichStyle) RichDocument {
	s = validRichStyle(s)
	return d.TransformStyle(r, func(RichStyle) RichStyle { return s })
}

// TransformStyle changes each run intersecting r. fn runs synchronously and
// receives a value; it cannot mutate the original document.
func (d RichDocument) TransformStyle(r TextRange, fn func(RichStyle) RichStyle) RichDocument {
	r = d.NormalizeRange(r)
	if r.Start == r.End || fn == nil {
		return d
	}
	var runs []StyledRange
	for _, run := range d.runs {
		a, z := run.Range.Start, run.Range.End
		if z <= r.Start || a >= r.End {
			runs = appendStyled(runs, run.Range, run.Style)
			continue
		}
		if a < r.Start {
			runs = appendStyled(runs, TextRange{a, r.Start}, run.Style)
		}
		runs = appendStyled(runs, TextRange{max(a, r.Start), min(z, r.End)}, validRichStyle(fn(run.Style)))
		if z > r.End {
			runs = appendStyled(runs, TextRange{r.End, z}, run.Style)
		}
	}
	d.runs = runs
	return d
}

func (d RichDocument) paragraphAt(i int) int {
	return strings.Count(d.text[:runeOffset(d.text, max(0, min(i, d.n)))], "\n")
}

// WithParagraphStyle styles the paragraphs touched by r. A selection ending
// at the start of a paragraph does not style that paragraph; a caret does.
func (d RichDocument) WithParagraphStyle(r TextRange, s ParagraphStyle) RichDocument {
	r = d.NormalizeRange(r)
	a, z := d.paragraphAt(r.Start), d.paragraphAt(r.End)
	if r.Start != r.End && r.End > 0 && d.text[runeOffset(d.text, r.End)-1] == '\n' {
		z--
	}
	d.paragraphs = d.Paragraphs()
	for p := a; p <= z; p++ {
		d.paragraphs[p] = validParagraphStyle(s)
	}
	return d
}

// Slice returns a styled fragment of r. It includes the styles of every
// paragraph touched, so copying an empty final paragraph preserves its style.
func (d RichDocument) Slice(r TextRange) RichDocument {
	r = d.NormalizeRange(r)
	out := NewRichDocument(d.text[runeOffset(d.text, r.Start):runeOffset(d.text, r.End)])
	out.runs = nil
	for _, run := range d.runs {
		a, z := max(run.Range.Start, r.Start), min(run.Range.End, r.End)
		if z > a {
			out.runs = appendStyled(out.runs, TextRange{a - r.Start, z - r.Start}, run.Style)
		}
	}
	p := d.paragraphAt(r.Start)
	for i := range out.paragraphs {
		out.paragraphs[i] = d.paragraph(p + i)
	}
	return out
}

// Replace replaces r with a styled fragment. Paragraphs split by inserted
// newlines take the fragment's paragraph styles. Joining paragraphs keeps the
// leading paragraph's style. The fragment's first style wins when replacing
// from the start of a paragraph. Graphemes newly joined by an edit take the
// style of their first rune.
func (d RichDocument) Replace(r TextRange, fragment RichDocument) RichDocument {
	r = d.NormalizeRange(r)
	ba, bz := runeOffset(d.text, r.Start), runeOffset(d.text, r.End)
	out := NewRichDocument(d.text[:ba] + fragment.text + d.text[bz:])
	out.runs = nil
	for _, run := range d.runs {
		if a, z := run.Range.Start, min(run.Range.End, r.Start); z > a {
			out.runs = appendStyled(out.runs, TextRange{a, z}, run.Style)
		}
	}
	for _, run := range fragment.runs {
		out.runs = appendStyled(out.runs, TextRange{r.Start + run.Range.Start, r.Start + run.Range.End}, run.Style)
	}
	delta := fragment.n - (r.End - r.Start)
	for _, run := range d.runs {
		if a, z := max(run.Range.Start, r.End), run.Range.End; z > a {
			out.runs = appendStyled(out.runs, TextRange{a + delta, z + delta}, run.Style)
		}
	}
	pa, pz := d.paragraphAt(r.Start), d.paragraphAt(r.End)
	styles := d.Paragraphs()
	out.paragraphs = slices.Clone(styles[:pa+1])
	if (r.Start == 0 || d.text[ba-1] == '\n') && fragment.n > 0 {
		out.paragraphs[pa] = fragment.paragraph(0)
	}
	for p := 1; p <= strings.Count(fragment.text, "\n"); p++ {
		out.paragraphs = append(out.paragraphs, fragment.paragraph(p))
	}
	out.paragraphs = append(out.paragraphs, styles[pz+1:]...)
	// Inserted combining marks and ZWJ sequences can join old clusters.
	// Normalize every run boundary once, assigning a cluster to its first run.
	out.runs = out.normalizeRuns(out.runs)
	return out
}

func (d RichDocument) normalizeRuns(runs []StyledRange) []StyledRange {
	var out []StyledRange
	start := 0
	for _, run := range runs {
		end := d.snap(run.Range.End, true)
		if end > start {
			out = appendStyled(out, TextRange{start, end}, validRichStyle(run.Style))
			start = end
		}
	}
	return out
}

func (d RichDocument) equal(other RichDocument) bool {
	return d.text == other.text && slices.Equal(d.runs, other.runs) && slices.Equal(d.Paragraphs(), other.Paragraphs())
}

func richSpan(s string, style RichStyle, accent Color) Span {
	color := style.Color
	if style.Link != "" && color.A == 0 {
		color = accent
	}
	return Span{Text: s, Font: style.Font, Size: style.Size, Weight: style.Weight, Italic: style.Italic,
		Underline: style.Underline || style.Link != "", Strikethrough: style.Strikethrough, Color: color, Background: style.Background}
}
