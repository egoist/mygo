package loading

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// hz is the refresh rate of the display the tests stand for, a ProMotion
// one's.
const hz = 120

// many shows every indicator copies times at once, with o, on a display of
// hz, beside a drawing that counts the frames painted in *paints: it asks
// to be painted again at 1 a second, which leaves the window's rate to the
// indicators'. *builds counts the view's builds.
func many(copies int, o *Options, builds, paints *int) *ui.Tester {
	tt := ui.NewTester(func(c *ui.Context) {
		*builds++
		ui.Column(c).Fill().Gap(2).Children(func() {
			ui.Box(c).Size(4, 4).Draw(func(p *ui.Painter, r ui.Rect) {
				*paints++
				p.AnimationFrameRate(1)
			})
			for _, s := range All() {
				ui.Row(c).Gap(2).Children(func() {
					for range copies {
						s.New(c, *o)
					}
				})
			}
		})
	}, 480, 640)
	tt.SetRefreshRate(hz)
	tt.SetFocused(true)
	return tt
}

// However many indicators move at once, the window paints at the rate
// they ask for, DefaultFPS unless set, not at each of the display's
// refreshes, and never builds the view again for them.
func TestManyAtTheirRate(t *testing.T) {
	for _, c := range []struct {
		fps  float32
		want int
	}{
		{0, DefaultFPS},
		{20, 20},
		{-1, hz}, // the display's
	} {
		o := Options{Size: 12, FPS: c.fps}
		var builds, paints int
		tt := many(4, &o, &builds, &paints)
		tt.Frame()
		builds, paints = 0, 0
		tt.Advance(time.Second)
		if paints < c.want-1 || paints > c.want+1 {
			t.Errorf("FPS %v: %d indicators painted %d frames in a second of a %d Hz display, want %d", c.fps, 4*len(All()), paints, hz, c.want)
		}
		if builds != 0 {
			t.Errorf("FPS %v: built the view %d times in a second, want 0", c.fps, builds)
		}
	}
}

// In a window without the keyboard focus, behind another, indicators move
// at half their rate.
func TestManyInTheBackground(t *testing.T) {
	o := Options{Size: 12}
	var builds, paints int
	tt := many(4, &o, &builds, &paints)
	tt.Frame()
	tt.SetFocused(false)
	tt.Advance(time.Second)
	paints = 0
	tt.Advance(time.Second)
	if want := DefaultFPS / 2; paints < want-1 || paints > want+1 {
		t.Errorf("in the background: painted %d frames in a second, want %d", paints, want)
	}
	tt.SetFocused(true)
	tt.Advance(time.Second)
	paints = 0
	tt.Advance(time.Second)
	if paints < DefaultFPS-1 || paints > DefaultFPS+1 {
		t.Errorf("focused again: painted %d frames in a second, want %d", paints, DefaultFPS)
	}
}

// A window that does not show paints none of its indicators.
func TestManyHidden(t *testing.T) {
	o := Options{Size: 12}
	var builds, paints int
	tt := many(4, &o, &builds, &paints)
	tt.Frame()
	tt.SetWindowHidden(true)
	paints = 0
	tt.Advance(time.Second)
	if paints != 0 {
		t.Errorf("hidden: painted %d frames in a second, want 0", paints)
	}
}

// BenchmarkManyForASecond is a second of 116 indicators moving at once on
// a 120 Hz display, at DefaultFPS and at the display's rate.
func BenchmarkManyForASecond(b *testing.B) {
	for _, bc := range []struct {
		name string
		fps  float32
	}{{"default", 0}, {"display", -1}} {
		b.Run(bc.name, func(b *testing.B) {
			o := Options{Size: 12, FPS: bc.fps}
			var builds, paints int
			tt := many(4, &o, &builds, &paints)
			tt.Frame()
			paints = 0
			b.ReportAllocs()
			for b.Loop() {
				tt.Advance(time.Second)
			}
			b.ReportMetric(float64(paints)/float64(b.N), "frames/op")
		})
	}
}
