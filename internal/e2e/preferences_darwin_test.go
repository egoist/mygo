//go:build darwin

package e2e

import (
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/darwin"
	"github.com/egoist/mygo/ui"
)

func TestContentWindowNativePreferences(t *testing.T) {
	var builds atomic.Int32
	var current atomic.Value
	w := mygo.NewWindow(mygo.WindowOptions{Width: 300, Height: 200, Content: ui.View(func(c *ui.Context) {
		current.Store(c.Preferences())
		builds.Add(1)
		ui.Scroll(c).Fill().Children(func() { ui.Text(c, "Native preferences"); ui.Box(c).Height(1000).Shrink(0) })
	})})
	defer w.Destroy()
	eventually(t, "first preference snapshot", func() bool { return current.Load() != nil })
	for _, always := range []bool{true, false} {
		before := builds.Load()
		var restore func()
		mygo.RunOnMain(func() { restore = darwin.TestScrollbarPreference(always) })
		func() {
			defer mygo.RunOnMain(restore)
			want := ui.ScrollbarOnScroll
			if always {
				want = ui.ScrollbarAlways
			}
			eventually(t, "AppKit scrollbar notification and preference", func() bool {
				return builds.Load() > before && current.Load().(ui.Preferences).ScrollbarVisibility == want
			})
		}()
	}
	before := builds.Load()
	mygo.RunOnMain(darwin.TestNotifyDisplayPreferences)
	eventually(t, "NSWorkspace display preference notification", func() bool { return builds.Load() > before })
	t.Logf("native display preferences: %+v", current.Load())
}
