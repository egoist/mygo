package terminal

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// scrolled returns a terminal with 100 lines of history in a Tester, at
// the bottom, and how to read where its viewport is: its top row, and how
// far the content is pulled down past it, in DIPs.
func scrolled(t *testing.T) (*Terminal, *ui.Tester, func() (uint64, float32), float32) {
	t.Helper()
	loadLib(t)
	term, err := New(Options{Conn: newPipe()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { term.Close() })
	tt := ui.NewTester(func(c *ui.Context) { View(c, term).Fill().AutoFocus() }, 400, 200)
	var b strings.Builder
	for i := range 100 {
		b.WriteString("line " + itoa(i) + "\r\n")
	}
	term.Feed([]byte(b.String()))
	tt.Frame()
	at := func() (uint64, float32) {
		term.mu.Lock()
		defer term.mu.Unlock()
		return term.term.Scrollbar().Offset, term.v.pull
	}
	return term, tt, at, float32(term.v.cellH) / term.v.scale
}

// TestTouchpadScrollsByPixels: a touchpad's moves scroll the scrollback
// by pixels: half a row pulls the content down half a row, showing the
// row above, and the viewport moves by whole rows only.
func TestTouchpadScrollsByPixels(t *testing.T) {
	_, tt, at, ch := scrolled(t)
	bottom, _ := at()
	tt.Scroll(100, 100, 0, -ch/2)
	tt.Advance(100 * time.Millisecond)
	if off, pull := at(); off != bottom || pull != ch/2 {
		t.Errorf("half a row up: offset %d, pull %v; want %d, %v", off, pull, bottom, ch/2)
	}
	// A paint for something else, as the cursor's blink, leaves the
	// content where it is, at the bottom too.
	tt.Advance(2 * blinkPeriod)
	if off, pull := at(); off != bottom || pull != ch/2 {
		t.Errorf("half a row up, past a blink: offset %d, pull %v; want %d, %v", off, pull, bottom, ch/2)
	}
	tt.Scroll(100, 100, 0, -ch)
	tt.Advance(100 * time.Millisecond)
	if off, pull := at(); off != bottom-1 || pull != ch/2 {
		t.Errorf("a row and a half up: offset %d, pull %v; want %d, %v", off, pull, bottom-1, ch/2)
	}
	tt.Scroll(100, 100, 0, 3*ch)
	tt.Advance(100 * time.Millisecond)
	if off, pull := at(); off != bottom || pull != 0 {
		t.Errorf("back down past the bottom: offset %d, pull %v; want %d, 0", off, pull, bottom)
	}
}

// TestTouchpadMovesEveryFrame: moves that come once a frame scroll every
// frame by about as much, though each lands just before or after a frame.
func TestTouchpadMovesEveryFrame(t *testing.T) {
	_, tt, at, ch := scrolled(t)
	pos := func() float32 {
		off, pull := at()
		return float32(off)*ch - pull
	}
	last := pos()
	var moves []float32
	for i := range 30 {
		// A move every 8 ms, 2 DIPs each; frames come at 60 Hz.
		tt.Scroll(100, 100, 0, -2)
		tt.Advance(8 * time.Millisecond)
		if i%2 == 1 {
			p := pos()
			moves = append(moves, last-p)
			last = p
		}
	}
	for i, m := range moves[2:] {
		if m < 2 || m > 6 {
			t.Errorf("frame %d moved %v DIPs, want about 4: %v", i+2, m, moves)
			break
		}
	}
}

// TestNotchGlides: a wheel's notch glides its three rows, rather than
// jumping them, and lands on a row.
func TestNotchGlides(t *testing.T) {
	_, tt, at, _ := scrolled(t)
	bottom, _ := at()
	tt.Wheel(100, 100, 0, -hostNotch())
	tt.Advance(notchGlide / 4)
	if off, pull := at(); off == bottom-3 && pull == 0 || off == bottom && pull == 0 {
		t.Errorf("a quarter of the way through the glide: offset %d, pull %v, not on the way", off, pull)
	}
	tt.Advance(notchGlide)
	if off, pull := at(); off != bottom-3 || pull != 0 {
		t.Errorf("after the glide: offset %d, pull %v; want %d, 0", off, pull, bottom-3)
	}
}

// TestTypingStopsTheScroll: a key typed while the scrollback moves goes
// back to the bottom and stays there.
func TestTypingStopsTheScroll(t *testing.T) {
	_, tt, at, _ := scrolled(t)
	bottom, _ := at()
	for range 10 {
		tt.Scroll(100, 100, 0, -16)
		tt.Advance(8 * time.Millisecond)
	}
	tt.Wheel(100, 100, 0, -hostNotch()) // and a notch, still gliding
	tt.Advance(notchGlide / 4)
	tt.Type("x")
	tt.Advance(time.Second)
	if off, pull := at(); off != bottom || pull != 0 {
		t.Errorf("after typing: offset %d, pull %v; want %d, 0", off, pull, bottom)
	}
}

// TestPressWhilePulled: a press lands on the row drawn under it while the
// content is pulled down part of a row, also in the lower part of a row,
// which the pull moves over the top of the next row's cell.
func TestPressWhilePulled(t *testing.T) {
	term, tt, at, ch := scrolled(t)
	bottom, _ := at()
	tt.Scroll(100, 100, 0, -ch/2)
	tt.Advance(100 * time.Millisecond)
	if off, pull := at(); off != bottom || pull != ch/2 {
		t.Fatalf("half a row up: offset %d, pull %v; want %d, %v", off, pull, bottom, ch/2)
	}
	// Row 2 is drawn half a row lower: the lower part of it is where row
	// 3's cell is without the pull.
	x0, y := cellCenter(term, 0, 2)
	y += ch * 0.8 // 0.8 of the way down row 2 as drawn
	x1, _ := cellCenter(term, 7, 2)
	tt.Press(x0-2, y)
	// The press settles the content on its rows, half a row up: the drag
	// follows row 2 there.
	if off, pull := at(); off != bottom || pull != 0 {
		t.Errorf("after the press: offset %d, pull %v; want %d, 0", off, pull, bottom)
	}
	y -= ch / 2
	tt.Move(x1+2, y)
	tt.Release(x1+2, y)
	tt.Command("copy")
	// The viewport's top row is line bottom (its offset), row 2 two past.
	wantLine := "line " + itoa(int(bottom)+2)
	if got := strings.TrimSpace(tt.Clipboard()); got != wantLine {
		t.Errorf("pressed in the lower part of row 2 while pulled half a row: copied %q, want %q", got, wantLine)
	}
}

// TestPulledPaintKeepsRows: a paint while the content is pulled and at
// rest rebuilds no row, though each paint copies the row above the
// viewport by moving the viewport a row up and back: above the bottom,
// and at it, where the cursor shows.
func TestPulledPaintKeepsRows(t *testing.T) {
	for _, rows := range []float32{0.5, 2.5} {
		t.Run(fmt.Sprint(rows), func(t *testing.T) { pulledPaint(t, rows) })
	}
}

func pulledPaint(t *testing.T, rows float32) {
	term, tt, at, ch := scrolled(t)
	tt.Scroll(100, 100, 0, -ch*rows)
	tt.Advance(100 * time.Millisecond)
	if _, pull := at(); pull != ch/2 {
		t.Fatalf("pull %v, want %v", pull, ch/2)
	}
	built := func() (int, int) {
		term.mu.Lock()
		defer term.mu.Unlock()
		return term.v.rowsBuilt, term.v.paints
	}
	for i := range 3 {
		rows, paints := built()
		tt.Advance(blinkPeriod) // the cursor blinks: a paint, nothing else
		r, p := built()
		if p == paints {
			t.Fatalf("no paint in a blink period")
		}
		term.mu.Lock()
		pulled, above := term.v.pull, term.v.aboveOK
		term.mu.Unlock()
		if pulled != ch/2 || !above {
			t.Errorf("blink %d: pull %v, row above drawn %v; want %v, true", i, pulled, above, ch/2)
		}
		if n := r - rows; n != 0 {
			t.Errorf("blink %d at rest while pulled rebuilt %d rows in %d paints, want 0", i, n, p-paints)
		}
	}
}
