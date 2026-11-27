package scene

import (
	"image"
	"testing"
)

// TestSceneReset checks that a scene emptied for another frame keeps its
// storage and takes the new frame.
func TestSceneReset(t *testing.T) {
	img := NewImageRGBA(2, 2, make([]byte, 16))
	s := &Scene{
		Width: 10, Height: 10, Clear: Color{R: 255}, Drawn: image.Point{5, 5},
		Ops:      []Op{{Kind: OpFill}},
		Glyphs:   []Glyph{{}},
		Stops:    []Stop{{}},
		Effects:  []EffectOp{{}},
		Wide:     []WideColors{{}},
		Words:    []uint32{1},
		Canvases: []*Image{img},
	}
	s.Reset(20, 30, Color{B: 255})
	if s.Width != 20 || s.Height != 30 || s.Clear != (Color{B: 255}) || s.Drawn != (image.Point{}) {
		t.Errorf("Reset kept the old frame: %+v", s)
	}
	if len(s.Ops) != 0 || len(s.Glyphs) != 0 || len(s.Stops) != 0 || len(s.Effects) != 0 ||
		len(s.Wide) != 0 || len(s.Words) != 0 || len(s.Canvases) != 0 {
		t.Errorf("Reset kept the old frame's drawing: %+v", s)
	}
}

// TestNewImages checks how images are made, told apart, and how their
// pixels come and go.
func TestNewImages(t *testing.T) {
	// A canvas draws from a scene of its own.
	c := NewCanvas(8, 6)
	if c.W != 8 || c.H != 6 || c.Version() != 1 || c.ID() == 0 ||
		c.Scene == nil || c.Scene.Width != 8 || c.Scene.Height != 6 {
		t.Errorf("NewCanvas = %+v", c)
	}

	// An RGBA picture with tight rows keeps its pixels.
	rgba := image.NewRGBA(image.Rect(0, 0, 4, 3))
	m := NewImage(rgba)
	if m.W != 4 || m.H != 3 || &m.Pix[0] != &rgba.Pix[0] || m.Version() != 1 {
		t.Errorf("NewImage of RGBA = %+v", m)
	}
	// With its rows padded, or another kind of picture, they are made over.
	padded := &image.RGBA{Pix: make([]byte, 8*4), Stride: 8, Rect: image.Rect(0, 0, 5, 2)}
	m = NewImage(padded)
	if m.W != 5 || m.H != 2 || len(m.Pix) != 5*4*2 || &m.Pix[0] == &padded.Pix[0] {
		t.Errorf("NewImage of padded RGBA = %+v", m)
	}
	m = NewImage(image.Rect(0, 0, 3, 2))
	if m.W != 3 || m.H != 2 || len(m.Pix) != 3*4*2 {
		t.Errorf("NewImage of a rectangle = %+v", m)
	}
	// Two images have two identities, for the texture caches.
	if m.ID() == c.ID() {
		t.Error("two images with one identity")
	}

	// Pixels come back when an owner dropped them, once; Uploaded runs the
	// owner's drop again.
	m = NewImageRGBA(2, 2, nil)
	restores, drops := 0, 0
	m.Restore = func() []byte { restores++; return make([]byte, 16) }
	m.Dropped = func() { drops++ }
	if pix := m.Pixels(); len(pix) != 16 || restores != 1 {
		t.Errorf("Pixels of a dropped image = %d bytes, restored %d times", len(pix), restores)
	}
	m.Pixels()
	if restores != 1 {
		t.Errorf("Pixels restored %d times for the same pixels", restores)
	}
	m.Uploaded()
	if drops != 1 {
		t.Error("Uploaded did not run the owner's drop")
	}
	// An image with no pixels and no restore has none to give.
	if pix := c.Pixels(); pix != nil {
		t.Errorf("Pixels of a canvas = %v", pix)
	}
}

// TestSceneEachCanvas checks the canvases a scene draws anew or shows,
// each once, those a canvas shows before it.
func TestSceneEachCanvas(t *testing.T) {
	a, b, loose := NewCanvas(4, 4), NewCanvas(4, 4), NewImageRGBA(2, 2, make([]byte, 16))
	s := &Scene{Width: 10, Height: 10, Canvases: []*Image{a}, Ops: []Op{
		{Kind: OpImage, Image: b, Src: Rect{W: 4, H: 4}},
		{Kind: OpImage, Image: loose, Src: Rect{W: 2, H: 2}},
		{Kind: OpImage, Image: a, Src: Rect{W: 4, H: 4}},
	}}
	var got []*Image
	s.EachCanvas(func(c *Image) { got = append(got, c) })
	// The canvas of the scene, then the one its ops show; the image with
	// no scene of its own is no canvas, and the canvas shown twice comes
	// once.
	if len(got) != 2 || got[0] != a || got[1] != b {
		t.Errorf("EachCanvas = %v, want %v then %v", got, a, b)
	}

	// A canvas showing another canvas is drawn after it.
	inner := NewCanvas(2, 2)
	inner.Scene.Ops = []Op{{Kind: OpImage, Image: a, Src: Rect{W: 4, H: 4}}}
	s = &Scene{Width: 10, Height: 10, Canvases: []*Image{b, inner}}
	got = nil
	s.EachCanvas(func(c *Image) { got = append(got, c) })
	if len(got) != 3 || got[0] != b || got[1] != a || got[2] != inner {
		t.Errorf("EachCanvas of nested canvases = %v, want %v, %v, %v", got, b, a, inner)
	}
}

// TestSceneBackdropOf checks that a scene's backdrops end at its frame,
// and at the edge of the part of it that is drawn.
func TestSceneBackdropOf(t *testing.T) {
	s := &Scene{Width: 100, Height: 100}
	if b := s.BackdropOf(Rect{X: 10, Y: 10, W: 60, H: 60}, 8, 100, 100); b.Area != (image.Rect(0, 0, 98, 98)) {
		t.Errorf("BackdropOf = %v, want (0, 0, 98, 98)", b.Area)
	}
	s.Drawn = image.Point{50, 60}
	if b := s.BackdropOf(Rect{X: 10, Y: 10, W: 60, H: 60}, 8, 100, 100); b.Area != (image.Rect(0, 0, 50, 60)) {
		t.Errorf("BackdropOf of a partly drawn frame = %v, want (0, 0, 50, 60)", b.Area)
	}
}
