package editor

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// Alt+Q refills the caret's paragraph to the first wrap guide, keeping its
// indentation and leaving the paragraphs around it alone; the caret stays
// after the same word.
func TestRewrapFillsTheCaretsParagraph(t *testing.T) {
	ct := newEditorTest(t, "", "first line\n\n  one two three four five six se|ven eight\n  nine\n\nlast\n")
	ct.st.WrapGuides = []int{20}
	ct.tt.Frame()
	if isMac() {
		ct.run(Rewrap)
	} else {
		ct.key(ui.Alt, ui.KeyQ)
	}
	ct.want("first line\n\n  one two three four\n  five six se|ven\n  eight nine\n\nlast\n")
	// Without guides, short lines join up to 80 columns.
	ct = newEditorTest(t, "", "a|\nb\nc\n")
	ct.run(Rewrap)
	ct.want("a| b c\n")
}

// The language's line comment stays on every line, with Rust's extra
// slash; a change of prefix ends the paragraph.
func TestRewrapKeepsLineComments(t *testing.T) {
	ct := newEditorTest(t, "go", "\t// alpha beta gamma delta| epsilon\n\t// zeta\nx := 1\n")
	ct.st.WrapGuides = []int{25}
	ct.tt.Frame()
	ct.run(Rewrap)
	ct.want("\t// alpha beta gamma\n\t// delta| epsilon zeta\nx := 1\n")
	ct = newEditorTest(t, "rust", "/// one two three| four\nfn f() {}\n")
	ct.st.WrapGuides = []int{14}
	ct.tt.Frame()
	ct.run(Rewrap)
	ct.want("/// one two\n/// three| four\nfn f() {}\n")
}

// Markdown's quote markers repeat on every line, and a list item's lines
// hang under its text; each item is a paragraph of its own. Other
// languages have neither.
func TestRewrapMarkdownQuotesAndLists(t *testing.T) {
	ct := newEditorTest(t, "markdown", "> one two three| four\n> five\n")
	ct.st.WrapGuides = []int{12}
	ct.tt.Frame()
	ct.run(Rewrap)
	ct.want("> one two\n> three| four\n> five\n")
	ct = newEditorTest(t, "markdown", "[- one two three four\n12. five six seven]\n")
	ct.st.WrapGuides = []int{12}
	ct.tt.Frame()
	ct.run(Rewrap)
	if got := ct.st.Buffer.String(); got != "- one two\n  three four\n12. five six\n    seven\n" {
		t.Fatalf("lists rewrapped to %q", got)
	}
	ct = newEditorTest(t, "", "- a|\n- b\n")
	ct.run(Rewrap)
	ct.want("- a| - b\n")
}

// A selection's lines rewrap paragraph by paragraph, and undo takes it
// back in one step.
func TestRewrapSelectionIsOneUndoStep(t *testing.T) {
	ct := newEditorTest(t, "", "[a b c d e f\n\ng h i j k l]\n")
	ct.st.WrapColumn = 5
	ct.tt.Frame()
	ct.run(Rewrap)
	if got := ct.st.Buffer.String(); got != "a b c\nd e f\n\ng h i\nj k l\n" {
		t.Fatalf("rewrapped to %q", got)
	}
	ct.run(Undo)
	if got := ct.st.Buffer.String(); got != "a b c d e f\n\ng h i j k l\n" {
		t.Fatalf("one undo left %q", got)
	}
}

// Markdown's headings, table rows, HTML and fenced code are left as they
// are, and the paragraphs around them stop at them.
func TestRewrapLeavesMarkdownBlocksAlone(t *testing.T) {
	for _, text := range []string{
		"# Title|\ntext\n",
		"```|\ncode\nmore\n```\n",
		"<div>|\n</div>\n",
	} {
		ct := newEditorTest(t, "markdown", text)
		plain := ct.st.Buffer.String()
		ct.run(Rewrap)
		if got := ct.st.Buffer.String(); got != plain {
			t.Errorf("%q rewrapped to %q", plain, got)
		}
	}
	// A table row, written without the carets' |.
	ct := newEditorTest(t, "markdown", "")
	ct.st.Buffer.Set("| a  b |\n| c d |\n")
	ct.tt.Frame()
	ct.run(Rewrap)
	if got := ct.st.Buffer.String(); got != "| a  b |\n| c d |\n" {
		t.Errorf("a table rewrapped to %q", got)
	}
	ct = newEditorTest(t, "markdown", "# Title\nsome| text\nmore\n```\nx y\n```\n[a\nb]\n")
	ct.run(Rewrap)
	ct.run(MoveDocEnd)
	ct.run(SelectAll)
	ct.run(Rewrap)
	if got := ct.st.Buffer.String(); got != "# Title\nsome text more\n```\nx y\n```\na b\n" {
		t.Fatalf("rewrapped to %q", got)
	}
}

// Block comments are left as they are; a line comment is the language's
// token with at most one / or ! after it, and a banner of more is left
// alone.
func TestRewrapLeavesBlockCommentsAlone(t *testing.T) {
	ct := newEditorTest(t, "go", "/* a b c d| e f\n   g h */\n// i j k\n// l\n")
	ct.st.WrapGuides = []int{7}
	ct.tt.Frame()
	ct.run(Rewrap)
	ct.run(MoveDocEnd)
	ct.run(MoveUp)
	ct.run(Rewrap)
	if got := ct.st.Buffer.String(); got != "/* a b c d e f\n   g h */\n// i j\n// k l\n" {
		t.Fatalf("rewrapped to %q", got)
	}
	ct = newEditorTest(t, "go", "//// a| b\n//// c\n")
	ct.st.WrapGuides = []int{10}
	ct.tt.Frame()
	ct.run(Rewrap)
	ct.want("//// a| b\n//// c\n")
	ct = newEditorTest(t, "python", "## a| b\n## c\n#! d\n#! e\n")
	ct.run(Rewrap)
	ct.run(MoveDocEnd)
	ct.run(MoveUp)
	ct.run(Rewrap)
	if got := ct.st.Buffer.String(); got != "## a b\n## c\n#! d e\n" {
		t.Fatalf("rewrapped to %q", got)
	}
}

// A selection keeps its ends by the same characters, and rewraps only its
// own lines of a paragraph.
func TestRewrapKeepsTheSelection(t *testing.T) {
	ct := newEditorTest(t, "", "a b\nc [d e\nf g]\nh\n")
	ct.run(Rewrap)
	ct.want("a b\nc [d e f g]\nh\n")
}
