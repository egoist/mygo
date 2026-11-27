package editor

import (
	"bytes"
	"testing"
	"time"
)

// What the app does to the selections between frames shows the caret for
// a whole blink, as a move by the keys does, however long the last frame
// is past.
func TestCodeEditorCaretShowsAfterTheAppMovesIt(t *testing.T) {
	for _, change := range []struct {
		name string
		do   func(st *State)
	}{
		{"set selections", func(st *State) { st.GoTo(Pos{Line: 1, Col: 2}) }},
		{"insert", func(st *State) { st.Insert("x") }},
	} {
		t.Run(change.name, func(t *testing.T) {
			for _, wait := range []time.Duration{blinkInterval - 10*time.Millisecond, blinkInterval * 3 / 2} {
				ct := newEditorTest(t, "", "|one\ntwo\nthree")
				tt := ct.tt
				tt.Advance(wait)
				change.do(ct.st)
				tt.Frame()
				shown := bytes.Clone(tt.Image().Pix)
				tt.Advance(blinkInterval - time.Millisecond)
				if !bytes.Equal(tt.Image().Pix, shown) {
					t.Fatalf("%v after the first frame: the caret blinks off before a whole blink", wait)
				}
				tt.Advance(time.Millisecond)
				if bytes.Equal(tt.Image().Pix, shown) {
					t.Fatalf("%v after the first frame: the caret does not blink off after a whole blink", wait)
				}
			}
		})
	}
}
