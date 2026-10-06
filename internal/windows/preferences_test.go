//go:build windows && (amd64 || arm64)

package windows

import (
	"runtime"
	"testing"
	"unsafe"
)

func TestPreferenceBroadcasts(t *testing.T) {
	for _, event := range []struct {
		action  uintptr
		section string
		changed bool
	}{
		{spiSetHighContrast, "", true}, {spiSetClientAreaAnimation, "", true},
		{0, "", true}, {0, "Accessibility", true}, {0, "ImmersiveColorSet", true},
		{1, "WindowMetrics", true}, {1, "TextScaleFactor", true},
		{1, "DynamicScrollbars", true}, {1, "EnableTransparency", true},
		{1, "UserPreferences", true}, {1, "unrelated", false},
	} {
		var text *uint16
		if event.section != "" {
			text = u16(event.section)
		}
		got := preferencesChanged(event.action, uintptr(unsafe.Pointer(text)))
		runtime.KeepAlive(text)
		if got != event.changed {
			t.Errorf("action %#x, section %q: %v", event.action, event.section, got)
		}
	}
}
