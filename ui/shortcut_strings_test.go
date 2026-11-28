package ui_test

import (
	"runtime"
	"testing"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/ui"
)

// TestParseShortcut reads shortcuts as menus' accelerators are written,
// and runs the keys read: CmdOrCtrl is Cmd, the case does not matter, and
// a key ui has no name for is an error.
func TestParseShortcut(t *testing.T) {
	for _, c := range []struct {
		in   string
		mods ui.Modifiers
		key  ui.Key
	}{
		{"CmdOrCtrl+Shift+K", ui.Cmd | ui.Shift, ui.KeyK},
		{"alt+f4", ui.Alt, ui.KeyF4},
		{"Ctrl+=", ui.Ctrl, ui.KeyEqual},
		{"Control+Option+PageDown", ui.Ctrl | ui.Alt, ui.KeyPageDown},
		{"Cmd+Return", ui.Super, ui.KeyEnter},
		{"Esc", 0, ui.KeyEscape},
		{"Shift+/", ui.Shift, ui.KeySlash},
		{"Super+Space", ui.Super, ui.KeySpace},
	} {
		mods, key, err := ui.ParseShortcut(c.in)
		if err != nil || mods != c.mods || key != c.key {
			t.Errorf("ParseShortcut(%q) = %v, %v, %v; want %v, %v", c.in, mods, key, err, c.mods, c.key)
		}
	}
	for _, c := range []struct{ in, err string }{
		{"", `ui: empty accelerator`},
		{"Ctrl+", `ui: invalid accelerator "Ctrl+"`},
		{"Hyper+K", `ui: unknown modifier "Hyper" in "Hyper+K"`},
		{"Ctrl+K Ctrl+C", `ui: unknown modifier "K Ctrl" in "Ctrl+K Ctrl+C"`},
		{"F13", `ui: shortcuts have no key "F13", in "F13"`},
		{"Num0", `ui: shortcuts have no key "Num0", in "Num0"`},
		{"CmdOrCtrl+Plus", `ui: shortcuts have no key "Plus", in "CmdOrCtrl+Plus"`},
		{"Ctrl++", `ui: shortcuts have no key "+", in "Ctrl++"`},
		{"VolumeUp", `ui: shortcuts have no key "VolumeUp", in "VolumeUp"`},
		{"Ctrl+ы", `ui: shortcuts have no key "ы", in "Ctrl+ы"`},
	} {
		if mods, key, err := ui.ParseShortcut(c.in); err == nil || err.Error() != c.err {
			t.Errorf("ParseShortcut(%q) = %v, %v, %v; want the error %s", c.in, mods, key, err, c.err)
		}
	}

	mods, key, err := ui.ParseShortcut("CmdOrCtrl+Shift+K")
	if err != nil {
		t.Fatal(err)
	}
	ran := 0
	tt := ui.NewTester(func(c *ui.Context) {
		if c.Shortcut(mods, key) {
			ran++
		}
	}, 200, 100)
	tt.Key(ui.Cmd|ui.Shift, ui.KeyK)
	if ran != 1 {
		t.Errorf("the shortcut read ran %d times for its key, want once", ran)
	}
}

// TestFormatShortcutRoundTrips writes every key ui names with modifiers,
// and reads it back as it was, in a browser on a Mac too.
func TestFormatShortcutRoundTrips(t *testing.T) {
	for _, macBrowser := range []bool{false, true} {
		t.Run(map[bool]string{false: "here", true: "mac browser"}[macBrowser], func(t *testing.T) {
			defer func(was bool) { platform.MacBrowser = was }(platform.MacBrowser)
			platform.MacBrowser = macBrowser
			roundTrip(t)
		})
	}
}

func roundTrip(t *testing.T) {
	keys := []ui.Key{ui.KeyEnter, ui.KeyEscape, ui.KeyBackspace, ui.KeyTab, ui.KeySpace, ui.KeyDelete, ui.KeyInsert,
		ui.KeyHome, ui.KeyEnd, ui.KeyPageUp, ui.KeyPageDown, ui.KeyLeft, ui.KeyRight, ui.KeyUp, ui.KeyDown,
		ui.KeyMinus, ui.KeyEqual, ui.KeyComma, ui.KeyPeriod, ui.KeySlash, ui.KeySemicolon, ui.KeyQuote,
		ui.KeyBracketLeft, ui.KeyBracketRight, ui.KeyBackslash, ui.KeyBackquote}
	for k := ui.KeyF1; k <= ui.KeyF12; k++ {
		keys = append(keys, k)
	}
	for k := ui.Key0; k <= ui.Key9; k++ {
		keys = append(keys, k)
	}
	for k := ui.KeyA; k <= ui.KeyZ; k++ {
		keys = append(keys, k)
	}
	for _, key := range keys {
		for _, mods := range []ui.Modifiers{0, ui.Shift, ui.Ctrl | ui.Alt, ui.Super | ui.Ctrl | ui.Alt | ui.Shift} {
			s := ui.FormatShortcut(mods, key)
			gotMods, gotKey, err := ui.ParseShortcut(s)
			if err != nil || gotMods != mods || gotKey != key {
				t.Errorf("ParseShortcut(FormatShortcut(%v, %v) = %q) = %v, %v, %v", mods, key, s, gotMods, gotKey, err)
			}
		}
	}
	if s := ui.FormatShortcut(ui.Ctrl, ui.KeyBack); s != "" {
		t.Errorf("a key with no name is written %q, want none", s)
	}
}

// TestShortcutLabel writes a shortcut as the platform's menus show it,
// as the screen reader hears it of a button given the same key.
func TestShortcutLabel(t *testing.T) {
	want := "Ctrl+Shift+K"
	if runtime.GOOS == "darwin" {
		want = "⇧⌘K"
	}
	if got := ui.ShortcutLabel(ui.Cmd|ui.Shift, ui.KeyK); got != want {
		t.Errorf("ShortcutLabel(Cmd+Shift+K) = %q, want %q", got, want)
	}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Button(c, "Palette").KeyShortcuts(ui.Cmd|ui.Shift, ui.KeyK)
	}, 200, 100)
	if a := accessOf(t, tt, ui.RoleButton, "Palette"); a.KeyShortcuts != want {
		t.Errorf("the button's key shortcuts %q, want the label %q", a.KeyShortcuts, want)
	}
	if got := ui.ShortcutLabel(0, ui.KeyBack); got != "" {
		t.Errorf("a key with no name is shown %q, want none", got)
	}
}

// TestShortcutsInAMacBrowser reads and writes shortcuts by the keys a Mac
// has, in a browser on one, which sends Command as Control and Control as
// Super: Cmd+K runs on Command+K and shows ⌘K, Ctrl+K on Control+K.
func TestShortcutsInAMacBrowser(t *testing.T) {
	defer func(was bool) { platform.MacBrowser = was }(platform.MacBrowser)
	platform.MacBrowser = true
	for _, c := range []struct {
		in           string
		mods         ui.Modifiers
		label, write string
	}{
		{"Cmd+K", ui.Ctrl, "⌘K", "Super+k"},
		{"CmdOrCtrl+Shift+K", ui.Ctrl | ui.Shift, "⇧⌘K", "Super+Shift+k"},
		{"Ctrl+K", ui.Super, "⌃K", "Ctrl+k"},
		{"Ctrl+Cmd+K", ui.Ctrl | ui.Super, "⌃⌘K", "Super+Ctrl+k"},
	} {
		mods, key, err := ui.ParseShortcut(c.in)
		if err != nil || mods != c.mods || key != ui.KeyK {
			t.Errorf("ParseShortcut(%q) = %v, %v, %v; want %v, K", c.in, mods, key, err, c.mods)
		}
		if got := ui.ShortcutLabel(mods, key); got != c.label {
			t.Errorf("%q shows as %q, want %q", c.in, got, c.label)
		}
		if got := ui.FormatShortcut(mods, key); got != c.write {
			t.Errorf("%q is written %q, want %q", c.in, got, c.write)
		}
	}
}
