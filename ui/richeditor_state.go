package ui

import (
	"sync"
	"time"
)

// RichFormat is a character-formatting command for RichEditorState.Format.
type RichFormat uint8

const (
	FormatBold RichFormat = iota
	FormatItalic
	FormatUnderline
	FormatStrikethrough
	FormatClear
)

type richSnapshot struct {
	document  RichDocument
	selection TextSelection
	typing    RichStyle
	typingSet bool
}

type richUndoStep struct{ before, after richSnapshot }

// RichEditorState owns a rich document, selection and undo history. Its zero
// value edits an empty document. Methods are safe from any goroutine and ask
// a mounted RichTextEditor to redraw. Mount a state in only one editor at a
// time. SetDocument starts a new document; other editing and formatting
// commands participate in undo. IME previews never change Document.
type RichEditorState struct {
	mu      sync.Mutex
	current richSnapshot
	undo    []richUndoStep
	redo    []richUndoStep
	version uint64
	wake    func()
	owner   *editor // main-thread editor receiving invalidations
	depth   int
	group   bool
	typing  bool
	last    time.Time
}

// NewRichEditorState starts editing d with the caret at its end.
func NewRichEditorState(d RichDocument) *RichEditorState {
	return &RichEditorState{current: richSnapshot{document: d, selection: TextSelection{d.n, d.n}}}
}

// Document returns an immutable snapshot of the committed document.
func (s *RichEditorState) Document() RichDocument {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current.document
}

// Selection returns the committed selection, including its direction.
func (s *RichEditorState) Selection() TextSelection {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current.selection
}

// SetDocument loads d, clears undo/redo and cancels any composition.
func (s *RichEditorState) SetDocument(d RichDocument) {
	s.update(func() {
		s.current = richSnapshot{document: d, selection: TextSelection{d.n, d.n}}
		s.undo, s.redo = nil, nil
		s.depth, s.group, s.typing = 0, false, false
	})
}

func normalizedSelection(d RichDocument, sel TextSelection) TextSelection {
	r := d.NormalizeRange(sel.Range())
	if sel.Anchor > sel.Caret {
		return TextSelection{r.End, r.Start}
	}
	return TextSelection{r.Start, r.End}
}

// Select sets the selection, snapping to complete graphemes. A movement
// breaks automatic typing coalescing and adopts the style at the caret.
func (s *RichEditorState) Select(sel TextSelection) {
	s.update(func() {
		s.current.selection = normalizedSelection(s.current.document, sel)
		s.current.typingSet, s.typing = false, false
	})
}

func (s *RichEditorState) update(fn func()) {
	s.mu.Lock()
	fn()
	s.version++
	wake := s.wake
	s.mu.Unlock()
	if wake != nil {
		wake()
	}
}

func (s *RichEditorState) record(typing bool) {
	now := time.Now()
	coalesce := s.depth > 0 && s.group || typing && s.typing && now.Sub(s.last) < time.Second
	if !coalesce || len(s.undo) == 0 {
		s.undo = append(s.undo, richUndoStep{before: s.current})
		if len(s.undo) > 200 {
			copy(s.undo, s.undo[len(s.undo)-200:])
			s.undo = s.undo[:200]
		}
	}
	if s.depth > 0 {
		s.group = true
	}
	s.redo = nil
	s.typing, s.last = typing, now
}

func (s *RichEditorState) finish() {
	if len(s.undo) > 0 {
		s.undo[len(s.undo)-1].after = s.current
	}
}

func (snapshot richSnapshot) typingStyle() RichStyle {
	if snapshot.typingSet {
		return snapshot.typing
	}
	if r := snapshot.selection.Range(); r.Start != r.End {
		return snapshot.document.StyleAt(r.Start)
	}
	return snapshot.document.StyleAt(max(snapshot.selection.Caret-1, 0))
}

func (s *RichEditorState) typingStyle() RichStyle { return s.current.typingStyle() }

// TypingStyle returns the style that newly typed text will receive.
func (s *RichEditorState) TypingStyle() RichStyle {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.typingStyle()
}

func (s *RichEditorState) replace(fragment RichDocument, typing bool) {
	r := s.current.document.NormalizeRange(s.current.selection.Range())
	if r.Start == r.End && fragment.n == 0 {
		return
	}
	s.record(typing)
	s.current.document = s.current.document.Replace(r, fragment)
	i := s.current.document.snap(r.Start+fragment.n, true)
	s.current.selection = TextSelection{i, i}
	s.finish()
}

// ReplaceSelection inserts a styled fragment as one undo step.
func (s *RichEditorState) ReplaceSelection(fragment RichDocument) {
	s.update(func() { s.replace(fragment, false) })
}

func (s *RichEditorState) textFragment(text string) RichDocument {
	f := NewRichDocument(text)
	f = f.WithStyle(TextRange{0, f.n}, s.typingStyle())
	p := s.current.document.paragraphAt(s.current.selection.Range().Start)
	for i := range f.paragraphs {
		f.paragraphs[i] = s.current.document.paragraph(p)
	}
	return f
}

// Insert replaces the selection with text in TypingStyle, as one undo step.
// Keystrokes from the editor coalesce automatically until a movement,
// formatting command, explicit group boundary or a one-second pause.
func (s *RichEditorState) Insert(text string) {
	s.update(func() { s.replace(s.textFragment(text), false) })
}

// SetStyle sets the selection's complete character style, or the typing style
// at a collapsed selection. Format and SetLink preserve unrelated attributes.
func (s *RichEditorState) SetStyle(style RichStyle) {
	style = validRichStyle(style)
	s.update(func() {
		s.record(false)
		if r := s.current.selection.Range(); r.Start == r.End {
			s.current.typing, s.current.typingSet = style, true
		} else {
			s.current.document = s.current.document.WithStyle(r, style)
		}
		s.finish()
	})
}

func hasRichFormat(s RichStyle, command RichFormat) bool {
	switch command {
	case FormatBold:
		return s.Weight >= 600
	case FormatItalic:
		return s.Italic
	case FormatUnderline:
		return s.Underline
	case FormatStrikethrough:
		return s.Strikethrough
	}
	return false
}

func (s *RichEditorState) transform(fn func(RichStyle) RichStyle) {
	s.record(false)
	r := s.current.selection.Range()
	if r.Start == r.End {
		s.current.typing = validRichStyle(fn(s.typingStyle()))
		s.current.typingSet = true
	} else {
		s.current.document = s.current.document.TransformStyle(r, fn)
	}
	s.finish()
}

func (s *RichEditorState) format(command RichFormat) {
	if command > FormatClear {
		return
	}
	r := s.current.selection.Range()
	on := hasRichFormat(s.typingStyle(), command)
	if r.Start != r.End {
		on = true
		for _, run := range s.current.document.runs {
			if run.Range.End > r.Start && run.Range.Start < r.End && !hasRichFormat(run.Style, command) {
				on = false
			}
		}
	}
	s.transform(func(style RichStyle) RichStyle {
		switch command {
		case FormatBold:
			style.Weight = 700
			if on {
				style.Weight = 400
			}
		case FormatItalic:
			style.Italic = !on
		case FormatUnderline:
			style.Underline = !on
		case FormatStrikethrough:
			style.Strikethrough = !on
		case FormatClear:
			style = RichStyle{}
		}
		return style
	})
}

// Format toggles an attribute: a mixed selection turns it on throughout;
// an entirely formatted selection turns it off. FormatClear clears all
// character attributes, including links. Paragraph styles are unaffected.
func (s *RichEditorState) Format(command RichFormat) { s.update(func() { s.format(command) }) }

// SetLink sets the selection's link, or the link for new text. An empty or
// unsupported URL removes the link. Supported schemes are http, https, mailto.
func (s *RichEditorState) SetLink(url string) {
	s.update(func() { s.transform(func(style RichStyle) RichStyle { style.Link = url; return style }) })
}

// SetParagraphStyle styles the paragraphs touched by the selection.
func (s *RichEditorState) SetParagraphStyle(style ParagraphStyle) {
	s.update(func() {
		s.record(false)
		s.current.document = s.current.document.WithParagraphStyle(s.current.selection.Range(), style)
		s.finish()
	})
}

// BeginUndoGroup groups subsequent edits and formatting into a single step.
// Groups may nest and must be balanced by EndUndoGroup. It does not hold a
// lock between calls, so a toolbar can issue several ordinary commands.
func (s *RichEditorState) BeginUndoGroup() {
	s.mu.Lock()
	if s.depth == 0 {
		s.group, s.typing = false, false
	}
	s.depth++
	s.mu.Unlock()
}

// EndUndoGroup ends a group and breaks automatic typing coalescing.
func (s *RichEditorState) EndUndoGroup() {
	s.mu.Lock()
	if s.depth == 0 {
		s.mu.Unlock()
		panic("ui: EndUndoGroup without BeginUndoGroup")
	}
	s.depth--
	if s.depth == 0 {
		s.group, s.typing = false, false
	}
	s.mu.Unlock()
}

func (s *RichEditorState) takeBack(redo bool) bool {
	s.depth, s.group, s.typing = 0, false, false
	from, to := &s.undo, &s.redo
	if redo {
		from, to = to, from
	}
	if len(*from) == 0 {
		return false
	}
	step := (*from)[len(*from)-1]
	(*from)[len(*from)-1] = richUndoStep{}
	*from = (*from)[:len(*from)-1]
	s.current = step.before
	if redo {
		s.current = step.after
	}
	*to = append(*to, step)
	return true
}

// Undo restores the document, selection and typing style before the last
// group. It ends any open explicit group. It reports whether a step existed.
func (s *RichEditorState) Undo() (changed bool) {
	s.update(func() { changed = s.takeBack(false) })
	return
}

// Redo restores the next undone group, including its selection.
func (s *RichEditorState) Redo() (changed bool) {
	s.update(func() { changed = s.takeBack(true) })
	return
}

// CanUndo and CanRedo report whether the corresponding history has a step.
func (s *RichEditorState) CanUndo() bool { s.mu.Lock(); defer s.mu.Unlock(); return len(s.undo) > 0 }
func (s *RichEditorState) CanRedo() bool { s.mu.Lock(); defer s.mu.Unlock(); return len(s.redo) > 0 }
