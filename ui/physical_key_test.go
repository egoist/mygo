package ui

import (
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

func TestPhysicalKeyMatchesShortcutOnOtherLayout(t *testing.T) {
	hits := 0
	tt := coreNewTester(func(c *context) {
		if c.Shortcut(Super, KeyS) {
			hits++
		}
	}, 200, 100)
	// Cmd+S on a Cyrillic layout: the layout types no US key.
	tt.KeyAt(Super, KeyS)
	if hits != 1 {
		t.Fatalf("physical Super+S matched %d times, want 1", hits)
	}
	// Without a command modifier the physical key is ignored.
	tt.KeyAt(0, KeyS)
	tt.KeyAt(Shift, KeyS)
	if hits != 1 {
		t.Fatalf("unmodified physical S matched a shortcut: %d", hits)
	}
}

func TestPhysicalKeyKeepsCyrillicTyping(t *testing.T) {
	text := "x"
	tt := coreNewTester(func(c *context) {
		coreTextInput(c, &text)
	}, 300, 100)
	tt.Key(0, KeyTab)
	tt.Key(0, KeyEnd)
	// A Cyrillic "ы" on the S key: KeyUnknown, physical S, text delivered.
	tt.send(platformKeyEvent(KeyUnknown, KeyS))
	tt.Type("ы")
	if text != "xы" {
		t.Fatalf("text = %q, want %q", text, "xы")
	}
}

func platformKeyEvent(key, physical Key) platform.SurfaceEvent {
	return platform.SurfaceEvent{Kind: platform.KeyPressed, Key: platform.Key(key), PhysicalKey: platform.Key(physical)}
}
