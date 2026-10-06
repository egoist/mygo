package ui

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

type cellTestRow struct {
	key    string
	values [3]string
}

type cellFixture struct {
	rows   []cellTestRow
	cols   []TableColumn
	state  ListState
	cells  TableCellState
	chosen int
	apply  int
	before func(c *Context)
	after  func(c *Context)
}

func newCellFixture(n int) *cellFixture {
	f := &cellFixture{chosen: -1}
	f.cols = []TableColumn{{Title: "Name", ID: "name", Width: 120, Sortable: true}, {Title: "Count", ID: "count", Width: 120}, {Title: "Note", ID: "note", Width: 120}}
	for i := range n {
		f.rows = append(f.rows, cellTestRow{fmt.Sprintf("key-%d", i), [3]string{fmt.Sprintf("name-%d", i), fmt.Sprint(i), fmt.Sprintf("note-%d", i)}})
	}
	f.state = ListState{Cells: &f.cells, Selected: &f.chosen, Key: func(i int) any { return f.rows[i].key }}
	f.cells.Value = func(i, j int) string { return f.rows[i].values[j] }
	f.cells.Apply = func(changes []TableCellChange) {
		f.apply++
		for _, change := range changes {
			for i := range f.rows {
				if f.rows[i].key == change.Cell.Row {
					f.rows[i].values[change.Column] = change.Value
				}
			}
		}
	}
	return f
}

func (f *cellFixture) tester(w, h int) *Tester {
	return NewTester(func(c *Context) {
		Column(c).Fill().Children(func() {
			if f.before != nil {
				f.before(c)
			}
			Table(c, &f.state, f.cols, len(f.rows), func(row, col int) {
				Text(c, f.rows[row].values[col]).SingleLine()
			}).Grow(1)
			if f.after != nil {
				f.after(c)
			}
		})
	}, w, h)
}

func expectCellRange(t *testing.T, f *cellFixture, aRow, aCol, zRow, zCol int) {
	t.Helper()
	want := TableCellRange{
		TableCell{f.rows[aRow].key, f.cols[aCol].id()},
		TableCell{f.rows[zRow].key, f.cols[zCol].id()},
	}
	if f.cells.Selection == nil || *f.cells.Selection != want {
		t.Fatalf("selection = %+v, want %+v", f.cells.Selection, want)
	}
}

func TestTableCellNavigation(t *testing.T) {
	f := newCellFixture(50)
	tt := f.tester(400, 200)
	tt.Click("name-0")
	expectCellRange(t, f, 0, 0, 0, 0)
	if f.chosen != 0 {
		t.Fatal("cell click did not preserve row selection")
	}
	tt.Key(0, KeyRight)
	tt.Key(Shift, KeyDown)
	tt.Key(Shift, KeyRight)
	expectCellRange(t, f, 0, 1, 1, 2)
	tt.Key(0, KeyHome)
	expectCellRange(t, f, 1, 0, 1, 0)
	tt.Key(Cmd|Shift, KeyEnd)
	expectCellRange(t, f, 1, 0, 49, 2)
	if f.chosen != 49 || f.state.last != 49 {
		t.Fatalf("navigation to an unbuilt row: chosen %d, last visible %d", f.chosen, f.state.last)
	}
	tt.Key(Cmd, KeyHome)
	expectCellRange(t, f, 0, 0, 0, 0)
	tt.Key(0, KeyPageDown)
	if f.cells.cursorRow <= 0 {
		t.Fatal("PageDown did not advance")
	}
}

func TestTableCellKeepsRowMultiSelection(t *testing.T) {
	f := newCellFixture(5)
	var chosen Selection[string]
	f.state.Selection = &chosen
	tt := f.tester(400, 250)
	tt.Click("name-0")
	tt.Key(Shift, KeyDown)
	tt.Key(Shift, KeyDown)
	if chosen.Len() != 3 || !chosen.Has("key-0") || !chosen.Has("key-2") {
		t.Fatal("cell range navigation lost row range selection")
	}
	tt.ClickWith(Cmd, "name-4")
	if chosen.Len() != 4 || !chosen.Has("key-4") {
		t.Fatal("cell click lost Cmd/Ctrl row selection")
	}
	// Row and cell selections both follow their stable keys after sorting.
	slices.Reverse(f.rows)
	tt.Frame()
	if f.chosen != 0 || f.cells.Selection.Cursor.Row != "key-4" || chosen.Len() != 4 {
		t.Fatal("sorting lost row or cell identity")
	}
}

func TestTableCellShiftClickAndHeaders(t *testing.T) {
	f := newCellFixture(5)
	f.state.Header = func(i int) bool { return i == 1 }
	tt := f.tester(400, 250)
	tt.Click("name-0")
	r, ok := tt.Find("note-3")
	if !ok {
		t.Fatal("no target")
	}
	tt.send(platform.SurfaceEvent{Kind: platform.PointerDown, X: float64(r.X + 2), Y: float64(r.Y + 2), Mods: platform.ModShift})
	tt.send(platform.SurfaceEvent{Kind: platform.PointerUp, X: float64(r.X + 2), Y: float64(r.Y + 2), Mods: platform.ModShift})
	expectCellRange(t, f, 0, 0, 3, 2)
	tt.Key(Cmd, KeyHome)
	tt.Key(0, KeyDown)
	expectCellRange(t, f, 2, 0, 2, 0)
	tt.Command("selectAll")
	tt.Command("copy")
	values, err := ParseTSV(tt.Clipboard())
	if err != nil || len(values) != 4 || len(values[0]) != 3 {
		t.Fatalf("headers included in copy: %q (%v)", tt.Clipboard(), err)
	}
}

func TestTableCellTabFocus(t *testing.T) {
	f := newCellFixture(2)
	f.before = func(c *Context) { Button(c, "Before") }
	f.after = func(c *Context) { Button(c, "After") }
	tt := f.tester(400, 220)
	tt.Click("name-0")
	tt.Key(0, KeyTab)
	expectCellRange(t, f, 0, 1, 0, 1)
	tt.Key(0, KeyEnd)
	tt.Key(0, KeyTab)
	expectCellRange(t, f, 1, 0, 1, 0)
	tt.Key(Shift, KeyTab)
	expectCellRange(t, f, 0, 2, 0, 2)
	tt.Key(Cmd, KeyEnd)
	tt.Key(0, KeyTab)
	if !tt.Focused("After") {
		t.Fatal("Tab at the last cell did not leave the table")
	}
	tt.Click("name-0")
	tt.Key(Shift, KeyTab)
	if !tt.Focused("Before") {
		t.Fatal("Shift+Tab at the first cell did not leave the table")
	}
}

func TestTableCellDragRectangle(t *testing.T) {
	f := newCellFixture(6)
	tt := f.tester(400, 280)
	a, _ := tt.Find("name-0")
	z, _ := tt.Find("note-3")
	tt.Press(a.X+2, a.Y+2)
	tt.Move(z.X+2, z.Y+2)
	tt.Release(z.X+2, z.Y+2)
	expectCellRange(t, f, 0, 0, 3, 2)
	tt.Key(Cmd, KeyC)
	values, err := ParseTSV(tt.Clipboard())
	if err != nil || len(values) != 4 || len(values[0]) != 3 {
		t.Fatalf("dragged range did not copy: %q, %v", tt.Clipboard(), err)
	}
}

func TestTableCellDragBeforeFrame(t *testing.T) {
	f := newCellFixture(6)
	tt := f.tester(400, 280)
	a, _ := tt.Find("name-0")
	z, _ := tt.Find("note-3")
	for _, ev := range []platform.SurfaceEvent{
		{Kind: platform.PointerDown, X: float64(a.X + 2), Y: float64(a.Y + 2)},
		{Kind: platform.PointerMove, X: float64(z.X + 2), Y: float64(z.Y + 2)},
		{Kind: platform.PointerUp, X: float64(z.X + 2), Y: float64(z.Y + 2)},
	} {
		tt.rt.event(ev)
	}
	tt.settle()
	expectCellRange(t, f, 0, 0, 3, 2)
}

func TestTableCellEmptyTableTab(t *testing.T) {
	f := newCellFixture(0)
	f.after = func(c *Context) { Button(c, "After") }
	tt := f.tester(400, 220)
	tt.Click("Name")
	tt.Key(0, KeyTab)
	if !tt.Focused("After") {
		t.Fatal("empty table trapped Tab")
	}
}

func TestTableCellLastEditorTabLeaves(t *testing.T) {
	f := newCellFixture(1)
	f.after = func(c *Context) { Button(c, "After") }
	tt := f.tester(400, 220)
	tt.Click("note-0")
	tt.Key(0, KeyF2)
	tt.Type("last")
	tt.Key(0, KeyTab)
	if !tt.Focused("After") || f.cells.Editing() != nil || f.rows[0].values[2] != "last" {
		t.Fatal("Tab stayed in an editor removed by commit")
	}
}

func TestTableCellQueuedTextBeforeCellClick(t *testing.T) {
	f := newCellFixture(2)
	tt := f.tester(400, 220)
	tt.Click("name-1")
	tt.Key(0, KeyF2)
	// Text and the click can arrive before the next display frame. The
	// clicked row is built before the editor, so an early commit loses it.
	r, _ := tt.Find("name-0")
	tt.rt.event(platform.SurfaceEvent{Kind: platform.TextInput, Text: "last keystroke"})
	tt.rt.event(platform.SurfaceEvent{Kind: platform.PointerDown, X: float64(r.X + 2), Y: float64(r.Y + 2)})
	tt.rt.event(platform.SurfaceEvent{Kind: platform.PointerUp, X: float64(r.X + 2), Y: float64(r.Y + 2)})
	tt.settle()
	if f.rows[1].values[0] != "last keystroke" || f.cells.Editing() != nil {
		t.Fatalf("queued text lost: %q, edit %+v", f.rows[1].values[0], f.cells.Editing())
	}
	expectCellRange(t, f, 0, 0, 0, 0)
}

func TestTableCellCompositionBlurCancels(t *testing.T) {
	f := newCellFixture(2)
	f.after = func(c *Context) { Button(c, "Other") }
	tt := f.tester(400, 220)
	tt.Click("name-0")
	tt.Key(0, KeyF2)
	tt.Compose("ni", 2)
	tt.Click("Other")
	if f.cells.Editing() != nil || f.rows[0].values[0] != "name-0" || f.apply != 0 || !tt.Focused("Other") {
		t.Fatalf("blur kept unconfirmed preedit: %+v", f.cells.Editing())
	}
}

func TestTableCellImmediateSortOnApply(t *testing.T) {
	for _, paste := range []bool{false, true} {
		t.Run(fmt.Sprint(paste), func(t *testing.T) {
			f := newCellFixture(3)
			apply := f.cells.Apply
			f.cells.Apply = func(changes []TableCellChange) {
				apply(changes)
				slices.Reverse(f.rows)
			}
			tt := f.tester(400, 220)
			tt.Click("name-0")
			if paste {
				tt.SetClipboard("updated")
				tt.Command("paste")
			} else {
				tt.Key(0, KeyF2)
				tt.Type("updated")
				tt.Key(0, KeyTab)
			}
			if f.cells.Selection == nil || f.cells.Selection.Cursor.Row != "key-0" || f.rows[2].values[0] != "updated" {
				t.Fatalf("Apply sort moved selection to another identity: %+v", f.cells.Selection)
			}
		})
	}
}

func TestTableCellEditingCommitCancelAndTab(t *testing.T) {
	f := newCellFixture(2)
	tt := f.tester(400, 220)
	tt.Click("name-0")
	tt.Key(0, KeyF2)
	tt.Type("changed")
	if f.rows[0].values[0] != "name-0" || f.apply != 0 {
		t.Fatal("draft leaked into the app before commit")
	}
	tt.Key(0, KeyEscape)
	if f.cells.Editing() != nil || f.rows[0].values[0] != "name-0" {
		t.Fatal("Escape did not cancel")
	}
	tt.Key(0, KeyEnter)
	tt.Type("committed")
	tt.Key(0, KeyTab)
	if f.cells.Editing() != nil || f.rows[0].values[0] != "committed" || f.apply != 1 {
		t.Fatalf("Tab commit: %q, edit %+v, apply %d", f.rows[0].values[0], f.cells.Editing(), f.apply)
	}
	expectCellRange(t, f, 0, 1, 0, 1)
	tt.Key(0, KeyF2)
	tt.Type("12")
	tt.Key(0, KeyEnter)
	if f.rows[0].values[1] != "12" || f.apply != 2 {
		t.Fatal("Return did not commit")
	}
	if _, active := tt.TextCaret(); active {
		t.Fatal("IME still active with focus on the table")
	}
}

func TestTableCellValidationAndFocus(t *testing.T) {
	f := newCellFixture(2)
	f.after = func(c *Context) { Button(c, "Other") }
	f.cells.Validate = func(changes []TableCellChange) error {
		if changes[0].Value == "bad" {
			return errors.New("invalid value")
		}
		changes[0].Value = "normalized"
		return nil
	}
	tt := f.tester(400, 220)
	tt.Click("name-0")
	tt.Key(0, KeyF2)
	tt.Type("bad")
	tt.Key(0, KeyEnter)
	if f.apply != 0 || f.cells.Error == nil || f.cells.Editing() == nil {
		t.Fatal("invalid draft was committed or lost")
	}
	tt.Click("Other")
	if tt.Focused("Other") || f.cells.Editing() == nil {
		t.Fatal("invalid draft did not retain focus")
	}
	tt.Key(Cmd, KeyA)
	tt.Type("good")
	tt.Click("Other")
	if !tt.Focused("Other") || f.cells.Editing() != nil || f.rows[0].values[0] != "normalized" {
		t.Fatalf("valid blur: focus %v, edit %+v, value %q", tt.Focused("Other"), f.cells.Editing(), f.rows[0].values[0])
	}
}

func TestTableCellEditorKeepsIdentity(t *testing.T) {
	f := newCellFixture(1500)
	tt := f.tester(400, 220)
	tt.Click("name-0")
	tt.Key(0, KeyF2)
	tt.Type("draft")
	edit, focused := f.cells.Editing(), tt.rt.focused
	// Move more than ListState's bounded search and reorder the column.
	f.rows[0], f.rows[1499] = f.rows[1499], f.rows[0]
	f.state.Columns.Order = []string{"note", "count", "name"}
	tt.Frame()
	if f.cells.Editing() != edit || edit.Cell != (TableCell{"key-0", "name"}) || tt.rt.focused != focused {
		t.Fatalf("edit/focus lost across moves: edit %+v, focus %d (was %d)", f.cells.Editing(), tt.rt.focused, focused)
	}
	tt.Key(0, KeyEnter)
	if f.rows[1499].values[0] != "draft" || f.rows[0].values[0] == "draft" {
		t.Fatalf("commit went to a different row: first %q, last %q", f.rows[0].values[0], f.rows[1499].values[0])
	}
	expectCellRange(t, f, 1499, 0, 1499, 0)
}

func TestTableCellRemovedIdentityCancels(t *testing.T) {
	for _, removeColumn := range []bool{false, true} {
		t.Run(fmt.Sprint(removeColumn), func(t *testing.T) {
			f := newCellFixture(2)
			tt := f.tester(400, 220)
			tt.Click("name-0")
			tt.Key(0, KeyF2)
			tt.Type("gone")
			if removeColumn {
				f.cols = f.cols[1:]
			} else {
				f.rows = f.rows[1:]
			}
			tt.Frame()
			if f.cells.Editing() != nil || f.cells.Selection != nil || f.apply != 0 {
				t.Fatal("removed cell applied a stale draft")
			}
		})
	}
}

func TestTableCellPluggableEditor(t *testing.T) {
	f := newCellFixture(2)
	f.cells.Editor = func(c *Context, e *TableCellEdit) *Element {
		box := Row(c)
		var in *Element
		box.Children(func() {
			in = TextInput(c, &e.Value).Label("Custom editor")
			if Button(c, "Save").Clicked() {
				e.Commit()
			}
			if Button(c, "Cancel").Clicked() {
				e.Cancel()
			}
		})
		return in
	}
	f.cols[0].Width = 270
	tt := f.tester(600, 220)
	tt.Click("name-0")
	tt.Key(0, KeyF2)
	tt.Type("custom")
	tt.Click("Save")
	if f.cells.Editing() != nil || f.rows[0].values[0] != "custom" {
		t.Fatal("custom editor did not commit")
	}
	tt.Key(0, KeyF2)
	tt.Type("canceled")
	tt.Click("Cancel")
	if f.cells.Editing() != nil || f.rows[0].values[0] != "custom" || f.apply != 1 {
		t.Fatal("custom editor did not cancel")
	}
}

func TestTableCellPopupEditor(t *testing.T) {
	f := newCellFixture(2)
	f.cols[0].Pin = PinLeft
	f.cells.Editor = func(c *Context, e *TableCellEdit) *Element {
		in := Select(c, &e.Value, []string{"name-0", "Chosen", "Other"}).MinWidth(0).Grow(1)
		if in.Changed() {
			e.Commit()
		}
		return in
	}
	tt := f.tester(300, 260)
	tt.Scroll(180, 100, 100, 0)
	tt.Click("name-0")
	tt.Key(0, KeyF2)
	tt.Key(0, KeyDown)
	if !tt.HasText("Chosen") || f.cells.Editing() == nil {
		t.Fatal("popup editor did not open")
	}
	tt.Key(0, KeyEscape)
	if tt.HasText("Chosen") || f.cells.Editing() == nil {
		t.Fatal("Escape should close the popup before canceling the editor")
	}
	tt.Key(0, KeyDown)
	r, _ := tt.Find("Chosen")
	if r.X < 0 || r.X > 120 {
		t.Fatalf("popup did not anchor to the frozen cell: %+v", r)
	}
	tt.Click("Chosen")
	if f.cells.Editing() != nil || f.rows[0].values[0] != "Chosen" || f.apply != 1 {
		t.Fatalf("popup press ended edit before choosing: %q, %+v, %d", f.rows[0].values[0], f.cells.Editing(), f.apply)
	}
}

func TestTableCellComposition(t *testing.T) {
	f := newCellFixture(2)
	tt := f.tester(400, 220)
	tt.Click("name-0")
	tt.Key(0, KeyF2)
	tt.Compose("ni", 2)
	tt.Key(0, KeyEnter)
	tt.Key(0, KeyEscape)
	if f.cells.Editing() == nil || f.apply != 0 {
		t.Fatal("composition keys committed or canceled the cell")
	}
	tt.Type("你")
	if f.apply != 0 {
		t.Fatal("IME commit leaked draft into the app")
	}
	tt.Key(0, KeyEnter)
	if f.rows[0].values[0] != "你" || f.cells.Editing() != nil {
		t.Fatalf("IME result was lost: %q", f.rows[0].values[0])
	}
}

func TestTableCellTSVPasteAtomic(t *testing.T) {
	f := newCellFixture(4)
	tt := f.tester(400, 220)
	tt.Click("name-0")
	tt.SetClipboard("new\t10\r\n\"a\tb\"\t20\r\n")
	tt.Command("paste")
	if f.apply != 1 || f.rows[0].values[0] != "new" || f.rows[1].values[0] != "a\tb" || f.rows[1].values[1] != "20" {
		t.Fatalf("rectangle paste: %+v (%v)", f.rows, f.cells.Error)
	}
	expectCellRange(t, f, 0, 0, 1, 1)
	tt.Key(Cmd, KeyC)
	values, err := ParseTSV(tt.Clipboard())
	if err != nil || !reflect.DeepEqual(values, [][]string{{"new", "10"}, {"a\tb", "20"}}) {
		t.Fatalf("copy: %q (%v)", tt.Clipboard(), err)
	}
	before := slices.Clone(f.rows)
	f.cells.Validate = func(changes []TableCellChange) error { return errors.New("last value invalid") }
	tt.SetClipboard("1\t2\n3\t4")
	tt.Key(Cmd, KeyV)
	if f.apply != 1 || f.cells.Error == nil || !reflect.DeepEqual(before, f.rows) {
		t.Fatal("failed validation partially applied a paste")
	}
	f.cells.Validate = nil
	f.cells.ReadOnly = func(row, col int) bool { return row == 1 && col == 1 }
	tt.Command("paste")
	if f.apply != 1 || f.cells.Error == nil || !reflect.DeepEqual(before, f.rows) {
		t.Fatal("read-only target partially applied a paste")
	}
	for _, value := range []string{"one", "1\t2\n3", "1\t2\t3\t4"} {
		tt.SetClipboard(value)
		tt.Command("paste")
		if f.apply != 1 || f.cells.Error == nil {
			t.Fatalf("invalid shape %q applied", value)
		}
	}
}

func TestTableCellPasteUsesDisplayedOrderAndKeys(t *testing.T) {
	f := newCellFixture(3)
	f.state.Columns.Order = []string{"note", "name", "count"}
	f.rows[0], f.rows[2] = f.rows[2], f.rows[0]
	tt := f.tester(400, 220)
	tt.Click("note-2")
	tt.SetClipboard("new note\tnew name\nother note\tother name")
	tt.Command("paste")
	if f.rows[0].key != "key-2" || f.rows[0].values != ([3]string{"new name", "2", "new note"}) || f.rows[1].values != ([3]string{"other name", "1", "other note"}) {
		t.Fatalf("paste ignored display order: %+v", f.rows)
	}
}

func TestTableCellAccessibility(t *testing.T) {
	f := newCellFixture(3)
	tt := f.tester(400, 220)
	tt.send(platform.SurfaceEvent{Kind: platform.AccessibilityOn})
	tt.Click("name-0")
	tt.Key(0, KeyRight)
	n := node(t, tt.h.access, platform.RoleCell, "Count: 0")
	if tt.h.access.Focus != n.ID || n.States&(platform.AccessSelectable|platform.AccessChecked) != platform.AccessSelectable|platform.AccessChecked {
		t.Fatalf("active cell: %+v, focus %d", n, tt.h.access.Focus)
	}
	other := node(t, tt.h.access, platform.RoleCell, "Note: note-1")
	tt.send(platform.SurfaceEvent{Kind: platform.AccessAction, ID: other.ID, Action: platform.AccessFocus})
	expectCellRange(t, f, 1, 2, 1, 2)
}

func TestTSV(t *testing.T) {
	for _, rows := range [][][]string{
		{{""}}, {{""}, {""}}, {{"a", ""}, {"", "b"}},
		{{"a\tb", "line\nline", "\"quoted\""}, {"你好", "a\r\nb", "tail"}},
	} {
		value := FormatTSV(rows)
		got, err := ParseTSV(value)
		if err != nil || !reflect.DeepEqual(got, rows) {
			t.Fatalf("roundtrip %q: %+v, %v", value, got, err)
		}
	}
	for _, value := range []string{"a\tb\r\nc\td\r\n", "a\tb\nc\td\n", "a\tb\rc\td\r"} {
		got, err := ParseTSV(value)
		if err != nil || !reflect.DeepEqual(got, [][]string{{"a", "b"}, {"c", "d"}}) {
			t.Fatalf("line endings %q: %v, %v", value, got, err)
		}
	}
	for _, value := range []string{"a\tb\nc", "\"unterminated", "\"a\"tail", "a\"b"} {
		if _, err := ParseTSV(value); err == nil {
			t.Fatalf("accepted invalid TSV %q", value)
		}
	}
}

func FuzzTSV(f *testing.F) {
	for _, seed := range []string{"", "a\tb\r\nc\td", "\"a\tb\"\t\"x\"\n\"\"\t\"\""} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		rows, err := ParseTSV(value)
		if err != nil {
			return
		}
		again, err := ParseTSV(FormatTSV(rows))
		if err != nil || !reflect.DeepEqual(rows, again) {
			t.Fatalf("TSV roundtrip: %+v, %+v, %v", rows, again, err)
		}
	})
}
