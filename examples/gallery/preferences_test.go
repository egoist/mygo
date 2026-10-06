package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestPreferenceGalleryLiveChangesKeepEditingState(t *testing.T) {
	g := &gallery{}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Scroll(c).Fill().Padding(24).Gap(12).Children(func() { g.preferencesPage(c) })
	}, 940, 900)
	if err := tt.Click("Preference selection"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("Kept across desktop preference changes")
	for _, prefs := range []ui.Preferences{
		{ScrollbarVisibility: ui.ScrollbarAlways, ReduceTransparency: true},
		{HighContrast: true, TextScale: 1.25, ReduceMotion: true},
		{},
	} {
		tt.SetPreferences(prefs)
		tt.Key(ui.Cmd, ui.KeyA)
		tt.Key(ui.Cmd, ui.KeyC)
		if got := tt.Clipboard(); got != "Kept across desktop preference changes" {
			t.Fatalf("live preference change lost editing state: %q", got)
		}
	}
	if !tt.HasText("Scrollbars: Automatic") || !tt.HasText("Reduced transparency: false") {
		t.Fatal("gallery retained stale preference values")
	}
}
