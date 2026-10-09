package raster

import (
	"image"
	"runtime"

	"github.com/egoist/mygo/internal/damage"
	"github.com/egoist/mygo/internal/scene"
)

// Renderer draws the successive scenes of a window into Image, redrawing
// only where a scene differs from the one before (damage.Tracker): a
// blinking caret, a ticking clock or a button under the pointer redraws
// only itself.
type Renderer struct {
	// Image holds the last scene drawn, in mem.
	Image Image
	mem   *pixels

	// d draws the bands of large damage.
	d drawer
	// t finds what changed since the last scene drawn.
	t damage.Tracker
}

// Render draws s, and returns the rectangles of Image it changed. s may
// change once Render returns.
func (r *Renderer) Render(s *scene.Scene) []image.Rectangle {
	r.t.Whole(s)
	if r.Image.W != s.Width || r.Image.H != s.Height {
		r.resize(s.Width, s.Height)
	}
	for _, d := range r.t.Rects() {
		r.d.draw(&r.Image, s, d, r.t.Bounds())
	}
	r.t.Remember(s)
	return r.t.Rects()
}

// pixels is the memory of a Renderer's image (allocPixels).
type pixels struct {
	b       []byte
	mapped  bool
	cleanup runtime.Cleanup
}

// resize makes Image w×h, reusing its memory when it can.
func (r *Renderer) resize(w, h int) {
	n := 4 * w * h
	if r.mem == nil || len(r.mem.b) < n {
		r.mem.free()
		r.mem = allocPixels(n)
	}
	r.Image = Image{W: w, H: h, Stride: 4 * w, Pix: r.mem.b[:n]}
}

// Release frees the image and what Render remembers: the next Render
// draws everything. Image must not be used meanwhile.
func (r *Renderer) Release() {
	r.mem.free()
	*r = Renderer{}
}

// Invalidate makes the next Render draw everything.
func (r *Renderer) Invalidate() { r.t.Invalidate() }
