//go:build !js

package mygo

import (
	"errors"
	"testing"
)

// TestDeviceOnDesktop checks what the things a device held in the hand has
// do where there is none: the haptics, the system bars, the orientations
// and a navigation bar's buttons are ignored, the share sheet is refused,
// and the battery is not known.
func TestDeviceOnDesktop(t *testing.T) {
	// Every kind of haptic, asked for and dropped.
	Haptics.Selection()
	Haptics.Light()
	Haptics.Medium()
	Haptics.Heavy()
	Haptics.Success()
	Haptics.Warning()
	Haptics.Error()

	// The system's bars take a style, hide, and come back.
	SystemBars.SetStyle(BarStyleDark)
	SystemBars.SetHidden(true)
	SystemBars.SetHidden(false)

	// The screen may turn as it likes, whatever the app says.
	SetOrientations(OrientationLandscape)

	// The share sheet is not there on a desktop.
	if err := Share(ShareOptions{Text: "a link"}); !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("Share = %v, want %v", err, errors.ErrUnsupported)
	}

	// A computer does not know its battery.
	if b := Power.Battery(); b.Level != -1 || b.Charging || b.LowPower {
		t.Errorf("Battery = %+v, want level -1", b)
	}

	// A window without a navigation bar takes no buttons.
	w, _ := testWindow(t, WindowOptions{})
	SetBarItems(w, BarItem{Title: "Probe"})
}
