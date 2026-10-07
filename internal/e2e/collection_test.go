package e2e

import (
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// TestContentWindowCollectionAccessibility calls the platform providers,
// including lazy offscreen cells, realization, headers and selection.
func TestContentWindowCollectionAccessibility(t *testing.T) {
	var frames atomic.Int32
	selected := -1
	var chosen ui.Selection[int]
	state := ui.ListState{Selected: &selected, Selection: &chosen, Label: func(i int) string { return fmt.Sprintf("Row %d", i) }}
	view := func(c *ui.Context) {
		frames.Add(1)
		ui.Table(c, &state, []ui.TableColumn{{Title: "Name", Width: 180}, {Title: "Size", Width: 180, AccessibilityLabel: func(i int) string { return fmt.Sprintf("Size %d", i) }}}, 5000, func(row, col int) { ui.Textf(c, "Cell %d %d", row, col) }).Label("Files").Grow(1)
	}
	w := newWindow(t, mygo.WindowOptions{Title: "Collection accessibility", Width: 300, Height: 220, Content: ui.View(view)})
	eventually(t, "a collection frame", func() bool { return frames.Load() > 0 })
	if _, ok := accessibility(w); !ok {
		t.Skip("native accessibility unavailable")
	}
	far, ok := collectionProbe(w, "Files", 4000, 1)
	if !ok {
		t.Fatal("native collection provider unavailable")
	}
	if far.Rows != 5000 || far.Columns != 2 || far.Row != 4000 || far.Column != 1 || far.RowSpan != 1 || far.ColumnSpan != 1 || far.Header != "Size" || !far.Virtualized {
		t.Fatalf("offscreen provider cell: %+v", far)
	}
	externalCollection(t, "Files", 5000, 2, 4000, 1)
	if !collectionPerform(w, "Files", 4000, 1, "realize") {
		t.Fatal("native realization request failed")
	}
	eventually(t, "a realized offscreen cell", func() bool {
		cell, ok := collectionProbe(w, "Files", 4000, 1)
		return ok && !cell.Virtualized && cell.Label == "Cell 4000 1"
	})
	for _, row := range []int{3, 4000} {
		if !collectionPerform(w, "Files", row, 0, "add") {
			t.Fatalf("native add selection %d failed", row)
		}
	}
	eventually(t, "two selected rows including one out of view", func() bool { probe, ok := collectionProbe(w, "Files", 4000, 1); return ok && probe.Selected == 2 })
	if !collectionPerform(w, "Files", 3, 0, "remove") {
		t.Fatal("native remove selection failed")
	}
	eventually(t, "remaining selection", func() bool { probe, ok := collectionProbe(w, "Files", 4000, 1); return ok && probe.Selected == 1 })
	var first int
	mygo.RunOnMain(func() { first, _ = state.Visible() })
	if !collectionPerform(w, "Files", 0, 0, "scroll") {
		t.Fatal("native page scroll failed")
	}
	eventually(t, "collection scrolled by its provider", func() bool {
		var after int
		mygo.RunOnMain(func() { after, _ = state.Visible() })
		return after > first
	})
	var selectionOK bool
	mygo.RunOnMain(func() { selectionOK = chosen.Len() == 1 && chosen.Has(4000) })
	if !selectionOK {
		t.Fatal("native scrolling changed row selection")
	}
	if !collectionLifetime(w, "Files", 4000, 1) {
		t.Fatal("retained provider did not become unavailable after window disposal")
	}
}

func TestContentWindowGridAccessibility(t *testing.T) {
	var frames atomic.Int32
	selected := -1
	var chosen ui.Selection[int]
	state := ui.GridState{Selected: &selected, Selection: &chosen, Label: func(i int) string { return fmt.Sprintf("Item %d", i) }}
	w := newWindow(t, mygo.WindowOptions{Title: "Grid accessibility", Width: 448, Height: 220, Content: ui.View(func(c *ui.Context) {
		frames.Add(1)
		ui.GridView(c, &state, 10_003, 100, 60, func(i int) { ui.Textf(c, "Tile %d", i) }).Label("Tiles").Grow(1)
	})})
	eventually(t, "a grid frame", func() bool { return frames.Load() > 0 })
	if _, ok := accessibility(w); !ok {
		t.Skip("native accessibility unavailable")
	}
	far, ok := collectionProbe(w, "Tiles", 2500, 2)
	if !ok || far.Rows != 2501 || far.Columns != 4 || far.Row != 2500 || far.Column != 2 || !far.Virtualized {
		t.Fatalf("offscreen grid provider: %+v (%v)", far, ok)
	}
	if !collectionPerform(w, "Tiles", 2500, 2, "realize") {
		t.Fatal("grid realization failed")
	}
	eventually(t, "a realized grid item", func() bool {
		n, ok := collectionProbe(w, "Tiles", 2500, 2)
		return ok && !n.Virtualized && n.Label == "Item 10002"
	})
	if !collectionPerform(w, "Tiles", 0, 1, "add") || !collectionPerform(w, "Tiles", 2500, 2, "add") {
		t.Fatal("grid multiselection failed")
	}
	eventually(t, "grid selection beyond the viewport", func() bool { n, ok := collectionProbe(w, "Tiles", 2500, 2); return ok && n.Selected == 2 })
}
