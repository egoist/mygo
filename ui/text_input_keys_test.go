package ui

import (
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

// A text input takes the keys it acts on, and leaves to the platform those
// that type text, whose text comes from the input method: macOS gives the
// input method no key the content took.
func TestTextInputLeavesTypingKeys(t *testing.T) {
	value := "abc"
	tt := coreNewTester(func(ctx *context) { coreTextInput(ctx, &value).AutoFocus().Width(250) }, 300, 100)
	cases := []struct {
		mods platform.Modifiers
		key  platform.Key
		want bool
	}{
		{0, platform.KeyJ, false},
		{platform.ModShift, platform.KeyJ, false},
		{0, platform.KeySpace, false},
		{0, platform.Key1, false},
		{0, platform.KeyLeft, true},
		{0, platform.KeyEnter, true},
	}
	if platform.MacKeys {
		// Option and a letter type a character, as Option+E the acute.
		cases = append(cases, struct {
			mods platform.Modifiers
			key  platform.Key
			want bool
		}{platform.ModAlt, platform.KeyE, false})
	}
	for _, c := range cases {
		taken := tt.send2(platform.SurfaceEvent{Kind: platform.KeyPressed, Key: c.key, PhysicalKey: c.key, Mods: c.mods})
		tt.send(platform.SurfaceEvent{Kind: platform.KeyReleased, Key: c.key, PhysicalKey: c.key, Mods: c.mods})
		if taken != c.want {
			t.Errorf("key %v with %v: taken %v, want %v", c.key, c.mods, taken, c.want)
		}
	}
}
