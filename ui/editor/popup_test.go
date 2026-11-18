package editor

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestCodeEditorPopup shows a list of the app's below the caret: it takes
// the keyboard for its own keys, and Escape or an edit closes it, the
// keyboard going back to the editor.
func TestCodeEditorPopup(t *testing.T) {
	ct := newEditorTest(t, "", "|cat dog")
	st := ct.st
	sel, chosen := 0, -1
	items := []string{"usage one", "usage two"}
	list := func(c *ui.Context, panel ui.Element) {
		if panel.Shortcut(0, ui.KeyDown) {
			sel = min(sel+1, len(items)-1)
		}
		if panel.Shortcut(0, ui.KeyEnter) {
			chosen = sel
			st.ClosePopup()
		}
		panel.Children(func() {
			for i, it := range items {
				mark := "  "
				if i == sel {
					mark = "> "
				}
				ui.Text(c, mark+it)
			}
		})
	}
	st.ShowPopup(4, list)
	ct.tt.Frame()
	ct.tt.Frame()
	if !st.PopupShown() || !ct.tt.HasText("> usage one") {
		t.Fatalf("popup not built: %q", ct.tt.Texts())
	}
	ct.key(0, ui.KeyDown)
	ct.key(0, ui.KeyEnter)
	ct.tt.Frame()
	if chosen != 1 || st.PopupShown() || ct.tt.HasText("usage two") {
		t.Fatalf("Enter in the popup: chosen %d, shown %v", chosen, st.PopupShown())
	}
	// The keys are the editor's again: typing goes into its text.
	ct.typ("x")
	ct.want("x|cat dog")

	st.ShowPopup(0, list)
	ct.tt.Frame()
	ct.tt.Frame()
	ct.key(0, ui.KeyEscape)
	ct.tt.Frame()
	if st.PopupShown() {
		t.Fatal("Escape did not close the popup")
	}
	ct.typ("y")
	ct.want("xy|cat dog")

	st.ShowPopup(0, list)
	ct.tt.Frame()
	st.Insert("z")
	ct.tt.Frame()
	if st.PopupShown() {
		t.Fatal("an edit did not close the popup")
	}
}

// A fold that hides the line the popup opened at closes it, as there is
// nothing to attach it to: it does not stay shown with nothing on screen.
func TestCodeEditorPopupClosesWhenItsLineIsHidden(t *testing.T) {
	ct := newEditorTest(t, "go", "func a() {\n\tx()\n}\nz|")
	st := ct.st
	build := func(c *ui.Context, panel ui.Element) {
		panel.Children(func() { ui.Text(c, "usage") })
	}
	st.ShowPopup(len("func a() {\n"), build)
	ct.tt.Frame()
	ct.tt.Frame()
	if !st.PopupShown() || !ct.tt.HasText("usage") {
		t.Fatalf("popup not built: %q", ct.tt.Texts())
	}
	st.Fold(0)
	ct.tt.Frame()
	ct.tt.Frame()
	if st.PopupShown() || ct.tt.HasText("usage") {
		t.Fatalf("the popup of a hidden line: shown %v, texts %q", st.PopupShown(), ct.tt.Texts())
	}
}
