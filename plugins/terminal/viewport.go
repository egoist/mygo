package terminal

// Viewport is what a terminal shows of its rows, which count from the top
// of its scrollback, for an app drawing over them or moving through them.
type Viewport struct {
	// Top is the row showing first, Rows and Cols the size shown, and Total
	// the rows there are, the scrollback's with the screen's.
	Top, Rows, Cols, Total int
	// AtBottom tells that the screen shows, not the scrollback.
	AtBottom bool
	// Alt tells a full-screen program's screen, which has only its own rows
	// and doesn't scroll.
	Alt bool
}

// Viewport returns what the terminal shows; all zero once it closed.
func (t *Terminal) Viewport() Viewport {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.term == nil {
		return Viewport{}
	}
	sb := t.term.Scrollbar()
	cols, rows := t.term.Size()
	return Viewport{
		Top:      int(sb.Offset),
		Rows:     rows,
		Cols:     cols,
		Total:    int(sb.Total),
		AtBottom: t.term.AtBottom(),
		Alt:      t.term.AltScreen(),
	}
}

// ScrollToRow shows row first, or as near as the rows below it allow. On a
// full-screen program's screen it does nothing.
func (t *Terminal) ScrollToRow(row int) {
	t.mu.Lock()
	if t.term == nil || t.term.AltScreen() {
		t.mu.Unlock()
		return
	}
	t.term.ScrollToRow(max(row, 0))
	t.mu.Unlock()
	t.redraw()
}

// ScrollToBottom shows the screen, as the terminal does until the user
// scrolls back.
func (t *Terminal) ScrollToBottom() {
	t.mu.Lock()
	if t.term == nil {
		t.mu.Unlock()
		return
	}
	t.term.ScrollToBottom()
	t.mu.Unlock()
	t.redraw()
}

// RowText returns the text of a row the terminal shows, 0 the first shown,
// with the blanks at its end.
func (t *Terminal) RowText(y int) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.term == nil {
		return ""
	}
	text, _ := t.term.RowText(y)
	return string(text)
}

// CursorRow returns the cursor's row, as Viewport counts them: on a
// full-screen program's screen, alt, a row of that screen.
func (t *Terminal) CursorRow() (row int, alt bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.term == nil {
		return 0, false
	}
	_, y := t.term.Cursor()
	alt = t.term.AltScreen()
	if alt {
		return y, true
	}
	return t.term.ScrollbackRows() + y, false
}

// RowMetrics returns the height of a row where a view shows the terminal
// and the first row's distance from the view's top, in the view's units;
// ok is false until a view drew it.
func (t *Terminal) RowMetrics() (height, top float32, ok bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	v := t.v
	if v == nil || v.scale == 0 || v.cellH == 0 {
		return 0, 0, false
	}
	return float32(v.cellH) / v.scale, float32(v.oy) / v.scale, true
}
