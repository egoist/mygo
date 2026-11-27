//go:build !js

package mygo

import "testing"

// TestWindowBlur checks that a window asked to drop the keyboard does, and
// tells its listeners until they are turned off.
func TestWindowBlur(t *testing.T) {
	w, _ := testWindow(t, WindowOptions{})
	w.Focus()
	if FocusedWindow() != w {
		t.Fatal("the window is not the focused one")
	}
	var blurred int
	off := w.OnBlur(func() { blurred++ })
	w.Blur()
	if FocusedWindow() != nil {
		t.Error("a blurred window is still the focused one")
	}
	if blurred != 1 {
		t.Errorf("OnBlur ran %d times, want 1", blurred)
	}
	off()
	w.Focus()
	w.Blur()
	if blurred != 1 {
		t.Errorf("the listener turned off was told of a blur again: %d times", blurred)
	}
}

// TestWindowUnmaximize checks that a maximized window is restored, telling
// its listeners as the system does.
func TestWindowUnmaximize(t *testing.T) {
	w, _ := testWindow(t, WindowOptions{})
	w.Maximize()
	if !w.IsMaximized() {
		t.Fatal("Maximize did not maximize the window")
	}
	var restored int
	off := w.OnUnmaximize(func() { restored++ })
	w.Unmaximize()
	if w.IsMaximized() {
		t.Error("Unmaximize left the window maximized")
	}
	if restored != 1 {
		t.Errorf("OnUnmaximize ran %d times, want 1", restored)
	}
	off()
	w.Maximize()
	w.Unmaximize()
	if restored != 1 {
		t.Errorf("the listener turned off was told of a restore again: %d times", restored)
	}
}

// TestWindowFullScreen checks entering and leaving full screen: the window
// says where it is, tells its listeners as the system does, and toggling
// follows where it is.
func TestWindowFullScreen(t *testing.T) {
	w, fw := testWindow(t, WindowOptions{})
	if w.IsFullScreen() {
		t.Fatal("a window starts out of full screen")
	}
	var entered, left int
	offEnter := w.OnEnterFullScreen(func() { entered++ })
	defer offEnter()
	offLeave := w.OnLeaveFullScreen(func() { left++ })
	defer offLeave()

	w.SetFullScreen(true)
	if !w.IsFullScreen() {
		t.Error("SetFullScreen(true) did not enter full screen")
	}
	onMain(func() { fw.H.EnteredFullScreen() })
	if entered != 1 {
		t.Errorf("OnEnterFullScreen ran %d times, want 1", entered)
	}

	w.SetFullScreen(false)
	if w.IsFullScreen() {
		t.Error("SetFullScreen(false) did not leave full screen")
	}
	onMain(func() { fw.H.LeftFullScreen() })
	if left != 1 {
		t.Errorf("OnLeaveFullScreen ran %d times, want 1", left)
	}

	// Toggling follows the window's own state.
	w.ToggleFullScreen()
	if !w.IsFullScreen() {
		t.Error("ToggleFullScreen did not enter full screen")
	}
	w.ToggleFullScreen()
	if w.IsFullScreen() {
		t.Error("ToggleFullScreen did not leave full screen")
	}

	offEnter()
	offLeave()
	onMain(func() { fw.H.EnteredFullScreen() })
	onMain(func() { fw.H.LeftFullScreen() })
	if entered != 1 || left != 1 {
		t.Errorf("the listeners turned off were told of %d entries and %d leaves", entered-1, left-1)
	}
}
