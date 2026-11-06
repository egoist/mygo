//go:build windows && (amd64 || arm64)

package windows

import (
	"syscall"

	"github.com/egoist/mygo/internal/platform"
)

// Tables, grids and tree grids have UI Automation's Grid and Table
// patterns, and their cells and headers GridItem and TableItem,
// as Chromium gives them: Narrator reads a cell's place, "row 4,121 of
// 10,000", from them. Places are counted from 0 among all the rows and
// columns, built or not: as the app gave them (aria-rowindex and the
// like, from 1), else as a list's rows say which of all they are
// (PositionInSet) after the header rows above them, else by the rows and
// cells built. They are found as a client asks, so frames pay nothing.

// cellOf reports whether nodes of a role are cells of a table, its
// column and row headers included.
func cellOf(r platform.AccessRole) bool {
	return r == platform.RoleCell || r == platform.RoleColumnHeader || r == platform.RoleRowHeader
}

// grid returns the table, grid or tree grid around the element.
func (e *uiaElement) grid() *uiaElement {
	for p := e.parent; p != nil && !p.root; p = p.parent {
		if p.n.Role.Tabular() {
			return p
		}
	}
	return nil
}

// row returns the row around a cell, if it is in one of its grid.
func (e *uiaElement) row() *uiaElement {
	for p := e.parent; p != nil && !p.root && !p.n.Role.Tabular(); p = p.parent {
		if p.n.Role == platform.RoleRow {
			return p
		}
	}
	return nil
}

// gridPlace is a row of a grid or a cell of a row, with its place: its
// row or its column, from 0.
type gridPlace struct {
	e  *uiaElement
	at int
}

// rows returns the rows of a grid in order with their places, leaving out
// those of grids inside it, and heads, the header rows above the first
// that says which of all it is.
func (e *uiaElement) rows() (rows []gridPlace, heads int) {
	listed := false
	stack := []*uiaElement{e}
	for len(stack) > 0 {
		p := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if p != e && p.n.Role.Tabular() {
			continue
		}
		if p == e || p.n.Role != platform.RoleRow {
			for i := len(p.children) - 1; i >= 0; i-- {
				stack = append(stack, p.children[i]) // in order, depth first
			}
			continue
		}
		at := len(rows)
		switch {
		case p.n.RowIndex > 0:
			at = p.n.RowIndex - 1
		case p.n.PosInSet > 0:
			at, listed = heads+p.n.PosInSet-1, true
		case !listed:
			heads++
		}
		rows = append(rows, gridPlace{p, at})
	}
	return rows, heads
}

// cells returns the cells of a row in order with their columns: a cell
// without one of its own is in the one after the cell before it.
func (e *uiaElement) cells() []gridPlace {
	var cells []gridPlace
	col := 0
	stack := []*uiaElement{e}
	for len(stack) > 0 {
		p := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if cellOf(p.n.Role) {
			if p.n.ColIndex > 0 {
				col = p.n.ColIndex - 1
			}
			cells = append(cells, gridPlace{p, col})
			col++
			continue
		}
		if p != e && (p.n.Role == platform.RoleRow || p.n.Role.Tabular()) {
			continue
		}
		for i := len(p.children) - 1; i >= 0; i-- {
			stack = append(stack, p.children[i])
		}
	}
	return cells
}

// rowCount returns how many rows a grid has: as the app gave it, else as
// its list says (SizeOfSet) with the header rows, else those built.
func (e *uiaElement) rowCount() int {
	if e.n.RowCount > 0 {
		return e.n.RowCount
	}
	rows, heads := e.rows()
	n := 0
	if e.n.SetSize > 0 {
		n = heads + e.n.SetSize
	}
	for _, r := range rows {
		n = max(n, r.at+1)
	}
	return n
}

// colCount returns how many columns a grid has: as the app gave it, else
// those of its rows' cells.
func (e *uiaElement) colCount() int {
	if e.n.ColCount > 0 {
		return e.n.ColCount
	}
	n := 0
	rows, _ := e.rows()
	for _, r := range rows {
		for _, c := range r.e.cells() {
			n = max(n, c.at+1)
		}
	}
	return n
}

// place returns the row and the column of a cell in its grid.
func (e *uiaElement) place() (row, col int) {
	r, g := e.row(), e.grid()
	if r != nil && g != nil {
		rows, _ := g.rows()
		for _, p := range rows {
			if p.e == r {
				row = p.at
			}
		}
		for _, p := range r.cells() {
			if p.e == e {
				col = p.at
			}
		}
	} else if e.n.ColIndex > 0 {
		col = e.n.ColIndex - 1
	}
	if e.n.RowIndex > 0 {
		row = e.n.RowIndex - 1
	}
	return row, col
}

// cellAt returns the cell of a grid at a row and a column, if built.
func (e *uiaElement) cellAt(row, col int) *uiaElement {
	rows, _ := e.rows()
	for _, r := range rows {
		for _, c := range r.e.cells() {
			if r.at == row && c.at == col {
				return c.e
			}
		}
	}
	return nil
}

// columnHeaders returns the column headers of a grid, those of column col
// alone when it is not negative.
func (e *uiaElement) columnHeaders(col int) []*uiaElement {
	var heads []*uiaElement
	rows, _ := e.rows()
	for _, r := range rows {
		for _, c := range r.e.cells() {
			if c.e.n.Role == platform.RoleColumnHeader && (col < 0 || c.at == col) {
				heads = append(heads, c.e)
			}
		}
	}
	return heads
}

// rowHeaders returns the row headers of a grid, those of row row alone
// when it is not negative.
func (e *uiaElement) rowHeaders(row int) []*uiaElement {
	var heads []*uiaElement
	rows, _ := e.rows()
	for _, r := range rows {
		if row >= 0 && r.at != row {
			continue
		}
		for _, c := range r.e.cells() {
			if c.e.n.Role == platform.RoleRowHeader {
				heads = append(heads, c.e)
			}
		}
	}
	return heads
}

// gridVtbls makes the vtables of the Grid, GridItem, Table and TableItem
// patterns, with vtbl adding IUnknown's methods and live finding the
// element of an interface still in the tree.
func gridVtbls(vtbl func(...uintptr) []uintptr, live func(this uintptr) (*uiaElement, uintptr)) {
	cb := syscall.NewCallback
	count := func(get func(e *uiaElement) int) uintptr {
		return cb(func(this, p uintptr) uintptr {
			e, hr := live(this)
			*(*int32)(native(p)) = 0
			if e == nil {
				return hr
			}
			*(*int32)(native(p)) = int32(get(e))
			return sOK
		})
	}
	uiaVtbls[ifaceGrid] = vtbl(
		cb(func(this, row, col, p uintptr) uintptr { // GetItem
			out(p, nil, 0)
			e, hr := live(this)
			if e == nil {
				return hr
			}
			r, c := int(int32(row)), int(int32(col))
			if r < 0 || c < 0 || r >= e.rowCount() || c >= e.colCount() {
				return eInvalidArg
			}
			// A cell not built, as in a row out of view, is none.
			return out(p, e.cellAt(r, c), ifaceSimple)
		}),
		count((*uiaElement).rowCount),
		count((*uiaElement).colCount),
	)
	uiaVtbls[ifaceGridItem] = vtbl(
		count(func(e *uiaElement) int { row, _ := e.place(); return row }),
		count(func(e *uiaElement) int { _, col := e.place(); return col }),
		count(func(*uiaElement) int { return 1 }), // RowSpan
		count(func(*uiaElement) int { return 1 }), // ColumnSpan
		cb(func(this, p uintptr) uintptr { // get_ContainingGrid
			out(p, nil, 0)
			e, hr := live(this)
			if e == nil {
				return hr
			}
			return out(p, e.grid(), ifaceSimple)
		}),
	)
	headers := func(get func(e *uiaElement) []*uiaElement) uintptr {
		return cb(func(this, p uintptr) uintptr {
			e, hr := live(this)
			*(*uintptr)(native(p)) = 0
			if e == nil {
				return hr
			}
			*(*uintptr)(native(p)) = elementArray(get(e))
			return sOK
		})
	}
	uiaVtbls[ifaceTable] = vtbl(
		headers(func(e *uiaElement) []*uiaElement { return e.rowHeaders(-1) }),
		headers(func(e *uiaElement) []*uiaElement { return e.columnHeaders(-1) }),
		count(func(*uiaElement) int { return 0 }), // RowOrColumnMajor_RowMajor
	)
	uiaVtbls[ifaceTableItem] = vtbl(
		headers(func(e *uiaElement) []*uiaElement { // GetRowHeaderItems
			g := e.grid()
			if g == nil || e.n.Role == platform.RoleRowHeader {
				return nil
			}
			row, _ := e.place()
			return g.rowHeaders(row)
		}),
		headers(func(e *uiaElement) []*uiaElement { // GetColumnHeaderItems
			g := e.grid()
			if g == nil || e.n.Role == platform.RoleColumnHeader {
				return nil
			}
			_, col := e.place()
			return g.columnHeaders(col)
		}),
	)
}
