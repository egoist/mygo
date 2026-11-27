package markdown

import (
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/internal/l10n"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// Finding: Cmd+F while the view has the focus opens a find bar over its
// top right, the code editor's own, which finds in the text as it reads,
// for the source is not the text: paragraphs, headings, list items,
// quotes, the cells of tables and the lines of code blocks match, and
// what the view shows otherwise (an image's description, a block of HTML
// or math) does not. Every match highlights, the one the bar is on
// stronger and in the middle of the view, and Enter, Cmd+G and F3 go
// through them, as the editor's bar does. Its options match as they do
// in the editor: by case, whole words only, as a regular expression, and
// within the text selected as the option was turned on.

// findMatch tells one match from another across frames: where it starts,
// in the text of the element showing it (as findScope says whose), which
// frames build the same at the same place, as a reply streams in. Where a
// match is, not how many came before it, tells where the bar is on as the
// query and its options change.
type findMatch struct {
	block, line int
	at          int // bytes into the element's text
}

// atOrAfter reports whether m comes at or after o in the document.
func (m findMatch) atOrAfter(o findMatch) bool {
	if m.block != o.block {
		return m.block > o.block
	}
	if m.line != o.line {
		return m.line > o.line
	}
	return m.at >= o.at
}

// findScope is a part of what the bar finds in as its option of finding in
// the selection is on: the text of one of the view's elements, a
// paragraph's (line -1) or a code block's chunk from its line, from a to
// b in bytes.
type findScope struct {
	block, line int
	a, b        int
}

// findState is what a view keeps of finding in its text.
type findState struct {
	open        bool
	query       string
	caseSens    bool
	wholeWord   bool
	regex       bool
	inSelection bool
	// focusQuery takes the field the focus, as the bar opens or is asked
	// for again.
	focusQuery bool
	// refocus gives the view the focus back, as the bar closes from it.
	refocus bool
	// asked is what Options.Find last asked, and askedV its version.
	asked  bool
	askedV uint64
	// painted is what the frame's content was cut for: rev changes as the
	// query, its options or the bar's being open does, and every paragraph
	// and code chunk cuts again. q is the query as its options have it
	// match, err the error of one that is not a regular expression, and
	// scope what was selected as finding in the selection was turned on.
	paintedQuery string
	paintedOpts  editor.FindOptions
	paintedInSel bool
	paintedOpen  bool
	q            *editor.FindQuery
	err          error
	scope        []findScope
	rev          uint32
	// pending is the first match at or after the one the bar is on, cur
	// where that match was, and reveal asks to bring it into view.
	pending findMatch
	cur     findMatch
	reveal  bool
	// The frame's matches, in the order the view builds them, the one the
	// bar is on among them (-1 for none), and revealHere its element
	// waiting to come into view. revealRunes is that match's range, in
	// runes of its element's text.
	frameKeys   []findMatch
	currentSeq  int
	revealHere  bool
	revealRunes [2]int
}

// active reports whether the view's text is being searched.
func (f *findState) active() bool { return f.open && f.query != "" }

// note notes a match of bytes [start, end) of text, the text of the element
// of block and line, and reports whether it is the one the bar is on: the
// first at or after pending is, and one a frame notes only.
func (f *findState) note(block, line, start, end int, text string) bool {
	m := findMatch{block, line, start}
	f.frameKeys = append(f.frameKeys, m)
	if f.currentSeq >= 0 || !m.atOrAfter(f.pending) {
		return false
	}
	f.currentSeq, f.cur = len(f.frameKeys)-1, m
	f.revealHere = f.reveal
	f.reveal = false
	f.revealRunes = [2]int{utf8.RuneCountInString(text[:start]), utf8.RuneCountInString(text[:end])}
	return true
}

// findBegin starts a frame: what was typed or asked of the bar since the
// last frame cuts the content again, the match the bar was on staying
// where it was, and the matches of the frame begin at none.
func (st *viewState) findBegin(c *ui.Context, root ui.Element, o *Options) {
	f := &st.find
	if !f.asked || o.FindVersion != f.askedV {
		f.asked, f.askedV = true, o.FindVersion
		if f.open = o.Find; f.open {
			f.reveal = true
		}
	}
	opts := editor.FindOptions{CaseSensitive: f.caseSens, WholeWord: f.wholeWord, Regexp: f.regex}
	if f.open {
		typed := f.query != f.paintedQuery || opts != f.paintedOpts
		if typed || f.inSelection != f.paintedInSel {
			// Typed, or an option turned: where the match was, not how many
			// came before it, tells where the match is.
			f.pending = f.cur
			f.rev++
			if f.inSelection != f.paintedInSel {
				// Finding in the selection takes the selection there is now.
				f.paintedInSel, f.scope = f.inSelection, nil
				if f.inSelection {
					f.scope = st.findSelection(c, root)
				}
			}
			if typed {
				f.paintedQuery, f.paintedOpts = f.query, opts
				f.q, f.err = editor.NewFindQuery(f.query, opts)
				// What was typed comes into view, as the editor brings its
				// match to; an option turned leaves the view where it is.
				f.reveal = true
			}
		}
	}
	if f.open != f.paintedOpen {
		f.paintedOpen = f.open
		f.rev++
		if f.open {
			f.reveal = true
			f.focusQuery = true
		}
	}
	f.frameKeys, f.currentSeq, f.revealHere = f.frameKeys[:0], -1, false
}

// findSelection returns what the bar finds in as its option of finding in
// the selection is turned on: what the view's selection holds, as of the
// last frame, each part as the bytes of the text of the element showing
// it, which the view noted by its block and line. A view that leaves its
// selection to a container around it (Options.ShareSelection) finds in
// that container's where it is another view, whose blocks hold it
// (outerView). An app's own Selectable container the view cannot see: its
// selection is not in the scope, and the option finds in all of the text.
func (st *viewState) findSelection(c *ui.Context, root ui.Element) []findScope {
	parts := root.SelectedTexts()
	if st.o.ShareSelection {
		if outer := outerView(c); outer != nil && outer.root.ID() != 0 {
			parts = outer.root.SelectedTexts()
		}
	}
	var out []findScope
	for _, part := range parts {
		for _, e := range st.elems { // the last frame's
			if e.id != part.ID {
				continue
			}
			a, b := byteOfRune(part.Text, part.Start), byteOfRune(part.Text, part.End)
			out = append(out, findScope{e.block, e.line, a, b})
			break
		}
	}
	return out
}

// findIn returns the matches of the query in text, the text of the element
// of block at line (a paragraph's, with line -1, or a code block's chunk
// from its line), as the bar's options have them: within what was selected
// as finding in the selection was turned on, where that option is on and
// something was, as the editor's find does.
func (st *viewState) findIn(block, line int, text string) [][2]int {
	f := &st.find
	if f.q == nil {
		return nil
	}
	rs := f.q.Matches(text)
	if !f.inSelection || len(f.scope) == 0 {
		return rs
	}
	kept := rs[:0]
	for _, r := range rs {
		for _, s := range f.scope {
			if s.block == block && s.line == line && s.a <= r[0] && r[1] <= s.b {
				kept = append(kept, r)
				break
			}
		}
	}
	return kept
}

// byteOfRune returns the byte where rune r of s starts, len(s) past its
// end.
func byteOfRune(s string, r int) int {
	for i := range s {
		if r == 0 {
			return i
		}
		r--
	}
	return len(s)
}

// findStep goes to the next match of the frame, or the previous, round the
// ends. What the bar chose cuts the content and comes into view on the
// frame the view asks for.
func (st *viewState) findStep(c *ui.Context, dir int) {
	f := &st.find
	if len(f.frameKeys) == 0 {
		return
	}
	i := 0
	if f.currentSeq >= 0 {
		i = (f.currentSeq + dir + len(f.frameKeys)) % len(f.frameKeys)
	} else if dir < 0 {
		i = len(f.frameKeys) - 1
	}
	f.pending = f.frameKeys[i]
	f.rev++
	f.reveal = true
	c.Invalidate()
}

// findReveal brings the current match, once its element is built, into the
// middle of the containers around the view: its own line where the element
// is taller than they show.
func (st *viewState) findReveal(el ui.Element) {
	if !st.find.revealHere {
		return
	}
	st.find.revealHere = false
	f := &st.find
	el.ScrollTextRangeIntoView(f.revealRunes[0], f.revealRunes[1], ui.ScrollIntoViewOptions{Block: ui.ScrollCenter})
}

// findPara returns the pieces of p, the paragraph of block i, cut at the
// matches of the query in their text, colored as matches, noted of the
// block. What it cut it keeps, as the paragraph keeps its spans, until
// the query, its options or the match the bar is on changes.
func (st *viewState) findPara(i int, p *para) []piece {
	f := &st.find
	if !f.active() {
		return p.pieces
	}
	pc := &st.paras[i]
	text := piecesText(p.pieces)
	if pc.findRev == f.rev {
		for _, r := range pc.findRanges {
			f.note(i, -1, r[0], r[1], text)
		}
		return pc.findPieces
	}
	pc.findRev = f.rev
	pc.findRanges = p.findable(st.findIn(i, -1, text))
	pc.findPieces = p.pieces
	if len(pc.findRanges) > 0 {
		pc.findPieces = cutPieces(p.pieces, pc.findRanges, func(k int) Color {
			if f.note(i, -1, pc.findRanges[k][0], pc.findRanges[k][1], text) {
				return st.th.CurrentMatch
			}
			return st.th.Match
		})
	}
	return pc.findPieces
}

// findChunk returns the spans of ch, a chunk of code block i from its line,
// cut at the matches of the query in its text, colored as matches, noted
// of the block. What it cut it keeps until the query, its options or the
// match the bar is on changes.
func (st *viewState) findChunk(ch *codeChunk, i, line int) []ui.Span {
	f := &st.find
	if !f.active() {
		return ch.spans
	}
	text := spansText(ch.spans)
	if ch.findRev == f.rev {
		for _, r := range ch.findRanges {
			f.note(i, line, r[0], r[1], text)
		}
		return ch.findSpans
	}
	ch.findRev = f.rev
	ch.findRanges = st.findIn(i, line, text)
	ch.findSpans = ch.spans
	if len(ch.findRanges) > 0 {
		ch.findSpans = cutSpans(ch.spans, ch.findRanges, func(k int) Color {
			if f.note(i, line, ch.findRanges[k][0], ch.findRanges[k][1], text) {
				return st.th.CurrentMatch
			}
			return st.th.Match
		})
	}
	return ch.findSpans
}

// buildFindBar builds the view's find bar, the code editor's own, over the
// top of the view after its content, which it counts the matches of.
func (st *viewState) buildFindBar(c *ui.Context) {
	f := &st.find
	if !f.open {
		return
	}
	// The match the bar was on went (the query was typed, the reply grew):
	// the first is on, cut again and brought into view.
	if f.currentSeq < 0 && len(f.frameKeys) > 0 {
		f.currentSeq = 0
		f.pending, f.cur = f.frameKeys[0], f.frameKeys[0]
		f.rev++
		f.reveal = true
		c.Invalidate()
	}
	editor.FindBarInRow(c, "markdown find", &findBar{c: c, st: st})
}

// findBar is the view's finding as its find bar shows and does it
// (editor.FindBarSource). The view's text only reads: the bar replaces
// nothing, and does not select every match at once, as an editor's does.
type findBar struct {
	c  *ui.Context
	st *viewState
}

var _ editor.FindBarSource = (*findBar)(nil)

// Query is the text of the bar's field, to bind an input to.
func (b *findBar) Query() *string { return &b.st.find.query }

// CaseSensitive, WholeWord, Regexp and InSelection are the options of the
// search, to bind toggles to.
func (b *findBar) CaseSensitive() *bool { return &b.st.find.caseSens }
func (b *findBar) WholeWord() *bool     { return &b.st.find.wholeWord }
func (b *findBar) Regexp() *bool        { return &b.st.find.regex }
func (b *findBar) InSelection() *bool   { return &b.st.find.inSelection }

// Width is the width the view was laid out at, 0 until it is: the bar
// shows all of itself until then.
func (b *findBar) Width() float32 { return b.st.root.Bounds().W }

// Status says what was found, as the editor's bar does: "3 of 12", "No
// results" or "Invalid" for a regular expression that is not one, and
// nothing without a query.
func (b *findBar) Status() string {
	tx := l10n.In(b.c.Locale())
	f := &b.st.find
	switch {
	case f.err != nil:
		return tx.Invalid()
	case f.query == "":
		return ""
	case len(f.frameKeys) == 0:
		return tx.NoResults()
	}
	return tx.Matches(max(f.currentSeq, 0)+1, len(f.frameKeys))
}

// Err is the error of a query that is not a valid regular expression.
func (b *findBar) Err() error { return b.st.find.err }

// Next and Previous go to the next match and the previous, round the ends;
// Close closes the bar and gives the view the focus back, where it is, as
// an editor's does, so that Cmd+F opens the bar again.
func (b *findBar) Next()     { b.st.findStep(b.c, 1) }
func (b *findBar) Previous() { b.st.findStep(b.c, -1) }
func (b *findBar) Close() {
	b.st.find.open, b.st.find.refocus = false, true
	b.c.Invalidate()
}

// FocusQuery reports that the bar's field takes the focus with the query
// selected, as the bar opens or is asked for again: once.
func (b *findBar) FocusQuery() bool {
	f := &b.st.find
	if !f.focusQuery {
		return false
	}
	f.focusQuery = false
	return true
}

// findable returns the matches of rs, ranges into the bytes of p's spans'
// text joined, that are in the paragraph's text: none through what it
// shows in place of text, an image's description (notText).
func (p *para) findable(rs [][2]int) [][2]int {
	if len(p.notText) == 0 {
		return rs
	}
	kept := rs[:0]
	for _, r := range rs {
		through := false
		for _, n := range p.notText {
			if r[0] < n[1] && n[0] < r[1] {
				through = true
				break
			}
		}
		if !through {
			kept = append(kept, r)
		}
	}
	return kept
}

// piecesText returns the text of pieces, their spans' text joined.
func piecesText(pieces []piece) string {
	var b strings.Builder
	for k := range pieces {
		for _, sp := range pieces[k].spans {
			b.WriteString(sp.Text)
		}
	}
	return b.String()
}

// spansText returns the text of spans, joined.
func spansText(spans []ui.Span) string {
	var b strings.Builder
	for _, sp := range spans {
		b.WriteString(sp.Text)
	}
	return b.String()
}

// cutPieces returns pieces cut at ranges, pairs into their spans' text
// joined, coloring each match as colorAt says, by its index. The pieces
// keep everything else, their links' pieces included.
func cutPieces(pieces []piece, ranges [][2]int, colorAt func(int) Color) []piece {
	sc := spanCutter{ranges: ranges, colors: colorsOf(ranges, colorAt)}
	out := make([]piece, len(pieces))
	for k := range pieces {
		out[k] = pieces[k]
		out[k].spans = nil
		for _, sp := range pieces[k].spans {
			sc.span(sp, &out[k].spans)
		}
	}
	return out
}

// cutSpans returns spans cut at ranges, pairs into their text joined,
// coloring each match as colorAt says, by its index.
func cutSpans(spans []ui.Span, ranges [][2]int, colorAt func(int) Color) []ui.Span {
	sc := spanCutter{ranges: ranges, colors: colorsOf(ranges, colorAt)}
	out := make([]ui.Span, 0, len(spans))
	for _, sp := range spans {
		sc.span(sp, &out)
	}
	return out
}

func colorsOf(ranges [][2]int, colorAt func(int) Color) []Color {
	colors := make([]Color, len(ranges))
	for k := range ranges {
		colors[k] = colorAt(k)
	}
	return colors
}

// spanCutter cuts spans where ranges of their joined text, one color
// each, fall. Ranges never overlap, and none ends within a character.
type spanCutter struct {
	ranges [][2]int
	colors []Color
	at     int // bytes of the joined text before the span being cut
	k      int // the first range that may reach into what is left
}

// span appends to out what it cuts sp into.
func (sc *spanCutter) span(sp ui.Span, out *[]ui.Span) {
	a, z := sc.at, sc.at+len(sp.Text)
	sc.at = z
	for sc.k < len(sc.ranges) && sc.ranges[sc.k][1] <= a {
		sc.k++ // a range that ended before this span
	}
	if sc.k >= len(sc.ranges) || sc.ranges[sc.k][0] >= z {
		*out = append(*out, sp) // no match through it
		return
	}
	// Where the span cuts: the starts and ends of the matches through it,
	// between its own ends.
	var cuts []int
	for k := sc.k; k < len(sc.ranges) && sc.ranges[k][0] < z; k++ {
		lo, hi := max(sc.ranges[k][0], a), min(sc.ranges[k][1], z)
		if hi > lo {
			cuts = append(cuts, lo-a, hi-a)
		}
	}
	bounds := make([]int, 0, len(cuts)+2)
	bounds = append(bounds, 0)
	bounds = append(bounds, cuts...)
	bounds = append(bounds, len(sp.Text))
	for k := 0; k+1 < len(bounds); k++ {
		lo, hi := bounds[k], bounds[k+1]
		if lo == hi {
			continue
		}
		cut := sp
		cut.Text = sp.Text[lo:hi]
		if k%2 == 1 {
			cut.Background = sc.colors[k/2]
			cut.Radius = 2
		}
		*out = append(*out, cut)
	}
}
