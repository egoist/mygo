package ui

import (
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

// rawKeysTester builds an element with the focus that notes its keys,
// asking for RawKeys when raw.
func rawKeysTester(raw bool, take func(InputEvent) bool) (*Tester, *[]InputEvent) {
	var got []InputEvent
	tt := coreNewTester(func(c *context) {
		e := coreBox(c).Size(100, 100).Focusable().AutoFocus().HandleInput(func(ev InputEvent) bool {
			if ev.Kind != InputKeyDown && ev.Kind != InputKeyUp {
				return false
			}
			got = append(got, ev)
			return take != nil && take(ev)
		})
		if raw {
			e.RawKeys()
		}
	}, 200, 200)
	return tt, &got
}

// A tap of Super alone comes down and up, and tells its side.
func TestRawKeysSuperTap(t *testing.T) {
	tt, got := rawKeysTester(true, nil)
	tt.CodeDown(Super, CodeSuperRight)
	tt.CodeUp(0, CodeSuperRight)
	want := []InputEvent{
		{Kind: InputKeyDown, Code: CodeSuperRight, Mods: Super},
		{Kind: InputKeyUp, Code: CodeSuperRight},
	}
	if len(*got) != len(want) {
		t.Fatalf("got %d events, want %d: %+v", len(*got), len(want), *got)
	}
	for i, w := range want {
		g := (*got)[i]
		if g.Kind != w.Kind || g.Code != w.Code || g.Mods != w.Mods || g.Key != KeyUnknown || g.PhysicalKey != KeyUnknown {
			t.Errorf("event %d: %+v, want %+v", i, g, w)
		}
	}
}

// The left and right of a modifier are told apart, with the modifiers held
// as they change.
func TestRawKeysSides(t *testing.T) {
	tt, got := rawKeysTester(true, nil)
	tt.CodeDown(Shift, CodeShiftLeft)
	tt.CodeDown(Shift, CodeShiftRight)
	tt.CodeUp(Shift, CodeShiftLeft)
	tt.CodeUp(0, CodeShiftRight)
	for i, w := range []struct {
		kind InputKind
		code KeyCode
		mods Modifiers
	}{
		{InputKeyDown, CodeShiftLeft, Shift},
		{InputKeyDown, CodeShiftRight, Shift},
		{InputKeyUp, CodeShiftLeft, Shift},
		{InputKeyUp, CodeShiftRight, 0},
	} {
		if g := (*got)[i]; g.Kind != w.kind || g.Code != w.code || g.Mods != w.mods {
			t.Errorf("event %d: %+v, want %+v", i, g, w)
		}
	}
}

// Keys of no Key reach the element: the lock keys, the keypad's, the 102nd.
func TestRawKeysMissingKeys(t *testing.T) {
	for _, code := range []KeyCode{CodeCapsLock, CodeNumLock, CodeScrollLock, CodePrintScreen, CodePause, CodeIntlBackslash, CodeNumpad5, CodeNumpadAdd, CodeAltRight} {
		tt, got := rawKeysTester(true, nil)
		tt.CodeDown(0, code)
		tt.CodeUp(0, code)
		if len(*got) != 2 || (*got)[0].Code != code || (*got)[0].Kind != InputKeyDown || (*got)[1].Code != code || (*got)[1].Kind != InputKeyUp {
			t.Errorf("key %#x: %+v", uint16(code), *got)
		}
	}
}

// Without RawKeys an element sees what it saw before, and the window takes
// nothing from the system: what goes to Alt's menu bar still does.
func TestRawKeysOptIn(t *testing.T) {
	tt, got := rawKeysTester(false, func(InputEvent) bool { return true })
	if tt.rt.event(platform.SurfaceEvent{Kind: platform.RawKeyPressed, Code: platform.CodeAltLeft, Mods: platform.ModAlt}) {
		t.Error("the key went to the content")
	}
	tt.CodeUp(0, CodeAltLeft)
	if len(*got) != 0 {
		t.Errorf("an element that did not ask got %+v", *got)
	}
	// The modifiers held still change, as ModifiersChanged tells them.
	var mods Modifiers
	tt = coreNewTester(func(c *context) { mods = c.Modifiers() }, 200, 200)
	tt.CodeDown(Ctrl, CodeControlLeft)
	tt.Frame()
	if mods != Ctrl {
		t.Errorf("modifiers %v, want Ctrl", mods)
	}
}

// Taking a raw key tells the backend, which keeps it from the system;
// leaving it does not.
func TestRawKeysTaken(t *testing.T) {
	tt, _ := rawKeysTester(true, func(ev InputEvent) bool { return ev.Code == CodeAltLeft })
	if !tt.rt.event(platform.SurfaceEvent{Kind: platform.RawKeyPressed, Code: platform.CodeAltLeft, Mods: platform.ModAlt}) {
		t.Error("Alt taken, not told")
	}
	if tt.rt.event(platform.SurfaceEvent{Kind: platform.RawKeyPressed, Code: platform.CodeShiftLeft, Mods: platform.ModShift}) {
		t.Error("Shift left, told taken")
	}
}

// Only the element with the focus gets them.
func TestRawKeysFocus(t *testing.T) {
	var got []InputEvent
	tt := coreNewTester(func(c *context) {
		coreBox(c).Size(50, 50).RawKeys().HandleInput(func(ev InputEvent) bool {
			got = append(got, ev)
			return false
		})
		coreButton(c, "Other").AutoFocus()
	}, 200, 200)
	tt.CodeDown(Shift, CodeShiftLeft)
	if len(got) != 0 {
		t.Errorf("an element without the focus got %+v", got)
	}
}

// Keys of a name carry their position too, for anything that takes them by
// it, and those without RawKeys do as well.
func TestInputEventCode(t *testing.T) {
	for _, raw := range []bool{false, true} {
		tt, got := rawKeysTester(raw, nil)
		tt.send(platform.SurfaceEvent{Kind: platform.KeyPressed, Key: platform.KeyEnter, Code: platform.CodeNumpadEnter})
		tt.send(platform.SurfaceEvent{Kind: platform.KeyReleased, Key: platform.KeyEnter, Code: platform.CodeNumpadEnter})
		if len(*got) != 2 {
			t.Fatalf("raw %v: %+v", raw, *got)
		}
		for _, ev := range *got {
			if ev.Key != KeyEnter || ev.Code != CodeNumpadEnter {
				t.Errorf("raw %v: %+v", raw, ev)
			}
		}
	}
}

// Shortcuts keep working on an element asking for raw keys, with the
// modifiers those bring.
func TestRawKeysKeepShortcuts(t *testing.T) {
	saved := 0
	tt := coreNewTester(func(c *context) {
		coreBox(c).Size(100, 100).Focusable().AutoFocus().RawKeys().HandleInput(func(InputEvent) bool { return false })
		if c.Shortcut(Ctrl, KeyS) {
			saved++
		}
	}, 200, 200)
	tt.CodeDown(Ctrl, CodeControlLeft)
	tt.Key(Ctrl, KeyS)
	tt.CodeUp(0, CodeControlLeft)
	tt.Frame()
	if saved != 1 {
		t.Errorf("the shortcut ran %d times", saved)
	}
}

// The held keys' releases, made up by a backend, tell the position too.
func TestHeldKeysLiftCode(t *testing.T) {
	var h platform.HeldKeys
	var sent []platform.SurfaceEvent
	send := func(ev platform.SurfaceEvent) bool {
		h.Track(ev)
		sent = append(sent, ev)
		return true
	}
	send(platform.SurfaceEvent{Kind: platform.KeyPressed, Key: platform.Key5, Code: platform.CodeNumpad5})
	h.Lift(platform.Key5, platform.KeyUnknown, 0, send)
	if last := sent[len(sent)-1]; last.Kind != platform.KeyReleased || last.Code != platform.CodeNumpad5 {
		t.Errorf("released %+v", last)
	}
}
