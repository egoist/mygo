package ui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/internal/textcheck"
)

type pendingTextCheck struct {
	ctx  context.Context
	text string
	opts TextCheckOptions
	done func(TextCheckResult, error)
}
type testTextService struct {
	info     TextServiceInfo
	err      error
	delay    bool
	requests []pendingTextCheck
	learned  []string
	issues   func(string, TextCheckOptions) []TextIssue
}

func (p *testTextService) Info(language string) (TextServiceInfo, error) {
	info := p.info
	if language != "" {
		info.Language = language
	}
	return info, p.err
}
func (p *testTextService) Check(ctx context.Context, s string, o TextCheckOptions, done func(TextCheckResult, error)) {
	r := pendingTextCheck{ctx, s, o, done}
	p.requests = append(p.requests, r)
	if !p.delay {
		p.reply(len(p.requests) - 1)
	}
}
func (p *testTextService) reply(i int) {
	r := p.requests[i]
	issues := p.issues(r.text, r.opts)
	r.done(TextCheckResult{Text: r.text, Info: p.info, Issues: issues}, nil)
}
func (p *testTextService) LearnWord(word, language string) error {
	p.learned = append(p.learned, language+":"+word)
	return nil
}
func textServiceFixture() *testTextService {
	p := &testTextService{info: TextServiceInfo{Provider: "test", Language: "en-US", Languages: []string{"en-US", "fr-FR"}, Spelling: true, Suggestions: true, Correction: true, SmartQuotes: true, SmartDashes: true, TextReplacement: true, LearnWord: true}}
	p.issues = func(s string, o TextCheckOptions) []TextIssue {
		var issues []TextIssue
		for _, r := range textcheck.Words(s) {
			word := textcheck.Slice(s, r[0], r[1])
			if word == "teh" || word == "mispeling" {
				kind := TextSpelling
				if o.Correction {
					kind = TextCorrection
				}
				replacement := "the"
				if word == "mispeling" {
					replacement = "misspelling"
				}
				issues = append(issues, TextIssue{Start: r[0], End: r[1], Kind: kind, Original: word, Replacements: []string{replacement}})
			}
			if word == "brb" && o.TextReplacement {
				issues = append(issues, TextIssue{Start: r[0], End: r[1], Kind: TextReplacement, Original: word, Replacements: []string{"be right back"}})
			}
		}
		return issues
	}
	return p
}
func inputEditor(t *testing.T, tt *Tester) *editor {
	t.Helper()
	for _, s := range tt.rt.states {
		if s.editor != nil && s.flags&flagEditable != 0 {
			return s.editor
		}
	}
	t.Fatal("no input")
	return nil
}
func advanceTextCheck(tt *Tester) {
	now := tt.rt.now().Add(300 * time.Millisecond)
	tt.rt.clock = func() time.Time { return now }
	tt.Frame()
}

func TestTextServicesSuggestionsAndUndo(t *testing.T) {
	for _, multi := range []bool{false, true} {
		t.Run(map[bool]string{false: "input", true: "area"}[multi], func(t *testing.T) {
			value := "😀 teh café"
			var status TextServiceStatus
			tt := NewTester(func(c *Context) {
				var e *Element
				if multi {
					e = TextArea(c, &value).Height(90)
				} else {
					e = TextInput(c, &value)
				}
				status = e.TextServices(TextServicesOptions{SpellChecking: true, Language: "en-US"}).TextServiceStatus()
			}, 360, 120)
			t.Cleanup(tt.rt.close)
			p := textServiceFixture()
			tt.SetTextServices(p)
			tt.Key(0, KeyTab)
			advanceTextCheck(tt)
			if len(status.Issues) != 1 || status.Issues[0].Start != 2 || status.Issues[0].End != 5 {
				t.Fatalf("issues %+v", status)
			}
			status.Issues[0].Replacements[0] = "corrupted"
			status.Availability.Languages[0] = "corrupted"
			ed := inputEditor(t, tt)
			ed.move(5, false)
			tt.Key(0, KeyContextMenu)
			if err := tt.ChooseMenuItem("Spelling Suggestions", "the"); err != nil {
				t.Fatal(err)
			}
			if value != "😀 the café" || ed.caret != 5 || ed.anchor != 5 {
				t.Fatalf("replacement %q @%d:%d", value, ed.anchor, ed.caret)
			}
			tt.Key(Cmd, KeyZ)
			if value != "😀 teh café" {
				t.Fatal("undo", value)
			}
			tt.Key(Cmd|Shift, KeyZ)
			if value != "😀 the café" {
				t.Fatal("redo", value)
			}
		})
	}
}

func TestTextReplacementRangesAndSelection(t *testing.T) {
	ed := newEditor()
	ed.multiline = true
	ed.setText("😀 teh\nmispeling café")
	ed.anchor, ed.caret = 19, 2 // backward selection
	edits := []textReplacement{
		{TextIssue{Start: 2, End: 5, Original: "teh"}, "the"},
		{TextIssue{Start: 6, End: 15, Original: "mispeling"}, "misspelling"},
	}
	if !ed.applyTextReplacements(edits) || ed.String() != "😀 the\nmisspelling café" || ed.anchor != 21 || ed.caret != 2 {
		t.Fatalf("%q @ %d:%d", ed.String(), ed.anchor, ed.caret)
	}
	if len(ed.undo) != 1 {
		t.Fatal("transaction not grouped")
	}
	ed.takeBack(false)
	if ed.String() != "😀 teh\nmispeling café" || ed.anchor != 19 || ed.caret != 2 {
		t.Fatal("undo selection")
	}
	ed.takeBack(true)
	if ed.anchor != 21 || ed.caret != 2 {
		t.Fatal("redo selection")
	}
	for _, bad := range [][]textReplacement{
		{{TextIssue{Start: -1, End: 3, Original: ""}, "bad"}},
		{{TextIssue{Start: 2, End: 5, Original: "wrong"}, "bad"}},
		{{TextIssue{Start: 2, End: 5, Original: "the"}, "a"}, {TextIssue{Start: 3, End: 5, Original: "he"}, "b"}},
		{{TextIssue{Start: 2, End: 5, Original: "the"}, "\x00"}},
	} {
		before := ed.String()
		steps := len(ed.undo)
		if ed.applyTextReplacements(bad) || ed.String() != before || len(ed.undo) != steps {
			t.Fatal("invalid transaction changed text")
		}
	}
	ed.setText("e\u0301 teh")
	if ed.applyTextReplacements([]textReplacement{{TextIssue{Start: 0, End: 1, Original: "e"}, "x"}}) {
		t.Fatal("split grapheme")
	}
	ed.multiline = false
	ed.setText("teh")
	if !ed.applyTextReplacements([]textReplacement{{TextIssue{Start: 0, End: 3, Original: "teh"}, "a\nb"}}) || ed.String() != "a b" {
		t.Fatal("single line newline")
	}
}

func TestTextServicesAutomaticReplacementUndoGrouping(t *testing.T) {
	for _, tc := range []struct {
		before, after string
		options       TextServicesOptions
	}{
		{"teh", "the ", TextServicesOptions{SpellChecking: true, AutomaticCorrection: true}},
		{"brb", "be right back ", TextServicesOptions{TextReplacement: true}},
	} {
		t.Run(tc.before, func(t *testing.T) {
			value := ""
			tt := NewTester(func(c *Context) { TextInput(c, &value).TextServices(tc.options) }, 400, 90)
			t.Cleanup(tt.rt.close)
			p := textServiceFixture()
			tt.SetTextServices(p)
			tt.Key(0, KeyTab)
			tt.Type(tc.before)
			tt.Type(" ")
			advanceTextCheck(tt)
			if value != tc.after {
				t.Fatalf("automatic: %q", value)
			}
			tt.Key(Cmd, KeyZ)
			if value != tc.before+" " {
				t.Fatalf("undo correction removed typing: %q", value)
			}
			advanceTextCheck(tt)
			if value != tc.before+" " {
				t.Fatal("undo was autocorrected again")
			}
			tt.Key(Cmd|Shift, KeyZ)
			if value != tc.after {
				t.Fatal("redo", value)
			}
			tt.Key(Cmd, KeyZ)
			tt.Type("x")
			tt.Key(Cmd|Shift, KeyZ)
			if value != tc.before+" x" {
				t.Fatal("typing did not clear redo", value)
			}
		})
	}
}

func TestTextServicesStaleResultsAndMenu(t *testing.T) {
	value := "teh"
	options := TextServicesOptions{Language: "en-US", SpellChecking: true, AutomaticCorrection: true}
	tt := NewTester(func(c *Context) { TextInput(c, &value).TextServices(options) }, 360, 90)
	t.Cleanup(tt.rt.close)
	p := textServiceFixture()
	p.delay = true
	tt.SetTextServices(p)
	tt.Key(0, KeyTab)
	advanceTextCheck(tt)
	first := p.requests[0]
	value = "other"
	tt.Frame()
	value = "teh"
	tt.Frame()
	p.reply(0)
	tt.Frame()
	if first.ctx.Err() == nil || len(inputEditor(t, tt).services.issues) != 0 || value != "teh" {
		t.Fatal("stale ABA result was accepted")
	}
	advanceTextCheck(tt)
	second := len(p.requests) - 1
	options.Language = "fr-FR"
	tt.Frame()
	p.reply(second)
	tt.Frame()
	if len(inputEditor(t, tt).services.issues) != 0 {
		t.Fatal("old language result accepted")
	}
	advanceTextCheck(tt)
	p.reply(len(p.requests) - 1)
	tt.Frame()
	tt.Key(0, KeyContextMenu)
	chosen := tt.h.chosen
	var suggestion int
	for _, item := range tt.h.menu.Items {
		if item.Label == "Spelling Suggestions" {
			suggestion = item.Submenu.Items[0].ID
		}
	}
	if suggestion == 0 {
		t.Fatal("no suggestion menu")
	}
	tt.CloseMenu()
	options.AutomaticCorrection = false
	tt.Frame()
	chosen(suggestion)
	tt.Frame()
	if value != "teh" {
		t.Fatal("stale menu applied after options changed")
	}
}

func TestTextServicesCompositionAndPrivacy(t *testing.T) {
	value := "teh"
	options := TextServicesOptions{SpellChecking: true, AutomaticCorrection: true}
	var composing bool
	tt := NewTester(func(c *Context) { e := TextInput(c, &value).TextServices(options); composing = e.Composing() }, 360, 90)
	t.Cleanup(tt.rt.close)
	p := textServiceFixture()
	p.delay = true
	tt.SetTextServices(p)
	tt.Key(0, KeyTab)
	inputEditor(t, tt).move(3, false)
	advanceTextCheck(tt)
	tt.Compose("に", 1)
	p.reply(0)
	tt.Frame()
	if !composing || len(inputEditor(t, tt).services.issues) != 0 || p.requests[0].ctx.Err() == nil {
		t.Fatal("composition accepted suggestions")
	}
	tt.Compose("", 0)
	tt.Type(" ")
	advanceTextCheck(tt)
	p.reply(len(p.requests) - 1)
	tt.Frame()
	if value != "teh " {
		t.Fatal("IME commit autocorrected", value)
	}
	for _, privacy := range []string{"password", "readonly", "disabled", "parent disabled"} {
		t.Run(privacy, func(t *testing.T) {
			secret := "teh"
			x := NewTester(func(c *Context) {
				build := func() {
					e := TextInput(c, &secret).TextServices(options).CheckSpelling()
					switch privacy {
					case "password":
						e.Password()
					case "readonly":
						e.ReadOnly(true)
					case "disabled":
						e.Disabled(true)
					}
				}
				if privacy == "parent disabled" {
					Column(c).Disabled(true).Children(build)
				} else {
					build()
				}
			}, 300, 90)
			t.Cleanup(x.rt.close)
			service := textServiceFixture()
			x.SetTextServices(service)
			advanceTextCheck(x)
			if len(service.requests) != 0 {
				t.Fatal("private text sent to service")
			}
		})
	}
}

func TestTextServicesDoNotCorrectPasteOrCaretMovement(t *testing.T) {
	value := ""
	tt := NewTester(func(c *Context) {
		TextInput(c, &value).TextServices(TextServicesOptions{SpellChecking: true, AutomaticCorrection: true})
	}, 360, 90)
	t.Cleanup(tt.rt.close)
	p := textServiceFixture()
	tt.SetTextServices(p)
	tt.Key(0, KeyTab)
	tt.SetClipboard("teh ")
	tt.Key(Cmd, KeyV)
	advanceTextCheck(tt)
	if value != "teh " {
		t.Fatal("paste autocorrected")
	}
	p.delay = true
	value = "teh"
	tt.Frame()
	inputEditor(t, tt).move(3, false)
	tt.Type(" ")
	tt.Key(0, KeyLeft)
	p.reply(len(p.requests) - 1)
	advanceTextCheck(tt)
	if value != "teh " {
		t.Fatal("correction followed a moved caret")
	}
}

func TestTextServicesIgnoreLearnAndSettings(t *testing.T) {
	value := "teh"
	opts := TextServicesOptions{SpellChecking: true, Language: "en-US"}
	tt := NewTester(func(c *Context) { TextInput(c, &value).TextServices(opts) }, 360, 90)
	t.Cleanup(tt.rt.close)
	p := textServiceFixture()
	tt.SetTextServices(p)
	tt.Key(0, KeyTab)
	advanceTextCheck(tt)
	tt.Key(0, KeyContextMenu)
	if err := tt.ChooseMenuItem("Ignore Spelling"); err != nil {
		t.Fatal(err)
	}
	if value != "teh" || len(inputEditor(t, tt).services.issues) != 0 || len(p.learned) != 0 {
		t.Fatal("ignore edited text or persisted")
	}
	opts.Language = "fr-FR"
	tt.Frame()
	advanceTextCheck(tt)
	if len(inputEditor(t, tt).services.issues) != 1 {
		t.Fatal("ignore leaked to another language")
	}
	tt.Key(0, KeyContextMenu)
	if err := tt.ChooseMenuItem("Learn Spelling"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(p.learned, []string{"fr-FR:teh"}) {
		t.Fatal(p.learned)
	}
	tt.Key(0, KeyContextMenu)
	if err := tt.ChooseMenuItem("Spelling and Substitutions", "Smart Quotes"); err != nil {
		t.Fatal(err)
	}
	if !inputEditor(t, tt).services.options.SmartQuotes || opts.SmartQuotes {
		t.Fatal("toggle changed app options")
	}
	tt.Frame()
	if !inputEditor(t, tt).services.options.SmartQuotes {
		t.Fatal("toggle did not survive frame")
	}
	opts.Language = "en-US"
	tt.Frame()
	if inputEditor(t, tt).services.options.SmartQuotes {
		t.Fatal("app options did not reset overrides")
	}
}

func TestTextServicesUnavailableAndDisposal(t *testing.T) {
	value := "teh"
	on := true
	var status TextServiceStatus
	tt := NewTester(func(c *Context) {
		e := TextInput(c, &value)
		if on {
			e.TextServices(TextServicesOptions{SpellChecking: true})
		}
		status = e.TextServiceStatus()
	}, 360, 90)
	t.Cleanup(tt.rt.close)
	if !errors.Is(status.Error, ErrTextServicesUnavailable) || status.Availability.Spelling {
		t.Fatal(status)
	}
	tt.Key(0, KeyTab)
	tt.Key(0, KeyContextMenu)
	if err := tt.ChooseMenuItem("Spelling Unavailable"); err == nil {
		t.Fatal("unavailable action enabled")
	}
	tt.CloseMenu()
	p := textServiceFixture()
	p.info.SmartQuotes = false
	p.delay = true
	tt.SetTextServices(p)
	advanceTextCheck(tt)
	on = false
	tt.Frame()
	p.reply(0)
	tt.Frame()
	if p.requests[0].ctx.Err() == nil || inputEditor(t, tt).services != nil || value != "teh" {
		t.Fatal("removed service retained request")
	}
	on = true
	tt.Frame()
	advanceTextCheck(tt)
	pending := len(p.requests) - 1
	tt.rt.close()
	p.reply(pending)
	if p.requests[pending].ctx.Err() == nil {
		t.Fatal("window close kept checking")
	}
}

func TestTextServicesRenderUnderlines(t *testing.T) {
	value := "teh\nשלום teh"
	enabled := false
	tt := NewTester(func(c *Context) {
		e := TextArea(c, &value).Size(300, 100)
		if enabled {
			e.TextServices(TextServicesOptions{SpellChecking: true})
		}
	}, 320, 120)
	t.Cleanup(tt.rt.close)
	before := slices.Clone(tt.Image().Pix)
	enabled = true
	p := textServiceFixture()
	tt.SetTextServices(p)
	advanceTextCheck(tt)
	if len(inputEditor(t, tt).services.issues) != 2 || slices.Equal(before, tt.Image().Pix) {
		t.Fatal("spell underlines not rendered")
	}
	if !strings.Contains(value, "שלום") {
		t.Fatal("changed bidi text")
	}
}

func TestTextServiceChoiceUsesCompleteFrameConfiguration(t *testing.T) {
	for _, change := range []string{"language", "options", "readonly", "disabled", "hidden", "removed"} {
		t.Run(change, func(t *testing.T) {
			value := "teh"
			opts := TextServicesOptions{SpellChecking: true, Language: "en-US"}
			restricted := false
			changed := 0
			tt := NewTester(func(c *Context) {
				e := TextInput(c, &value)
				if !restricted || change != "removed" {
					e.TextServices(opts)
				}
				if restricted {
					switch change {
					case "readonly":
						e.ReadOnly(true)
					case "disabled":
						e.Disabled(true)
					case "hidden":
						e.Invisible()
					}
				}
				if e.Changed() {
					changed++
				}
			}, 360, 90)
			t.Cleanup(tt.rt.close)
			p := textServiceFixture()
			tt.SetTextServices(p)
			tt.Key(0, KeyTab)
			inputEditor(t, tt).move(3, false)
			advanceTextCheck(tt)
			tt.Key(0, KeyContextMenu)
			chosen := tt.h.chosen
			var id int
			for _, it := range tt.h.menu.Items {
				if it.Label == "Spelling Suggestions" {
					id = it.Submenu.Items[0].ID
				}
			}
			// Both the new configuration and queued choice reach the SAME frame.
			restricted = true
			if change == "language" {
				opts.Language = "fr-FR"
			}
			if change == "options" {
				opts.SpellChecking = false
			}
			chosen(id)
			tt.Frame()
			if value != "teh" || changed != 0 {
				t.Fatalf("choice ignored new %s: %q, changed=%d", change, value, changed)
			}
		})
	}
}

func TestTextServiceAutomaticChoiceRechecksSelection(t *testing.T) {
	value := "teh"
	tt := NewTester(func(c *Context) { TextInput(c, &value).TextServices(TextServicesOptions{AutomaticCorrection: true}) }, 360, 90)
	t.Cleanup(tt.rt.close)
	p := textServiceFixture()
	p.delay = true
	tt.SetTextServices(p)
	tt.Key(0, KeyTab)
	ed := inputEditor(t, tt)
	ed.move(3, false)
	tt.Type(" ")
	advanceTextCheck(tt)
	p.reply(0) // queues the automatic replacement for the following frame
	ed.move(0, false)
	tt.Frame()
	if value != "teh " || ed.caret != 0 {
		t.Fatal("queued correction followed a moved caret")
	}
}

func TestTextServicesAutomaticQuotesAndDashes(t *testing.T) {
	value := ""
	tt := NewTester(func(c *Context) {
		TextInput(c, &value).TextServices(TextServicesOptions{SmartQuotes: true, SmartDashes: true})
	}, 360, 90)
	t.Cleanup(tt.rt.close)
	p := textServiceFixture()
	p.issues = func(s string, o TextCheckOptions) []TextIssue {
		runes := []rune(s)
		if len(runes) > 0 && runes[len(runes)-1] == '"' && o.SmartQuotes {
			return []TextIssue{{Start: len(runes) - 1, End: len(runes), Original: "\"", Kind: TextQuote, Replacements: []string{"“"}}}
		}
		if strings.HasSuffix(s, "--") && o.SmartDashes {
			return []TextIssue{{Start: len(runes) - 2, End: len(runes), Original: "--", Kind: TextDash, Replacements: []string{"—"}}}
		}
		return nil
	}
	tt.SetTextServices(p)
	tt.Key(0, KeyTab)
	tt.Type("\"")
	if value != "“" {
		t.Fatal("quote was not substituted at the typed boundary", value)
	}
	tt.Key(Cmd, KeyZ)
	if value != "\"" {
		t.Fatal("undo did not keep the typed quote")
	}
	value = ""
	tt.Frame()
	inputEditor(t, tt).move(0, false)
	tt.Type("-")
	tt.Type("-")
	if value != "—" {
		t.Fatal("dashes were not substituted", value)
	}
	tt.Key(Cmd, KeyZ)
	if value != "--" {
		t.Fatal("undo did not keep the typed dashes")
	}
}

func TestTextServicesInVirtualizedRows(t *testing.T) {
	var list ListState
	values := []string{"teh", "mispeling", "clean"}
	tt := NewTester(func(c *Context) {
		List(c, &list, len(values), func(i int) {
			Row(c).Height(36).Children(func() {
				TextInput(c, &values[i]).Label(fmt.Sprint("row", i)).TextServices(TextServicesOptions{SpellChecking: true})
			})
		}).Fill()
	}, 360, 160)
	t.Cleanup(tt.rt.close)
	p := textServiceFixture()
	tt.SetTextServices(p)
	advanceTextCheck(tt)
	for _, s := range tt.rt.states {
		if s.editor != nil && s.editor.buf.s == "mispeling" && len(s.editor.services.issues) != 1 {
			t.Fatal("late-built row discarded results")
		}
	}
	if err := tt.Click("row1"); err != nil {
		t.Fatal(err)
	}
	ed := tt.rt.states[tt.rt.focused].editor
	ed.move(ed.buf.n, false)
	tt.Key(0, KeyContextMenu)
	if err := tt.ChooseMenuItem("Spelling Suggestions", "misspelling"); err != nil {
		t.Fatal(err)
	}
	if values[1] != "misspelling" {
		t.Fatal("late-built row did not apply suggestion")
	}
}
