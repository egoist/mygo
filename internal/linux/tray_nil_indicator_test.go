//go:build linux && (amd64 || arm64)

package linux

import (
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

// TestStaleTrayIgnoresTooltipAndMenu reproduces the crash that killed a
// running app when the desktop shell restarted under it.
//
// app_indicator_new returns NULL once the indicator host is gone, and the
// tray stores that NULL without checking it. Every later call then hands it
// to libayatana, whose gtk_status_icon_set_tooltip_markup fails the
// GTK_IS_STATUS_ICON assertion and dereferences it: SIGSEGV inside cgo, which
// Go cannot turn into a panic, so the process dies with nothing in its log.
//
// The calls run against a zero handle on purpose. Without the guards the
// first of them would dereference a real indicator, so the test states the
// contract: a tray with no handle is inert, not fatal.
func TestStaleTrayIgnoresTooltipAndMenu(t *testing.T) {
	tray := &tray{id: 1}

	tray.SetToolTip("sing-bird")
	tray.SetMenu(&platform.Menu{})
	tray.SetMenu(nil)
	tray.SetActivatable(true)
	tray.SetActivatable(false)

	if tray.ind != 0 {
		t.Fatalf("a stale tray gained a handle: %#x", tray.ind)
	}
}
