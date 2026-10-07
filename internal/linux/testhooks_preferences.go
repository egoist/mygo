//go:build linux && (amd64 || arm64)

package linux

import "unsafe"

// TestScrollbarPreference toggles the process's GTK setting, exercising
// notify::gtk-overlay-scrolling without changing the desktop configuration.
// Main thread only. Older GTK versions return nil.
func TestScrollbarPreference(always bool) func() {
	settings := gtkSettingsGetDefault()
	if !gtkHasSetting(settings, "gtk-overlay-scrolling") {
		return nil
	}
	var old int32
	gObjectGetPtr(settings, cs("gtk-overlay-scrolling"), unsafe.Pointer(&old), 0)
	gObjectSetBool(settings, cs("gtk-overlay-scrolling"), !always, 0)
	return func() { gObjectSetBool(settings, cs("gtk-overlay-scrolling"), old != 0, 0) }
}
