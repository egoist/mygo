package editor

import (
	"slices"
	"strconv"
	"unicode/utf8"
)

// InsertSnippet replaces each selection with s and selects its first
// numbered placeholders. Tab and Shift+Tab visit stops; Escape, undo,
// an external buffer edit or moving outside the active fields ends the
// session. A new snippet replaces the session, rather than nesting it.
// Redo restores the insertion and its selections, without restarting a
// snippet session. It reports whether it inserted, and leaves a read-only
// editor unchanged, as it does when its variables' values lay it out
// past ParseSnippet's bounds at a site. Call it on the UI thread, as
// other State methods.
func (st *State) InsertSnippet(s *Snippet) bool {
	if s == nil {
		return false
	}
	st.ensure()
	return st.ed.retry(func() bool {
		edits := make([]Edit, len(st.ed.sels))
		for i, sel := range st.ed.sels {
			edits[i] = Edit{Start: sel.start(), End: sel.end()}
		}
		return st.ed.insertSnippet(s, edits)
	})
}

type snippetSession struct {
	fields      []snippetField // in document order, including each insertion site
	stops       []int
	at          int
	primarySite int
	local       bool // the next buffer change was made through this editor's apply
}

// insertSnippet replaces the ranges of edits, one for each selection,
// with s, laid out with each site's variables.
func (ed *editor) insertSnippet(s *Snippet, edits []Edit) bool {
	if ed.o.readOnly {
		return false
	}
	// Completion prefixes can overlap when two carets are in one word.
	// Merge those targets before computing each inserted template's fields.
	targets := make([]sel, len(edits))
	for i, e := range edits {
		targets[i] = sel{anchor: e.Start, head: e.End, id: ed.sels[i].id}
	}
	targets = mergeSels(targets)
	edits = make([]Edit, len(targets))
	fields := make([][]snippetField, len(targets))
	var sels map[int]sel
	if s.variables {
		sels = make(map[int]sel, len(ed.sels))
		for _, x := range ed.sels {
			sels[x.id] = x
		}
	}
	primarySite := 0
	for i, target := range targets {
		if target.id == ed.primary().id {
			primarySite = i
		}
		text, fs := s.text, s.fields
		// The zero Snippet has no layout, not even its final stop.
		if s.variables || fs == nil {
			at := sels[target.id]
			var err error
			text, fs, err = s.render(func(name string) string { return ed.snippetVariable(name, at, i) })
			if err != nil {
				// Values past the template's bounds insert nothing.
				return false
			}
		}
		edits[i] = Edit{Start: target.start(), End: target.end(), Text: text}
		fields[i] = fs
	}
	ed.snippet = nil
	ed.endGroup()
	version := ed.view.version
	stops := s.stops
	if len(stops) == 0 {
		stops = []int{0}
	}
	session := &snippetSession{stops: stops, primarySite: primarySite}
	ok := ed.apply(edits, editOther, func(func(int, bias) int) []sel {
		session.fields = nil
		delta := 0
		for site, e := range edits {
			for _, f := range fields[site] {
				f.start += e.Start + delta
				f.end += e.Start + delta
				f.site = site
				session.fields = append(session.fields, f)
			}
			delta += len(e.Text) - (e.End - e.Start)
		}
		// The first stops can have no fields, where variables' values
		// replaced the defaults holding them.
		session.at = slices.IndexFunc(session.stops, session.has)
		return session.selections(ed)
	})
	if ok && session.at < len(session.stops)-1 && ed.view.version <= version+1 {
		ed.snippet = session
	}
	return ok
}

// snippetVariable returns a variable's value at an insertion site, where
// at was: the editor's own variables, else the app's.
func (ed *editor) snippetVariable(name string, at sel, site int) string {
	r := ed.view.text
	line := r.LineOf(at.head)
	switch name {
	case "TM_SELECTED_TEXT", "SELECTION":
		return ed.slice(at.start(), at.end())
	case "TM_CURRENT_LINE":
		return r.Line(line)
	case "TM_CURRENT_WORD":
		a, z := wordAt(r, at.head, ed.wordChars())
		word := ed.slice(a, z)
		if c, _ := utf8.DecodeRuneInString(word); word != "" && classOf(c, ed.wordChars()) == classWord {
			return word
		}
		return ""
	case "TM_LINE_INDEX":
		return strconv.Itoa(line)
	case "TM_LINE_NUMBER":
		return strconv.Itoa(line + 1)
	case "CURSOR_INDEX":
		return strconv.Itoa(site)
	case "CURSOR_NUMBER":
		return strconv.Itoa(site + 1)
	case "LINE_COMMENT", "BLOCK_COMMENT_START", "BLOCK_COMMENT_END":
		l := ed.language()
		switch {
		case l == nil:
			return ""
		case name == "LINE_COMMENT":
			return l.LineComment
		case name == "BLOCK_COMMENT_START":
			return l.BlockComment[0]
		}
		return l.BlockComment[1]
	}
	if ed.o.snippetVars != nil {
		return lf(ed.o.snippetVars(name))
	}
	return ""
}

// has reports whether stop id has a field left: editing a placeholder
// over the ones nested in it removes those.
func (s *snippetSession) has(id int) bool {
	return slices.ContainsFunc(s.fields, func(f snippetField) bool { return f.id == id })
}

func (s *snippetSession) selections(ed *editor) []sel {
	var sels []sel
	primary := -1
	for _, f := range s.fields {
		if f.id == s.stops[s.at] {
			if f.site == s.primarySite && primary < 0 {
				primary = len(sels)
			}
			sels = append(sels, ed.newSel(f.start, f.end))
		}
	}
	// Input methods and scrolling follow the first occurrence in the
	// insertion site that held the primary caret, not its last mirror.
	if primary >= 0 {
		ed.nextID++
		sels[primary].id = ed.nextID
	}
	return sels
}

func (ed *editor) stepSnippet(back bool) bool {
	s := ed.snippet
	if s == nil {
		return false
	}
	if ed.o.readOnly || !s.contains(ed.sels) {
		ed.snippet = nil
		return false
	}
	at := s.at
	for {
		if back {
			at--
		} else {
			at++
		}
		if at < 0 || at >= len(s.stops) || s.has(s.stops[at]) {
			break
		}
	}
	ed.snippet = nil // setSels is an explicit selection change, except here
	switch {
	case at < 0:
		// Shift+Tab at the first stop selects it again.
	case at >= len(s.stops):
		// The final stop went with a placeholder that held it.
		ed.endGroup()
		return true
	default:
		s.at = at
	}
	ed.setSels(s.selections(ed), revealFit)
	ed.endGroup()
	if s.at < len(s.stops)-1 {
		ed.snippet = s
	}
	return true
}

// contains requires the same relative selection in every active field,
// so moving one caret alone does not leave a partially linked session.
func (s *snippetSession) contains(sels []sel) bool {
	i, anchor, head := 0, 0, 0
	for _, f := range s.fields {
		if f.id != s.stops[s.at] {
			continue
		}
		if i >= len(sels) || sels[i].start() < f.start || sels[i].end() > f.end {
			return false
		}
		a, h := sels[i].anchor-f.start, sels[i].head-f.start
		if i == 0 {
			anchor, head = a, h
		} else if a != anchor || h != head {
			return false
		}
		i++
	}
	return i == len(sels)
}

func (s *snippetSession) accepts(edits []Edit) bool {
	i := 0
	var first Edit
	for _, f := range s.fields {
		if f.id != s.stops[s.at] {
			continue
		}
		if i >= len(edits) || edits[i].Start < f.start || edits[i].End > f.end {
			return false
		}
		e := edits[i]
		e.Start -= f.start
		e.End -= f.start
		if i == 0 {
			first = e
		} else if e != first {
			return false
		}
		i++
	}
	return i == len(edits)
}

// moved follows only the one pending local change, each of its changes
// in one active field. Fields move in template order, so an edit at a
// shared boundary belongs to the active field rather than its neighbour,
// and the fields enclosing an active one grow with it. The fields nested
// in an active one keep their place in its text, and those its edit
// replaces or cuts into are removed, as VS Code skips them.
func (s *snippetSession) moved(cs []change) bool {
	if !s.local {
		return false
	}
	s.local = false
	active := s.stops[s.at]
	i, delta := 0, 0
	var open []int // the fields enclosing the next one, outermost first
	close := func(depth int) {
		for len(open) > 0 && s.fields[open[len(open)-1]].depth >= depth {
			s.fields[open[len(open)-1]].end += delta
			open = open[:len(open)-1]
		}
	}
	for n := 0; n < len(s.fields); n++ {
		f := &s.fields[n]
		close(f.depth)
		open = append(open, n)
		if f.id != active {
			f.start += delta
			continue
		}
		if i >= len(cs) || cs[i].oldStart < f.start || cs[i].oldEnd > f.end {
			return false
		}
		c := cs[i]
		i++
		d := (c.newEnd - c.newStart) - (c.oldEnd - c.oldStart)
		f.start += delta
		removed := -1 // the depth of a field removed: those nested in it go too
		for n+1 < len(s.fields) && s.fields[n+1].depth > f.depth {
			n++
			in := &s.fields[n]
			if removed >= 0 && in.depth > removed {
				in.id = -1
				continue
			}
			removed = -1
			switch {
			case c.oldEnd <= in.start:
				in.start += delta + d
				in.end += delta + d
			case c.oldStart >= in.end:
				in.start += delta
				in.end += delta
			case in.start <= c.oldStart && c.oldEnd <= in.end && (in.start < c.oldStart || c.oldEnd < in.end):
				in.start += delta
				in.end += delta + d
			default:
				in.id, removed = -1, in.depth
			}
		}
		delta += d
	}
	close(0)
	if i != len(cs) {
		return false
	}
	// Deleting adjacent mirrors can leave coincident empty fields,
	// which coalesce just as their carets do.
	s.fields = coalesceFields(slices.DeleteFunc(s.fields, func(f snippetField) bool { return f.id < 0 }))
	return true
}

// Keep adjacent linked fields separate: merging their deletions would
// discard which portion belonged to each placeholder. Other deletions
// retain the editor's usual overlap handling.
func (ed *editor) mergeDeletes(edits []Edit) []Edit {
	if ed.snippet != nil && ed.snippet.accepts(edits) {
		return edits
	}
	return mergeEdits(edits)
}
