package main

import (
	"errors"
	"image"
	"testing"

	"github.com/egoist/mygo/plugins/desktopcontrol"
	"github.com/egoist/mygo/ui"
)

var (
	notes  = desktopcontrol.Window{ID: 7, PID: 70, App: "Notes", Title: "Groceries"}
	finder = desktopcontrol.Window{ID: 9, PID: 90, App: "Finder"}
)

// The window lists the desktop's windows, and choosing one captures it
// and shows its screenshot.
func TestCapture(t *testing.T) {
	var captured []desktopcontrol.Window
	m := &model{
		list:    func() ([]desktopcontrol.Window, error) { return []desktopcontrol.Window{notes, finder}, nil },
		capture: func(w desktopcontrol.Window) { captured = append(captured, w) },
	}
	m.refresh()
	tt := ui.NewTester(m.view, 820, 560)
	for _, s := range []string{"Notes — Groceries", "Finder", "No window chosen"} {
		if _, ok := tt.Find(s); !ok {
			t.Errorf("%q isn't shown", s)
		}
	}
	if err := tt.Click("Capture Notes — Groceries"); err != nil {
		t.Fatal(err)
	}
	if len(captured) != 1 || captured[0] != notes {
		t.Fatalf("captured %v, want Notes' window", captured)
	}
	if _, ok := tt.Find("Capturing…"); !ok {
		t.Error("a capture under way isn't shown")
	}
	// Busy, the other windows wait for it.
	if err := tt.Click("Capture Finder"); err != nil {
		t.Fatal(err)
	}
	if len(captured) != 1 {
		t.Errorf("captured %v while busy, want Notes' window alone", captured)
	}

	m.shot, m.busy = ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 960, 600))), false
	tt.Frame()
	if _, ok := tt.Find("Capturing…"); ok {
		t.Error("the screenshot isn't shown in place of the capture")
	}

	m.shot, m.shotErr = nil, errors.New("desktopcontrol: screen capture failed")
	tt.Frame()
	if _, ok := tt.Find("desktopcontrol: screen capture failed"); !ok {
		t.Error("a failed capture says nothing")
	}
}

// Without Screen Recording, the window offers to guide the user to it.
func TestScreenRecording(t *testing.T) {
	guided := 0
	m := &model{
		list:  func() ([]desktopcontrol.Window, error) { return nil, desktopcontrol.ErrScreenRecording },
		guide: func() { guided++ },
	}
	m.refresh()
	tt := ui.NewTester(m.view, 820, 560)
	if err := tt.Click("Guide to Screen Recording"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Guide to Screen Recording"); err != nil {
		t.Fatal(err)
	}
	if guided != 1 {
		t.Errorf("guided %d times, want once while the guide shows", guided)
	}
}
