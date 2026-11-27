package editor

import (
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/internal/rope"
	"github.com/egoist/mygo/ui"
)

func TestCodeBufferEditUndo(t *testing.T) {
	b := NewBuffer("hello\nworld\n")
	if b.LineCount() != 3 || b.Line(1) != "world" || b.Modified() {
		t.Fatalf("lines %d, %q, modified %v", b.LineCount(), b.Line(1), b.Modified())
	}
	b.Edit(Edit{Start: 0, End: 5, Text: "goodbye"}, Edit{Start: 6, End: 6, Text: "big "})
	if got := b.String(); got != "goodbye\nbig world\n" {
		t.Fatalf("String = %q", got)
	}
	if !b.Modified() || !b.CanUndo() {
		t.Fatal("not modified after an edit")
	}
	b.Undo()
	if got := b.String(); got != "hello\nworld\n" || b.Modified() {
		t.Fatalf("after undo %q, modified %v", got, b.Modified())
	}
	b.Redo()
	if got := b.String(); got != "goodbye\nbig world\n" {
		t.Fatalf("after redo %q", got)
	}
	if p := b.Pos(12); p != (Pos{1, 4}) {
		t.Fatalf("Pos = %+v", p)
	}
	if o := b.Offset(Pos{1, 100}); o != 17 {
		t.Fatalf("Offset = %d", o)
	}
	b.Transact(func() {
		b.Edit(Edit{Start: 0, End: 0, Text: "a"})
		b.Edit(Edit{Start: 0, End: 0, Text: "b"})
	})
	b.Undo()
	if got := b.String(); got != "goodbye\nbig world\n" {
		t.Fatalf("Transact undo %q", got)
	}
}

// The log moves offsets and lines through edits as the text moved.
func TestCodeBufferLog(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	b := NewBuffer("one\ntwo\nthree\nfour\nfive\n")
	for range 500 {
		before, v := b.snapshot()
		text := before.String()
		// A marker line, to find where line moved.
		at := r.IntN(len(text) + 1)
		end := min(len(text), at+r.IntN(6))
		ins := []string{"", "x", "\n", "ab\ncd", "\n\n"}[r.IntN(5)]
		probe := r.IntN(len(text) + 1)
		b.Edit(Edit{Start: at, End: end, Text: ins})
		if b.String() == text {
			continue
		}
		changes, _, ok := b.changesSince(v)
		if !ok || len(changes) != 1 {
			t.Fatalf("changesSince = %v, %v", changes, ok)
		}
		got := mapOffset(probe, changes[0], biasRight)
		var want int
		switch {
		case probe < at:
			want = probe
		case probe >= end && (probe > at || end > at):
			want = probe + len(ins) - (end - at)
		default:
			want = at + len(ins)
		}
		if probe > at && probe < end {
			want = at + len(ins)
		}
		if got != want {
			t.Fatalf("mapOffset(%d) through [%d,%d)->%q = %d, want %d", probe, at, end, ins, got, want)
		}
		after, _ := b.snapshot()
		// Lines wholly after the change keep their text.
		pl := before.LineOf(probe)
		if before.LineStart(pl) > end && pl > before.LineOf(end) {
			if nl := mapLine(pl, changes[0]); after.Line(nl) != before.Line(pl) {
				t.Fatalf("mapLine(%d) = %d: %q, want %q", pl, nl, after.Line(nl), before.Line(pl))
			}
		}
		if after.LineCount() != strings.Count(after.String(), "\n")+1 {
			t.Fatal("line count")
		}
	}
}

// A text loaded after the editor's first frame leaves the caret at the
// start, rather than after the text as if typed there.
func TestBufferResetKeepsTheCaretAtTheStart(t *testing.T) {
	ct := newEditorTest(t, "", "")
	ct.st.Buffer.Reset("first line\nsecond line\n")
	ct.tt.Frame()
	ct.want("|first line\nsecond line\n")
}

// A file reloaded from disk keeps the editor on the text it was on, as
// VS Code reloads one: what is above and below the lines that changed
// stays with its text, as an edit of those lines would move it, and a
// caret within them goes to their end.
func TestBufferResetKeepsTheEditorsPlace(t *testing.T) {
	text := strings.Repeat("func f() {\n\tx()\n}\n", 100)
	ct := newEditorTest(t, "go", text)
	st := ct.st
	at := func(line, col int) int { return st.Buffer.Offset(Pos{line, col}) }
	above, within := Selection{Anchor: at(49, 1), Head: at(49, 3)}, Selection{Anchor: at(150, 0), Head: at(150, 0)}
	st.SetSelections(above, within, Selection{Anchor: at(262, 1), Head: at(262, 3)})
	st.Fold(201)
	st.Fold(255)
	diag := Diagnostic{Start: at(256, 1), End: at(256, 4), Message: "unused"}
	st.SetDiagnostics(st.Buffer.Version(), []Diagnostic{diag})
	st.SetInlays([]Inlay{{Offset: at(241, 1), Text: "hint"}})
	st.SetBlocks([]Block{{Line: 240, Content: func(*ui.Context, ui.Element, Block) {}}})
	st.SetLineStyles([]LineStyle{{Line: 245, LineHeight: 2}})
	ct.tt.Frame()
	// Line 201, the first after the lines that change, at the top.
	st.ScrollTo(201)
	ct.tt.Frame()

	// Line 100 becomes two, and line 200 changes: lines 100 to 200 are
	// what changed, a line longer.
	lines := strings.Split(text, "\n")
	lines[100], lines[200] = "// one\n// two", "} // f"
	st.Buffer.Reset(strings.Join(lines, "\n"))
	ct.tt.Frame()
	after := func(line, col int) int { return st.Buffer.Offset(Pos{line + 1, col}) }
	want := []Selection{above, {Anchor: after(201, 0), Head: after(201, 0)}, {Anchor: after(262, 1), Head: after(262, 3)}}
	if got := st.Selections(); !slices.Equal(got, want) {
		t.Errorf("selections %+v, want %+v", got, want)
	}
	if got, _ := st.VisibleLines(); got != 202 {
		t.Errorf("scrolled to line %d, want 202", got)
	}
	if got := st.Folded(); !slices.Equal(got, []int{202, 256}) {
		t.Errorf("folded %v, want [202 256]", got)
	}
	if got := st.Diagnostics(); len(got) != 1 || got[0].Start != after(256, 1) || got[0].End != after(256, 4) {
		t.Errorf("diagnostics %+v, want %d-%d", got, after(256, 1), after(256, 4))
	}
	if got := st.ed.view.inlays; len(got) != 1 || got[0].at != after(241, 1) {
		t.Errorf("inlays %+v, want one at %d", got, after(241, 1))
	}
	if got := st.Blocks(); len(got) != 1 || got[0].Line != 241 {
		t.Errorf("blocks %+v, want one on line 241", got)
	}
	if got := st.LineStyles(); len(got) != 1 || got[0].Line != 246 {
		t.Errorf("line styles %+v, want one on line 246", got)
	}

	// A line put in above the top line and the first fold, and no other
	// change.
	lines = strings.Split(st.Buffer.String(), "\n")
	st.Buffer.Reset(strings.Join(slices.Insert(lines, 202, "// new"), "\n"))
	ct.tt.Frame()
	if got, _ := st.VisibleLines(); got != 203 {
		t.Errorf("scrolled to line %d after a line put in above it, want 203", got)
	}
	if got := st.Folded(); !slices.Equal(got, []int{203, 257}) {
		t.Errorf("folded %v after a line put in above, want [203 257]", got)
	}

	// A text without the carets' lines leaves them where they were.
	st.Buffer.Reset("func f() {}\n")
	ct.tt.Frame()
	if got := st.Selections(); len(got) != 1 || got[0].Anchor != 12 || got[0].Head != 12 {
		t.Errorf("selections %+v in the shorter text, want a caret at its end", got)
	}
}

// The highlighter follows the lines Reset changed, as it does an edit:
// a comment opened in one line and closed in another colors the lines
// between, and only those.
func TestBufferResetHighlights(t *testing.T) {
	text := strings.Repeat("x := 1\n", 50)
	b := NewBuffer(text)
	b.SetLanguage(LanguageNamed("go"))
	b.highlighter().Highlight(49, time.Hour)
	lines := strings.Split(text, "\n")
	lines[10], lines[20] = "/* open", "close */"
	b.Reset(strings.Join(lines, "\n"))
	h := b.highlighter().(*tokenHighlighter)
	if !h.Highlight(b.LineCount()-1, time.Hour) {
		t.Fatal("highlighting stopped")
	}
	tok := b.Language().Tokenizer
	var state uint64
	for i := range b.LineCount() {
		if got, ok := h.stateOf(i); !ok || got != state {
			t.Fatalf("line %d starts in %x (%v), want %x", i, got, ok, state)
		}
		_, state = tok.Tokenize(b.Line(i), state, nil)
	}
}

// Reset's change replaces the lines between those the texts start and
// end with alike, and makes the new text.
func TestReloadChange(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	pieces := []string{"a", "b", "\n", "é", "è", "\n\n"}
	random := func() string {
		var sb strings.Builder
		for range r.IntN(200) {
			sb.WriteString(pieces[r.IntN(len(pieces))])
		}
		return sb.String()
	}
	for range 3000 {
		// Some of more than one chunk of the rope.
		a := strings.Repeat(random(), 1+r.IntN(40))
		b := a
		for range r.IntN(3) {
			at := r.IntN(len(b) + 1)
			end := min(len(b), at+r.IntN(10))
			b = b[:at] + pieces[r.IntN(len(pieces))] + b[end:]
		}
		if r.IntN(10) == 0 {
			b = random()
		}
		old, new := rope.New(a), rope.New(b)
		c, ok := reloadChange(old, b)
		if !ok {
			if a != b {
				t.Fatalf("%q to %q: no change", a, b)
			}
			continue
		}
		lineStart := func(s string, i int) bool { return i == 0 || s[i-1] == '\n' }
		lineEnd := func(s string, i int) bool { return i == len(s) || lineStart(s, i) }
		switch {
		case a[:c.oldStart]+b[c.newStart:c.newEnd]+a[c.oldEnd:] != b:
			t.Fatalf("%q to %q: %+v makes another text", a, b, c)
		case !lineStart(a, c.oldStart) || !lineStart(b, c.newStart) || !lineEnd(a, c.oldEnd) || !lineEnd(b, c.newEnd):
			t.Fatalf("%q to %q: %+v cuts a line", a, b, c)
		case c.oldLine != old.LineOf(c.oldStart) || c.oldEndLine != old.LineOf(c.oldEnd) || c.newLine != new.LineOf(c.newStart) || c.newEndLine != new.LineOf(c.newEnd):
			t.Fatalf("%q to %q: %+v has other lines", a, b, c)
		}
		// The lines alike next to the change, and the last, go to their
		// own.
		for _, i := range []int{c.oldLine - 1, c.oldEndLine, c.oldEndLine + 1, old.LineCount() - 1} {
			if i < 0 || i >= old.LineCount() || i >= c.oldLine && (i < c.oldEndLine || i == c.oldEndLine && !c.keepsEndLine) {
				continue
			}
			want := i
			if i >= c.oldLine {
				want += c.newEndLine - c.oldEndLine
			}
			if got := mapLine(i, []change{c}); got != want || new.Line(got) != old.Line(i) {
				t.Fatalf("%q to %q: %+v moves line %d to %d, want %d", a, b, c, i, got, want)
			}
		}
		// No more lines alike at either end.
		la, lb := strings.SplitAfter(a, "\n"), strings.SplitAfter(b, "\n")
		p := 0
		for p < min(len(la), len(lb)) && la[p] == lb[p] {
			p++
		}
		q := 0
		for q < min(len(la), len(lb))-p && la[len(la)-1-q] == lb[len(lb)-1-q] {
			q++
		}
		start, end := len(strings.Join(la[:p], "")), len(a)-len(strings.Join(la[len(la)-q:], ""))
		if c.oldStart != start || c.oldEnd != end {
			t.Fatalf("%q to %q: %+v replaces %d to %d, want %d to %d", a, b, c, c.oldStart, c.oldEnd, start, end)
		}
	}
}
