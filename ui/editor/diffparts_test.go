package editor

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// A wheel over a separator scrolls the diff, as a wheel over its lines
// does: the separator lies over them, and its input, which keeps the
// lines' own from coming through, lets the wheel through to them.
func TestDiffWheelOverSeparator(t *testing.T) {
	// A patch of two hunks, the second one tall, so the diff stands
	// taller than the view and scrolls.
	var added strings.Builder
	for i := range 40 {
		added.WriteString("+added line " + strings.Repeat("x", i%7) + "\n")
	}
	patch := "--- a/x\n+++ b/x\n@@ -1,5 +1,2 @@\n c1\n-X\n-Y\n-Z\n c2\n@@ -40,2 +100,42 @@\n c3\n" + added.String() + " c4\n"
	files, err := ParsePatch(patch)
	if err != nil {
		t.Fatal(err)
	}
	d := &DiffState{}
	d.SetPatch(files[0])
	tt := ui.NewTester(func(c *ui.Context) { DiffView(c, d).Fill() }, 600, 300)
	tt.Frame()
	ed := &d.eds[1]
	if ed.scrollMax() <= 0 {
		t.Fatal("the diff does not stand taller than its view")
	}
	// A separator in view: the header of the second hunk, over its
	// lines.
	var top float64 = -1
	ed.blocksInView(func(b *block, at float64) {
		if b.kind == blockHeader && top < 0 && at > 40 {
			top = at
		}
	})
	if top < 0 {
		t.Fatal("no separator in view")
	}
	before := ed.scrollY
	tt.Scroll(300, float32(top-ed.scrollY)+8, 0, 120)
	tt.Frame()
	if ed.scrollY <= before {
		t.Fatalf("a wheel over the separator did not scroll: %v stays %v", before, ed.scrollY)
	}
}
