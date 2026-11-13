package editor

import (
	"fmt"
	"strings"
	"testing"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/ui"
)

// accessDiff returns 60 lines and the same with line 20 changed, line 40
// deleted and a line added after line 30.
func accessDiff() (old, nw string) {
	var a, b strings.Builder
	for i := range 60 {
		fmt.Fprintf(&a, "line %d\n", i)
		switch i {
		case 20:
			b.WriteString("changed 20\n")
		case 40:
		case 30:
			b.WriteString("line 30\nadded after 30\n")
		default:
			fmt.Fprintf(&b, "line %d\n", i)
		}
	}
	return a.String(), b.String()
}

// accessLine is a line of a diff as assistive technology reads it.
type accessLine struct {
	role       ui.Role
	text, line string
}

// sideLines returns the lines inside the text field named side, in the
// order screen readers read them, and their boxes.
func sideLines(tt *ui.Tester, side string) ([]accessLine, []ui.Rect) {
	var lines []accessLine
	var boxes []ui.Rect
	in := false
	for _, a := range tt.AccessAll() {
		if a.Role == ui.RoleTextField {
			in = a.Name == side
			continue
		}
		if !in {
			continue
		}
		switch a.Role {
		case ui.RoleText, ui.RoleInsertion, ui.RoleDeletion:
			if strings.HasPrefix(a.Description, "Line ") {
				lines = append(lines, accessLine{a.Role, a.Name, a.Description})
				boxes = append(boxes, a.Bounds)
				continue
			}
		}
		in = false // past the side's lines
	}
	return lines, boxes
}

// hasLines reports whether want is a run of got.
func hasLines(got, want []accessLine) bool {
	for i := 0; i+len(want) <= len(got); i++ {
		match := true
		for j := range want {
			if got[i+j] != want[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// Every line of a split diff in view reads as what it is, with its
// number: the old side's changed lines as deletions, the new side's as
// insertions, the others as texts, in the order they show (#1814).
func TestDiffAccessSplitLines(t *testing.T) {
	old, nw := accessDiff()
	d := &DiffState{Old: NewBuffer(old), New: NewBuffer(nw)}
	tt := ui.NewTester(func(c *ui.Context) { DiffView(c, d).Fill() }, 900, 600)
	tt.Frame()
	oldLines, oldBoxes := sideLines(tt, "Old text")
	newLines, newBoxes := sideLines(tt, "New text")
	for _, want := range [][]accessLine{
		{{ui.RoleText, "line 19", "Line 20"}, {ui.RoleDeletion, "line 20", "Line 21"}, {ui.RoleText, "line 21", "Line 22"}},
		{{ui.RoleText, "line 39", "Line 40"}, {ui.RoleDeletion, "line 40", "Line 41"}, {ui.RoleText, "line 41", "Line 42"}},
	} {
		if !hasLines(oldLines, want) {
			t.Errorf("the old side reads %v, want %v among them", oldLines, want)
		}
	}
	for _, want := range [][]accessLine{
		{{ui.RoleText, "line 19", "Line 20"}, {ui.RoleInsertion, "changed 20", "Line 21"}, {ui.RoleText, "line 21", "Line 22"}},
		{{ui.RoleText, "line 30", "Line 31"}, {ui.RoleInsertion, "added after 30", "Line 32"}, {ui.RoleText, "line 31", "Line 33"}},
		{{ui.RoleText, "line 39", "Line 41"}, {ui.RoleText, "line 41", "Line 42"}},
	} {
		if !hasLines(newLines, want) {
			t.Errorf("the new side reads %v, want %v among them", newLines, want)
		}
	}
	// Collapsed lines are not read: the separator says which they are.
	for _, l := range append(oldLines, newLines...) {
		if l.text == "line 5" || l.text == "line 50" {
			t.Errorf("%q, collapsed, is read", l.text)
		}
	}
	// Down the view, each on its side.
	for side, boxes := range map[string][]ui.Rect{"old": oldBoxes, "new": newBoxes} {
		for i, b := range boxes {
			if i > 0 && b.Y < boxes[i-1].Y+boxes[i-1].H {
				t.Errorf("the %s side's line %d at %v overlaps the one before, at %v", side, i, b, boxes[i-1])
			}
			if b.Y+b.H <= 0 || b.Y >= 600 || side == "old" && b.X >= 450 || side == "new" && b.X < 450 {
				t.Errorf("the %s side's line %d is at %v", side, i, b)
			}
		}
	}
	if len(oldLines) == 0 || len(newLines) == 0 {
		t.Fatalf("no lines: old %v, new %v", oldLines, newLines)
	}
}

// A unified diff reads its deleted lines, which are not the new text's,
// as deletions with their old numbers, before the lines added in their
// place.
func TestDiffAccessUnifiedLines(t *testing.T) {
	old, nw := accessDiff()
	d := &DiffState{Old: NewBuffer(old), New: NewBuffer(nw), Layout: DiffUnified}
	tt := ui.NewTester(func(c *ui.Context) { DiffView(c, d).Fill() }, 900, 600)
	tt.Frame()
	lines, _ := sideLines(tt, "New text")
	for _, want := range [][]accessLine{
		{{ui.RoleText, "line 19", "Line 20"}, {ui.RoleDeletion, "line 20", "Line 21"}, {ui.RoleInsertion, "changed 20", "Line 21"}, {ui.RoleText, "line 21", "Line 22"}},
		{{ui.RoleText, "line 30", "Line 31"}, {ui.RoleInsertion, "added after 30", "Line 32"}},
		{{ui.RoleText, "line 39", "Line 41"}, {ui.RoleDeletion, "line 40", "Line 41"}, {ui.RoleText, "line 41", "Line 42"}},
	} {
		if !hasLines(lines, want) {
			t.Errorf("the diff reads %v, want %v among them", lines, want)
		}
	}
}

// A separator says which lines it collapses, by their numbers in both
// texts where they differ.
func TestDiffAccessSeparators(t *testing.T) {
	old, nw := accessDiff()
	for _, layout := range []DiffLayout{DiffSplit, DiffUnified} {
		d := &DiffState{Old: NewBuffer(old), New: NewBuffer(nw), Layout: layout}
		tt := ui.NewTester(func(c *ui.Context) { DiffView(c, d).Fill() }, 900, 600)
		tt.Frame()
		for name, want := range map[string]string{
			"17 unmodified lines": "Lines 1 to 17",
			"4 unmodified lines":  "Lines 25 to 28",
			"3 unmodified lines":  "Old lines 35 to 37, new lines 36 to 38",
			"16 unmodified lines": "Lines 45 to 60",
		} {
			a, ok := tt.AccessByRole(ui.RoleGroup, name)
			if !ok || a.Description != want {
				t.Errorf("layout %d: the separator %q is described %q (%v), want %q", layout, name, a.Description, ok, want)
			}
		}
	}
}

// A patch's lines are numbered as in its file.
func TestDiffAccessPatchNumbers(t *testing.T) {
	files, err := ParsePatch("--- a/x\n+++ b/x\n@@ -21,4 +21,4 @@ func main\n a\n-b\n+B\n c\n d\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, layout := range []DiffLayout{DiffSplit, DiffUnified} {
		d := &DiffState{Layout: layout}
		d.SetPatch(files[0])
		tt := ui.NewTester(func(c *ui.Context) { DiffView(c, d).Fill() }, 900, 400)
		tt.Frame()
		lines, _ := sideLines(tt, "New text")
		want := []accessLine{{ui.RoleText, "a", "Line 21"}, {ui.RoleInsertion, "B", "Line 22"}, {ui.RoleText, "c", "Line 23"}}
		if layout == DiffUnified {
			want = []accessLine{{ui.RoleText, "a", "Line 21"}, {ui.RoleDeletion, "b", "Line 22"}, {ui.RoleInsertion, "B", "Line 22"}, {ui.RoleText, "c", "Line 23"}}
		}
		if !hasLines(lines, want) {
			t.Errorf("layout %d reads %v, want %v", layout, lines, want)
		}
	}
}

// Only the lines in view are read, of a file as long as a deleted one,
// and scrolling reads those that come into view, each the same node from
// frame to frame.
func TestDiffAccessLinesInView(t *testing.T) {
	const n = 100000
	var sb strings.Builder
	for i := range n {
		fmt.Fprintf(&sb, "deleted %d\n", i)
	}
	for _, layout := range []DiffLayout{DiffSplit, DiffUnified} {
		d := &DiffState{Old: NewBuffer(sb.String()), New: NewBuffer(""), Layout: layout}
		tt := ui.NewTester(func(c *ui.Context) { DiffView(c, d).Fill() }, 900, 400)
		tt.Frame()
		count := func() (int, string) {
			deleted, first := 0, ""
			for _, a := range tt.AccessAll() {
				if a.Role == ui.RoleDeletion {
					if deleted == 0 {
						first = a.Name
					}
					deleted++
				}
			}
			return deleted, first
		}
		got, first := count()
		if got == 0 || got > 40 || first != "deleted 0" {
			t.Errorf("layout %d: %d deleted lines read, from %q, in a view of 20", layout, got, first)
		}
		d.SetScrollY(float64(n / 2 * 20))
		tt.Frame()
		got, first = count()
		var num int
		if _, err := fmt.Sscanf(first, "deleted %d", &num); err != nil || got == 0 || got > 40 || num < n/2-200 || num > n/2+200 {
			t.Errorf("layout %d: scrolled halfway, %d deleted lines read from %q", layout, got, first)
		}
		// The empty new text reads no line.
		if lines, _ := sideLines(tt, "New text"); layout == DiffSplit && len(lines) > 0 {
			t.Errorf("the new side, empty, reads %v", lines)
		}
	}
}

// An editor tells where its lines in view are, by their index in the
// text it gives, by which a range of it is found.
func TestEditorLineBoxes(t *testing.T) {
	var sb strings.Builder
	for i := range 1000 {
		fmt.Fprintf(&sb, "line %d\n", i)
	}
	st := &State{Buffer: NewBuffer(sb.String())}
	tt := ui.NewTester(func(c *ui.Context) { View(c, st).Fill() }, 600, 400)
	tt.Frame()
	st.SetScrollY(100 * float64(st.ed.view.lineHeight))
	tt.Frame()
	var n platform.AccessNode
	n.Bounds = platform.RectF{W: 600, H: 400}
	st.ed.accessibility(&n)
	boxes := n.LineBoxes
	if len(boxes) == 0 || len(boxes) > 30 || boxes[0].Line < 99 || boxes[0].Line > 101 {
		t.Fatalf("scrolled to line 100, the boxes are of %d lines from %v", len(boxes), boxes)
	}
	for i, b := range boxes {
		if i > 0 && (b.Line != boxes[i-1].Line+1 || b.Bounds.Y < boxes[i-1].Bounds.Y+boxes[i-1].Bounds.H-0.5) {
			t.Fatalf("box %d, %+v, does not follow %+v", i, b, boxes[i-1])
		}
	}
	// Line 105's text, by its units, is where its box is.
	start, count, _ := platform.LineUnits(n.Value, 105)
	var want platform.RectF
	for _, b := range boxes {
		if b.Line == 105 {
			want = b.Bounds
		}
	}
	if got := platform.RangeBounds(&n, start+2, count-2); got != want || want.H == 0 {
		t.Errorf("a range of line 105 is at %v, want %v", got, want)
	}
	if got := platform.RangeBounds(&n, 0, 1); got != n.Bounds {
		t.Errorf("line 0, out of view, is at %v, want the editor's bounds", got)
	}
}
