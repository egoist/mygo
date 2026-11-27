//go:build !js

package mygo

import "testing"

// TestPageControls checks that the page's controls reach the backend:
// reloading counts a load as a navigation does, going back and forward
// goes nowhere with nothing to go to, the user agent the backend gave
// reads back, and a page left alone is not loading.
func TestPageControls(t *testing.T) {
	w, _ := testWindow(t, WindowOptions{Hidden: true, URL: "https://example.com/"})
	p := w.Page()

	before := onMainValue(func() int { return w.loads })
	p.Reload()
	if got := onMainValue(func() int { return w.loads }); got != before+1 {
		t.Errorf("loads after Reload = %d, want %d", got, before+1)
	}
	before++
	p.ReloadIgnoringCache()
	if got := onMainValue(func() int { return w.loads }); got != before+1 {
		t.Errorf("loads after ReloadIgnoringCache = %d, want %d", got, before+1)
	}

	// Stopping, and walking a history that is not there, asks the backend
	// and answers what it says: nowhere to go, nothing loading.
	p.Stop()
	p.GoBack()
	p.GoForward()
	if p.CanGoBack() || p.CanGoForward() {
		t.Error("a page that has not navigated has a history")
	}
	if p.IsLoading() {
		t.Error("a page left alone is loading")
	}

	p.SetUserAgent("Probe")
	if p.UserAgent() == "" {
		t.Error("the backend answered the user agent with nothing")
	}
}

// TestPageDevTools checks the web inspector's controls: it opens, tells
// whether it is open, toggles, and closes.
func TestPageDevTools(t *testing.T) {
	w, _ := testWindow(t, WindowOptions{Hidden: true, URL: "https://example.com/"})
	p := w.Page()

	p.OpenDevTools()
	if !p.IsDevToolsOpened() {
		t.Error("OpenDevTools left the inspector closed")
	}
	p.ToggleDevTools()
	if p.IsDevToolsOpened() {
		t.Error("ToggleDevTools left an open inspector open")
	}
	p.ToggleDevTools()
	if !p.IsDevToolsOpened() {
		t.Error("ToggleDevTools left a closed inspector closed")
	}
	p.CloseDevTools()
	if p.IsDevToolsOpened() {
		t.Error("CloseDevTools left the inspector open")
	}

	// The page's print dialog is the backend's to show.
	p.Print()
}
