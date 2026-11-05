//go:build darwin

package e2e

import (
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// TestAccessDisabled reads, as VoiceOver does, a button that is only
// unavailable (AccessDisabled, aria-disabled): AXEnabled false, yet
// focused when assistive technology asks, and not pressed, where a button
// Disabled takes neither.
func TestAccessDisabled(t *testing.T) {
	var frames, deleted atomic.Int32
	var focused atomic.Bool
	view := func(c *ui.Context) {
		frames.Add(1)
		ui.Column(c).Padding(20).Gap(10).Children(func() {
			del := ui.Button(c, "Delete project").AccessDisabled(true)
			focused.Store(del.Focused())
			if del.Clicked() {
				deleted.Add(1)
			}
			ui.Button(c, "Close").Disabled(true)
		})
	}
	w := newWindow(t, mygo.WindowOptions{Title: "Unavailable", Width: 400, Height: 200, Content: ui.View(view)})
	eventually(t, "a frame", func() bool { return frames.Load() > 0 })
	for _, label := range []string{"Delete project", "Close"} {
		eventually(t, label+"'s AXEnabled", func() bool {
			v, _, _, ok := axAttribute(w, label, "AXEnabled")
			return ok && v == "0"
		})
	}
	if accessPerform(w, "Close", "focus", "") {
		t.Error("assistive technology focused the disabled button")
	}
	if accessPerform(w, "Delete project", "press", "") {
		t.Error("assistive technology pressed the unavailable button")
	}
	if !accessPerform(w, "Delete project", "focus", "") {
		t.Fatal("cannot focus the unavailable button")
	}
	eventually(t, "the unavailable button focused", focused.Load)
	if deleted.Load() != 0 {
		t.Error("the unavailable button was pressed")
	}
}
