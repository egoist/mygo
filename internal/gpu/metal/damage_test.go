//go:build darwin

package metal

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"testing"
	"unsafe"

	"github.com/egoist/mygo/internal/gpu/gputest"
	"github.com/egoist/mygo/internal/scene"
)

// TestDamageDrawsAsWhole draws a scene, then changes it a little at a
// time, each frame drawing only what damage found over the last frame kept,
// and checks every frame against the same scene drawn whole: a frame that
// draws part of the window must have the pixels of one that draws all of
// it.
func TestDamageDrawsAsWhole(t *testing.T) {
	testDamageDrawsAsWhole(t, gputest.Scene())
}

// TestDamageDrawsAsWholeTransparent does the same over a scene clearing to
// transparent, where erasing the damaged area matters: without it the
// pixels of the last frame would ghost through what this one does not draw.
func TestDamageDrawsAsWholeTransparent(t *testing.T) {
	s := gputest.Scene()
	s.Clear = scene.Color{}
	// Leave a bare area for what moves over to change visibly.
	s.Ops = append(s.Ops, scene.Op{Kind: scene.OpFill, Rect: scene.Rect{X: 8, Y: 8, W: 24, H: 24},
		Color: scene.Color{R: 200, G: 40, B: 40, A: 255}})
	testDamageDrawsAsWhole(t, s)
}

func testDamageDrawsAsWhole(t *testing.T, s *scene.Scene) {
	r, err := newRenderer()
	if err != nil {
		t.Skip("no Metal:", err)
	}
	defer r.Release()
	if r.cur.pipeline == 0 {
		if err := r.makePipelines(r.cur); err != nil {
			t.Fatal(err)
		}
	}
	var kept id
	pool(func() {
		kept = r.newTexture(s.Width, s.Height, pixelFormatBGRA8Unorm, usageRenderTarget|usageShaderRead, nil, 0)
	})
	if kept == 0 {
		t.Fatal("cannot create the target")
	}
	defer pool(func() { release(&kept) })

	var partial int
	for step := range 60 {
		what := "first"
		if step > 0 {
			what = mutate(s, step)
		}
		got, whole, err := r.renderKept(s, kept)
		if err != nil {
			t.Fatal(err)
		}
		want, err := r.renderOffscreen(s)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("step %d (%s): the frame drawn in parts differs from a whole one in %d pixels of %d",
				step, what, differing(got, want), len(got)/4)
		}
		if step > 0 && !whole {
			partial++
		}
	}
	if partial == 0 {
		t.Errorf("every frame was drawn whole")
	}
}

// renderKept draws s over the target kept, drawing only what damage found,
// and returns its premultiplied BGRA rows and whether it drew it whole.
func (r *Renderer) renderKept(s *scene.Scene, kept id) (pix []byte, whole bool, err error) {
	pool(func() {
		r.waitLast()
		r.cur = &r.formats[0]
		whole = r.damage.Whole(s)
		var cb id
		if cb, err = r.encode(s, kept, whole); err != nil {
			return
		}
		defer r.damage.Remember(s)
		blit := send(cb, "blitCommandEncoder")
		if !r.checked {
			if err = need(blit, "blit encoder", "synchronizeResource:", "endEncoding"); err != nil {
				return
			}
		}
		send(blit, "synchronizeResource:", kept)
		send(blit, "endEncoding")
		send(cb, "commit")
		send(cb, "waitUntilCompleted")
		if int(send(cb, "status")) == statusError {
			err = fmt.Errorf("metal: the frame failed: %s", describe(send(cb, "error")))
			return
		}
		pix = make([]byte, s.Width*s.Height*4)
		msgGetBytes(kept, sel("getBytes:bytesPerRow:fromRegion:mipmapLevel:"), unsafe.Pointer(&pix[0]), uint(s.Width*4),
			mtlRegion{W: uint(s.Width), H: uint(s.Height), D: 1}, 0)
	})
	return pix, whole, err
}

func differing(a, b []byte) (n int) {
	for i := range a {
		if a[i] != b[i] {
			n++
		}
	}
	return n
}

// mutate changes s a little, as a frame of an app would, and says how.
func mutate(s *scene.Scene, step int) string {
	rnd := rand.New(rand.NewPCG(uint64(step), 0x5eed))
	var drawing []int
	for i, op := range s.Ops {
		if op.Kind != scene.OpPushClip && op.Kind != scene.OpPopClip {
			drawing = append(drawing, i)
		}
	}
	color := func() scene.Color {
		return scene.Color{R: uint8(rnd.IntN(256)), G: uint8(rnd.IntN(256)), B: uint8(rnd.IntN(256)), A: uint8(64 + rnd.IntN(192))}
	}
	i := drawing[rnd.IntN(len(drawing))]
	switch rnd.IntN(11) {
	case 0:
		s.Ops[i].Color = color()
		return "color"
	case 1:
		s.Ops[i].Rect.X += float32(rnd.IntN(9)) - 4
		s.Ops[i].Rect.Y += float32(rnd.IntN(9)) - 4
		return "moved"
	case 2:
		s.Ops[i].Rect.W += float32(rnd.IntN(7)) - 3
		return "resized"
	case 3:
		s.Ops[i].Radii = [4]float32{float32(rnd.IntN(9)), float32(rnd.IntN(9)), float32(rnd.IntN(9)), float32(rnd.IntN(9))}
		return "radii"
	case 4:
		s.Ops[i].Border = scene.Uniform(float32(rnd.IntN(3)))
		s.Ops[i].BorderColor = color()
		return "border"
	case 5:
		op := scene.Op{Kind: scene.OpFill, Rect: scene.Rect{X: float32(rnd.IntN(200)), Y: float32(rnd.IntN(400)), W: 12, H: 9}, Color: color()}
		s.Ops = append(s.Ops[:i+1], append([]scene.Op{op}, s.Ops[i+1:]...)...)
		return "inserted"
	case 6:
		if len(drawing) > 4 {
			s.Ops = append(s.Ops[:i], s.Ops[i+1:]...)
			return "deleted"
		}
		return "nothing"
	case 7:
		if op := &s.Ops[i]; op.Kind == scene.OpGlyphs && op.Start < op.End {
			s.Glyphs[op.Start].X += float32(rnd.IntN(5)) - 2
			return "glyph moved"
		}
		if a := s.MaskAtlas; a != nil {
			a.Put(0, 0, 4, 4, pixels(rnd, 16), 4)
			return "atlas pixels"
		}
		return "nothing"
	case 9:
		s.Text.Contrast = 0.5 + float32(rnd.IntN(5))*0.25
		return "text parameters"
	case 10:
		if a := s.ColorAtlas; a != nil {
			for i := range s.Glyphs {
				g := &s.Glyphs[i]
				if g.Subpixel && g.UW > 0 && g.VH > 0 {
					w, h := int(g.UW), int(g.VH)
					a.Put(int(g.U), int(g.V), w, h, pixels(rnd, w*h*4), w*4)
					return "subpixel atlas pixels"
				}
			}
		}
		return "nothing"
	case 8:
		if s.Ops[i].Image != nil {
			copy(s.Ops[i].Image.Pix, pixels(rnd, len(s.Ops[i].Image.Pix)))
			s.Ops[i].Image.Changed()
			return "image pixels"
		}
		// A clip that changes changes everything it cuts.
		for j := range s.Ops {
			if s.Ops[j].Kind == scene.OpPushClip {
				s.Ops[j].Rect.X += float32(rnd.IntN(7)) - 3
				return "clip moved"
			}
		}
		return "nothing"
	}
	return "nothing"
}

func pixels(rnd *rand.Rand, n int) []byte {
	p := make([]byte, n)
	for i := range p {
		p[i] = byte(rnd.IntN(256))
	}
	return p
}

// TestFrameTarget checks which frames draw over the texture kept and which
// draw straight into the drawable, and that the frame after one drawn
// straight, as the drawable catching up with a resize leaves, draws whole
// rather than over a texture that frame did not update.
func TestFrameTarget(t *testing.T) {
	r, err := newRenderer()
	if err != nil {
		t.Skip("no Metal:", err)
	}
	defer r.Release()
	if err := r.makePipelines(r.cur); err != nil {
		t.Fatal(err)
	}
	s := gputest.Scene()
	drawable := func(w, h int) id {
		tex := r.newTexture(w, h, pixelFormatBGRA8Unorm, usageRenderTarget|usageShaderRead, nil, 0)
		if tex == 0 {
			t.Fatal("cannot create a drawable")
		}
		t.Cleanup(func() { pool(func() { release(&tex) }) })
		return tex
	}
	// The first frame has no last frame to draw over: whole, and copied.
	if !r.damage.Whole(s) {
		t.Fatal("the first frame did not draw whole")
	}
	kept, copy, made, err := r.frameTarget(drawable(s.Width, s.Height), s.Width, s.Height)
	if err != nil {
		t.Fatal(err)
	}
	if !copy || !made {
		t.Errorf("the first frame: copy %v, made %v, want both true", copy, made)
	}
	r.damage.Remember(s)
	// A frame that changed little draws into the texture kept, which holds
	// the last frame, and is copied.
	op := 0
	for i := range s.Ops {
		if s.Ops[i].Kind != scene.OpPushClip && s.Ops[i].Kind != scene.OpPopClip {
			op = i
			break
		}
	}
	s.Ops[op].Color = scene.Color{R: 1, G: 2, B: 3, A: 255}
	if r.damage.Whole(s) {
		t.Fatal("a small change drew whole")
	}
	target, copy, made, err := r.frameTarget(drawable(s.Width, s.Height), s.Width, s.Height)
	if err != nil {
		t.Fatal(err)
	}
	if target != kept || !copy || made {
		t.Errorf("the damaged frame: target %v (kept %v), copy %v, made %v, want the texture kept, copied, not made", target, kept, copy, made)
	}
	// A drawable of another size, as during a resize, draws straight and
	// whole.
	if _, copy, made, err := r.frameTarget(drawable(s.Width+40, s.Height), s.Width, s.Height); err != nil || copy || !made {
		t.Errorf("a drawable of another size: copy %v, made %v, err %v, want copy false, made true", copy, made, err)
	}
	// The frame after it draws whole too, rather than over the texture kept,
	// which that frame did not update.
	if !r.damage.Whole(s) {
		t.Error("the frame after one drawn straight did not draw whole")
	}
}
