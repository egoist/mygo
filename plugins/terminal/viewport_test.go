package terminal

import (
	"fmt"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestViewport(t *testing.T) {
	loadLib(t)
	term, err := New(Options{Conn: newPipe()})
	if err != nil {
		t.Fatal(err)
	}
	term.Resize(40, 10)
	for i := range 50 {
		term.Feed(fmt.Appendf(nil, "line %d\r\n", i))
	}
	if got, want := term.Viewport(), (Viewport{Top: 41, Rows: 10, Cols: 40, Total: 51, AtBottom: true}); got != want {
		t.Errorf("Viewport = %+v, want %+v", got, want)
	}
	if row, alt := term.CursorRow(); row != 50 || alt {
		t.Errorf("CursorRow = %d, %v", row, alt)
	}

	term.ScrollToRow(5)
	if v := term.Viewport(); v.Top != 5 || v.AtBottom {
		t.Errorf("after ScrollToRow(5): %+v", v)
	}
	if s := term.RowText(0); !strings.HasPrefix(s, "line 5") {
		t.Errorf("RowText(0) = %q", s)
	}
	term.ScrollToRow(1000)
	if v := term.Viewport(); v.Top != 41 {
		t.Errorf("past the last row: %+v", v)
	}
	term.ScrollToRow(-3)
	if v := term.Viewport(); v.Top != 0 {
		t.Errorf("before the first row: %+v", v)
	}
	term.ScrollToBottom()
	if v := term.Viewport(); !v.AtBottom {
		t.Errorf("after ScrollToBottom: %+v", v)
	}

	// A full-screen program's screen has its own rows, and stays put.
	term.ScrollToRow(5)
	term.Feed([]byte("\x1b[?1049h\x1b[3;1H"))
	if v := term.Viewport(); !v.Alt {
		t.Errorf("on the alternate screen: %+v", v)
	}
	if row, alt := term.CursorRow(); row != 2 || !alt {
		t.Errorf("CursorRow on the alternate screen = %d, %v", row, alt)
	}
	before := term.Viewport()
	term.ScrollToRow(1)
	if v := term.Viewport(); v != before {
		t.Errorf("ScrollToRow moved the alternate screen: %+v, was %+v", v, before)
	}

	term.Close()
	if v := term.Viewport(); v != (Viewport{}) {
		t.Errorf("closed: %+v", v)
	}
	term.ScrollToRow(1)
	term.ScrollToBottom()
	if s := term.RowText(0); s != "" {
		t.Errorf("closed: RowText = %q", s)
	}
	if row, alt := term.CursorRow(); row != 0 || alt {
		t.Errorf("closed: CursorRow = %d, %v", row, alt)
	}
}

func TestRowMetrics(t *testing.T) {
	loadLib(t)
	term, err := New(Options{Conn: newPipe()})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	if _, _, ok := term.RowMetrics(); ok {
		t.Error("RowMetrics: ok before a view drew it")
	}
	tt := ui.NewTester(func(c *ui.Context) { View(c, term).Fill().AutoFocus() }, 400, 200)
	tt.Frame()
	h, top, ok := term.RowMetrics()
	if !ok || h <= 0 || top < 0 {
		t.Errorf("RowMetrics = %v, %v, %v", h, top, ok)
	}
	// What the view draws its rows at: the row under a point is the one the
	// metrics say.
	term.mu.Lock()
	v := term.v
	_, row := v.cellAt(0, top+h*2.5)
	term.mu.Unlock()
	if row != 2 {
		t.Errorf("a point in the third row is in row %d", row)
	}
}
