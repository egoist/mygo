//go:build linux && (amd64 || arm64)

package e2e

import (
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/linux"
	"github.com/egoist/mygo/ui"
)

func TestContentWindowNativePreferences(t *testing.T) {
	var current atomic.Value
	w := mygo.NewWindow(mygo.WindowOptions{Width: 300, Height: 200, Content: ui.View(func(c *ui.Context) {
		current.Store(c.Preferences())
		ui.Scroll(c).Fill().Children(func() { ui.Text(c, "Native preferences"); ui.Box(c).Height(1000).Shrink(0) })
	})})
	defer w.Destroy()
	eventually(t, "first preference snapshot", func() bool { return current.Load() != nil })
	var restore func()
	mygo.RunOnMain(func() { restore = linux.TestScrollbarPreference(true) })
	if restore == nil {
		t.Skip("GTK does not expose overlay scrolling")
	}
	defer mygo.RunOnMain(restore)
	eventually(t, "GTK overlay scrolling notification", func() bool {
		return current.Load().(ui.Preferences).ScrollbarVisibility == ui.ScrollbarAlways
	})
}
