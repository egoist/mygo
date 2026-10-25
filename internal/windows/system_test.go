//go:build windows && (amd64 || arm64)

package windows

import "testing"

// TestPreferredColorScheme checks which color scheme of WebView2's profiles
// each theme source sets: light and dark override the system's, which
// "system" leaves to WebView2 (and so does a source not known).
func TestPreferredColorScheme(t *testing.T) {
	for source, want := range map[string]uintptr{
		"system": preferredColorSchemeAuto,
		"light":  preferredColorSchemeLight,
		"dark":   preferredColorSchemeDark,
		"":       preferredColorSchemeAuto,
	} {
		if got := preferredColorScheme(source); got != want {
			t.Errorf("preferredColorScheme(%q) = %d, want %d", source, got, want)
		}
	}
}
