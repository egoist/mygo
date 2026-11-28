package editor

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// EndGroup ends the step typing goes into: what is typed next is a step
// of its own.
func TestEndGroup(t *testing.T) {
	ct := newEditorTest(t, "", "|")
	ct.tt.Type("ab")
	ct.st.Buffer.EndGroup()
	ct.tt.Type("c")
	ct.run(Undo)
	ct.want("ab|")
	ct.run(Undo)
	ct.want("|")
}

// A save that finishes after more typing marks the text it wrote saved:
// the typing since leaves the buffer modified, and undoing it makes it
// unmodified again. The line endings saved are those it wrote too.
func TestMarkSavedAtASaveFinishingAfterTyping(t *testing.T) {
	ct := newEditorTest(t, "", "a\r\n|")
	b := ct.st.Buffer
	ct.typ("b")
	saving := b.Snapshot()
	b.EndGroup()
	ct.typ("c")
	b.MarkSavedAt(saving)
	if !b.Modified() {
		t.Fatal("unmodified with typing after the text saved")
	}
	ct.run(Undo)
	ct.want("a\nb|")
	if b.Modified() {
		t.Fatal("modified at the text saved")
	}
	saving = b.Snapshot()
	b.SetLineEnding("\n")
	b.MarkSavedAt(saving)
	if !b.Modified() {
		t.Fatal("unmodified with line endings other than those saved")
	}
}

// An app renaming an opening tag as the closing one is typed in joins its
// edit to the typing: undo takes both back, and redo makes both again
// with the caret where the typing left it. It copies what is typed, not
// the versions undo and redo make.
func TestEditJoinedIsUndoneWithTheStepBefore(t *testing.T) {
	ct := newEditorTest(t, "", "<a></a|>")
	b := ct.st.Buffer
	typing := false
	ct.st.OnInput = func(ev ui.InputEvent) bool {
		typing = ev.Kind == ui.InputText
		return false
	}
	b.OnEdit(func(_, _ Snapshot, _ []Change) {
		if typing {
			typing = false
			b.EditJoined(Edit{Start: 2, End: 2, Text: "b"})
		}
	})
	ct.tt.Frame()
	ct.typ("b")
	ct.want("<ab></ab|>")
	ct.run(Undo)
	ct.want("<a></a|>")
	if b.Modified() || b.CanUndo() {
		t.Fatalf("after one undo, modified %v, can undo %v", b.Modified(), b.CanUndo())
	}
	ct.run(Redo)
	ct.want("<ab></ab|>")
}

// With no step to join, the edits make one.
func TestEditJoinedWithoutAStep(t *testing.T) {
	b := NewBuffer("a")
	b.EditJoined(Edit{Start: 1, End: 1, Text: "b"})
	if !b.Undo() || b.String() != "a" {
		t.Fatalf("after undo %q", b.String())
	}
}

// EditJoined joins the step the last edit went into only while it is
// open: after Undo, Redo, EndGroup or Reset it makes a step of its own,
// and inside Transact it goes into the transaction's step, not the one
// before it.
func TestEditJoinedJoinsOnlyTheOpenStep(t *testing.T) {
	for _, tc := range []struct {
		name string
		do   func(b *Buffer)
		want string // after EditJoined of X at the end, and one Undo
	}{
		{"open", func(b *Buffer) { b.Edit(Edit{Start: 1, End: 1, Text: "B"}) }, "A"},
		{"after Undo", func(b *Buffer) { b.Edit(Edit{Start: 1, End: 1, Text: "B"}); b.Undo() }, "A"},
		{"after Redo", func(b *Buffer) { b.Edit(Edit{Start: 1, End: 1, Text: "B"}); b.Undo(); b.Redo() }, "AB"},
		{"after EndGroup", func(b *Buffer) { b.Edit(Edit{Start: 1, End: 1, Text: "B"}); b.EndGroup() }, "AB"},
		{"after Reset", func(b *Buffer) { b.Reset("R") }, "R"},
	} {
		b := NewBuffer("")
		b.Edit(Edit{Start: 0, End: 0, Text: "A"})
		tc.do(b)
		b.EditJoined(Edit{Start: b.Len(), End: b.Len(), Text: "X"})
		if b.CanRedo() {
			t.Errorf("%s: a redo step left after EditJoined", tc.name)
		}
		b.Undo()
		if got := b.String(); got != tc.want {
			t.Errorf("%s: after one undo %q, want %q", tc.name, got, tc.want)
		}
	}

	b := NewBuffer("")
	b.Edit(Edit{Start: 0, End: 0, Text: "A"})
	b.Transact(func() {
		b.EditJoined(Edit{Start: 1, End: 1, Text: "X"})
		b.Edit(Edit{Start: 2, End: 2, Text: "C"})
	})
	b.Undo()
	if got := b.String(); got != "A" {
		t.Fatalf("a transaction starting with EditJoined, undone: %q, want A", got)
	}
}

// MarkSavedAt takes the line endings the snapshot has, as a file
// converted and saved: the buffer is unmodified.
func TestMarkSavedAtTakesTheEndings(t *testing.T) {
	b := NewBuffer("a\nb")
	b.SetLineEnding("\r\n")
	b.MarkSavedAt(b.Snapshot())
	if b.Modified() {
		t.Fatal("modified once saved with the endings written")
	}
}

// Edits joined to a step put back the line endings they took away when
// undone, and make theirs again when redone, as the step's own do.
func TestEditJoinedKeepsLineEndings(t *testing.T) {
	b := NewBuffer("x\ny\r\nz")
	b.Edit(Edit{Start: b.Len(), End: b.Len(), Text: "!"})
	b.EditJoined(Edit{Start: 1, End: 2}, Edit{Start: 3, End: 4, Text: "\n-\n"})
	written := func() string {
		var w strings.Builder
		b.WriteTo(&w)
		return w.String()
	}
	joined := written()
	b.Undo()
	if got := written(); got != "x\ny\r\nz" {
		t.Fatalf("undone, writes %q", got)
	}
	b.Redo()
	if got := written(); got != joined {
		t.Fatalf("redone, writes %q, want %q", got, joined)
	}
}
