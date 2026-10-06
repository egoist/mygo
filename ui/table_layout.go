package ui

// Table cells remain in flow for measurement and collection coordinates.
// Placement freezes their x coordinates; one shared clip limits painting,
// hits and accessibility to the column's part of the viewport.
type tableGeometry struct {
	head, list *Element
	columns    []TableColumn
	order      []int
	pinned     bool
}

type tableRowLayout struct {
	geometry *tableGeometry
	cells    []*Element
}

type tableCellLayout struct {
	row    *tableRowLayout
	column int // index in the original column slice, also for accessibility
	pin    TablePin
	clip   Rect
}

func (r *tableRowLayout) insets() (left, right float32) {
	for _, cell := range r.cells {
		switch cell.tableCell.pin {
		case PinLeft:
			left += cell.w
		case PinRight:
			right += cell.w
		}
	}
	return left, right
}

// place adjusts cells relative to the origin that place will give them.
// Pin widths may fill the viewport: left pins then take precedence, and
// the scrolling region becomes empty, without overlapping hits or paint.
func (r *tableRowLayout) place(cx float32) {
	if !r.geometry.pinned {
		return
	}
	head := r.geometry.head
	left, right := r.insets()
	lo, hi := head.x, head.x+head.w
	leftEdge, rightEdge := min(hi, lo+left), max(min(hi, lo+left), hi-right)
	lx, rx := lo, hi-right
	for _, cell := range r.cells {
		tc := cell.tableCell
		start, end := leftEdge, rightEdge
		switch tc.pin {
		case PinLeft:
			cell.x = lx - cx
			lx += cell.w
			start, end = lo, leftEdge
		case PinRight:
			cell.x = rx - cx
			rx += cell.w
			start, end = rightEdge, hi
		}
		tc.clip = Rect{start, -1e6, max(0, end-start), 2e6}
	}
}

// pinnedOrigin returns the x coordinate that a pinned cell will have,
// before place runs, for popovers attached to editor controls in it.
func (tc *tableCellLayout) pinnedOrigin() float32 {
	r := tc.row
	x, _ := laidOutOrigin(r.geometry.head)
	if tc.pin == PinRight {
		_, right := r.insets()
		x += r.geometry.head.w - right
	}
	for _, cell := range r.cells {
		if cell.tableCell == tc {
			break
		}
		if cell.tableCell.pin == tc.pin {
			x += cell.w
		}
	}
	return x
}
