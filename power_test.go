//go:build !js

package mygo

import (
	"errors"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/internal/wait"
)

func TestPower(t *testing.T) {
	var got []string
	record := func(event string) func() { return func() { got = append(got, event) } }
	offs := []func(){
		Power.OnSuspend(record("suspend")),
		Power.OnResume(record("resume")),
		Power.OnLockScreen(record("lock")),
		Power.OnUnlockScreen(record("unlock")),
	}
	defer func() {
		for _, off := range offs {
			off()
		}
	}()
	wait.Until(t, "the backend asked to watch power events", func() bool { return onMainValue(func() bool { return fb.Watching }) })
	onMain(func() {
		for _, e := range []string{"suspend", "resume", "lock-screen", "unlock-screen", "unknown"} {
			fb.EmitPower(e)
		}
	})
	if want := "suspend resume lock unlock"; join(got) != want {
		t.Errorf("events = %q, want %q", join(got), want)
	}

	release := Power.KeepAwake("testing", true)
	if n := onMainValue(func() int { return fb.Awake }); n != 1 {
		t.Errorf("awake = %d", n)
	}
	release()
	release() // once
	if n := onMainValue(func() int { return fb.Awake }); n != 0 {
		t.Errorf("awake after release = %d", n)
	}
	if Power.IsOnBattery() || Power.IdleTime() != 42*time.Second {
		t.Error("IsOnBattery or IdleTime")
	}
}

func join(s []string) string {
	out := ""
	for i, x := range s {
		if i > 0 {
			out += " "
		}
		out += x
	}
	return out
}

func TestDockMenuAndRelaunch(t *testing.T) {
	m := NewMenu([]*MenuItem{{Label: "New Window", ID: "new"}})
	App.Dock.SetMenu(m)
	if App.Dock.Menu() != m {
		t.Error("Dock.Menu")
	}
	dm := onMainValue(func() any { return fb.DockMenu })
	if dm == nil || len(fb.DockMenu.Items) != 1 || fb.DockMenu.Items[0].Label != "New Window" {
		t.Errorf("dock menu = %+v", dm)
	}
	App.Dock.SetMenu(nil)
	if onMainValue(func() bool { return fb.DockMenu != nil }) {
		t.Error("SetMenu(nil) kept the menu")
	}

	// A relaunch whose quit is canceled does nothing. Before the windows
	// close, it lets the app it starts take the foreground, which the
	// app gives up as they close (Windows).
	canceled := make(chan struct{}, 1)
	allowed := onMainValue(func() int { return fb.ForegroundAllowed })
	off := App.OnBeforeQuit(func(e *QuitEvent) {
		if fb.ForegroundAllowed != allowed+1 {
			t.Errorf("the foreground was passed on %d times before the quit", fb.ForegroundAllowed-allowed)
		}
		e.PreventDefault()
		canceled <- struct{}{}
	})
	defer off()
	App.Relaunch()
	select {
	case <-canceled:
	case <-time.After(wait.Guard):
		t.Fatal("Relaunch did not start quitting")
	}
	if onMainValue(func() bool { return App.relaunch }) {
		t.Error("a canceled relaunch is still pending")
	}
}

func TestPermissionHandler(t *testing.T) {
	w, fw := testWindow(t, WindowOptions{})
	ask := func(kinds []string, origin string) bool {
		return onMainValue(func() bool { return fw.H.PermissionRequested(kinds, origin) })
	}
	// By default the app's own pages get what they ask for.
	if !ask([]string{"camera"}, "mygo://localhost") || ask([]string{"camera"}, "https://evil.example") {
		t.Error("default permission decisions")
	}
	var got PermissionRequest
	w.Page().SetPermissionHandler(func(req PermissionRequest) bool {
		got = req
		return req.Origin == "https://meet.example"
	})
	if !ask([]string{"camera", "microphone"}, "https://meet.example") || ask([]string{"geolocation"}, "mygo://localhost") {
		t.Error("the handler's decisions were not applied")
	}
	if got.Origin != "mygo://localhost" || len(got.Permissions) != 1 || got.Permissions[0] != PermissionGeolocation {
		t.Errorf("last request = %+v", got)
	}
	w.Page().SetPermissionHandler(nil)
	if !ask([]string{"camera"}, "mygo://localhost") {
		t.Error("SetPermissionHandler(nil) did not restore the default")
	}
}

func TestClearBrowsingDataFake(t *testing.T) {
	before := onMainValue(func() int { return fb.Cleared })
	if err := App.ClearBrowsingData(); err != nil {
		t.Fatal(err)
	}
	if onMainValue(func() int { return fb.Cleared }) != before+1 {
		t.Error("the backend did not clear")
	}
}

// TestClearStorageFake sends a StorageID to the backend, and neither the
// empty one nor an invalid one: those fail before it.
func TestClearStorageFake(t *testing.T) {
	counts := func() map[string]int {
		return onMainValue(func() map[string]int { return maps.Clone(fb.StorageCleared) })
	}
	before := counts()
	cleared := onMainValue(func() int { return fb.Cleared })
	if err := App.ClearStorage("acct-a"); err != nil {
		t.Fatal(err)
	}
	if err := App.ClearStorage("acct-a"); err != nil {
		t.Fatal(err)
	}
	if err := App.ClearStorage("acct-b"); err != nil {
		t.Fatal(err)
	}
	after := counts()
	want := maps.Clone(before)
	if want == nil {
		want = map[string]int{}
	}
	want["acct-a"] += 2
	want["acct-b"]++
	if !maps.Equal(after, want) {
		t.Errorf("the backend cleared %v, want %v: acct-a twice, acct-b once, no other ID", after, want)
	}
	if onMainValue(func() int { return fb.Cleared }) != cleared {
		t.Error("ClearStorage cleared the default store")
	}

	for _, id := range []string{"", "a/b", strings.Repeat("x", 65)} {
		if err := App.ClearStorage(id); !errors.Is(err, ErrInvalidStorageID) {
			t.Errorf("ClearStorage(%q) = %v, want ErrInvalidStorageID", id, err)
		}
	}
	if got := counts(); !maps.Equal(got, after) {
		t.Errorf("an ID that is not valid reached the backend: %v", got)
	}
}

// quitLoop simulates the end of the event loop for a test, as quitting
// does: pending native calls are failed and later posts are rejected. The
// cleanup restores them for the next test. The returned function reopens
// the loop at once, and may be called again by the cleanup.
func quitLoop(t *testing.T) (reopen func()) {
	t.Helper()
	onMain(stopNativeCalls)
	loop.mu.Lock()
	loop.closed = true
	loop.mu.Unlock()
	reopen = func() {
		loop.mu.Lock()
		loop.closed = false
		loop.mu.Unlock()
	}
	t.Cleanup(func() {
		reopen()
		onMain(func() { nativeCalls.stopped = false })
	})
	return reopen
}

// clearCalls are the calls that wait for a backend to clear a store.
var clearCalls = []struct {
	name string
	call func() error
}{
	{"ClearBrowsingData", func() error { return App.ClearBrowsingData() }},
	{"ClearStorage", func() error { return App.ClearStorage("acct-a") }},
}

// TestClearAfterQuit checks that the calls return an error once the event
// loop has stopped, rather than wait for ever.
func TestClearAfterQuit(t *testing.T) {
	for _, c := range clearCalls {
		t.Run(c.name, func(t *testing.T) {
			quitLoop(t)
			done := make(chan error, 1)
			go func() { done <- c.call() }()
			if err := wait.For(t, c.name+" to return", done); !errors.Is(err, errLoopStopped) {
				t.Errorf("%s after quit = %v, want errLoopStopped", c.name, err)
			}
		})
	}
}

// TestClearInFlightQuit checks that a call the backend never answers
// returns an error when the app quits, and that an answer after that is
// harmless, as is a quit after an answer.
func TestClearInFlightQuit(t *testing.T) {
	for _, c := range clearCalls {
		t.Run(c.name, func(t *testing.T) {
			onMain(func() { fb.HoldClears = true; fb.HeldClears = nil })
			t.Cleanup(func() { onMain(func() { fb.HoldClears = false; fb.HeldClears = nil }) })
			held := func() []func(error) {
				return onMainValue(func() []func(error) { return append([]func(error){}, fb.HeldClears...) })
			}
			done := make(chan error, 1)
			go func() { done <- c.call() }()
			wait.Until(t, "the backend to get the call", func() bool { return len(held()) == 1 })
			reopen := quitLoop(t)
			if err := wait.For(t, c.name+" to return", done); !errors.Is(err, errLoopStopped) {
				t.Errorf("%s in flight at quit = %v, want errLoopStopped", c.name, err)
			}
			// The backend answers late: nothing to deliver to, no panic.
			reopen()
			onMain(func() { held()[0](nil) })
		})
	}
}

// TestClearQuitAfterAnswer checks that a quit after a call has returned
// does not complete it a second time.
func TestClearQuitAfterAnswer(t *testing.T) {
	for _, c := range clearCalls {
		t.Run(c.name, func(t *testing.T) {
			if err := c.call(); err != nil {
				t.Fatal(err)
			}
			quitLoop(t)
		})
	}
}
