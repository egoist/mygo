package ui

import (
	"fmt"
	"strings"

	accel "github.com/egoist/mygo/internal/accelerator"
	"github.com/egoist/mygo/internal/platform"
)

// ParseShortcut reads a key with modifiers written as a menu's
// accelerator, as "CmdOrCtrl+Shift+K", "Alt+F4" or "Ctrl+=", in any case,
// for a shortcut read from a file or a setting:
//
//	mods, key, err := ui.ParseShortcut(app.settings.PaletteKey)
//	if err == nil && c.Shortcut(mods, key) {
//		app.palette.Open()
//	}
//
// CmdOrCtrl is Cmd, and Cmd, Super, Meta and Win the Command key on a
// Mac, in its browsers too. A key ui has no Key for, as F13, Num0 or
// Plus, is an error.
func ParseShortcut(s string) (Modifiers, Key, error) {
	goos := "linux"
	if platform.MacKeys || platform.MacBrowser {
		goos = "darwin"
	}
	a, err := accel.Parse(s, goos)
	if err != nil {
		return 0, KeyUnknown, shortcutError{err}
	}
	key := keyNamed(a.Key)
	if key == KeyUnknown {
		name := strings.TrimSpace(s[strings.LastIndexByte(s, '+')+1:])
		if name == "" {
			name = "+"
		}
		return 0, KeyUnknown, fmt.Errorf("ui: shortcuts have no key %q, in %q", name, s)
	}
	var mods Modifiers
	for _, m := range [...]struct {
		from accel.Modifiers
		to   Modifiers
	}{{accel.Super, Super}, {accel.Ctrl, Ctrl}, {accel.Alt, Alt}, {accel.Shift, Shift}} {
		if a.Has(m.from) {
			mods |= m.to
		}
	}
	return macBrowserMods(mods), key, nil
}

// shortcutError is accelerator's error, said as ui's.
type shortcutError struct{ err error }

func (e shortcutError) Error() string {
	return "ui: " + strings.TrimPrefix(e.err.Error(), "accelerator: ")
}

func (e shortcutError) Unwrap() error { return e.err }

// FormatShortcut writes a key with modifiers as ParseShortcut reads it
// back on this platform, as "Ctrl+Shift+z", for saving a shortcut the
// user chose. The text is this platform's: Cmd is written Super on a Mac
// and Ctrl elsewhere, so a file shared between systems writes CmdOrCtrl
// instead. It is "" for a key with no name, as KeyContextMenu or KeyBack.
func FormatShortcut(mods Modifiers, key Key) string {
	return accelerator(macBrowserMods(mods), key)
}

// macBrowserMods swaps Super and Ctrl in a browser on a Mac, which sends
// Command as Control and Control as Super (platform.MacBrowser), between
// the keys an accelerator names and those ui's shortcuts get.
func macBrowserMods(mods Modifiers) Modifiers {
	if !platform.MacBrowser {
		return mods
	}
	swapped := mods &^ (Super | Ctrl)
	if mods&Super != 0 {
		swapped |= Ctrl
	}
	if mods&Ctrl != 0 {
		swapped |= Super
	}
	return swapped
}

// ShortcutLabel writes a key with modifiers as the platform's menus show
// it, as "⇧⌘K" on macOS and "Ctrl+Shift+K" elsewhere, for a hint or a
// list of shortcuts. It is "" for a key with no name.
func ShortcutLabel(mods Modifiers, key Key) string {
	return shortcutText(accelerator(mods, key))
}

// keyNamed is the key keyName names name, or KeyUnknown.
func keyNamed(name string) Key {
	for k := KeyUnknown + 1; k <= KeyForward; k++ {
		if keyName(k) == name {
			return k
		}
	}
	return KeyUnknown
}
