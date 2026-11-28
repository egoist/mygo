package editor

import (
	"regexp"
	"slices"
)

// Linked editing, as VS Code's editor.linkedEditing on a language
// server's textDocument/linkedEditingRange: ranges holding the same text,
// as an HTML tag's name and its closing tag's, where an edit of the one
// with the caret is made in the others too, in the same step of undo.

// LinkedEditingRequest asks for the ranges that are edited together with
// the text at Offset of the text at Version, the caret's place: answer
// with SetLinkedEditingRanges.
type LinkedEditingRequest struct {
	Offset  int
	Version uint64
}

type linkedSession struct {
	ranges []Range // in order, apart, each holding the same text
	word   *regexp.Regexp
}

// linkedAsk is the caret's place OnLinkedEditing was last asked about.
type linkedAsk struct {
	version uint64
	offset  int
}

// SetLinkedEditingRanges starts linked editing of ranges of the text at
// version, as a language server's linked editing ranges: while the only
// caret stays in one of them, an edit there is made in the others too,
// and undone with it. The session ends when the caret leaves the ranges,
// another caret is added, an edit leaves the edited range's text outside
// wordPattern, or an edit made elsewhere, as another editor's, changes
// the text of a range. wordPattern must match the whole text, unless it
// is empty; nil takes the editor's word characters.
//
// Ranges of an older version are ignored, as an answer that comes late.
// Fewer than two, overlapping or touching ranges, ranges of different
// texts, or ranges the caret is not in start nothing, and end the
// session running.
func (st *State) SetLinkedEditingRanges(version uint64, ranges []Range, wordPattern *regexp.Regexp) {
	st.ensure()
	ed := &st.ed
	if version != ed.view.version {
		return
	}
	ed.linked = nil
	if len(ranges) < 2 {
		return
	}
	s := &linkedSession{ranges: slices.Clone(ranges), word: wordPattern}
	slices.SortFunc(s.ranges, func(a, b Range) int { return a.Start - b.Start })
	for i, r := range s.ranges {
		if r.Start < 0 || r.End < r.Start || r.End > ed.view.text.Len() || i > 0 && r.Start <= s.ranges[i-1].End {
			return
		}
	}
	ed.linked = s
	if ed.linkedBroken() {
		ed.linked = nil
	}
}

// LinkedEditingRanges returns the ranges being edited together, where
// the edits since moved them; nil without a session.
func (st *State) LinkedEditingRanges() []Range {
	if st.ed.linked == nil {
		return nil
	}
	return slices.Clone(st.ed.linked.ranges)
}

// askLinked asks the app for the linked ranges at the only caret of a
// focused editor, once for each place and version.
func (ed *editor) askLinked() {
	if ed.o.onLinked == nil || ed.o.readOnly || !ed.focused || ed.linked != nil || len(ed.sels) != 1 {
		return
	}
	at := linkedAsk{ed.view.version, ed.sels[0].head}
	if at == ed.linkedAt {
		return
	}
	ed.linkedAt = at
	ed.o.onLinked(LinkedEditingRequest{Offset: at.offset, Version: at.version})
}

// linkedRef returns the range the only caret is in, -1 for none.
func (ed *editor) linkedRef() int {
	if ed.linked == nil || len(ed.sels) != 1 {
		return -1
	}
	head := ed.sels[0].head
	for i, r := range ed.linked.ranges {
		if head >= r.Start && head <= r.End {
			return i
		}
	}
	return -1
}

// keepLinked ends the session once the caret is out of its ranges or
// their texts differ, and asks again at the caret's place, as after a
// second caret is taken away.
func (ed *editor) keepLinked() {
	if ed.linked != nil && ed.linkedBroken() {
		ed.linked, ed.linkedAt.offset = nil, -1
	}
}

// linkedBroken reports whether the caret is out of the session's ranges
// or their texts differ.
func (ed *editor) linkedBroken() bool {
	if ed.linkedRef() < 0 {
		return true
	}
	s := ed.linked
	first := s.ranges[0]
	text := ed.slice(first.Start, first.End)
	for _, r := range s.ranges[1:] {
		if r.End-r.Start != len(text) || ed.slice(r.Start, r.End) != text {
			return true
		}
	}
	return false
}

// moved follows the changes of a version, reporting false when one
// crosses a range's edge. A change within a range, its edges included,
// resizes it.
func (s *linkedSession) moved(cs []change) bool {
	j, delta := 0, 0
	for i := range s.ranges {
		r := &s.ranges[i]
		start, end := r.Start, r.End
		for j < len(cs) && (cs[j].oldEnd < start || cs[j].oldEnd == start && cs[j].oldStart < start) {
			delta += cs[j].newEnd - cs[j].newStart - (cs[j].oldEnd - cs[j].oldStart)
			j++
		}
		r.Start = start + delta
		for j < len(cs) && cs[j].oldStart >= start && cs[j].oldEnd <= end {
			delta += cs[j].newEnd - cs[j].newStart - (cs[j].oldEnd - cs[j].oldStart)
			j++
		}
		r.End = end + delta
		if j < len(cs) && cs[j].oldStart < end {
			return false
		}
	}
	return true
}

// mirrorLinked makes the changes cs, all within the range ref of the
// text before them, in the other ranges too, as part of the step of undo
// they made; b.mu is held. It ends the session when the range's new text
// is not a word, of the word characters words when no pattern is set.
func (ed *editor) mirrorLinked(b *Buffer, ref int, words string, cs []change) {
	s := ed.linked
	r := s.ranges[ref]
	for _, c := range cs {
		if c.oldStart < r.Start || c.oldEnd > r.End {
			return
		}
	}
	moved := &linkedSession{ranges: slices.Clone(s.ranges)}
	moved.moved(cs)
	now := moved.ranges[ref]
	if !s.isWord(b.text.Slice(now.Start, now.End), words) {
		ed.linked = nil
		return
	}
	var mirrors []Edit
	for i, m := range moved.ranges {
		if i == ref {
			continue
		}
		// Each range holds the text ref held, at the same offsets.
		for _, c := range cs {
			mirrors = append(mirrors, Edit{Start: m.Start + c.oldStart - r.Start, End: m.Start + c.oldEnd - r.Start, Text: b.text.Slice(c.newStart, c.newEnd)})
		}
	}
	// Into the step the edit made or joined, the last one.
	open := b.openTx
	b.openTx = &b.undo[len(b.undo)-1]
	b.edit(mirrors, editOther, nil, nil)
	b.openTx = open
}

// isWord reports whether text, a range's after an edit, may still be
// edited together with the others, as VS Code checks the word pattern.
func (s *linkedSession) isWord(text, extra string) bool {
	if s.word != nil {
		if text == "" {
			return true
		}
		loc := s.word.FindStringIndex(text)
		return loc != nil && loc[1]-loc[0] == len(text)
	}
	for _, r := range text {
		if classOf(r, extra) != classWord {
			return false
		}
	}
	return true
}
