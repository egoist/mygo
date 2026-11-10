//go:build !js

package e2e

import (
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// TestWindowHiddenBuildsAsItHides checks the promise of
// Context.WindowHidden: a view that asks builds once as its window hides,
// out of sight, and again as it shows. On macOS a minimized window's
// display link stops, so a frame waiting for it would never come.
func TestWindowHiddenBuildsAsItHides(t *testing.T) {
	type state struct {
		name string
		// do hides the window, and returns what shows it again.
		do func(w *mygo.Window) (undo func())
	}
	states := []state{{"hidden", func(w *mygo.Window) func() {
		w.Hide()
		return w.Show
	}}}
	if runtime.GOOS != "linux" { // Xvfb has no window manager to minimize windows
		states = append(states, state{"minimized", func(w *mygo.Window) func() {
			w.Minimize()
			return w.Restore
		}})
	}
	for _, s := range states {
		t.Run(s.name, func(t *testing.T) {
			var builtHidden, builtShown atomic.Bool
			w := newWindow(t, mygo.WindowOptions{Title: "WindowHidden", Width: 320, Height: 240, Content: ui.View(func(c *ui.Context) {
				if c.WindowHidden() {
					builtHidden.Store(true)
				} else {
					builtShown.Store(true)
				}
				ui.Text(c, "WindowHidden")
			})})
			// In front, the window shows: one behind a full screen app's
			// would be out of sight from the start.
			w.Focus()
			eventually(t, "the window focused", w.IsFocused)
			eventually(t, "a build while the window shows", builtShown.Load)
			builtShown.Store(false)
			undo := s.do(w)
			eventually(t, "a build as the window hides", builtHidden.Load)
			undo()
			eventually(t, "a build as the window shows again", builtShown.Load)
		})
	}
}
