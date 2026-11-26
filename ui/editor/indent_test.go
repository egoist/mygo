package editor

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestDetectIndent(t *testing.T) {
	cases := []struct {
		name string
		text string
		want Indentation
		ok   bool
	}{
		{
			name: "tabs",
			text: "package main\n\nfunc main() {\n\tif x {\n\t\ty()\n\t}\n}\n",
			want: Indentation{Style: IndentTabs},
			ok:   true,
		},
		{
			name: "two spaces",
			text: "function f() {\n  if (x) {\n    y()\n  }\n}\n",
			want: Indentation{Style: IndentSpaces, Width: 2},
			ok:   true,
		},
		{
			name: "four spaces",
			text: "func f() {\n    if x {\n        y()\n    }\n}\n",
			want: Indentation{Style: IndentSpaces, Width: 4},
			ok:   true,
		},
		{
			name: "eight spaces",
			text: "a\n        b\n                c\n",
			want: Indentation{Style: IndentSpaces, Width: 8},
			ok:   true,
		},
		{
			name: "three spaces",
			text: "a\n   b\n      c\n",
			want: Indentation{Style: IndentSpaces, Width: 3},
			ok:   true,
		},
		{
			name: "a text that starts indented",
			text: "  a\n    b\n",
			want: Indentation{Style: IndentSpaces, Width: 2},
			ok:   true,
		},
		{
			name: "tabs outnumber spaces",
			text: "\ta\n\tb\n  c\n",
			want: Indentation{Style: IndentTabs},
			ok:   true,
		},
		{
			name: "a tie is spaces",
			text: "\ta\n  b\n",
			want: Indentation{Style: IndentSpaces, Width: 2},
			ok:   true,
		},
		{
			name: "a tab in the white space of a line is a tab",
			text: " \ta\n \tb\n",
			want: Indentation{Style: IndentTabs},
			ok:   true,
		},
		{
			name: "blank lines and white space alone indents nothing",
			text: "a\n\n   \n\t\n  b\n",
			want: Indentation{Style: IndentSpaces, Width: 2},
			ok:   true,
		},
		{
			name: "CRLF",
			text: "a\r\n\tb\r\n\t\tc\r\n",
			want: Indentation{Style: IndentTabs},
			ok:   true,
		},
		{
			// The aligned continuation takes a step of 2, which the
			// levels take less often than their 4.
			name: "an aligned continuation does not set the level",
			text: "func f() {\n    call(a,\n      b)\n    if x {\n        y()\n    }\n}\n",
			want: Indentation{Style: IndentSpaces, Width: 4},
			ok:   true,
		},
		{
			name: "no indented line",
			text: "a\nb\nc\n",
			want: Indentation{},
			ok:   false,
		},
		{
			name: "no text",
			text: "",
			want: Indentation{},
			ok:   false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := DetectIndent(c.text)
			if ok != c.ok || got != c.want {
				t.Fatalf("DetectIndent(%q) = %+v, %v; want %+v, %v", c.text, got, ok, c.want, c.ok)
			}
		})
	}
}

// TestDetectIndentHardTabs is DetectIndent's reading with a language whose
// files are written with tabs: it keeps them unless spaces are clearly what
// the text is written with.
func TestDetectIndentHardTabs(t *testing.T) {
	cases := []struct {
		name string
		text string
		want Indentation
		ok   bool
	}{
		{
			// The shape of a Go file that embeds HTML: half its indented
			// lines are tabs, half the HTML's spaces.
			name: "a tie is the language's tabs",
			text: "var page = `\n  <div>\n    <p>x</p>\n  </div>\n`\n\nfunc f() {\n\tif x {\n\t\ty()\n\t}\n}\n",
			want: Indentation{Style: IndentTabs},
			ok:   true,
		},
		{
			name: "spaces clearly outnumber tabs",
			text: "\ta\n  b\n  c\n  d\n  e\n",
			want: Indentation{Style: IndentSpaces, Width: 2},
			ok:   true,
		},
		{
			name: "spaces are twice the tabs, which keeps tabs",
			text: "\ta\n  b\n  c\n",
			want: Indentation{Style: IndentTabs},
			ok:   true,
		},
		{
			name: "no tab at all is the text's spaces",
			text: "func f() {\n  if x {\n    y()\n  }\n}\n",
			want: Indentation{Style: IndentSpaces, Width: 2},
			ok:   true,
		},
		{
			name: "no indented line",
			text: "a\nb\n",
			want: Indentation{},
			ok:   false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := detectIndent(c.text, true)
			if ok != c.ok || got != c.want {
				t.Fatalf("detectIndent(%q, true) = %+v, %v; want %+v, %v", c.text, got, ok, c.want, c.ok)
			}
			if ok && got.Style == IndentTabs && got.Width != 0 {
				t.Fatalf("a tab indentation has no width, got %d", got.Width)
			}
		})
	}
}

// newDetectTest is newEditorTest with the text's own indentation turned on.
func newDetectTest(t *testing.T, lang, text string) *editorTest {
	t.Helper()
	plain, sels := parseCarets(text)
	st := &State{Buffer: NewBuffer(plain), Indent: IndentDetect}
	if lang != "" {
		st.Buffer.SetLanguage(LanguageNamed(lang))
	}
	ct := &editorTest{t: t, st: st}
	ct.tt = ui.NewTester(func(c *ui.Context) {
		View(c, st).Fill().AutoFocus()
	}, 600, 400)
	if len(sels) > 0 {
		st.SetSelections(sels...)
	}
	ct.tt.Frame()
	return ct
}

func TestIndentDetectInTheEditor(t *testing.T) {
	// Go's files indent with tabs; this one does not, and it is 2 wide
	// where Go's language says 4.
	ct := newDetectTest(t, "go", "func f() {\n  if x {|")
	ct.key(0, ui.KeyEnter)
	ct.want("func f() {\n  if x {\n    |")
	// A tab in the text is a tab inserted, whatever the language says.
	ct = newDetectTest(t, "python", "\tif x:|")
	ct.key(0, ui.KeyEnter)
	ct.want("\tif x:\n\t\t|")
	// A text with no indented line is indented as its language's files
	// are: Go's tab, Python's 4 spaces.
	ct = newDetectTest(t, "go", "func f() {|")
	ct.key(0, ui.KeyEnter)
	ct.want("func f() {\n\t|")
	ct = newDetectTest(t, "python", "def f():|")
	ct.key(0, ui.KeyEnter)
	ct.want("def f():\n    |")
}

func TestIndentDetectReadsTheStart(t *testing.T) {
	// The editor reads the start of a large text, not all of it: the
	// indentation of a file shows there.
	padding := strings.Repeat("a\n", 40000)
	ct := newDetectTest(t, "go", "func f() {\n  if x {|"+padding)
	ct.key(0, ui.KeyEnter)
	if want := "func f() {\n  if x {\n    "; !strings.HasPrefix(ct.st.Buffer.String(), want) {
		t.Fatalf("text = %.40q, want it to begin %q", ct.st.Buffer.String(), want)
	}
	// Past what it reads, a text with no indented line there is indented
	// as its language's files are.
	ct = newDetectTest(t, "go", padding+"func f() {|")
	ct.key(0, ui.KeyEnter)
	if want := "func f() {\n\t"; !strings.HasSuffix(ct.st.Buffer.String(), want) {
		t.Fatalf("text ends %.40q, want %q", ct.st.Buffer.String(), want)
	}
}

func TestIndentDetectSurvivesAnEdit(t *testing.T) {
	// An edit of the text is not the text being set: the editor keeps the
	// indentation it read when the buffer was set, so that a paste of lines
	// of another style does not flip the tab width of the whole buffer.
	ct := newDetectTest(t, "python", "def f():|")
	ct.key(0, ui.KeyEnter)
	ct.want("def f():\n    |")
	ct.st.Buffer.Edit(Edit{Start: 0, End: 0, Text: "if a:\n  b()\n"})
	ct.tt.Frame()
	ct.key(0, ui.KeyEnter)
	ct.want("if a:\n  b()\ndef f():\n\n    |")
	// Setting the text again reads it again: this one is 2 wide.
	text := "if a:\n  b()\n\nif c:\n  d()"
	ct.st.Buffer.Set(text)
	ct.st.SetSelections(Selection{Anchor: len(text), Head: len(text)})
	ct.tt.Frame()
	ct.key(0, ui.KeyEnter)
	ct.want("if a:\n  b()\n\nif c:\n  d()\n  |")
}

func TestIndentDetectKeepsTheLanguagesTabs(t *testing.T) {
	// Go's files are written with tabs, and this text's spaces are the HTML
	// half of it: the language is the word on the tie.
	ct := newDetectTest(t, "go", "func f() {\n\tif x {|\n}\n\nvar page = `\n  <div>\n  </div>\n`\n")
	ct.key(0, ui.KeyEnter)
	ct.want("func f() {\n\tif x {\n\t\t|\n}\n\nvar page = `\n  <div>\n  </div>\n`\n")
}

func TestIndentDetectFollowsTheText(t *testing.T) {
	// Editing the text changes what it indents with, and the editor reads
	// it again: the version of the buffer is what the read is kept for.
	ct := newDetectTest(t, "python", "def f():|")
	ct.key(0, ui.KeyEnter)
	ct.want("def f():\n    |")
	ct.typ("x")
	text := "def f():\n  return 1\n\ndef g():"
	ct.st.Buffer.Set(text)
	ct.st.SetSelections(Selection{Anchor: len(text), Head: len(text)})
	ct.tt.Frame()
	ct.key(0, ui.KeyEnter)
	ct.want("def f():\n  return 1\n\ndef g():\n  |")
}
