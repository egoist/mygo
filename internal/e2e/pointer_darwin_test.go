//go:build darwin

package e2e

import (
	"slices"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/darwin"
	"github.com/egoist/mygo/ui"
)

func TestContentWindowPointerBlur(t *testing.T) {
	var frames atomic.Int32
	var got []ui.InputKind
	w := newWindow(t, mygo.WindowOptions{Title: "Pointer blur", Width: 200, Height: 180, Content: ui.View(func(c *ui.Context) {
		frames.Add(1)
		ui.Box(c).Fill().HandleInput(func(ev ui.InputEvent) bool { got = append(got, ev.Kind); return true })
	})})
	eventually(t, "pointer blur frame", func() bool { return frames.Load() > 0 })
	mygo.RunOnMain(func() { darwin.TestDrag(w.NativeHandle(), [][2]float64{{30, 40}}) }) // one point presses without releasing
	w.Hide()
	eventually(t, "pointer cancellation on native blur", func() (cancelled bool) {
		mygo.RunOnMain(func() { cancelled = slices.Contains(got, ui.InputPointerCancel) })
		return
	})
	mygo.RunOnMain(func() {
		if !slices.Contains(got, ui.InputPointerCaptureLost) || slices.Contains(got, ui.InputPointerUp) {
			t.Errorf("blur lifecycle=%v", got)
		}
	})
}
