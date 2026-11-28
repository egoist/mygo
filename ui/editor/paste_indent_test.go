package editor

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// pasteTest returns an editor test of init with IndentOnPaste and an
// empty record of the last copy.
func pasteTest(t *testing.T, lang, init string) *editorTest {
	t.Helper()
	oldClip := lastClip
	t.Cleanup(func() { lastClip = oldClip })
	lastClip = clipboard{}
	ct := newEditorTest(t, lang, init)
	ct.st.IndentOnPaste = true
	ct.tt.Frame()
	return ct
}

// TestIndentOnPaste pastes several lines at one caret with IndentOnPaste:
// they take the indentation Return would give, or the caret line's after
// text on it, keep their own relative to it, and the paste is one step of
// undo.
func TestIndentOnPaste(t *testing.T) {
	for _, tc := range []struct {
		name, lang, init, clip, want string
	}{
		{name: "into-indented-blank-line", lang: "JavaScript", init: "{\n  |\n}", clip: "if x {\n    y()\n}", want: "{\n  if x {\n      y()\n  }|\n}"},
		{name: "empty-unindented-blank-line", lang: "JavaScript", init: "{\n|\n}", clip: "if x {\n  y()\n}", want: "{\n  if x {\n    y()\n  }|\n}"},
		{name: "copied-mid-line", init: "  a = |", clip: "f(\n        b,\n    )", want: "  a = f(\n      b,\n  )|"},
		{name: "less-indented", init: "|", clip: "        a\n            b\n        c", want: "a\n    b\nc|"},
		{name: "below-an-opener", lang: "Go", init: "func f() {|}", clip: "\n  x\n  y\n", want: "func f() {\n\tx\n\ty\n|}"},
		{name: "tab-file-stays-tabs", lang: "Go", init: "func f() {\n\t|\n}", clip: "a\n    b", want: "func f() {\n\ta\n\t\tb|\n}"},
		{name: "tabs-into-spaces", lang: "JavaScript", init: "{\n  |\n}", clip: "a\n\tb", want: "{\n  a\n    b|\n}"},
		{name: "off-tab-stop-with-tabs", lang: "Go", init: "  x\n  |", clip: "a\n\tb", want: "  x\n  a\n\t  b|"},
		{name: "off-tab-stop-with-spaces", init: "   x\n   |", clip: "a\n    b", want: "   x\n   a\n       b|"},
		{name: "blank-lines-left-empty", init: "  x\n  |", clip: "a\n  \n    b", want: "  x\n  a\n\n      b|"},
		{name: "whitespace-first-line", init: "  x\n  |", clip: "   \n  x", want: "  x\n\n  x|"},
		{name: "trailing-newline", init: "  x\n  |", clip: "a\n  b\n", want: "  x\n  a\n    b\n|"},
		{name: "one-line-unchanged", init: "  |", clip: "    a", want: "      a|"},
		{name: "over-a-selection", init: "  x\n  [y]", clip: "a\n  b", want: "  x\n  a\n    b|"},
		{name: "crlf", init: "  x\n  |", clip: "a\r\n  b", want: "  x\n  a\n    b|"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ct := pasteTest(t, tc.lang, tc.init)
			ct.tt.SetClipboard(tc.clip)
			ct.key(ui.Cmd, ui.KeyV)
			ct.want(tc.want)
			ct.key(ui.Cmd, ui.KeyZ)
			ct.want(tc.init)
		})
	}
}

// TestIndentOnPasteCopies pastes what the editor copied: a line copied at
// a caret goes above the caret's line as it was, over a selection it is
// re-indented, and a copied selection is re-indented.
func TestIndentOnPasteCopies(t *testing.T) {
	ct := pasteTest(t, "JavaScript", "{\n  a\n    b|\n}")
	ct.key(ui.Cmd, ui.KeyC)
	ct.st.SetSelections(Selection{Anchor: 2, Head: 2})
	ct.tt.Frame()
	ct.key(ui.Cmd, ui.KeyV)
	if got := ct.st.Buffer.String(); got != "{\n    b\n  a\n    b\n}" {
		t.Errorf("a copied line pasted above a caret's made %q, want it as it was", got)
	}
	// Over "  a", below the line pasted, at its indentation.
	ct.st.SetSelections(Selection{Anchor: 8, Head: 11})
	ct.tt.Frame()
	ct.key(ui.Cmd, ui.KeyV)
	if got := ct.st.Buffer.String(); got != "{\n    b\n    b\n\n    b\n}" {
		t.Errorf("a copied line pasted over a selection made %q, want it re-indented", got)
	}

	ct = pasteTest(t, "JavaScript", "if (x) {\n[  g(\n    b,\n  )]\n}\n")
	ct.key(ui.Cmd, ui.KeyC)
	end := len(ct.st.Buffer.String())
	ct.st.SetSelections(Selection{Anchor: end, Head: end})
	ct.tt.Frame()
	ct.key(ui.Cmd, ui.KeyV)
	if got := ct.st.Buffer.String(); got != "if (x) {\n  g(\n    b,\n  )\n}\ng(\n  b,\n)" {
		t.Errorf("a copied selection pasted made %q, want it re-indented", got)
	}
}

// TestIndentOnPasteCRLF re-indents into a buffer of \r\n lines, which
// keeps them.
func TestIndentOnPasteCRLF(t *testing.T) {
	ct := pasteTest(t, "JavaScript", "{\n|\n}")
	ct.st.Buffer.SetLineEnding("\r\n")
	ct.tt.Frame()
	ct.tt.SetClipboard("if x {\n  y()\n}")
	ct.key(ui.Cmd, ui.KeyV)
	if got := written(t, ct.st.Buffer); got != "{\r\n  if x {\r\n    y()\r\n  }\r\n}" {
		t.Errorf("written %q", got)
	}
}

// TestIndentOnPasteOff keeps the pasted text as it is by default, at
// several carets, and pastes nothing read-only.
func TestIndentOnPasteOff(t *testing.T) {
	oldClip := lastClip
	t.Cleanup(func() { lastClip = oldClip })

	lastClip = clipboard{}
	ct := newEditorTest(t, "", "    |")
	ct.tt.SetClipboard("a\n  b")
	ct.key(ui.Cmd, ui.KeyV)
	ct.want("    a\n  b|")

	ct = pasteTest(t, "", "    |\n    |")
	ct.tt.SetClipboard("a\n  b\nc")
	ct.key(ui.Cmd, ui.KeyV)
	ct.want("    a\n  b\nc|\n    a\n  b\nc|")

	ct = pasteTest(t, "", "  x\n  |")
	ct.st.ReadOnly = true
	ct.tt.Frame()
	ct.tt.SetClipboard("a\n  b")
	ct.key(ui.Cmd, ui.KeyV)
	ct.want("  x\n  |")
}
