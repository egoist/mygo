//go:build linux && (amd64 || arm64)

package linux

import (
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

// setWatcher forces the cached watcher state, which is what decides whether a
// tray call reaches libayatana.
func setWatcher(t *testing.T, present bool) {
	t.Helper()
	watcher.Lock()
	was := watcher.present
	watcher.present, watcher.known, watcher.subscribed = present, true, true
	watcher.Unlock()
	t.Cleanup(func() {
		watcher.Lock()
		watcher.present, watcher.known, watcher.subscribed = was, false, false
		watcher.Unlock()
	})
}

// TestTrayDefersWhileTheWatcherIsAway pins the fix: with no shell owning the
// watcher name, a tray call must not reach libayatana, whose fallback icon is
// what failed GTK_IS_STATUS_ICON and dereferenced a bad pointer. The state is
// kept instead, so the icon can be drawn again later.
//
// The handle is deliberately non-zero here. A guard on it cannot cover this
// crash: the handle stays valid across a shell restart, which is why the
// defense belongs on the call rather than on the handle.
func TestTrayDefersWhileTheWatcherIsAway(t *testing.T) {
	setWatcher(t, false)
	tray := &tray{id: 1, ind: 0x1}

	tray.SetToolTip("sing-bird")
	tray.SetTitle("sing-bird")
	tray.SetMenu(&platform.Menu{})

	if !tray.pending {
		t.Fatal("a deferred tray did not record that it owes the shell a redraw")
	}
	if tray.tooltip != "sing-bird" || tray.title != "sing-bird" || tray.wanted == nil {
		t.Fatalf("a deferred tray forgot what the app set: %+v", tray)
	}
	if tray.ind != 0x1 {
		t.Fatalf("a deferred tray dropped its handle: %#x", tray.ind)
	}
}

// TestTrayResumesFromWhatItKept checks the other half: the tray draws itself
// from the state it stored, and only once. The libayatana calls are skipped
// here because they need a display; what this pins is that resume consumes the
// debt and keeps everything it needs to redraw.
func TestTrayResumesFromWhatItKept(t *testing.T) {
	setWatcher(t, true)
	tray := &tray{
		id:      1,
		ind:     0, // no display: resume stops before drawing
		pending: true,
		title:   "sing-bird",
		tooltip: "sing-bird \u00b7 tun",
	}

	tray.resume()

	if tray.pending {
		t.Fatal("resume left the tray owing a redraw")
	}
	if tray.title != "sing-bird" || tray.tooltip != "sing-bird \u00b7 tun" {
		t.Fatalf("resume dropped the state it redraws from: %+v", tray)
	}
	// A second resume on a settled tray must not draw again.
	tray.resume()
	if tray.pending {
		t.Fatal("a settled tray went back to owing a redraw")
	}
}
