//go:build linux && !android && (amd64 || arm64)

package gl

import (
	"image"
	"slices"
	"testing"

	"github.com/egoist/mygo/internal/gpu/gputest"
)

// TestRedrawnHoldsWhatChanged draws scenes changing a little at a time
// into a framebuffer that keeps what was drawn, as a GtkGLArea's, whose
// window GTK then repaints only where Redrawn says the frame changed:
// every pixel that differs from the last frame must be in it, and a frame
// the same as the last one redraws nothing.
func TestRedrawnHoldsWhatChanged(t *testing.T) {
	const w, h = 320, 240
	for _, api := range apis {
		t.Run(api.name, func(t *testing.T) {
			read := offscreen(t, api.es, w, h)
			kinds := map[string]int{}
			for seed := range uint64(6) {
				m := gputest.NewChanging(seed)
				s := m.Scene()
				r, err := New()
				if err != nil {
					t.Fatal(err)
				}
				r.Partial = true
				var last []byte
				for step := range 30 {
					what := "first"
					switch {
					case step%7 == 6:
						what = "unchanged"
					case step > 0:
						what = m.Change(s)
					}
					if err := r.Render(s); err != nil {
						t.Fatal(err)
					}
					got := read()
					rects, whole := r.Redrawn()
					switch {
					case whole:
						kinds["whole"]++
					case len(rects) == 0:
						kinds["nothing"]++
					default:
						kinds["part"]++
					}
					if step == 0 && !whole {
						t.Fatalf("seed %d: the first frame redrew %v, not all of it", seed, rects)
					}
					if what == "unchanged" && (whole || len(rects) > 0) {
						t.Errorf("seed %d, step %d: a frame the same as the last redrew %v, whole %v", seed, step, rects, whole)
					}
					for i := 0; last != nil && !whole && i < len(got); i += 4 {
						if got[i] == last[i] && got[i+1] == last[i+1] && got[i+2] == last[i+2] && got[i+3] == last[i+3] {
							continue
						}
						p := image.Pt(i/4%w, i/4/w)
						if !slices.ContainsFunc(rects, p.In) {
							t.Fatalf("seed %d, step %d (%s): %v changed, outside what the frame redrew, %v", seed, step, what, p, rects)
						}
					}
					last = got
				}
				r.Release()
			}
			if kinds["whole"] == 0 || kinds["part"] == 0 || kinds["nothing"] == 0 {
				t.Errorf("frames redrawn %v: not a test of each", kinds)
			}
		})
	}
}
