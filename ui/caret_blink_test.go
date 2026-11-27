package ui_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// A caret the app moves, or a text it replaces, in a text input with the
// keyboard shows for a whole blink, as a move by the keys does; a caret
// the app keeps where it is goes on blinking.
func TestCaretShowsAfterTheAppMovesIt(t *testing.T) {
	const blink = 530 * time.Millisecond
	for _, change := range []struct {
		name string
		do   func(e ui.Element)
	}{
		{"set text selection", func(e ui.Element) { e.SetTextSelection(2, 2) }},
		{"replace text", func(e ui.Element) { e.ReplaceText(0, 1, "x") }},
	} {
		t.Run(change.name, func(t *testing.T) {
			for _, area := range []bool{false, true} {
				for _, wait := range []time.Duration{blink - 10*time.Millisecond, blink * 3 / 2} {
					value, apply := "hello", false
					tt := ui.NewTester(func(c *ui.Context) {
						var e ui.Element
						if area {
							e = ui.TextAreaBase(c, &value)
						} else {
							e = ui.TextInputBase(c, &value)
						}
						e.AutoFocus().Size(200, 40).Label("input")
						if apply {
							change.do(e)
							apply = false
						}
					}, 300, 100)
					tt.Frame()
					tt.Advance(wait)
					apply = true
					tt.Frame()
					shown := bytes.Clone(tt.Image().Pix)
					tt.Advance(blink - time.Millisecond)
					if !bytes.Equal(tt.Image().Pix, shown) {
						t.Fatalf("area %v, %v after the first frame: the caret blinks off before a whole blink", area, wait)
					}
					tt.Advance(time.Millisecond)
					if bytes.Equal(tt.Image().Pix, shown) {
						t.Fatalf("area %v, %v after the first frame: the caret does not blink off after a whole blink", area, wait)
					}
				}
			}
		})
	}
	// Set again in every frame, where it is, the caret still blinks.
	value := "hello"
	tt := ui.NewTester(func(c *ui.Context) {
		ui.TextInputBase(c, &value).AutoFocus().Size(200, 40).Label("input").SetTextSelection(2, 2)
	}, 300, 100)
	tt.Frame()
	shown := bytes.Clone(tt.Image().Pix)
	tt.Advance(blink)
	if bytes.Equal(tt.Image().Pix, shown) {
		t.Fatal("a caret the app keeps where it is does not blink off")
	}
}
