package ui

import (
	"context"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/text"
)

// TextServicesOptions opts an input into native text services. Zero options
// leave automatic services off. Language is a language tag such as en-US;
// empty uses App.Locale. Features absent from Availability are skipped.
// Correction and substitutions apply only to freshly typed text at the caret,
// never to pasted text, app values, undo/redo, or IME commits.
type TextServicesOptions struct {
	Language                                  string
	SpellChecking, AutomaticCorrection        bool
	SmartQuotes, SmartDashes, TextReplacement bool
}

// These types are shared with mygo.TextServices for custom editor integrations.
type TextServiceInfo = platform.TextServiceInfo
type TextCheckOptions = platform.TextCheckOptions
type TextCheckResult = platform.TextCheckResult
type TextIssue = platform.TextIssue
type TextIssueKind = platform.TextIssueKind

const (
	TextSpelling    = platform.TextSpelling
	TextCorrection  = platform.TextCorrection
	TextQuote       = platform.TextQuote
	TextDash        = platform.TextDash
	TextReplacement = platform.TextReplacement
)

var ErrTextServicesUnavailable = platform.ErrUnsupported
var ErrTextLanguageUnavailable = platform.ErrTextLanguageUnavailable

// TextServiceStatus is a snapshot of an input's checking state. Issues belong
// to its current committed text and use rune offsets. Error distinguishes
// missing services/dictionaries from a correctly spelled document.
type TextServiceStatus struct {
	Availability        TextServiceInfo
	Options             TextServicesOptions // includes this input's context-menu overrides
	Checking, Truncated bool
	Issues              []TextIssue
	Error               error
}

// TextServiceProvider supplies deterministic services to a Tester. Info,
// Check and LearnWord are called in its owning goroutine; Check's completion
// must also run there, exactly once. A test can retain it to simulate delay.
type TextServiceProvider interface {
	Info(language string) (TextServiceInfo, error)
	Check(context.Context, string, TextCheckOptions, func(TextCheckResult, error))
	LearnWord(word, language string) error
}

type textServicesHost interface {
	textServicesInfo(string) (TextServiceInfo, error)
	checkText(context.Context, string, TextCheckOptions, func(TextCheckResult, error))
	learnWord(string, string) error
}

func (h *windowHost) textServicesInfo(language string) (TextServiceInfo, error) {
	if h.conn.TextServicesInfo == nil {
		return TextServiceInfo{}, ErrTextServicesUnavailable
	}
	return h.conn.TextServicesInfo(language)
}
func (h *windowHost) checkText(ctx context.Context, s string, o TextCheckOptions, done func(TextCheckResult, error)) {
	if h.conn.CheckText == nil {
		done(TextCheckResult{Text: s}, ErrTextServicesUnavailable)
		return
	}
	h.conn.CheckText(ctx, s, o, done)
}
func (h *windowHost) learnWord(word, language string) error {
	if h.conn.LearnWord == nil {
		return ErrTextServicesUnavailable
	}
	return h.conn.LearnWord(word, language)
}
func (h *headless) textServicesInfo(language string) (TextServiceInfo, error) {
	if h.textServices == nil {
		return TextServiceInfo{}, ErrTextServicesUnavailable
	}
	return h.textServices.Info(language)
}
func (h *headless) checkText(ctx context.Context, s string, o TextCheckOptions, done func(TextCheckResult, error)) {
	if h.textServices == nil {
		done(TextCheckResult{Text: s}, ErrTextServicesUnavailable)
		return
	}
	h.textServices.Check(ctx, s, o, done)
}
func (h *headless) learnWord(word, language string) error {
	if h.textServices == nil {
		return ErrTextServicesUnavailable
	}
	return h.textServices.LearnWord(word, language)
}

// SetTextServices installs a deterministic text service provider. By default
// Tester reports unavailable, so tests never depend on a host dictionary.
func (t *Tester) SetTextServices(provider TextServiceProvider) {
	t.h.textServices = provider
	for _, s := range t.rt.states {
		if ed := s.editor; ed != nil && ed.services != nil {
			ed.cancelTextCheck()
			ed.services.infoKnown = false
		}
	}
	t.Frame()
}

type textServicesState struct {
	requested, options                  TextServicesOptions
	info                                TextServiceInfo
	infoKnown                           bool
	err                                 error
	infoError                           error
	frame                               uint64
	pass                                int
	version, serial, generation         uint64
	due                                 time.Time
	cancel                              context.CancelFunc
	checking, checked, force, truncated bool
	issues                              []TextIssue
	ignored                             map[string]bool // actual dictionary language + word; input-local
	autoVersion                         uint64
	autoCaret, autoAnchor, autoStart    int
	pending                             []*textServiceAction
}

// TextServices configures an ordinary TextInput/TextArea (and their bases).
// Its context menu gains suggestions, Ignore/Learn Spelling, Check Spelling,
// and capability-aware toggles. A custom ContextMenu replaces that menu.
// Menu toggles are input-local until the app supplies different options.
// Password, read-only and disabled inputs never send text to a service.
func (e *Element) TextServices(options TextServicesOptions) *Element {
	ed := e.st.editor
	if ed == nil || e.flags&flagEditable == 0 {
		return e
	}
	for _, r := range options.Language {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			panic("ui: text service language must be a language tag")
		}
	}
	s := ed.services
	if s == nil {
		s = &textServicesState{requested: options, options: options}
		ed.services = s
	}
	s.frame, s.pass = e.c.rt.frame, e.c.rt.pass
	if s.requested != options {
		ed.cancelTextCheck()
		s.requested, s.options = options, options
		s.infoKnown = false
		s.generation++
	}
	if !s.infoKnown {
		s.info, s.err = e.c.rt.host.(textServicesHost).textServicesInfo(s.options.Language)
		s.infoError = s.err
		s.info.Languages = slices.Clone(s.info.Languages)
		s.infoKnown = true
	}
	if s.version != ed.buf.version {
		ed.cancelTextCheck()
		s.version = ed.buf.version
		s.due = e.c.now.Add(250 * time.Millisecond)
		if s.autoVersion == ed.buf.version && (s.options.AutomaticCorrection || s.options.SmartQuotes || s.options.SmartDashes || s.options.TextReplacement) {
			// Check substitutions at the typed boundary, before the user
			// advances through another word. Ordinary spelling is debounced.
			s.due = e.c.now
		}
	}
	return e
}

// TextServiceStatus reports availability, effective options, progress and
// current suggestions. Returned slices are copies. Call after TextServices.
func (e *Element) TextServiceStatus() TextServiceStatus {
	if ed := e.st.editor; ed != nil && ed.services != nil {
		s := ed.services
		info := s.info
		info.Languages = slices.Clone(info.Languages)
		issues := slices.Clone(s.issues)
		for i := range issues {
			issues[i].Replacements = slices.Clone(issues[i].Replacements)
		}
		return TextServiceStatus{info, s.options, s.checking, s.truncated, issues, s.err}
	}
	return TextServiceStatus{Error: ErrTextServicesUnavailable}
}

// CheckSpelling requests an immediate spelling check, even with continuous
// SpellChecking off. It does not wait or change text. Configure TextServices
// first; password/read-only/disabled fields and compositions suppress checks.
func (e *Element) CheckSpelling() *Element {
	if ed := e.st.editor; ed != nil && ed.services != nil {
		ed.cancelTextCheck()
		ed.services.force = true
		ed.services.infoKnown = false
	}
	return e
}

func (ed *editor) cancelTextCheck() {
	if s := ed.services; s != nil {
		if s.cancel != nil {
			s.cancel()
			s.cancel = nil
		}
		s.serial++
		s.checking = false
		s.checked = false
		s.issues = nil
		s.truncated = false
	}
}

func (s *textServicesState) checkOptions() TextCheckOptions {
	o := s.options
	return TextCheckOptions{Language: o.Language, Spelling: o.SpellChecking, Correction: o.AutomaticCorrection,
		SmartQuotes: o.SmartQuotes, SmartDashes: o.SmartDashes, TextReplacement: o.TextReplacement}
}

func (ed *editor) commitTextServices(e *Element, invisible bool) {
	s := ed.services
	if s == nil {
		return
	}
	if s.frame != e.c.rt.frame || s.pass != e.c.rt.pass {
		ed.cancelTextCheck()
		clear(s.pending)
		ed.services = nil
		return
	}
	if invisible || ed.password || ed.readOnly || e.IsDisabled() || ed.compose != "" {
		ed.cancelTextCheck()
		s.autoVersion = 0
		s.force = false
		return
	}
	if s.force && !s.infoKnown {
		s.info, s.err = e.c.rt.host.(textServicesHost).textServicesInfo(s.options.Language)
		s.info.Languages = slices.Clone(s.info.Languages)
		s.infoError = s.err
		s.infoKnown = true
	}
	if s.infoError != nil || s.checking || s.checked && !s.force {
		return
	}
	o := s.checkOptions()
	if s.force {
		o.Spelling = true
		s.autoVersion = 0
	}
	if !o.Spelling && !o.Correction && !o.SmartQuotes && !o.SmartDashes && !o.TextReplacement {
		return
	}
	if !s.force && e.c.now.Before(s.due) {
		e.c.After(s.due.Sub(e.c.now))
		return
	}
	s.force = false
	s.checking = true
	s.err = nil
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	serial, version := s.serial, ed.buf.version
	value := ed.buf.s
	rt, id := e.c.rt, e.id
	rt.host.(textServicesHost).checkText(ctx, value, o, func(result TextCheckResult, err error) {
		st := rt.states[id]
		if ctx.Err() != nil || st == nil || st.editor != ed || ed.services != s || s.serial != serial || ed.buf.version != version || ed.compose != "" || result.Text != value {
			return
		}
		s.cancel = nil
		cancel()
		s.checking = false
		s.checked = true
		s.err = err
		s.truncated = result.Truncated
		if err == nil {
			for _, issue := range result.Issues {
				if issue.Start < 0 || issue.End <= issue.Start || issue.End > ed.buf.n || issue.Original != ed.buf.slice(issue.Start, issue.End) {
					continue
				}
				if s.ignored[ignoreKey(s.info.Language, issue.Original)] {
					continue
				}
				issue.Replacements = slices.Clone(issue.Replacements)
				s.issues = append(s.issues, issue)
			}
			if edits := ed.automaticTextReplacements(); len(edits) > 0 {
				action := &textServiceAction{state: s, generation: s.generation, serial: serial, version: version, edits: edits,
					automatic: true, caret: ed.caret, anchor: ed.anchor}
				ed.queue = append(ed.queue, editEvent{kind: editTextService, service: action})
			}
		}
		s.autoVersion = 0
		// A deterministic provider may reply synchronously during commit.
		// Ask for the following frame even while this frame is being built.
		rt.changed()
	})
}

func ignoreKey(language, word string) string { return language + "\x00" + word }

func (ed *editor) noteTextServiceTyping(typed string, suppressed bool) {
	s := ed.services
	if s == nil {
		return
	}
	s.autoVersion = 0
	if suppressed || utf8.RuneCountInString(typed) != 1 || ed.readOnly || ed.password {
		return
	}
	r, _ := utf8.DecodeRuneInString(typed)
	if !unicode.IsSpace(r) && !unicode.IsPunct(r) {
		return
	}
	s.autoVersion = ed.buf.version
	s.autoCaret, s.autoAnchor, s.autoStart = ed.caret, ed.anchor, max(0, ed.caret-64)
}

type textReplacement struct {
	issue TextIssue
	text  string
}

func (ed *editor) automaticTextReplacements() []textReplacement {
	s := ed.services
	if s.autoVersion != ed.buf.version || ed.caret != s.autoCaret || ed.anchor != s.autoAnchor {
		return nil
	}
	var edits []textReplacement
	for _, issue := range s.issues {
		if issue.Start < s.autoStart || issue.End != s.autoCaret && issue.End != s.autoCaret-1 || len(issue.Replacements) == 0 || issue.Original == issue.Replacements[0] {
			continue
		}
		o := s.options
		if !(issue.Kind == TextCorrection && o.AutomaticCorrection || issue.Kind == TextQuote && o.SmartQuotes || issue.Kind == TextDash && o.SmartDashes || issue.Kind == TextReplacement && o.TextReplacement) {
			continue
		}
		edits = append(edits, textReplacement{issue, issue.Replacements[0]})
	}
	return edits
}

type textServiceAction struct {
	state                       *textServicesState
	generation, serial, version uint64
	name                        string
	word                        string
	edits                       []textReplacement
	automatic                   bool
	caret, anchor               int
}

func (ed *editor) textServiceAction(c *Context, a *textServiceAction) {
	s := ed.services
	if a == nil || s == nil || s != a.state || s.generation != a.generation || ed.readOnly || ed.password || ed.compose != "" {
		return
	}
	if len(a.edits) > 0 {
		if a.automatic && (ed.caret != a.caret || ed.anchor != a.anchor) {
			return
		}
		if s.serial == a.serial && ed.buf.version == a.version {
			ed.applyTextReplacements(a.edits)
		}
		return
	}
	switch a.name {
	case "check":
		ed.cancelTextCheck()
		s.force = true
		s.infoKnown = false
	case "ignore", "learn":
		if s.serial != a.serial || ed.buf.version != a.version {
			return
		}
		if a.name == "learn" {
			if err := c.rt.host.(textServicesHost).learnWord(a.word, s.info.Language); err != nil {
				s.err = err
				return
			}
		}
		if s.ignored == nil {
			s.ignored = map[string]bool{}
		}
		s.ignored[ignoreKey(s.info.Language, a.word)] = true
		ed.cancelTextCheck()
		s.force = true
	case "spelling":
		s.options.SpellChecking = !s.options.SpellChecking
	case "correction":
		s.options.AutomaticCorrection = !s.options.AutomaticCorrection
	case "quotes":
		s.options.SmartQuotes = !s.options.SmartQuotes
	case "dashes":
		s.options.SmartDashes = !s.options.SmartDashes
	case "replacement":
		s.options.TextReplacement = !s.options.TextReplacement
	default:
		return
	}
	if a.name != "check" && a.name != "ignore" && a.name != "learn" {
		ed.cancelTextCheck()
		s.generation++
		s.autoVersion = 0
		s.due = c.now
	}
}

// Apply queued choices after the complete view configured language, options,
// read-only/disabled state and visibility. A next build pass then observes
// Changed and the updated value, as for ordinary text input. Virtualized rows
// use the same hook after their late build (lateInput).
func (rt *engine) consumeTextServiceActions() {
	if !rt.hasTextServiceActions {
		return
	}
	rt.hasTextServiceActions = false
	for i := 0; i < rt.c.used; i++ {
		e := &rt.c.chunks[i/chunkSize][i%chunkSize]
		if e.inputValue == nil || e.st == nil || e.st.editor == nil {
			continue
		}
		ed := e.st.editor
		s := ed.services
		if s == nil || len(s.pending) == 0 {
			continue
		}
		pending := s.pending
		s.pending = nil
		hidden := false
		for parent := e; parent != nil; parent = parent.parent {
			if parent.flags&(flagInvisible|flagInert) != 0 {
				hidden = true
				break
			}
		}
		if !hidden && !e.IsDisabled() && !ed.readOnly && !ed.password && s.frame == rt.frame && s.pass == rt.pass {
			version := ed.buf.version
			for _, action := range pending {
				ed.textServiceAction(e.c, action)
			}
			if ed.buf.version != version {
				if ed.buf.s != *e.inputValue {
					e.st.changed = true
				}
				*e.inputValue = ed.buf.s
				ed.cancelTextCheck()
				s.version = ed.buf.version
				s.due = e.c.now.Add(250 * time.Millisecond)
			}
			rt.consumed = true
		}
		clear(pending)
		s.pending = pending[:0]
	}
}

// Replacements have their own undo step, including directional selection.
// Validate the entire transaction before changing even one range.
func (ed *editor) applyTextReplacements(edits []textReplacement) bool {
	if ed.readOnly || ed.password || ed.compose != "" {
		return false
	}
	edits = slices.Clone(edits)
	slices.SortFunc(edits, func(a, b textReplacement) int { return a.issue.Start - b.issue.Start })
	last := -1
	changed := false
	for _, edit := range edits {
		i := edit.issue
		if i.Start < 0 || i.End <= i.Start || i.End > ed.buf.n || i.Start < last || ed.buf.slice(i.Start, i.End) != i.Original || !utf8.ValidString(edit.text) || strings.ContainsRune(edit.text, 0) {
			return false
		}
		// A service cannot split a grapheme, even if its scalar offsets are valid.
		if i.Start > 0 && ed.graphemes.next(&ed.buf, i.Start-1) != i.Start || ed.graphemes.next(&ed.buf, i.End-1) != i.End {
			return false
		}
		last = i.End
		changed = changed || i.Original != edit.text
	}
	if !changed {
		return false
	}
	ed.record(false)
	caret, anchor := ed.caret, ed.anchor
	for i := len(edits) - 1; i >= 0; i-- {
		edit := edits[i]
		value := edit.text
		if !ed.multiline {
			value = strings.NewReplacer("\r", " ", "\n", " ").Replace(value)
		}
		start, end, n := edit.issue.Start, edit.issue.End, utf8.RuneCountInString(value)
		translate := func(at int) int {
			if at >= end {
				return at + n - (end - start)
			}
			if at > start {
				return start + n
			}
			return at
		}
		caret, anchor = translate(caret), translate(anchor)
		ed.replace(start, end, value)
	}
	ed.caret, ed.anchor = caret, anchor
	step := &ed.undo[len(ed.undo)-1]
	step.caretAfter, step.anchorAfter = caret, anchor
	ed.coalesce = false
	ed.hasDesired = false
	if ed.area != nil {
		ed.area.reveal = true
	}
	return true
}

func (ed *editor) paintTextIssues(p *Painter, l *text.Layout, ox, oy float32, start, end int, color Color) {
	s := ed.services
	if s == nil || ed.password || ed.compose != "" || s.version != ed.buf.version {
		return
	}
	for _, issue := range s.issues {
		if issue.Kind != TextSpelling && issue.Kind != TextCorrection || issue.Start >= end || issue.End <= start {
			continue
		}
		for _, r := range l.Selection(max(issue.Start, start)-start, min(issue.End, end)-start) {
			// Dotted underlines remain separate from the IME's solid underline.
			for x := float32(0); x < r.W; x += 4 {
				p.Fill(Rect{ox + r.X + x, oy + r.Y + r.H - 1, min(2, r.W-x), 1}, color, 0)
			}
		}
	}
}
