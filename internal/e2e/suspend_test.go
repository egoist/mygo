//go:build !js

package e2e

import (
	"errors"
	"fmt"
	"runtime"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/wait"
)

// idlePage counts the ticks of a 10 ms timer, which stop while it is
// suspended.
const idlePage = `<!doctype html><body><script>
window.ticks = 0; setInterval(() => ticks++, 10);
</script></body>`

// suspendPage suspends p and returns the error Suspend's done got.
func suspendPage(t *testing.T, p *mygo.Page) error {
	t.Helper()
	ch := make(chan error, 1)
	p.Suspend(func(err error) { ch <- err })
	return wait.For(t, "Suspend's answer", ch)
}

// ticking waits for the timer of idlePage's page to tick again: its
// scripts run.
func ticking(t *testing.T, w *mygo.Window) {
	t.Helper()
	n, err := mygo.EvalAs[int](w.Page(), "ticks")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, w, fmt.Sprintf("ticks > %d", n))
}

// TestPageSuspend does what an app keeping a page open in a hidden window,
// as a player between the tracks it plays, does while the page is idle:
// it sets the page's memory target low, hides the window and suspends the
// page, then resumes it, or shows the window, and the page runs again. A
// page that shows is not suspended, and stays shown. Only WebView2 has
// these: elsewhere the target does nothing and Suspend is unsupported.
func TestPageSuspend(t *testing.T) {
	w := newWindow(t, mygo.WindowOptions{Title: "Suspend", Width: 300, Height: 200})
	p := w.Page()
	p.LoadHTML(idlePage, "")
	waitFor(t, w, "window.ticks > 0")
	p.SetMemoryTarget(mygo.MemoryLow)

	if runtime.GOOS != "windows" {
		if err := suspendPage(t, p); !errors.Is(err, errors.ErrUnsupported) {
			t.Errorf("Suspend: %v, want errors.ErrUnsupported", err)
		}
		if p.IsSuspended() {
			t.Error("Suspend: the page is suspended where it can't be")
		}
		p.Resume() // does nothing
		ticking(t, w)
		return
	}

	switch visible, target := pageController(t, w); {
	case target == -1:
		t.Log("a WebView2 runtime without ICoreWebView2_19: no memory target")
	case target != 1: // COREWEBVIEW2_MEMORY_USAGE_TARGET_LEVEL_LOW
		t.Errorf("SetMemoryTarget(MemoryLow): MemoryUsageTargetLevel %d, want 1", target)
	case !visible:
		t.Error("the page of a visible window has an invisible controller")
	}

	if err := suspendPage(t, p); err == nil {
		t.Error("Suspend of a visible window's page: no error")
	}
	if visible, _ := pageController(t, w); !visible || p.IsSuspended() {
		t.Errorf("after Suspend of a visible window's page: controller visible %v, suspended %v", visible, p.IsSuspended())
	}

	w.Hide()
	eventually(t, "the window hidden", func() bool { return !w.IsVisible() })
	if err := suspendPage(t, p); err != nil {
		t.Fatalf("Suspend of a hidden window's page: %v", err)
	}
	if visible, _ := pageController(t, w); visible || !p.IsSuspended() {
		t.Errorf("after Suspend: controller visible %v, suspended %v", visible, p.IsSuspended())
	}
	p.Resume()
	if visible, _ := pageController(t, w); !visible || p.IsSuspended() {
		t.Errorf("after Resume: controller visible %v, suspended %v", visible, p.IsSuspended())
	}
	ticking(t, w)

	// Suspended again, the page wakes as its window shows.
	if err := suspendPage(t, p); err != nil {
		t.Fatalf("Suspend after Resume: %v", err)
	}
	w.Show()
	eventually(t, "the page awake as its window shows", func() bool { return !p.IsSuspended() })
	if visible, _ := pageController(t, w); !visible {
		t.Error("the shown window's page has an invisible controller")
	}
	ticking(t, w)

	p.SetMemoryTarget(mygo.MemoryNormal)
	if _, target := pageController(t, w); target != -1 && target != 0 {
		t.Errorf("SetMemoryTarget(MemoryNormal): MemoryUsageTargetLevel %d, want 0", target)
	}
}
