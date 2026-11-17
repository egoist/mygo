package main

import (
	"image"
	"testing"

	"github.com/egoist/mygo/ui"
)

// reference is the screen as it was written by hand, before counter.view
// took it: the generated View must behave the same.
func (s *counter) reference(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Center().Gap(16).Children(func() {
		ui.Textf(c, "%d", s.n).FontSize(56).Bold()
		ui.Row(c).Gap(8).Children(func() {
			if ui.Button(c.Key("decrement"), "−").Label("Decrement").Width(44).Clicked() {
				s.n--
			}
			if ui.Button(c.Key("reset"), "Reset").Disabled(s.n == 0).Clicked() {
				s.n = 0
			}
			if ui.PrimaryButton(c.Key("increment"), "+").Label("Increment").Width(44).Clicked() {
				s.n++
			}
		})
		ui.Text(c, "↑ and ↓ count too").FontSize(12).TextColor(t.TextMuted)
	})
}

// run drives a view of one counter through a user's steps and reports
// what showed.
func run(view func(*ui.Context)) (steps []string, pixels []image.Image) {
	s := &counter{}
	tt := ui.NewTester(func(c *ui.Context) {
		if c.Shortcut(0, ui.KeyUp) {
			s.n++
		}
		if c.Shortcut(0, ui.KeyDown) {
			s.n--
		}
		view(c)
	}, 360, 260)
	snapshot := func(step string) {
		tt.Frame()
		steps = append(steps, step+":"+texts(tt))
		pixels = append(pixels, tt.Image())
	}
	snapshot("start")
	for _, up := range []int{0, 2} { // click +, then − twice, Reset at 0
		_ = up
		break
	}
	tt.Click("+")
	snapshot("up")
	tt.Click("−")
	tt.Click("−")
	snapshot("down")
	// Reset is disabled at 0: the click does nothing.
	tt.Click("Reset")
	snapshot("reset-at-0")
	tt.Click("+")
	tt.Click("Reset")
	snapshot("reset-at-1")
	tt.Key(0, ui.KeyUp)
	tt.Key(0, ui.KeyUp)
	tt.Key(0, ui.KeyDown)
	snapshot("arrows")
	return steps, pixels
}

// texts is what the window shows, top to bottom.
func texts(tt *ui.Tester) string {
	out := ""
	for _, s := range tt.Texts() {
		out += s + "|"
	}
	return out
}

// TestGeneratedViewMatchesTheHandwrittenOne checks the .view's View
// against the screen as it was: what shows, what the clicks and keys do,
// and the pixels themselves.
func TestGeneratedViewMatchesTheHandwrittenOne(t *testing.T) {
	wantSteps, wantPixels := run((&counter{}).reference)
	gotSteps, gotPixels := run((&counter{}).View)
	for i := range wantSteps {
		if gotSteps[i] != wantSteps[i] {
			t.Fatalf("step %d: generated %q, handwritten %q", i, gotSteps[i], wantSteps[i])
		}
		if !samePixels(gotPixels[i], wantPixels[i]) {
			t.Fatalf("step %d (%s): pixels differ", i, gotSteps[i])
		}
	}
}

// samePixels compares two frames, allowing none of a difference: the
// generated screen draws the same as the handwritten one.
func samePixels(a, b image.Image) bool {
	ab, bb := a.Bounds(), b.Bounds()
	if ab != bb {
		return false
	}
	for y := bb.Min.Y; y < bb.Max.Y; y++ {
		for x := bb.Min.X; x < bb.Max.X; x++ {
			if a.At(x, y) != b.At(x, y) {
				return false
			}
		}
	}
	return true
}
