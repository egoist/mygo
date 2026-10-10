//go:build darwin

package darwin

import (
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

func TestKeyAtCodeANSI(t *testing.T) {
	// Carbon Events.h kVK_ANSI_* values.
	want := map[uint16]platform.Key{
		0x00: platform.KeyA, 0x01: platform.KeyS, 0x06: platform.KeyZ, 0x09: platform.KeyV, 0x0B: platform.KeyB,
		0x10: platform.KeyY, 0x2E: platform.KeyM, 0x2D: platform.KeyN, 0x1F: platform.KeyO, 0x23: platform.KeyP,
		0x12: platform.Key1, 0x13: platform.Key2, 0x14: platform.Key3, 0x15: platform.Key4, 0x17: platform.Key5,
		0x16: platform.Key6, 0x1A: platform.Key7, 0x1C: platform.Key8, 0x19: platform.Key9, 0x1D: platform.Key0,
		0x18: platform.KeyEqual, 0x1B: platform.KeyMinus, 0x1E: platform.KeyBracketRight, 0x21: platform.KeyBracketLeft,
		0x27: platform.KeyQuote, 0x29: platform.KeySemicolon, 0x2A: platform.KeyBackslash, 0x2B: platform.KeyComma,
		0x2C: platform.KeySlash, 0x2F: platform.KeyPeriod, 0x32: platform.KeyBackquote,
		// Non-text keys come from macKeys.
		0x24: platform.KeyEnter, 0x31: platform.KeySpace, 0x7B: platform.KeyLeft,
	}
	for code, k := range want {
		if got := keyAtCode(code); got != k {
			t.Errorf("code %#x: got %v, want %v", code, got, k)
		}
	}
	if keyAtCode(0x0A) != platform.KeyUnknown || keyAtCode(0x7F) != platform.KeyUnknown {
		t.Error("unmapped codes must be KeyUnknown")
	}
	// Letters cover all 26 keys exactly once.
	seen := map[platform.Key]bool{}
	for _, k := range ansiKeys {
		if seen[k] {
			t.Errorf("key %v mapped twice", k)
		}
		seen[k] = true
	}
	if len(ansiKeys) != 26+10+11 {
		t.Errorf("ansiKeys has %d entries, want 47", len(ansiKeys))
	}
}
