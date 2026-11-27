package ui

import (
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

// A key a handler acts on is Handled, for macOS to give the input method
// none of it; one a text input only keeps to type it is not, as its text
// comes from the input method.
func TestKeyHandled(t *testing.T) {
	value := "abc"
	tt := coreNewTester(func(ctx *context) {
		coreTextInput(ctx, &value).AutoFocus().Width(250).HandleInput(func(ev InputEvent) bool {
			return ev.Kind == InputKeyDown && ev.Mods == Alt|Shift && ev.Key == KeyF
		})
	}, 300, 100)
	for _, c := range []struct {
		mods platform.Modifiers
		key  platform.Key
		want bool
	}{
		{0, platform.KeyJ, false},
		{platform.ModShift, platform.KeyJ, false},
		{0, platform.KeySpace, false},
		{0, platform.KeyLeft, false},
		{platform.ModAlt | platform.ModShift, platform.KeyF, true},
	} {
		handled := false
		tt.send(platform.SurfaceEvent{Kind: platform.KeyPressed, Key: c.key, PhysicalKey: c.key, Mods: c.mods, Handled: &handled})
		tt.send(platform.SurfaceEvent{Kind: platform.KeyReleased, Key: c.key, PhysicalKey: c.key, Mods: c.mods})
		if handled != c.want {
			t.Errorf("key %v with %v: handled %v, want %v", c.key, c.mods, handled, c.want)
		}
	}
}

// PageUp and PageDown with Control, Command or the Windows key go to
// shortcuts from a text field, as switching tabs with Ctrl+PageDown does;
// Alt with them does too but on macOS, where it pages the text, as
// NSTextView's pageDown:. Alone or with Shift, a text area pages its text.
func TestTextFieldsLeaveModifiedPageKeysToShortcuts(t *testing.T) {
	all := []Modifiers{0, Shift, Ctrl, Ctrl | Shift, Alt, Super}
	for _, multiline := range []bool{false, true} {
		value := "one\ntwo\nthree"
		ran := map[Modifiers]int{}
		tt := coreNewTester(func(c *context) {
			if multiline {
				coreTextArea(c, &value).AutoFocus().Size(250, 100)
			} else {
				coreTextInput(c, &value).AutoFocus().Width(250)
			}
			for _, m := range all {
				if c.Shortcut(m, KeyPageDown) {
					ran[m]++
				}
			}
		}, 300, 200)
		tt.Frame()
		for _, m := range all {
			tt.Key(m, KeyPageDown)
			tt.Frame()
			field := m&^Shift == 0 && multiline || m == Alt && platform.MacKeys
			if got := ran[m] == 0; got != field {
				t.Errorf("multiline %v: the field took %v+PageDown: %v, want %v", multiline, m, got, field)
			}
		}
		tt.rt.close()
	}
}
