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
	watcher.seen = true
	watcher.Unlock()
	t.Cleanup(func() {
		watcher.Lock()
		watcher.present, watcher.known, watcher.subscribed, watcher.seen = was, false, false, false
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

// TestWatcherSignalOnlyRecordsTheResume pins the thread boundary. The
// NameOwnerChanged handler runs on the GLib dispatch thread, where GTK must
// not be called and b.trays must not be walked: both belong to the main
// thread. What the handler is allowed to do is record that a redraw is due,
// which the dispatch callback picks up.
//
// The test drives the same flag without a GVariant, because building the
// signal's parameters needs a display; the contract, not the parsing, is what
// matters here.
func TestWatcherSignalOnlyRecordsTheResume(t *testing.T) {
	resumePending.Store(false)
	t.Cleanup(func() { resumePending.Store(false) })

	resumePending.Store(true)
	if !resumePending.Load() {
		t.Fatal("the signal could not record a due resume")
	}
	// resumeTrays consumes the flag, so a second dispatch does not redraw a
	// second time.
	resumeTrays()
	if resumePending.Load() {
		t.Fatal("resumeTrays left the flag set")
	}
	resumeTrays()
	if resumePending.Load() {
		t.Fatal("resumeTrays set the flag again")
	}
}

// TestTrayCallsThroughWithoutAWatcher pins the case the deferral must not
// break: a desktop that never had a StatusNotifierWatcher at all, such as
// GNOME without the appindicator extension or a plain X11 window manager.
// There libayatana's own GtkStatusIcon fallback is the only icon the user
// gets, so holding calls would freeze that icon instead of protecting it.
//
// watcherGone keys on "seen and then lost", not on "not present": a session
// that never had one must keep calling.
func TestTrayCallsThroughWithoutAWatcher(t *testing.T) {
	watcher.Lock()
	watcher.present, watcher.known, watcher.seen, watcher.subscribed = false, true, false, true
	watcher.Unlock()
	t.Cleanup(func() {
		watcher.Lock()
		watcher.present, watcher.known, watcher.seen, watcher.subscribed = false, false, false, false
		watcher.Unlock()
	})

	// The setters are stubbed: this test is about whether the call goes
	// through at all, and a fake handle would fault inside libayatana.
	oldTitle, oldTip := appIndicatorSetTitle, appIndicatorSetTooltipFull
	t.Cleanup(func() { appIndicatorSetTitle, appIndicatorSetTooltipFull = oldTitle, oldTip })
	calls := 0
	appIndicatorSetTitle = func(_ ptr, _ *byte) { calls++ }
	appIndicatorSetTooltipFull = func(_ ptr, _, _, _ *byte) { calls++ }

	tray := &tray{id: 1, ind: 0x1}
	tray.SetToolTip("sing-bird")
	if calls == 0 {
		t.Fatal("a session that never had a watcher did not reach libayatana")
	}
	if tray.pending {
		t.Fatal("a session that never had a watcher deferred a call")
	}

	// Once one was seen and then lost, the same call waits.
	watcher.Lock()
	watcher.seen = true
	watcher.Unlock()
	tray.SetToolTip("sing-bird")
	if !tray.pending {
		t.Fatal("a lost watcher did not defer the call")
	}
}

// TestResumeAppliesClearedText pins the second review point: an app that
// clears its title or tooltip while the shell is away must see it cleared when
// the shell returns. resume used to skip empty strings, so the old text stayed
// on the icon.
//
// The libayatana setters are stubbed, because what matters is that they are
// called with "" rather than not called at all.
func TestResumeAppliesClearedText(t *testing.T) {
	setWatcher(t, true)
	oldLabel, oldTitle, oldTip := appIndicatorSetLabel, appIndicatorSetTitle, appIndicatorSetTooltipFull
	t.Cleanup(func() {
		appIndicatorSetLabel, appIndicatorSetTitle, appIndicatorSetTooltipFull = oldLabel, oldTitle, oldTip
	})

	labelCalls, tipCalls := 0, 0
	appIndicatorSetLabel = func(_ ptr, label, _ *byte) { labelCalls++; _ = label }
	appIndicatorSetTitle = func(_ ptr, _ *byte) { tipCalls++ }
	appIndicatorSetTooltipFull = func(_ ptr, _, _, _ *byte) { tipCalls++ }

	tray := &tray{id: 1, ind: 0x1, pending: true, title: "", tooltip: ""}
	tray.resume()

	if labelCalls != 1 {
		t.Errorf("resume called the label setter %d times, want 1 (clearing counts)", labelCalls)
	}
	// applyToolTip sets both the title and the tooltip property, so the two
	// stubs together see two calls. What matters is that neither is skipped
	// for being empty.
	if tipCalls != 2 {
		t.Errorf("resume called the tooltip setters %d times, want 2 (clearing counts)", tipCalls)
	}
}
