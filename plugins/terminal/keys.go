package terminal

import (
	"fmt"
	"unicode"
	"unicode/utf16"

	"github.com/egoist/mygo/plugins/terminal/internal/vt"
	"github.com/egoist/mygo/ui"
)

// keys maps MyGo's keys to Ghostty's, and to the character they type
// without modifiers, which MyGo's keys name (KeyA types an a).
var keys = map[ui.Key]struct {
	key vt.Key
	r   rune
}{
	ui.KeyEnter: {vt.KeyEnter, '\r'}, ui.KeyEscape: {vt.KeyEscape, 0}, ui.KeyBackspace: {vt.KeyBackspace, 0},
	ui.KeyTab: {vt.KeyTab, '\t'}, ui.KeySpace: {vt.KeySpace, ' '}, ui.KeyDelete: {vt.KeyDelete, 0},
	ui.KeyInsert: {vt.KeyInsert, 0}, ui.KeyHome: {vt.KeyHome, 0}, ui.KeyEnd: {vt.KeyEnd, 0},
	ui.KeyPageUp: {vt.KeyPageUp, 0}, ui.KeyPageDown: {vt.KeyPageDown, 0},
	ui.KeyLeft: {vt.KeyArrowLeft, 0}, ui.KeyRight: {vt.KeyArrowRight, 0}, ui.KeyUp: {vt.KeyArrowUp, 0}, ui.KeyDown: {vt.KeyArrowDown, 0},
	ui.KeyMinus: {vt.KeyMinus, '-'}, ui.KeyEqual: {vt.KeyEqual, '='}, ui.KeyComma: {vt.KeyComma, ','},
	ui.KeyPeriod: {vt.KeyPeriod, '.'}, ui.KeySlash: {vt.KeySlash, '/'}, ui.KeySemicolon: {vt.KeySemicolon, ';'},
	ui.KeyQuote: {vt.KeyQuote, '\''}, ui.KeyBracketLeft: {vt.KeyBracketLeft, '['}, ui.KeyBracketRight: {vt.KeyBracketRight, ']'},
	ui.KeyBackslash: {vt.KeyBackslash, '\\'}, ui.KeyBackquote: {vt.KeyBackquote, '`'}, ui.KeyContextMenu: {vt.KeyContextMenu, 0},
}

func init() {
	for i := range 12 {
		keys[ui.KeyF1+ui.Key(i)] = struct {
			key vt.Key
			r   rune
		}{vt.KeyF1 + vt.Key(i), 0}
	}
	for i := range 10 {
		keys[ui.Key0+ui.Key(i)] = struct {
			key vt.Key
			r   rune
		}{vt.KeyDigit0 + vt.Key(i), '0' + rune(i)}
	}
	for i := range 26 {
		keys[ui.KeyA+ui.Key(i)] = struct {
			key vt.Key
			r   rune
		}{vt.KeyA + vt.Key(i), 'a' + rune(i)}
	}
}

// vtKey returns Ghostty's key for k, and the character it types without
// modifiers (0 for keys that type none).
func vtKey(k ui.Key) (vt.Key, rune) {
	m := keys[k]
	return m.key, m.r
}

// typesText reports whether a key types text, rather than moving or
// erasing: the text comes after it.
func typesText(k ui.Key) bool {
	_, r := vtKey(k)
	return r > ' ' || k == ui.KeySpace
}

// keyOfRune returns the key that types r, and whether it takes Shift, on
// a US keyboard, for text that came without its key, as on Linux.
func keyOfRune(r rune) (vt.Key, rune, bool) {
	if r < 0x80 {
		lower := unicode.ToLower(r)
		for k, m := range keys {
			if m.r == lower && m.r > ' ' || k == ui.KeySpace && r == ' ' {
				return m.key, m.r, lower != r
			}
		}
		if i := shifted[r]; i != 0 {
			return keys[i].key, keys[i].r, true
		}
	}
	return vt.KeyUnidentified, 0, false
}

// shifted are the keys of the symbols Shift types on a US keyboard.
var shifted = map[rune]ui.Key{
	'!': ui.Key1, '@': ui.Key2, '#': ui.Key3, '$': ui.Key4, '%': ui.Key5, '^': ui.Key6, '&': ui.Key7, '*': ui.Key8,
	'(': ui.Key9, ')': ui.Key0, '_': ui.KeyMinus, '+': ui.KeyEqual, '<': ui.KeyComma, '>': ui.KeyPeriod,
	'?': ui.KeySlash, ':': ui.KeySemicolon, '"': ui.KeyQuote, '{': ui.KeyBracketLeft, '}': ui.KeyBracketRight,
	'|': ui.KeyBackslash, '~': ui.KeyBackquote,
}

func vtMods(m ui.Modifiers) vt.Mods { return vt.Mods(m) } // the same bits

var win32Keys = map[vt.Key]uint16{
	vt.KeyEnter: 0x0d, vt.KeyEscape: 0x1b, vt.KeyBackspace: 0x08,
	vt.KeyTab: 0x09, vt.KeySpace: 0x20, vt.KeyDelete: 0x2e, vt.KeyInsert: 0x2d,
	vt.KeyHome: 0x24, vt.KeyEnd: 0x23, vt.KeyPageUp: 0x21, vt.KeyPageDown: 0x22,
	vt.KeyArrowLeft: 0x25, vt.KeyArrowUp: 0x26, vt.KeyArrowRight: 0x27, vt.KeyArrowDown: 0x28,
	vt.KeyMinus: 0xbd, vt.KeyEqual: 0xbb, vt.KeyComma: 0xbc, vt.KeyPeriod: 0xbe,
	vt.KeySlash: 0xbf, vt.KeySemicolon: 0xba, vt.KeyQuote: 0xde,
	vt.KeyBracketLeft: 0xdb, vt.KeyBracketRight: 0xdd, vt.KeyBackslash: 0xdc,
	vt.KeyBackquote: 0xc0, vt.KeyContextMenu: 0x5d,
}

func encodeWin32Key(k vt.KeyEvent) []byte {
	vk := win32Keys[k.Key]
	switch {
	case k.Key >= vt.KeyA && k.Key <= vt.KeyZ:
		vk = uint16('A') + uint16(k.Key-vt.KeyA)
	case k.Key >= vt.KeyDigit0 && k.Key <= vt.KeyDigit9:
		vk = uint16('0') + uint16(k.Key-vt.KeyDigit0)
	case k.Key >= vt.KeyF1 && k.Key < vt.KeyF1+12:
		vk = 0x70 + uint16(k.Key-vt.KeyF1)
	}
	text := k.Text
	if text == "" {
		ch := k.Unshifted
		switch k.Key {
		case vt.KeyEscape:
			ch = 0x1b
		case vt.KeyBackspace:
			ch = '\b'
		}
		if k.Mods&(vt.ModCtrl|vt.ModAlt) == vt.ModCtrl|vt.ModAlt {
			ch = 0
		}
		if k.Mods&vt.ModShift != 0 && ch >= 'a' && ch <= 'z' {
			ch = unicode.ToUpper(ch)
		}
		text = string(ch)
	}
	if k.Mods&vt.ModCtrl != 0 && k.Mods&vt.ModAlt == 0 {
		switch {
		case k.Key >= vt.KeyA && k.Key <= vt.KeyZ:
			text = string(rune(k.Key-vt.KeyA) + 1)
		case k.Key == vt.KeySpace || k.Key == vt.KeyDigit2:
			text = "\x00"
		case k.Key == vt.KeyBracketLeft:
			text = "\x1b"
		case k.Key == vt.KeyBackslash:
			text = "\x1c"
		case k.Key == vt.KeyBracketRight:
			text = "\x1d"
		case k.Key == vt.KeyDigit6:
			text = "\x1e"
		case k.Key == vt.KeyMinus || k.Key == vt.KeySlash:
			text = "\x1f"
		case k.Key == vt.KeyEnter:
			text = "\n"
		case k.Key == vt.KeyBackspace:
			text = "\x7f"
		}
	}
	mods := 0
	if k.Mods&vt.ModShift != 0 {
		mods |= 0x10
	}
	if k.Mods&vt.ModCtrl != 0 {
		mods |= 0x08
	}
	if k.Mods&vt.ModAlt != 0 {
		mods |= 0x02
	}
	if vk >= 0x21 && vk <= 0x2e {
		mods |= 0x100
	}
	down := 1
	if k.Action == vt.KeyRelease {
		down = 0
	}
	var b []byte
	for _, ch := range utf16.Encode([]rune(text)) {
		b = fmt.Appendf(b, "\x1b[%d;0;%d;%d;%d;1_", vk, ch, down, mods)
	}
	return b
}
