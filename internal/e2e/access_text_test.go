package e2e

import (
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type textAccess struct {
	content, selected, firstLine string
	start, end                   int
	bounds                       [4]float64
	visible, selectable          bool
}

func TestContentWindowTextAccessibility(t *testing.T) {
	var frames atomic.Int32
	value := "A😀e\u0301 שלום\nsecond line"
	secret := "never expose this🔒"
	view := func(c *ui.Context) {
		frames.Add(1)
		ui.Column(c).Fill().Padding(10).Gap(8).Children(func() {
			ui.TextArea(c, &value).Label("Editor").Width(310).Height(110)
			ui.TextInput(c, &value).Label("Read only").ReadOnly(true)
			ui.RichText(c, ui.Span{Text: "Read ", Weight: 600}, ui.Span{Text: "this 😀", Italic: true}).Label("Selectable").Selectable()
			ui.TextInput(c, &secret).Label("Password").Password()
		})
	}
	w := newWindow(t, mygo.WindowOptions{Title: "Text accessibility", Width: 360, Height: 360, Content: ui.View(view)})
	eventually(t, "a text frame", func() bool { return frames.Load() > 0 })
	if _, ok := accessibility(w); !ok {
		t.Skip("native accessibility provider unavailable")
	}
	for _, label := range []string{"Editor", "Read only", "Selectable"} {
		n, ok := accessText(w, label)
		if !ok {
			t.Fatalf("no text provider for %s", label)
		}
		if n.content == "" || n.bounds[2] <= 0 || n.bounds[3] <= 0 || !n.visible || !n.selectable {
			t.Fatalf("%s: %+v", label, n)
		}
	}
	if n, ok := accessText(w, "Editor"); !ok || n.content != value || n.firstLine != "A😀e\u0301 שלום\n" {
		t.Fatalf("editor ranges: %+v, %v", n, ok)
	}
	// Provider helpers select the first two user-perceived characters,
	// converting to native units on macOS. They read back through the
	// native Text/AX/ATK interface after the next frame.
	for _, label := range []string{"Editor", "Read only"} {
		if !accessSelectText(w, label, 0, 2) {
			t.Fatalf("cannot select in %s", label)
		}
		eventually(t, "native text selection", func() bool { n, ok := accessText(w, label); return ok && n.selected == "A😀" })
	}
	if accessPerform(w, "Read only", "value", "overwrite") { // ATK refuses the editable interface; AppKit guards AXValue
		mygo.RunOnMain(func() {
			if value != "A😀e\u0301 שלום\nsecond line" {
				t.Error("read-only value changed")
			}
		})
	}
	if n, ok := accessText(w, "Password"); ok && (n.content != "" || n.selected != "" || n.visible) {
		t.Fatalf("password exposed ranges: %+v", n)
	}
	if accessSelectText(w, "Password", 0, 2) {
		t.Fatal("password exposes text selection")
	}
	w.Update(func() { value = "changed 😀\nnew line" })
	eventually(t, "updated provider text", func() bool { n, ok := accessText(w, "Editor"); return ok && n.content == "changed 😀\nnew line" })
}
