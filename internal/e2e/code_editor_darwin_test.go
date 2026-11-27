//go:build darwin && !ios

package e2e

import (
	"strings"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// A key OnInput takes is not the input method's too: Option+Shift+F taken
// as a shortcut types no Ï after it, and the keys after it still type.
func TestContentWindowCodeEditorKeyTaken(t *testing.T) {
	var frames, taken atomic.Int32
	st := &editor.State{Buffer: editor.NewBuffer("abc")}
	st.OnInput = func(ev ui.InputEvent) bool {
		if ev.Kind == ui.InputKeyDown && ev.Mods == ui.Alt|ui.Shift && ev.PhysicalKey == ui.KeyF {
			taken.Add(1)
			return true
		}
		return false
	}
	w := newWindow(t, mygo.WindowOptions{Title: "Code editor", Width: 500, Height: 240, Content: ui.View(func(c *ui.Context) {
		editor.View(c, st).Fill().AutoFocus()
		frames.Add(1)
	})})
	eventually(t, "an editor frame", func() bool { return frames.Load() > 0 })
	if !click(w, 400, 10) {
		t.Skip("click automation unavailable")
	}
	eventually(t, "the caret at the end", func() bool {
		var head int
		mygo.RunOnMain(func() { head = st.Cursor() })
		return head == len("abc")
	})
	const option, shift = 1 << 19, 1 << 17
	if !pressKeyFlags(w, 3, "Ï", option|shift) { // kVK_ANSI_F
		t.Skip("key automation unavailable")
	}
	eventually(t, "the key for OnInput", func() bool { return taken.Load() == 1 })
	if !clickAndType(w, 400, 10, "j") {
		return
	}
	var text string
	eventually(t, "text typed", func() bool {
		mygo.RunOnMain(func() { text = st.Buffer.String() })
		return strings.HasSuffix(text, "j")
	})
	if text != "abcj" {
		t.Fatalf("the text is %q, want %q", text, "abcj")
	}
}
