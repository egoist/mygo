package editor

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func newMarkdownTest(t *testing.T, text string) *editorTest {
	t.Helper()
	at := strings.IndexByte(text, '|')
	if at < 0 {
		t.Fatal("no caret")
	}
	st := &State{Buffer: NewBuffer(strings.Replace(text, "|", "", 1)), TabSize: 2}
	st.Buffer.SetLanguage(LanguageNamed("markdown"))
	ct := &editorTest{t: t, st: st}
	ct.tt = ui.NewTester(func(c *ui.Context) {
		View(c, st).Fill().AutoFocus()
	}, 600, 400)
	st.SetSelections(Selection{Anchor: at, Head: at})
	ct.tt.Frame()
	return ct
}

func TestMarkdownNewlineListsAndQuotes(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{"bullet", "- item|", "- item\n- |"},
		{"star", "* item|", "* item\n* |"},
		{"plus", "+ item|", "+ item\n+ |"},
		{"ordered period", "1. item|", "1. item\n2. |"},
		{"ordered parenthesis", "3) item|", "3) item\n4) |"},
		{"unchecked task", "- [ ] item|", "- [ ] item\n- [ ] |"},
		{"checked task", "- [x] item|", "- [x] item\n- [ ] |"},
		{"uppercase checked task", "- [X] item|", "- [X] item\n- [ ] |"},
		{"quote", "> quote|", "> quote\n> |"},
		{"nested quote", "> > item|", "> > item\n> > |"},
		{"empty bullet", "- |", "|"},
		{"empty unchecked task", "- [ ] |", "|"},
		{"empty checked task", "- [x] |", "|"},
		{"empty quote", "> |", "|"},
		{"empty nested quote", "> > |", "|"},
		{"quoted list", "> - item|", "> - item\n> - |"},
		{"quoted ordered list", "> 1. item|", "> 1. item\n> 2. |"},
		{"quoted task list", "> - [x] item|", "> - [x] item\n> - [ ] |"},
		{"empty quoted list", "> - |", "> |"},
		{"mid-line bullet", "- hello| world", "- hello\n- | world"},
		{"mid-line quote", "> hello| world", "> hello\n> | world"},
		{"quote with extra spaces", ">   quote|", ">   quote\n> |"},
		{"quote with extra spaces before list", ">   - parent|", ">   - parent\n>   - |"},
		{"quoted nested list with extra spaces", ">   - parent\n>     - child|", ">   - parent\n>     - child\n>     - |"},
		{"not a list", "text|", "text\n|"},
		{"before marker", "|- item", "\n|- item"},
		{"fenced code", "```\n- item|\n```", "```\n- item\n|\n```"},
		{"four spaces without parent", "    - item|", "    - item\n    |"},
		{"four spaces with parent", "- parent\n    - child|", "- parent\n    - child\n    - |"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ct := newMarkdownTest(t, tt.text)
			ct.key(0, ui.KeyEnter)
			if got := markdownText(t, ct); got != tt.want {
				t.Fatalf("got\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

func TestMarkdownNewlineMultipleCarets(t *testing.T) {
	ct := newEditorTest(t, "markdown", "- one|\n> two|\n- three|")
	ct.key(0, ui.KeyEnter)
	ct.want("- one\n- |\n> two\n> |\n- three\n- |")
}

func TestMarkdownLineQuoteIndent(t *testing.T) {
	tests := []struct {
		text       string
		indent     string
		quote      string
		pad        string
		listIndent string
		continues  string
	}{
		{"  > >  quote", "  ", "> > ", " ", " ", "  > > "},
		{">   - parent", "", "> ", "  ", "  ", ">   - "},
		{">     - child", "", "> ", "    ", "    ", ">     - "},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			p, ok := parseMarkdownLine(tt.text)
			if !ok {
				t.Fatal("not a Markdown line")
			}
			if p.indent != tt.indent || p.quote != tt.quote || p.pad != tt.pad {
				t.Fatalf("parts = (%q, %q, %q), want (%q, %q, %q)", p.indent, p.quote, p.pad, tt.indent, tt.quote, tt.pad)
			}
			if got := p.listIndent(); got != tt.listIndent {
				t.Fatalf("listIndent() = %q, want %q", got, tt.listIndent)
			}
			if got := p.continuation(); got != tt.continues {
				t.Fatalf("continuation() = %q, want %q", got, tt.continues)
			}
		})
	}
}

func TestMarkdownQuoteIndentWidth(t *testing.T) {
	tests := []struct {
		text string
		want int
	}{
		{">   - parent", 2},
		{">     - child", 4},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			p, ok := parseMarkdownLine(tt.text)
			if !ok {
				t.Fatal("not a Markdown line")
			}
			if got := indentWidth(p.listIndent(), 2); got != tt.want {
				t.Fatalf("indentWidth(p.listIndent(), 2) = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestMarkdownNewlineIsLanguageSpecific(t *testing.T) {
	ct := newEditorTest(t, "", "- item|")
	ct.key(0, ui.KeyEnter)
	ct.want("- item\n|")
}

func markdownText(t *testing.T, ct *editorTest) string {
	t.Helper()
	at := ct.st.Selections()[0].Head
	text := ct.st.Buffer.String()
	return text[:at] + "|" + text[at:]
}

func TestMarkdownIndentation(t *testing.T) {
	tests := []struct {
		name string
		text string
		mods ui.Modifiers
		want string
	}{
		{"two spaces", "  - item|", ui.Shift, "- item|"},
		{"one space", " - item|", ui.Shift, "- item|"},
		{"tab", "\t- item|", ui.Shift, "- item|"},
		{"space and tab", " \t- item|", ui.Shift, "\t- item|"},
		{"no indent", "- item|", ui.Shift, "- item|"},
		{"tab at line start", "|- item", 0, "  |- item"},
		{"tab after marker", "- child|", 0, "- child |"},
		{"selected lines", "[  - one]\n[  - two]", ui.Shift, "[- one]\n[- two]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ct := newEditorTest(t, "markdown", tt.text)
			ct.key(tt.mods, ui.KeyTab)
			ct.want(tt.want)
		})
	}
}

func TestMarkdownBacktickAutoClose(t *testing.T) {
	ct := newEditorTest(t, "markdown", "|")
	ct.typ("`")
	ct.want("`|`")
}
