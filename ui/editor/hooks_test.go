package editor

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// OnInput sees keys and typed text before the editor: what it takes the
// editor leaves, the rest goes on as without it.
func TestOnInputTakesKeys(t *testing.T) {
	ct := newEditorTest(t, "", "ab|")
	var texts, keys int
	ct.st.OnInput = func(ev ui.InputEvent) bool {
		switch ev.Kind {
		case ui.InputText:
			texts++
			return ev.Text == "x"
		case ui.InputKeyDown:
			keys++
			return ev.Key == ui.KeyBackspace
		}
		return false
	}
	ct.tt.Frame()
	ct.typ("x")
	ct.key(0, ui.KeyBackspace)
	ct.want("ab|")
	ct.typ("y")
	ct.key(0, ui.KeyLeft)
	ct.want("ab|y")
	if texts != 2 || keys != 2 {
		t.Fatalf("OnInput saw %d texts and %d keys, want 2 and 2", texts, keys)
	}
}

// VisibleLines are the lines the last frame showed, scrolled or not.
func TestVisibleLines(t *testing.T) {
	ct := newEditorTest(t, "", strings.Repeat("line\n", 200))
	lh := float64(ct.st.ed.view.lineHeight)
	first, last := ct.st.VisibleLines()
	if want := int(400 / lh); first != 0 || last != want {
		t.Fatalf("visible lines %d to %d, want 0 to %d", first, last, want)
	}
	ct.st.ScrollTo(50)
	ct.tt.Frame()
	first, last = ct.st.VisibleLines()
	if want := 50 + int(400/lh); first != 50 || last != want {
		t.Fatalf("scrolled, visible lines %d to %d, want 50 to %d", first, last, want)
	}
}

// pixel returns the color of the last frame at x, y in DIPs of an
// editor test's window, 600 wide.
func (ct *editorTest) pixel(x, y float32) (r, g, b uint8) {
	img := ct.tt.Image()
	s := float32(img.Bounds().Dx()) / 600
	c := img.RGBAAt(int(x*s), int(y*s))
	return c.R, c.G, c.B
}

// redTheme paints carets red on white, for tests to find them.
var redTheme = &Theme{Background: ui.RGB(255, 255, 255), Cursor: ui.RGB(255, 0, 0)}

// A block caret covers the character after it, or a space's width at the
// end of a line, in the color of carets made translucent.
func TestBlockCaret(t *testing.T) {
	ct := newEditorTest(t, "", "|abc\nab")
	ct.st.Theme, ct.st.NoActiveLine = redTheme, true
	ct.tt.Frame()
	ed := &ct.st.ed
	cw, lh := ed.view.charWidth, ed.view.lineHeight
	inBlock := func(line, col int) bool {
		r, g, _ := ct.pixel(ed.textX+(float32(col)+0.6)*cw, float32(line)*lh+2)
		return r > 200 && g < 230
	}
	if inBlock(0, 0) {
		t.Fatal("a bar caret painted a block")
	}
	ct.st.BlockCaret = true
	ct.tt.Frame()
	if !inBlock(0, 0) || inBlock(0, 1) {
		t.Fatal("the block caret did not cover the first character alone")
	}
	if r, g, b := ct.pixel(ed.textX+0.6*cw, 2); r < 250 || g < 60 || g != b {
		t.Fatalf("the block caret is %d,%d,%d, not translucent red", r, g, b)
	}
	// At the end of a line, a space's width.
	ct.st.Select(7, 7)
	ct.tt.Frame()
	if !inBlock(1, 2) || inBlock(1, 3) || inBlock(0, 0) {
		t.Fatal("the block caret at the end of a line is not a space wide")
	}
}
