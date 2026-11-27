package ui

import (
	"strings"
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
	// With no shortcut of the key, a text area still pages its text.
	for _, m := range all {
		value := strings.Repeat("line\n", 40)
		var area *node
		tt := coreNewTester(func(c *context) {
			area = coreTextArea(c, &value).AutoFocus().Size(250, 100)
		}, 300, 200)
		tt.Frame()
		area.SetTextSelection(0, 0)
		tt.Frame()
		tt.Key(m, KeyPageDown)
		tt.Frame()
		if _, caret := area.TextSelection(); caret == 0 || caret == len([]rune(value)) {
			t.Errorf("%v+PageDown with no shortcut of it: the caret at %d, want a page down", m, caret)
		}
		tt.rt.close()
	}
}
