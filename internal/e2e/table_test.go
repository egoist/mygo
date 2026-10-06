package e2e

import (
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// TestContentWindowTablePins verifies native mouse hit testing at both
// frozen edges, and sorting/resizing through the real surface's events.
func TestContentWindowTablePins(t *testing.T) {
	var frames atomic.Int32
	cells := ui.TableCellState{Value: func(row, col int) string { return fmt.Sprintf("row%d col%d", row, col) }}
	sort := ui.SortOrder{}
	s := ui.ListState{Cells: &cells, Sort: &sort}
	cols := []ui.TableColumn{{Title: "Name", Width: 120, Pin: ui.PinLeft, Sortable: true}, {Title: "Details", Width: 400}, {Title: "ID", Width: 80, Pin: ui.PinRight}}
	view := func(c *ui.Context) {
		frames.Add(1)
		ui.Table(c, &s, cols, 2, func(row, col int) { ui.Text(c, cells.Value(row, col)).SingleLine() }).Fill()
	}
	w := newWindow(t, mygo.WindowOptions{Title: "Table pins", Width: 400, Height: 240, UseContentSize: true, Content: ui.View(view)})
	eventually(t, "a pinned table frame", func() bool { return frames.Load() > 0 })
	if !click(w, 360, 48) {
		t.Skip("native pointer automation unavailable on this platform")
	}
	eventually(t, "the right-pinned cell", func() (ok bool) {
		mygo.RunOnMain(func() { ok = cells.Selection != nil && cells.Selection.Cursor == (ui.TableCell{Row: 0, Column: "ID"}) })
		return
	})
	click(w, 40, 48)
	eventually(t, "the left-pinned cell", func() (ok bool) {
		mygo.RunOnMain(func() { ok = cells.Selection.Cursor.Column == "Name" })
		return
	})
	click(w, 40, 16)
	eventually(t, "sorting a pinned column", func() (ok bool) {
		mygo.RunOnMain(func() { ok = sort.Column == "Name" })
		return
	})
	if !drag(w, [][2]float64{{118, 16}, {123, 16}, {138, 16}, {148, 16}}) {
		t.Skip("native header drag automation unavailable on this platform")
	}
	eventually(t, "resizing a pin", func() (ok bool) {
		mygo.RunOnMain(func() { ok = s.Columns.Widths["Name"] == 150 })
		return
	})
}

// TestContentWindowTableCells uses the native keyboard/input-method and
// Edit menu paths for a pinned cell editor and rectangular TSV transfer.
func TestContentWindowTableCells(t *testing.T) {
	var frames atomic.Int32
	values := [2][2]string{{"cafe", "4"}, {"tea", "8"}}
	cells := ui.TableCellState{}
	s := ui.ListState{Cells: &cells, Key: func(i int) any { return []string{"a", "b"}[i] }}
	cells.Value = func(row, col int) string { return values[row][col] }
	cells.Apply = func(changes []ui.TableCellChange) {
		for _, change := range changes {
			values[change.Row][change.Column] = change.Value
		}
	}
	view := func(c *ui.Context) {
		frames.Add(1)
		ui.Table(c, &s, []ui.TableColumn{{Title: "Name", Width: 120, Pin: ui.PinLeft}, {Title: "Quantity", Width: 400}}, 2, func(row, col int) {
			ui.Text(c, values[row][col]).SingleLine()
		}).Fill()
	}
	state := func() (value string, draft string, editing bool) {
		mygo.RunOnMain(func() {
			value = values[0][0]
			if e := cells.Editing(); e != nil {
				draft, editing = e.Value, true
			}
		})
		return
	}
	w := newWindow(t, mygo.WindowOptions{Title: "Table cells", Width: 400, Height: 240, Content: ui.View(view)})
	eventually(t, "a table frame", func() bool { return frames.Load() > 0 })
	if !click(w, 30, 48) || !pressKey(w, 36, "\r") {
		t.Skip("native table key automation unavailable on this platform")
	}
	eventually(t, "a pinned cell editor", func() bool { _, _, editing := state(); return editing })
	compose(w, "ni", 2, false)
	pressKey(w, 36, "\r") // chooses an IME candidate, never the cell
	pressKey(w, 53, "\x1b")
	if value, _, editing := state(); value != "cafe" || !editing {
		t.Fatalf("composition keys changed the model: %q, editing %v", value, editing)
	}
	compose(w, "你", 0, true)
	eventually(t, "the composed draft", func() bool { value, draft, editing := state(); return value == "cafe" && draft == "你" && editing })
	pressKey(w, 36, "\r")
	eventually(t, "Return committing the cell", func() bool { value, _, editing := state(); return value == "你" && !editing })
	pressKey(w, 36, "\r")
	eventually(t, "editing again", func() bool { _, _, editing := state(); return editing })
	compose(w, "discarded", 0, true)
	pressKey(w, 53, "\x1b")
	eventually(t, "Escape canceling the draft", func() bool { value, _, editing := state(); return value == "你" && !editing })
	previous := mygo.Clipboard.ReadText()
	defer mygo.Clipboard.WriteText(previous)
	mygo.Clipboard.WriteText("pasted\t12")
	if handled, supported := pressShortcut("v", false); !supported || !handled {
		t.Skip("native Edit menu shortcut automation unavailable")
	}
	eventually(t, "a rectangular paste", func() (ok bool) {
		mygo.RunOnMain(func() { ok = values[0] == [2]string{"pasted", "12"} })
		return
	})
	pressShortcut("c", false)
	eventually(t, "TSV from the selected rectangle", func() bool { return mygo.Clipboard.ReadText() == "pasted\t12" })
}
