package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
)

type gallerySheetRow struct {
	id       int
	item     string
	owner    string
	quantity int
	note     string
}

func (g *gallery) sheetCard(c *ui.Context) {
	if g.sheetRows == nil {
		for i, item := range []string{"Keyboard", "Display", "Notebook", "Headphones", "Camera", "Adapter", "Desk lamp", "Microphone"} {
			g.sheetRows = append(g.sheetRows, gallerySheetRow{i + 1, item, []string{"Alex", "Sam", "Lee"}[i%3], i + 1, "Ready for pickup"})
		}
	}
	rows := make([]*gallerySheetRow, len(g.sheetRows))
	for i := range g.sheetRows {
		rows[i] = &g.sheetRows[i]
	}
	value := func(r *gallerySheetRow, col int) string {
		switch col {
		case 0:
			return r.item
		case 1:
			return r.owner
		case 2:
			return strconv.Itoa(r.quantity)
		case 3:
			return r.note
		default:
			return strconv.Itoa(r.id)
		}
	}
	cols := []ui.TableColumn{
		{Title: "Item", Width: 160, Pin: ui.PinLeft, Sortable: true},
		{Title: "Owner", Width: 180, Sortable: true},
		{Title: "Quantity", Width: 100, Align: ui.End, Sortable: true},
		{Title: "Notes", Width: 480},
		{Title: "ID", Width: 60, Pin: ui.PinRight, Fixed: true},
	}
	slices.SortStableFunc(rows, func(a, b *gallerySheetRow) int {
		col := slices.IndexFunc(cols, func(col ui.TableColumn) bool { return col.Title == g.sheetSort.Column })
		if col < 0 {
			return 0
		}
		d := strings.Compare(value(a, col), value(b, col))
		if col == 2 {
			d = a.quantity - b.quantity
		}
		if g.sheetSort.Descending {
			return -d
		}
		return d
	})
	g.sheet.Key = func(i int) any { return rows[i].id }
	g.sheet.Cells, g.sheet.Sort = &g.sheetCells, &g.sheetSort
	g.sheetCells.Value = func(row, col int) string { return value(rows[row], col) }
	g.sheetCells.ReadOnly = func(row, col int) bool { return col == 4 }
	g.sheetCells.Validate = func(changes []ui.TableCellChange) error {
		for i := range changes {
			change := &changes[i]
			switch change.Cell.Column {
			case "Item", "Owner":
				change.Value = strings.TrimSpace(change.Value)
				if change.Value == "" {
					return fmt.Errorf("%s cannot be empty", change.Cell.Column)
				}
			case "Quantity":
				n, err := strconv.Atoi(change.Value)
				if err != nil || n < 0 {
					return fmt.Errorf("Quantity must be a nonnegative integer")
				}
				change.Value = strconv.Itoa(n)
			}
		}
		return nil
	}
	g.sheetCells.Apply = func(changes []ui.TableCellChange) {
		for _, change := range changes {
			for i := range g.sheetRows {
				r := &g.sheetRows[i]
				if r.id != change.Cell.Row {
					continue
				}
				switch change.Cell.Column {
				case "Item":
					r.item = change.Value
				case "Owner":
					r.owner = change.Value
				case "Quantity":
					r.quantity, _ = strconv.Atoi(change.Value)
				case "Notes":
					r.note = change.Value
				}
			}
		}
	}
	g.sheetCells.Editor = func(c *ui.Context, edit *ui.TableCellEdit) *ui.Element {
		if edit.Cell.Column == "Owner" {
			in := ui.Select(c, &edit.Value, []string{"Alex", "Sam", "Lee"}).MinWidth(0).Grow(1)
			if in.Changed() {
				edit.Commit()
			}
			return in
		}
		return ui.TextInput(c, &edit.Value).MinWidth(0).Grow(1)
	}
	card(c, "Editable cells", func() {
		ui.Table(c, &g.sheet, cols, len(rows), func(row, col int) {
			ui.Text(c, value(rows[row], col)).SingleLine()
		}).Grow(1)
		ui.Text(c, "Click a cell; drag or Shift+arrows to select a rectangle. Return/F2 edits, Escape cancels, and Tab commits and moves. Copy/paste TSV from a spreadsheet. Owner uses a custom dropdown; Quantity validates the whole paste. Item and ID stay visible while Notes scrolls.").FontSize(12).TextColor(c.Theme().TextMuted)
		if err := g.sheetCells.Error; err != nil {
			ui.Text(c, err.Error()).TextColor(c.Theme().Danger)
		}
	}).Height(340)
}
