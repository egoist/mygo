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
