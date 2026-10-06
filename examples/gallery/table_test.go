package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestEditableInventory(t *testing.T) {
	g := &gallery{}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() { g.sheetCard(c) })
	}, 720, 420)
	tt.Click("Keyboard")
	tt.Key(0, ui.KeyRight)
	tt.Key(0, ui.KeyRight)
	tt.Key(0, ui.KeyEnter)
	tt.Type("invalid")
	tt.Key(0, ui.KeyEnter)
	if g.sheetCells.Error == nil || g.sheetRows[0].quantity != 1 || g.sheetCells.Editing() == nil {
		t.Fatal("inventory quantity validator did not retain the draft")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Key(0, ui.KeyLeft)
	tt.Key(0, ui.KeyEnter)
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyEnter)
	if g.sheetRows[0].owner != "Sam" || g.sheetCells.Editing() != nil {
		t.Fatal("inventory dropdown did not apply the choice")
	}
	tt.Key(0, ui.KeyRight)
	tt.Key(0, ui.KeyRight)
	tt.SetClipboard("Line 1\nLine 2")
	tt.Command("paste")
	if g.sheetCells.Error != nil || g.sheetRows[0].note != "Line 1" || g.sheetRows[1].note != "Line 2" {
		t.Fatalf("inventory TSV paste failed: %v", g.sheetCells.Error)
	}
}
