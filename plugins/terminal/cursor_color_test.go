package terminal

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// A theme's see-through cursor stays see-through, as Ghostty's
// cursor-opacity: the terminal keeps colors opaque, and the cursor drew
// the theme's color at full alpha. A program's color (OSC 12) is its own.
func TestSeeThroughCursor(t *testing.T) {
	loadLib(t)
	th := &Theme{Foreground: ui.Hex("#c0c0c0"), Background: ui.Hex("#000000"), Cursor: ui.Hex("#ff000080")}
	term, err := New(Options{Conn: newPipe(), Theme: th, NoBlink: true})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	tt := ui.NewTester(func(c *ui.Context) { View(c, term).Fill().AutoFocus() }, 400, 200)
	cursor := func(name string, want ui.Color) {
		t.Helper()
		tt.Frame()
		x, y := cellCenter(term, 0, 0)
		s := term.v.scale
		c := tt.Image().RGBAAt(int(x*s), int(y*s))
		d := func(a, b uint8) int { return max(int(a)-int(b), int(b)-int(a)) }
		if d(c.R, want.R) > 2 || d(c.G, want.G) > 2 || d(c.B, want.B) > 2 {
			t.Errorf("%s: the cursor is %v, want %v", name, ui.RGB(c.R, c.G, c.B), want)
		}
	}
	cursor("the theme's", ui.RGB(128, 0, 0))
	term.Feed([]byte("\x1b]12;#00ff00\x1b\\"))
	cursor("a program's", ui.RGB(0, 255, 0))
	term.Feed([]byte("\x1b]112\x1b\\"))
	cursor("the theme's again", ui.RGB(128, 0, 0))
}
