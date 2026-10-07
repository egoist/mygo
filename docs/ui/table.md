# Table

`ui.Table` puts a [list](list.md)'s rows in columns, under a header of
`ui.TableColumn`s, each with a title, a width (or 0 to share the room the
others leave) and an alignment; `cell` builds the content of a row's
column, usually a `Text`. It builds only the rows in view, as a list does,
and rows are as high as their tallest cell, so text that wraps makes its
row taller.

```go
cols := []ui.TableColumn{{Title: "Name"}, {Title: "Size", Width: 90, Align: ui.End}}
app.files.Selected = &app.file
if ui.Table(c, &app.files, cols, len(files), func(row, col int) {
	switch col {
	case 0:
		ui.Text(c, files[row].Name).SingleLine()
	case 1:
		ui.Text(c, files[row].Size())
	}
}).Grow(1).Submitted() {
	app.open(files[app.file])
}
```

## Choosing rows

The table takes the same `ListState` as a list, or `nil`, and takes the
keyboard focus for it: with `Selected`, a click or the keys choose a row,
with `Selection` several, and `Submitted` reports a double click or Enter;
`Key`, `Label`, `Reorder` and the rest work as for a list. A row `Header`
names spans every column, which `cell` builds as column 0, and stays at the
top while its section's rows scroll under it.

## Arranging columns

The user arranges the columns, as in Finder:

- **Resizing.** Dragging the right edge of a column's header resizes the
  column, between its `MinWidth` and `MaxWidth`, and a double click there
  fits it to its cells. The columns before it that share the room left keep
  their widths, for the edge to follow the pointer.
- **Moving.** Dragging a header moves its column among the others.
- **Scrolling sideways.** Columns wider than the table scroll sideways, the
  header with them.
- **Fixed columns.** A `Fixed` column stays where it is, as wide as it is.
- **Pinned columns.** `Pin: ui.PinLeft` or `ui.PinRight` freezes a column
  at that edge while other columns scroll. Pins are independent of
  `Fixed`: a pin can still be resized and moved within its pin group.

`ListState.Columns` keeps the order and the widths, by the columns' `ID`s
(their titles by default): set it from saved settings to restore them, and
save it as it changes.

Pinned columns appear in left, scrolling, right groups, each preserving
its layout order. Headers, cells and editors use the same geometry and
clip: scrolling content never paints over or receives hits through a pin.
Navigation reveals cells in the space between pins. If pins fill the
viewport, left pins take precedence and the scrolling region is empty;
give the table enough room for the columns the user needs to reach.

## Sorting

With `ListState.Sort`, a click on the header of a `Sortable` column sorts
the rows by it, ascending, and a second click reverses the order: the header
shows an arrow, assistive technology reads the order, and the table's
`Changed` reports it. The table shows the rows in the order `cell` gets
them, so sort them as it says, keyed by `Key` for the choice to follow its
rows:

```go
cols := []ui.TableColumn{{Title: "Name", Sortable: true}, {Title: "Size", Width: 90, Align: ui.End, Sortable: true}}
app.files.Sort = &app.sort
sorted := sortFiles(app.files, app.sort) // by app.sort.Column, Descending or not
app.files.Key = func(i int) any { return sorted[i].ID }
```

## Renaming in place

An [editable text](editable-text.md) in a cell is renamed in place, as
Finder's file names: Return on macOS and F2 elsewhere edit the chosen row's
text, as does a click on it once the row is chosen.

```go
ui.Table(c, &app.files, cols, len(files), func(row, col int) {
	if col == 0 && ui.EditableText(c, &files[row].Name).Changed() {
		app.rename(files[row])
	}
})
```

## Accessibility

Assistive technology sees a table, named by its `Label`, of rows named by
the text inside them, each saying which of all the rows it is, with column
headers that say the order the rows are sorted in.

With cell selection enabled, the active cell is the table's accessibility
active descendant. Each built cell exposes its name, selection and focus
action, and only its visible bounds, also when it is pinned or partially
hidden by a pin. Native collection providers and offscreen cell discovery
are covered separately by the collection-accessibility roadmap work.

## Choosing and editing cells

Set `ListState.Cells` to a `TableCellState` to opt into spreadsheet
interactions. Existing tables retain row selection and `EditableText`
behavior. Cell mode uses the arrows and editing keys for cells; row
selection still follows cell clicks and navigation. A configured row
`Reorder` keeps its drag gesture; otherwise dragging selects a rectangle.

```go
// Keep these in the app's view state, like ListState itself.
app.table.Cells = &app.cells
app.table.Key = func(i int) any { return rows[i].ID }
app.cells.Value = func(row, col int) string {
    return rows[row].Values[col]
}
app.cells.Validate = func(changes []ui.TableCellChange) error {
    for _, change := range changes {
        if change.Cell.Column == "Quantity" {
            n, err := strconv.Atoi(change.Value)
            if err != nil || n < 0 {
                return fmt.Errorf("Quantity must be a nonnegative integer")
            }
        }
    }
    return nil
}
app.cells.Apply = func(changes []ui.TableCellChange) {
    // Use stable keys: a sort can change indexes when values change.
    for _, change := range changes {
        app.byID[change.Cell.Row.(string)].Values[change.Column] = change.Value
    }
}
ui.Table(c, &app.table, columns, len(rows), func(row, col int) {
    ui.Text(c, rows[row].Values[col]).SingleLine()
}).Grow(1)
if err := app.cells.Error; err != nil {
    ui.Text(c, err.Error())
}
```

`Value` reads all cells, including rows outside the viewport. Without
`Apply` the table can select and copy cells but cannot edit or paste.
`ReadOnly(row, col)` can protect individual cells. Column indexes in these
hooks and `TableCellChange.Column` refer to the original columns slice;
transfer and keyboard navigation follow the displayed order.

The inclusive `Selection` has `Anchor` and `Cursor` cells, each a row key
and column ID. Set it to select from the app; `nil` selects nothing. Keys
must be comparable and unique, column IDs nonempty and unique. Stable keys
keep both ends and the current editor with their items across sorting and
reordering. The rectangle spans the endpoints in the *current* displayed
order, so interior rows can change after a sort. Removing an endpoint
clears the range; removing an edited cell cancels its draft. `RowIndex`
can resolve keys efficiently in very large datasets; the fallback searches
the whole dataset only when an endpoint moves.

| Key/action | Behavior |
|---|---|
| Click / drag / Shift-click | Select a cell / rectangle / extend from the anchor |
| Arrows / Shift+arrows | Move the cursor / extend a rectangle |
| Home, End | First, last displayed column of the current row |
| Cmd/Ctrl+Home, End | First, last cell of the table |
| Page Up, Down | Move a viewport of rows |
| Tab, Shift+Tab | Next, previous cell, wrapping rows; leave at the ends |
| Return, F2, double click | Edit the active cell |
| Escape while editing | Discard the draft and return focus to the table |
| Return while editing | Validate and commit, then return focus to the table |
| Tab, Shift+Tab while editing | Validate and commit, then traverse cells |
| Cmd/Ctrl+C, V, A | Copy, paste, select all data cells |

Section headers are skipped by selection/navigation and transfer. Editors
keep their own text arrows, clipboard commands, undo history and IME
context. Composition keys do not commit or cancel a table edit. Moving
focus away commits a finished draft and leaves focus at the destination;
an unfinished composition cancels. A validation failure retains the editor,
draft and focus and sets `Error` for the app to display. Edits are applied
after rows finish building so an app may sort immediately in `Apply`.

### Custom editors

The default is a single-line text input with all its value selected.
`Editor` can return a different control bound to the session's `Value`:

```go
app.cells.Editor = func(c *ui.Context, edit *ui.TableCellEdit) *ui.Element {
    if edit.Cell.Column == "Owner" {
        in := ui.Select(c, &edit.Value, []string{"Alex", "Sam", "Lee"})
        if in.Changed() {
            edit.Commit()
        }
        return in // return the editor's focus target
    }
    return ui.TextInput(c, &edit.Value).MinWidth(0).Grow(1)
}
```

`Editing()` returns the active draft. Custom actions can call its `Commit`
or `Cancel` from the view; no model changes reach `Apply` before commit.
Editor controls may take their own shortcuts, as a dropdown choosing an
option does. Keep the focus target's identity consistent between frames.

## Rectangular copy/paste and TSV

Copy reads the selected rectangle in displayed order. Paste at a single
cell uses it as the rectangle's top-left origin; into a larger selected
range it must match that range's dimensions. Out-of-bounds, ragged and
read-only targets reject the entire paste. No cells change until the app's
`Validate` accepts the whole batch; it may normalize each `Value` in place,
but must preserve identities and avoid mutating the model. `Apply` receives
one batch once. The table keeps the resulting range attached to its keys
even if applying values changes the sort. App-level undo can record each
batch's `Before` values; table-level undo is not provided.

`ui.FormatTSV` and `ui.ParseTSV` also support file or other app interchange.
They preserve tabs and multiline values with quoted fields, double quotes
with doubled quotes, and empty fields/rows. The parser accepts LF, CRLF and
CR record separators and ignores one final separator. Empty input is one
empty cell. Transfers are bounded to 1,048,576 cells. TSV goes through the
existing text clipboard; no new clipboard platform API is required.

Run `go run ./examples/gallery` and open **Lists → Editable cells** for
editable inventory rows, a dropdown editor, quantity validation, TSV
paste, sorting, and pins at both edges.

For a tree of rows in columns, see [Outline](outline.md#in-columns).
