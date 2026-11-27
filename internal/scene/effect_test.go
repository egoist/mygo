package scene

import (
	"bytes"
	"image"
	"math"
	"testing"
)

// TestShaderLayoutPack checks what a shader's uniform words become: each
// lane's bytes little end first, the buffer its size, and the spill of a
// bool in the last bytes.
func TestShaderLayoutPack(t *testing.T) {
	l := &ShaderLayout{Lanes: []int32{0, 4, 8}, Size: 16}
	buf := l.Pack(nil, []uint32{0x11223344, 0xAABBCCDD, 0xFFFFFFFF})
	want := []byte{
		0x44, 0x33, 0x22, 0x11,
		0xDD, 0xCC, 0xBB, 0xAA,
		0xFF, 0xFF, 0xFF, 0xFF,
		0, 0, 0, 0,
	}
	if !bytes.Equal(buf, want) {
		t.Errorf("Pack = %v, want %v", buf, want)
	}

	// A lane near the end writes what fits: two of the word's four bytes.
	l = &ShaderLayout{Lanes: []int32{0, 14}, Size: 16}
	buf = l.Pack(nil, []uint32{0x11223344, 0xAABBCCDD})
	want = []byte{0x44, 0x33, 0x22, 0x11, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xDD, 0xCC}
	if !bytes.Equal(buf, want) {
		t.Errorf("Pack with a spill = %v, want %v", buf, want)
	}

	// Fewer words than lanes write no more.
	buf = l.Pack(make([]byte, 16), []uint32{0x11223344})
	want = []byte{0x44, 0x33, 0x22, 0x11, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	if !bytes.Equal(buf, want) {
		t.Errorf("Pack of fewer words = %v, want %v", buf, want)
	}
}

// TestEffectOpInstances checks how many quads an effect paints, the
// rectangle and size each holds, and the words after them.
func TestEffectOpInstances(t *testing.T) {
	// No words or fewer than the head: no quads.
	if n := (&EffectOp{}).InstanceCount(); n != 0 {
		t.Errorf("InstanceCount of nothing = %d", n)
	}
	if n := (&EffectOp{InstanceWords: InstanceHead - 1, Instances: make([]uint32, 100)}).InstanceCount(); n != 0 {
		t.Errorf("InstanceCount under the head = %d", n)
	}

	w := func(v float32) uint32 { return math.Float32bits(v) }
	op := &EffectOp{InstanceWords: 8, Instances: []uint32{
		w(1), w(2), w(3), w(4), w(5), w(6), 100, 200,
		w(7), w(8), w(9), w(10), w(11), w(12), 300, 301,
	}}
	if n := op.InstanceCount(); n != 2 {
		t.Errorf("InstanceCount = %d, want 2", n)
	}
	r, size := op.InstanceRect(0)
	if r != (Rect{X: 1, Y: 2, W: 3, H: 4}) || size != [2]float32{5, 6} {
		t.Errorf("InstanceRect(0) = %v, %v", r, size)
	}
	if d := op.InstanceData(0); len(d) != 2 || d[0] != 100 || d[1] != 200 {
		t.Errorf("InstanceData(0) = %v", d)
	}
	r, size = op.InstanceRect(1)
	if r != (Rect{X: 7, Y: 8, W: 9, H: 10}) || size != [2]float32{11, 12} {
		t.Errorf("InstanceRect(1) = %v, %v", r, size)
	}
	if d := op.InstanceData(1); len(d) != 2 || d[0] != 300 || d[1] != 301 {
		t.Errorf("InstanceData(1) = %v", d)
	}

	// Glyphs paint instead of the rectangle when there are more to paint.
	if (&EffectOp{Glyphs: [2]int32{5, 2}}).Masked() {
		t.Error("an empty range of glyphs is no mask")
	}
	if !(&EffectOp{Glyphs: [2]int32{2, 5}}).Masked() {
		t.Error("glyphs to paint are a mask")
	}
}

// TestBackdropOf checks what a blur reads: how far down it is computed,
// the sigma and radius of the Gaussian, and the area it reaches.
func TestBackdropOf(t *testing.T) {
	// No blur reads the shape and a pixel around it.
	b := BackdropOf(Rect{X: 10, Y: 10, W: 10, H: 10}, 0, 100, 100)
	if b.Down != 1 || b.Sigma != 0 || b.Radius != 0 || b.Area != image.Rect(9, 9, 21, 21) {
		t.Errorf("backdrop of no blur = %+v", b)
	}

	// A blur of 1 is computed whole, at a radius of three texels.
	b = BackdropOf(Rect{X: 10, Y: 10, W: 10, H: 10}, 1, 100, 100)
	if b.Down != 1 || b.Area != image.Rect(6, 6, 24, 24) {
		t.Errorf("backdrop of blur 1 = %+v", b)
	}
	if b.Sigma < 0.95 || b.Sigma > 0.97 || b.Radius != 3 {
		t.Errorf("blur 1: sigma %v, radius %d", b.Sigma, b.Radius)
	}

	// A blur of 8 is computed a quarter of the size, which takes its
	// variance out of the sigma.
	b = BackdropOf(Rect{X: 10, Y: 10, W: 10, H: 10}, 8, 100, 100)
	if b.Down != 4 || b.Radius != 6 || b.Area != image.Rect(0, 0, 48, 48) {
		t.Errorf("backdrop of blur 8 = %+v", b)
	}
	if b.Sigma < 1.97 || b.Sigma > 1.99 {
		t.Errorf("blur 8: sigma %v, want about 1.979", b.Sigma)
	}

	// A huge blur of a small frame reads the whole frame.
	b = BackdropOf(Rect{X: 10, Y: 10, W: 10, H: 10}, 1000, 30, 30)
	if b.Down != 8 || b.Area != image.Rect(0, 0, 30, 30) {
		t.Errorf("backdrop of a huge blur = %+v", b)
	}
}

// TestImageBlurSrcAreaSize checks the backdrop an image's blur reads: the
// blur's numbers with the image's area, where a source lies in the
// blurred texels, the pixels it touches, and the size of the average.
func TestImageBlurSrcAreaSize(t *testing.T) {
	b := ImageBlur(image.Rect(0, 0, 10, 10), 8)
	if b.Down != 4 || b.Radius != 6 || b.Area != image.Rect(0, 0, 10, 10) {
		t.Errorf("ImageBlur = %+v", b)
	}
	if b.Sigma < 1.97 || b.Sigma > 1.99 {
		t.Errorf("ImageBlur sigma = %v, want about 1.979", b.Sigma)
	}

	// A source of the area, a quarter of the way in: the blurred texels
	// are a quarter of the pixels.
	b = Backdrop{Area: image.Rect(8, 8, 40, 40), Down: 4}
	if s := b.ImageSrc(Rect{X: 12, Y: 16, W: 20, H: 12}); s != (Rect{X: 1, Y: 2, W: 5, H: 3}) {
		t.Errorf("ImageSrc = %v", s)
	}

	// A flipped source is the same pixels, read the other way.
	if a := ImageArea(Rect{X: 10, Y: 10, W: -6, H: -4}, 20, 20); a != image.Rect(4, 6, 10, 10) {
		t.Errorf("ImageArea of a flipped source = %v", a)
	}
	// One reaching out of the picture is cut to it.
	if a := ImageArea(Rect{X: -5, Y: -5, W: 20, H: 20}, 10, 10); a != image.Rect(0, 0, 10, 10) {
		t.Errorf("ImageArea past the picture = %v", a)
	}

	// The average rounds up: ten pixels at a quarter are three texels.
	if w, h := (Backdrop{Area: image.Rect(0, 0, 10, 12), Down: 4}).Size(); w != 3 || h != 3 {
		t.Errorf("Size = %d×%d", w, h)
	}
	if w, h := (Backdrop{Area: image.Rect(0, 0, 10, 12), Down: 1}).Size(); w != 10 || h != 12 {
		t.Errorf("Size without a downsample = %d×%d", w, h)
	}
}

// TestBlurWeight checks the Gaussian's weights: one in the middle, and
// exp(−i²/2σ²) away, one of sigma zero everywhere.
func TestBlurWeight(t *testing.T) {
	for _, i := range []int{0, 1, 5} {
		if w := BlurWeight(i, 0); w != 1 {
			t.Errorf("BlurWeight(%d, 0) = %v, want 1", i, w)
		}
	}
	if w := BlurWeight(0, 2); w != 1 {
		t.Errorf("BlurWeight(0, 2) = %v, want 1", w)
	}
	for _, tc := range []struct {
		i     int
		sigma float32
		want  float64
	}{
		{1, 2, 0.8824969026},
		{2, 2, 0.6065306597},
		{3, 1, 0.0111089965},
	} {
		if w := float64(BlurWeight(tc.i, tc.sigma)); math.Abs(w-tc.want) > 1e-6 {
			t.Errorf("BlurWeight(%d, %v) = %v, want %v", tc.i, tc.sigma, w, tc.want)
		}
	}
}

// TestSDRoundRect checks the signed distance to a rounded rectangle's
// edge: negative inside, positive out, each corner its own radius.
func TestSDRoundRect(t *testing.T) {
	r := Rect{X: 0, Y: 0, W: 10, H: 10}
	if d := SDRoundRect(r, [4]float32{}, 5, 5); d != -5 {
		t.Errorf("the center of a square = %v, want -5", d)
	}
	if d := SDRoundRect(r, [4]float32{}, 15, 5); d != 5 {
		t.Errorf("five pixels past the square = %v, want 5", d)
	}
	if d := SDRoundRect(r, [4]float32{}, 0, 0); d != 0 {
		t.Errorf("the corner of a square = %v, want 0", d)
	}
	if d := SDRoundRect(r, [4]float32{}, 15, 15); d < 7.06 || d > 7.08 {
		t.Errorf("five pixels past the diagonal corner of a square = %v, want about 7.07", d)
	}

	// Each corner takes its own radius, and the distance is measured
	// from the center of the corner's arc.
	radii := [4]float32{1, 2, 3, 4}
	for _, tc := range []struct {
		x, y float32
		want float64
	}{
		{0, 0, math.Sqrt2 - 1},
		{10, 0, 2*math.Sqrt2 - 2},
		{10, 10, 3*math.Sqrt2 - 3},
		{0, 10, 4*math.Sqrt2 - 4},
	} {
		if d := float64(SDRoundRect(r, radii, tc.x, tc.y)); math.Abs(d-tc.want) > 1e-4 {
			t.Errorf("SDRoundRect at (%v, %v) = %v, want %v", tc.x, tc.y, d, tc.want)
		}
	}
	// A point inside the rounded corner.
	if d := SDRoundRect(r, [4]float32{2, 2, 2, 2}, 1, 1); d > -0.58 || d < -0.60 {
		t.Errorf("inside the rounded corner = %v, want about -0.586", d)
	}
}

// TestBackdropImageSample checks the bilinear sample of a backdrop: the
// texels around the point, averaged, clamped to its edges, and moved as
// its transform says.
func TestBackdropImageSample(t *testing.T) {
	b := &BackdropImage{
		Backdrop: Backdrop{Area: image.Rect(0, 0, 4, 4), Down: 1},
		W:        4, H: 4,
		Pix: make([]float32, 4*4*4),
	}
	set := func(x, y int, r, g, bl float32) {
		p := b.Pix[4*(y*b.W+x):]
		p[0], p[1], p[2], p[3] = r, g, bl, 1
	}
	set(1, 1, 0.25, 0.5, 0.75)
	set(0, 1, 1, 0, 0)
	set(0, 0, 1, 1, 1)
	set(3, 3, 0, 0, 1)

	// The center of a texel is that texel.
	if c := b.Sample(1.5, 1.5); c != [3]float32{0.25, 0.5, 0.75} {
		t.Errorf("Sample at a texel's center = %v", c)
	}
	// Between the four texels around it is their bilinear mean.
	if c := b.Sample(1.0, 1.0); c[0] < 0.55 || c[0] > 0.58 || c[1] < 0.36 || c[1] > 0.39 || c[2] < 0.42 || c[2] > 0.45 {
		t.Errorf("Sample between texels = %v, want the bilinear mean", c)
	}
	// Past the edges is the edge's texel.
	if c := b.Sample(-10, -10); c != [3]float32{1, 1, 1} {
		t.Errorf("Sample past the edge = %v, want the corner texel", c)
	}
	// Its transform moves the point before the sampling.
	b.Transform = Translation(10, 10)
	if c := b.Sample(11.5, 11.5); c != [3]float32{0, 0, 1} {
		t.Errorf("Sample with a transform = %v, want the far texel", c)
	}
}

// TestEffectOpSame checks what makes two effect operations the same:
// the effect, its parameters, its uniforms, its quads and no images.
func TestEffectOpSame(t *testing.T) {
	fx := &Effect{Name: "glass"}
	a := &EffectOp{Effect: fx, Blur: 2, Params: [5][4]float32{{1, 2, 3, 4}},
		Uniforms: []uint32{1, 2}, WideUniforms: []uint32{3, 4}, InstanceWords: 6}
	b := &EffectOp{Effect: fx, Blur: 2, Params: [5][4]float32{{1, 2, 3, 4}},
		Uniforms: []uint32{1, 2}, WideUniforms: []uint32{3, 4}, InstanceWords: 6}
	if !a.Same(b) {
		t.Error("the same effect op is not the same")
	}
	// Each field apart makes it another.
	for name, mutate := range map[string]func(*EffectOp){
		"effect":       func(o *EffectOp) { o.Effect = &Effect{Name: "other"} },
		"blur":         func(o *EffectOp) { o.Blur = 3 },
		"params":       func(o *EffectOp) { o.Params[0][0] = 9 },
		"uniforms":     func(o *EffectOp) { o.Uniforms = []uint32{1, 3} },
		"wideUniforms": func(o *EffectOp) { o.WideUniforms = nil },
		"masked":       func(o *EffectOp) { o.Glyphs = [2]int32{0, 1} },
		"origin":       func(o *EffectOp) { o.Origin[0] = 1 },
		"instances":    func(o *EffectOp) { o.Instances = []uint32{1} },
		"images":       func(o *EffectOp) { o.Images[0] = &Image{} },
	} {
		c := *b
		c.Uniforms = append([]uint32(nil), b.Uniforms...)
		c.WideUniforms = append([]uint32(nil), b.WideUniforms...)
		mutate(&c)
		if a.Same(&c) {
			t.Errorf("an op with another %s is the same", name)
		}
	}
}
