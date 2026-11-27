//go:build !js

package mygo

import "testing"

// TestWindowOnMove checks that the window is told when it was moved, and
// that a listener turned off is not told again.
func TestWindowOnMove(t *testing.T) {
	w, fw := testWindow(t, WindowOptions{})
	moves := 0
	off := w.OnMove(func() { moves++ })
	onMain(func() { fw.H.Moved() })
	if moves != 1 {
		t.Errorf("OnMove ran %d times, want 1", moves)
	}
	off()
	onMain(func() { fw.H.Moved() })
	if moves != 1 {
		t.Errorf("the listener turned off was told of the move again: %d times", moves)
	}
}

// TestPageOnDidNavigate checks that the page is told when a navigation
// commits, with the URL it went to, that a navigation which only starts
// is not one that committed, and that a listener turned off is not told
// again.
func TestPageOnDidNavigate(t *testing.T) {
	w, fw := testWindow(t, WindowOptions{})
	var urls []string
	off := w.Page().OnDidNavigate(func(url string) { urls = append(urls, url) })

	// A navigation that starts is not one that committed yet.
	onMain(func() { fw.H.NavigationStarted("https://example.com/") })
	if len(urls) != 0 {
		t.Errorf("a navigation that started was told of as one that committed: %q", urls)
	}

	onMain(func() { fw.H.NavigationCommitted("https://example.com/") })
	if len(urls) != 1 || urls[0] != "https://example.com/" {
		t.Errorf("OnDidNavigate heard %q, want the URL that committed", urls)
	}

	off()
	onMain(func() { fw.H.NavigationCommitted("https://example.com/other") })
	if len(urls) != 1 {
		t.Errorf("the listener turned off was told of the navigation again: %q", urls)
	}
}

// TestPageOnDidFinishLoad checks that the page is told when its load
// finishes, and that a listener turned off is not told again.
func TestPageOnDidFinishLoad(t *testing.T) {
	w, fw := testWindow(t, WindowOptions{})
	loads := 0
	off := w.Page().OnDidFinishLoad(func() { loads++ })
	onMain(func() { fw.H.LoadFinished() })
	if loads != 1 {
		t.Errorf("OnDidFinishLoad ran %d times, want 1", loads)
	}
	off()
	onMain(func() { fw.H.LoadFinished() })
	if loads != 1 {
		t.Errorf("the listener turned off was told of the load again: %d times", loads)
	}
}

// TestWindowPopped checks that a window the user took off its stack is
// closed, as its back button closes it.
func TestWindowPopped(t *testing.T) {
	w, _ := testWindow(t, WindowOptions{})
	onMain(func() { (&windowHandler{w}).Popped() })
	if onMainValue(func() bool { return w.native != nil }) {
		t.Error("the popped window is still attached")
	}
}
