package ui

import (
	"fmt"
	"slices"
	"strings"
)

// TableCell identifies a cell by its row's ListState.Key (the row index
// without Key) and its TableColumn.ID (the title without ID).
type TableCell struct {
	Row    any
	Column string
}

// TableCellRange is an inclusive rectangle between two cells. Its ends
// follow their keys and column IDs; the rectangle uses the current row
// and displayed column order, including pinned columns.
type TableCellRange struct {
	Anchor, Cursor TableCell
}

// TableCellChange is a proposed edit, before it reaches the app. Row and
// Column index the current data, Column in the original columns slice;
// Cell is the stable identity. Value is the proposed text.
type TableCellChange struct {
	Cell          TableCell
	Row, Column   int
	Before, Value string
}

// TableCellEdit is an editor's draft. Bind a control to Value, and return
// its focus target from TableCellState.Editor. Original and Cell describe
// the session; the table keeps them attached to the same cell as it moves.
// Call Commit or Cancel from the view for a custom editor's own actions.
type TableCellEdit struct {
	Cell            TableCell
	Original, Value string
	commit, cancel  bool
}

// Commit asks the table to validate and apply the draft in this frame.
// Validation failure keeps the draft, editor and focus for correction.
func (e *TableCellEdit) Commit() { e.commit = true }

// Cancel discards the draft and returns focus to the table.
func (e *TableCellEdit) Cancel() { e.cancel = true }

// TableCellState opts a Table into cell interactions (ListState.Cells).
// Keep it in the view's state and access it from the view, like ListState.
// Arrows move the cursor, Shift extends a rectangle, Home/End move within
// a row, Cmd/Ctrl+Home/End to the table's ends, and Tab traverses cells.
// F2, Return or a double click edits; Escape cancels; Return and Tab commit.
// At either end Tab leaves the table. Copy/paste use rectangular TSV.
type TableCellState struct {
	// Selection is nil before a cell is chosen. Set it to choose cells
	// from the app; removed endpoints clear it. Keys must be comparable
	// and unique, and column IDs nonempty and unique.
	Selection *TableCellRange
	// Value reads a cell's transfer value and initial editor text, even
	// for rows outside the viewport. Without it copy and editing are off.
	Value func(row, col int) string
	// Apply receives a whole validated batch once, in displayed row/column
	// order. Update the model by Cell for stable identity. Nil is read-only.
	Apply func(changes []TableCellChange)
	// Validate checks the entire edit or paste before Apply is called.
	// It may normalize Value in the changes. Rejecting any value rejects
	// the whole batch. It must not change identities or mutate the model.
	Validate func(changes []TableCellChange) error
	// ReadOnly may protect individual cells. A paste touching any of them
	// is rejected as a whole, including cells whose values are unchanged.
	ReadOnly func(row, col int) bool
	// Editor builds a control bound to the draft and returns its focus
	// target. Nil uses a single-line text input with all text selected.
	// Text inputs keep their own arrows, clipboard, undo and IME behavior.
	Editor func(c *Context, edit *TableCellEdit) *Element
	// RowIndex optionally resolves keys in large keyed datasets. Return
	// -1 for a removed row. Without it, a moved endpoint is found by a
	// full search; stationary endpoints need only one key lookup.
	RowIndex func(key any) int
	// Error is the last validation/transfer failure, cleared by a
	// successful edit/paste or cancellation. Render it beside the table.
	Error error

	edit                          *TableCellEdit
	anchorRow, cursorRow, editRow int
	editID                        uint64
	editBoxID                     uint64
	fresh, reveal                 bool
	clicked                       *tableCellClick
	dragging                      bool
	press                         *tableCellPress
	action                        *tableEditAction
}

type tableCellClick struct {
	row    int
	cell   TableCell
	mods   Modifiers
	double bool
}

type tableEditAction struct {
	commit, focus bool
	tab           int // -1 back, +1 forward, 0 stays
}

type tableCellPress struct {
	row, col int
	x, y     float32
	mods     Modifiers
}

// Editing returns the current draft, or nil. Inspect it from the view;
// the same draft survives sorting, row moves and column reordering.
func (s *TableCellState) Editing() *TableCellEdit { return s.edit }

type tableCellFrame struct {
	c       *Context
	owner   *Element
	s       *ListState
	state   *TableCellState
	columns []TableColumn
	order   []int
	n       int
	a, z    int // displayed columns of the range ends
	boxes   []tableCellBox
}

type tableCellBox struct {
	e        *Element
	row, col int
}

func (f *tableCellFrame) key(row int) any {
	if f.s.Key != nil {
		return f.s.Key(row)
	}
	return row
}

func (f *tableCellFrame) header(row int) bool {
	return f.s.Header != nil && f.s.Header(row)
}

func (f *tableCellFrame) ref(row, displayed int) TableCell {
	return TableCell{f.key(row), f.columns[f.order[displayed]].id()}
}

func (f *tableCellFrame) resolve(cell TableCell, hint int) (row, col int, ok bool) {
	col = slices.IndexFunc(f.order, func(j int) bool { return f.columns[j].id() == cell.Column })
	if col < 0 {
		return 0, 0, false
	}
	row = -1
	switch {
	case f.s.Key == nil:
		row, _ = cell.Row.(int)
		if _, ok := cell.Row.(int); !ok {
			row = -1
		}
	case hint >= 0 && hint < f.n && f.key(hint) == cell.Row:
		row = hint
	case f.state.RowIndex != nil:
		row = f.state.RowIndex(cell.Row)
	default:
		for i := range f.n {
			if f.key(i) == cell.Row {
				row = i
				break
			}
		}
	}
	return row, col, row >= 0 && row < f.n && f.key(row) == cell.Row && !f.header(row)
}

func (f *tableCellFrame) sync() {
	s := f.state
	ids := make(map[string]bool, len(f.columns))
	for _, col := range f.columns {
		id := col.id()
		if id == "" || ids[id] {
			panic("ui: cell tables require nonempty, unique column IDs")
		}
		ids[id] = true
	}
	if r := s.Selection; r != nil {
		a, ac, aok := f.resolve(r.Anchor, s.anchorRow)
		z, zc, zok := f.resolve(r.Cursor, s.cursorRow)
		if !aok || !zok {
			s.Selection = nil
		} else {
			if cur := f.s.cursor(); cur != nil && *cur == f.s.selRow && f.s.selKey == r.Cursor.Row {
				*cur, f.s.selRow = z, z
			}
			if f.s.Selection != nil && f.s.pivotKey == r.Anchor.Row {
				f.s.pivot = a
			}
			s.anchorRow, s.cursorRow, f.a, f.z = a, z, ac, zc
		}
	}
	if s.edit != nil {
		row, _, ok := f.resolve(s.edit.Cell, s.editRow)
		if !ok {
			f.finish(false, f.editorFocused())
		} else {
			if row != s.editRow {
				// Build the editor even after a move beyond the list's
				// bounded search for its previously focused row.
				f.s.ScrollIntoView(row)
			}
			s.editRow = row
		}
	}
}

func (f *tableCellFrame) selectCell(row, col int, extend bool) {
	s, ref := f.state, f.ref(row, col)
	if s.Selection == nil || !extend {
		s.Selection = &TableCellRange{ref, ref}
		s.anchorRow, f.a = row, col
	} else {
		s.Selection.Cursor = ref
	}
	s.cursorRow, f.z, s.reveal = row, col, true
	f.s.ScrollIntoView(row)
	f.owner.st.changed, f.c.rt.consumed = true, true
}

func (f *tableCellFrame) selected(row, col int) bool {
	s := f.state
	return s.Selection != nil && row >= min(s.anchorRow, s.cursorRow) && row <= max(s.anchorRow, s.cursorRow) &&
		col >= min(f.a, f.z) && col <= max(f.a, f.z)
}

func (f *tableCellFrame) editable(row, col int) bool {
	s := f.state
	return s.Value != nil && s.Apply != nil && (s.ReadOnly == nil || !s.ReadOnly(row, col))
}

func (f *tableCellFrame) begin(row, displayed int) {
	s, col := f.state, f.order[displayed]
	if s.edit != nil || !f.editable(row, col) {
		return
	}
	f.selectCell(row, displayed, false)
	value := s.Value(row, col)
	s.edit = &TableCellEdit{Cell: f.ref(row, displayed), Original: value, Value: value}
	s.editRow, s.fresh, s.Error = row, true, nil
	f.c.rt.consumed = true
}

func (f *tableCellFrame) editorFocused() bool {
	rt := f.c.rt
	box := &Element{id: f.state.editBoxID}
	if rt.pressedWithin(rt.focused, box) {
		return true
	}
	// Popup options can take presses without taking keyboard focus. The
	// editor owns that gesture through its popup's anchor.
	return rt.pressed != nil && rt.pressedWithin(rt.pressed.id, box)
}

func (f *tableCellFrame) finish(commit, focus bool) bool {
	s := f.state
	if s.edit == nil {
		return true
	}
	if commit {
		row, col, ok := f.resolve(s.edit.Cell, s.editRow)
		if ok {
			if !f.editable(row, f.order[col]) {
				s.Error = fmt.Errorf("cell %v/%s is read-only", s.edit.Cell.Row, s.edit.Cell.Column)
				return false
			}
			if err := f.apply([]TableCellChange{{s.edit.Cell, row, f.order[col], s.Value(row, f.order[col]), s.edit.Value}}); err != nil {
				s.Error = err
				return false
			}
		}
	}
	s.edit, s.editID, s.editBoxID, s.Error = nil, 0, 0, nil
	// Applying a value can sort or filter the model immediately. Resolve
	// range ends before Tab uses their positions again.
	f.sync()
	if focus {
		f.owner.Focus()
		f.c.rt.focusVisible = true
	}
	f.c.rt.consumed = true
	return true
}

func (f *tableCellFrame) apply(changes []TableCellChange) error {
	s := f.state
	for _, v := range changes {
		if !f.editable(v.Row, v.Column) {
			return fmt.Errorf("cell %v/%s is read-only", v.Cell.Row, v.Cell.Column)
		}
	}
	if s.Validate != nil {
		if err := s.Validate(changes); err != nil {
			return err
		}
	}
	s.Apply(changes)
	s.Error = nil
	f.owner.st.changed, f.c.rt.consumed = true, true
	return nil
}

func (f *tableCellFrame) cell(box *Element, row, displayed int, fill func()) {
	s, col := f.state, f.order[displayed]
	ref := f.ref(row, displayed)
	box.flags |= flagClickable | flagChoosable
	f.boxes = append(f.boxes, tableCellBox{box, row, displayed})
	if s.edit == nil && f.s.Reorder == nil {
		// A row drag keeps the existing reorder gesture. Otherwise a
		// press moving past the click threshold extends a cell rectangle.
		box.flags |= flagDraggable
		box.HandleInput(func(ev InputEvent) bool {
			switch ev.Kind {
			case InputPointerDown:
				if ev.Button == 0 {
					s.press = &tableCellPress{row, displayed, f.c.rt.pointerX, f.c.rt.pointerY, ev.Mods}
				}
			case InputPointerMove:
				if p := s.press; p != nil && ev.Button == 0 {
					if !s.dragging && (abs(f.c.rt.pointerX-p.x) > 4 || abs(f.c.rt.pointerY-p.y) > 4) {
						f.selectCell(p.row, p.col, p.mods&Shift != 0)
						s.dragging = true
						f.owner.Focus()
					}
					if s.dragging {
						f.dragPointer()
						return true
					}
				}
			case InputPointerUp:
				s.press = nil
			}
			return false
		})
	}
	if s.Value != nil {
		box.Label(f.columns[col].Title + ": " + s.Value(row, col))
	}
	if box.Clicked() {
		// Process clicks after all rows consumed queued editor input,
		// including text arriving immediately before focus moved.
		if !s.dragging {
			s.clicked = &tableCellClick{row: row, cell: ref, mods: box.ClickModifiers()}
		}
	}
	if box.DoubleClicked() {
		if s.clicked != nil {
			s.clicked.double = true
		}
	}
	on := f.selected(row, displayed)
	box.checked = 1
	if on {
		box.checked = 2
		box.Background(f.c.theme.Selection).TextColor(f.c.theme.Text)
	}
	if s.Selection != nil && s.Selection.Cursor == ref {
		f.owner.ActiveDescendant(box)
		if s.reveal {
			box.ScrollIntoView()
			s.reveal = false
		}
		box.DrawOver(func(p *Painter, r Rect) {
			if f.owner.FocusVisible() {
				p.FocusRing(Rect{r.X + 2, r.Y + 2, max(0, r.W-4), max(0, r.H-4)}, [4]float32{})
			}
		})
	}
	if s.edit == nil || s.edit.Cell != ref {
		box.Children(fill)
		return
	}
	var in *Element
	wasComposing := false
	if st := f.c.rt.states[s.editID]; st != nil && st.editor != nil {
		wasComposing = st.editor.compose != ""
	}
	box.Children(func() {
		if s.Editor != nil {
			in = s.Editor(f.c, s.edit)
		} else {
			in = TextInputBase(f.c, &s.edit.Value).Grow(1).MinWidth(0).
				Background(f.c.theme.Background).Border(1, f.c.theme.Accent).TextColor(f.c.theme.Text)
		}
	})
	if in == nil {
		panic("ui: TableCellState.Editor must return a focus target")
	}
	if s.Error != nil {
		in.Error(s.Error.Error())
	}
	// KeepFocus prevents pressing the cell around an editor from moving
	// focus out before its controls see the press.
	box.flags |= flagKeepFocus
	s.editID, s.editBoxID = in.id, box.id
	composing := in.Composing()
	// Backends normally filter composition keys. Guard direct delivery
	// too, so Return cannot turn preedit into a table commit.
	previousInput := in.inputFn
	in.HandleInput(func(ev InputEvent) bool {
		if ev.Kind == InputKeyDown && in.Composing() {
			return true
		}
		return previousInput != nil && previousInput(ev)
	})
	cancel, forward, back, enter := false, false, false, false
	if !composing {
		if !f.editorPopup(box) {
			cancel = box.Shortcut(0, KeyEscape)
		}
		forward, back = box.Shortcut(0, KeyTab), box.Shortcut(Shift, KeyTab)
		enter = box.Shortcut(0, KeyEnter) || in.Submitted()
	}
	if s.fresh {
		s.fresh = false
		in.Focus()
		if ed := in.st.editor; ed != nil {
			ed.selectAll()
		}
		return
	}
	switch {
	case s.edit.cancel || cancel && !composing:
		s.action = &tableEditAction{focus: true}
	case (s.edit.commit || enter || forward || back) && !composing:
		s.edit.commit = false
		s.action = &tableEditAction{commit: true, focus: true}
		if forward {
			s.action.tab = 1
		} else if back {
			s.action.tab = -1
		}
	case !f.editorFocused():
		// Moving focus away commits without stealing that focus. An
		// unfinished composition cancels; its preedit is never a value.
		if composing || wasComposing {
			s.action = &tableEditAction{}
		} else if s.clicked == nil {
			s.action = &tableEditAction{commit: true}
		}
	}
	if s.action != nil {
		// Apply may reorder the model: defer it until all rows have been
		// built. A row built during layout completes it in the next frame.
		f.c.rt.requestFrame()
	}
}

// A dropdown's Escape closes its popup before cancelling the draft.
func (f *tableCellFrame) editorPopup(box *Element) bool {
	var walk func(e *Element) bool
	walk = func(e *Element) bool {
		if e == nil {
			return false
		}
		if e.popover != nil && f.c.rt.pressedWithin(e.popover.id, box) {
			return true
		}
		for ch := e.first; ch != nil; ch = ch.next {
			if walk(ch) {
				return true
			}
		}
		return false
	}
	return walk(f.c.overlay)
}

func (f *tableCellFrame) step(row, delta int) int {
	for row += delta; row >= 0 && row < f.n; row += delta {
		if !f.header(row) {
			return row
		}
	}
	return -1
}

func (f *tableCellFrame) move(row, col int, extend bool) {
	if row < 0 || row >= f.n || col < 0 || col >= len(f.order) {
		return
	}
	f.selectCell(row, col, extend)
	if f.s.cursor() != nil {
		if extend && f.s.Selection != nil {
			f.s.frame.extend(row, false)
		} else {
			f.s.frame.choose(row)
		}
	}
}

func (f *tableCellFrame) tab(back bool) {
	s := f.state
	if len(f.order) == 0 || f.step(-1, 1) < 0 {
		f.leave(back)
		return
	}
	if s.Selection == nil {
		f.move(f.step(-1, 1), 0, false)
		return
	}
	row, col, delta := s.cursorRow, f.z, 1
	if back {
		delta = -1
	}
	col += delta
	if col < 0 || col >= len(f.order) {
		row, col = f.step(row, delta), 0
		if back {
			col = len(f.order) - 1
		}
	}
	if row < 0 {
		f.leave(back)
	} else {
		f.move(row, col, false)
	}
}

// leave skips focus targets inside the table, including an editor that
// just committed but is still in the previous frame's focus order.
func (f *tableCellFrame) leave(back bool) {
	rt := f.c.rt
	for range len(rt.focusOrder) {
		rt.moveFocus(back)
		inside := false
		for st := rt.states[rt.focused]; st != nil; st = rt.states[st.parent] {
			if st.id == f.owner.id {
				inside = true
				break
			}
			if st.parent == 0 {
				break
			}
		}
		if !inside {
			return
		}
	}
	f.owner.Focus()
}

func (f *tableCellFrame) navigate() {
	s, owner := f.state, f.owner
	if action := s.action; action != nil {
		s.action = nil
		if f.finish(action.commit, action.focus) {
			if action.tab != 0 {
				f.tab(action.tab < 0)
			}
		} else {
			f.c.rt.focused = s.editID
			s.clicked = nil
		}
	}
	if s.dragging {
		f.dragPointer()
		if f.c.rt.pressed == nil {
			s.dragging = false
		}
	}
	if click := s.clicked; click != nil {
		s.clicked = nil
		if s.edit == nil || f.finish(true, false) {
			row, col, ok := f.resolve(click.cell, click.row)
			if !ok {
				return
			}
			f.selectCell(row, col, click.mods&Shift != 0)
			if f.s.cursor() != nil {
				f.s.frame.click(row, click.mods)
			}
			owner.Focus()
			if click.double {
				f.begin(row, col)
			}
		} else {
			f.c.rt.focused = s.editID
		}
	}
	owner.HandleInput(func(ev InputEvent) bool {
		return ev.Kind == InputCommand && s.edit == nil && f.command(ev.Text)
	})
	if s.edit != nil {
		return // editors own their keys, clipboard and input methods
	}
	for _, key := range []Key{KeyLeft, KeyRight, KeyUp, KeyDown, KeyHome, KeyEnd, KeyPageUp, KeyPageDown} {
		for _, mods := range []Modifiers{0, Shift, Cmd, Cmd | Shift} {
			if !owner.Shortcut(mods, key) || f.n == 0 || len(f.order) == 0 {
				continue
			}
			row, col := s.cursorRow, f.z
			if s.Selection == nil {
				f.move(f.step(-1, 1), 0, false)
				continue
			}
			switch key {
			case KeyLeft:
				col = max(0, col-1)
			case KeyRight:
				col = min(len(f.order)-1, col+1)
			case KeyUp:
				row = f.step(row, -1)
			case KeyDown:
				row = f.step(row, 1)
			case KeyHome:
				col = 0
				if mods&Cmd != 0 {
					row = f.step(-1, 1)
				}
			case KeyEnd:
				col = len(f.order) - 1
				if mods&Cmd != 0 {
					row = f.step(f.n, -1)
				}
			case KeyPageUp, KeyPageDown:
				d := 1
				if key == KeyPageUp {
					d = -1
				}
				to := f.s.heights.rowAt(max(0, f.s.heights.top(row, f.s.gap)+float64(d)*float64(f.s.frame.e.st.h)), f.s.gap)
				row = max(0, min(to, f.n-1))
				if f.header(row) {
					row = f.step(row, d)
				}
			}
			f.move(row, col, mods&Shift != 0)
		}
	}
	if owner.Shortcut(0, KeyTab) {
		f.tab(false)
	}
	if owner.Shortcut(Shift, KeyTab) {
		f.tab(true)
	}
	if edit, enter := owner.Shortcut(0, KeyF2), owner.Shortcut(0, KeyEnter); (edit || enter) && s.Selection != nil {
		f.begin(s.cursorRow, f.z)
	}
	for _, k := range []struct {
		key Key
		cmd string
	}{{KeyC, "copy"}, {KeyV, "paste"}, {KeyA, "selectAll"}} {
		if owner.Shortcut(Cmd, k.key) {
			f.command(k.cmd)
		}
	}
}

func (f *tableCellFrame) dragPointer() {
	for _, b := range f.boxes {
		st := b.e.st
		if (Rect{st.vx, st.vy, st.vw, st.vh}).Contains(f.c.rt.pointerX, f.c.rt.pointerY) && f.state.Selection != nil && f.state.Selection.Cursor != f.ref(b.row, b.col) {
			f.move(b.row, b.col, true)
			return
		}
	}
}

// tableTransferCells bounds allocations for clipboard data.
const tableTransferCells = 1 << 20

func (f *tableCellFrame) command(command string) bool {
	s := f.state
	if command == "selectAll" {
		first, last := f.step(-1, 1), f.step(f.n, -1)
		if first >= 0 && len(f.order) > 0 {
			f.selectCell(first, 0, false)
			f.selectCell(last, len(f.order)-1, true)
		}
		return true
	}
	if command != "copy" && command != "paste" {
		return false
	}
	if s.Selection == nil || s.Value == nil {
		return true
	}
	lo, hi := min(s.anchorRow, s.cursorRow), max(s.anchorRow, s.cursorRow)
	a, z := min(f.a, f.z), max(f.a, f.z)
	if command == "copy" {
		if hi-lo+1 > tableTransferCells/(z-a+1) {
			s.Error = fmt.Errorf("table selection exceeds %d cells", tableTransferCells)
			return true
		}
		var rows [][]string
		for row := lo; row <= hi; row++ {
			if f.header(row) {
				continue
			}
			values := make([]string, 0, z-a+1)
			for _, col := range f.order[a : z+1] {
				values = append(values, s.Value(row, col))
			}
			rows = append(rows, values)
		}
		f.c.WriteClipboard(FormatTSV(rows))
		return true
	}
	values, err := ParseTSV(f.c.ReadClipboard())
	if err == nil {
		// A multi-cell range must match the paste's shape; a single cell
		// is an insertion origin for a larger rectangle. Headers are skipped.
		rows := make([]int, 0, len(values))
		for row := lo; row < f.n && len(rows) < len(values); row++ {
			if !f.header(row) {
				rows = append(rows, row)
			}
		}
		width := len(values[0])
		selectedRows := 0
		for row := lo; row <= hi; row++ {
			if !f.header(row) {
				selectedRows++
			}
		}
		if len(rows) != len(values) || a+width > len(f.order) {
			err = fmt.Errorf("pasted rectangle exceeds table bounds")
		} else if (selectedRows > 1 || z > a) && (selectedRows != len(values) || z-a+1 != width) {
			err = fmt.Errorf("pasted rectangle must match the selected range")
		} else {
			changes := make([]TableCellChange, 0, len(values)*width)
			for i, row := range rows {
				for j, value := range values[i] {
					col := f.order[a+j]
					changes = append(changes, TableCellChange{f.ref(row, a+j), row, col, s.Value(row, col), value})
				}
			}
			err = f.apply(changes)
			if err == nil {
				// Apply may sort the data. The pasted range stays with the
				// cells that received it, rather than their former indexes.
				s.Selection = &TableCellRange{changes[0].Cell, changes[len(changes)-1].Cell}
				s.anchorRow, s.cursorRow = rows[0], rows[len(rows)-1]
				f.a, f.z, s.reveal = a, a+width-1, true
				f.sync()
				if s.Selection != nil {
					f.s.ScrollIntoView(s.cursorRow)
				}
			}
		}
	}
	s.Error = err
	return true
}

// FormatTSV formats rows for spreadsheet interchange. Tabs, line breaks
// and quotes are quoted; a quote inside a quoted value is doubled. Pass a
// rectangle for a result ParseTSV accepts. No final newline is added.
func FormatTSV(rows [][]string) string {
	var out strings.Builder
	for i, row := range rows {
		if i > 0 {
			out.WriteByte('\n')
		}
		for j, value := range row {
			if j > 0 {
				out.WriteByte('\t')
			}
			if value == "" || strings.ContainsAny(value, "\t\r\n\"") {
				out.WriteByte('"')
				out.WriteString(strings.ReplaceAll(value, "\"", "\"\""))
				out.WriteByte('"')
			} else {
				out.WriteString(value)
			}
		}
	}
	return out.String()
}

// ParseTSV reads a rectangular TSV value, including quoted multiline
// fields, empty cells/rows and LF, CRLF or CR record separators. One final
// record separator is ignored. Ragged records, malformed quotes and more
// than 1,048,576 cells are errors. Empty input is one empty cell.
func ParseTSV(value string) ([][]string, error) {
	var rows [][]string
	var row []string
	i, count, width := 0, 0, -1
	for {
		var field string
		if i < len(value) && value[i] == '"' {
			i++
			var b strings.Builder
			closed := false
			for i < len(value) {
				if value[i] != '"' {
					b.WriteByte(value[i])
					i++
				} else if i+1 < len(value) && value[i+1] == '"' {
					b.WriteByte('"')
					i += 2
				} else {
					i++
					closed = true
					break
				}
			}
			if !closed || i < len(value) && !strings.ContainsRune("\t\r\n", rune(value[i])) {
				return nil, fmt.Errorf("invalid quoted TSV field")
			}
			field = b.String()
		} else {
			start := i
			for i < len(value) && !strings.ContainsRune("\t\r\n", rune(value[i])) {
				if value[i] == '"' {
					return nil, fmt.Errorf("quote in an unquoted TSV field")
				}
				i++
			}
			field = value[start:i]
		}
		count++
		if count > tableTransferCells {
			return nil, fmt.Errorf("TSV exceeds %d cells", tableTransferCells)
		}
		row = append(row, field)
		if i < len(value) && value[i] == '\t' {
			i++
			continue
		}
		if width < 0 {
			width = len(row)
		} else if len(row) != width {
			return nil, fmt.Errorf("TSV rows have different widths")
		}
		rows = append(rows, row)
		row = nil
		if i == len(value) {
			break
		}
		if value[i] == '\r' && i+1 < len(value) && value[i+1] == '\n' {
			i++
		}
		i++
		if i == len(value) {
			break
		}
	}
	return rows, nil
}
