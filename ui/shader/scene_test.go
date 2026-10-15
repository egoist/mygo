package shader

import (
	"image"
	"image/color"
	"math"

	"github.com/egoist/mygo/internal/scene"
	"github.com/egoist/mygo/internal/shaderir"
)

// testSource reads two images, one exactly and one sampled, and uniforms
// of each kind, as the renderers must alike.
const testSource = `package main

var Seed uint
var Tint vec4
var Count int
var Spots [3]vec3
var Turn mat2

func pcg(v uint) uint {
	s := v*747796405 + 2891336453
	w := ((s >> ((s >> 28) + 4)) ^ s) * 277803737
	return (w >> 22) ^ w
}

func Fragment(dst vec4, pos vec2) vec4 {
	q := Turn * (pos - rectSize()/2)
	glow := 0.0
	for i := 0; i < Count; i++ {
		d := distance(q, Spots[i].xy)
		glow += exp(-d*d/(Spots[i].z*Spots[i].z))
	}
	h := float(pcg(uint(int(pos.x/4))*0x9E3779B9^pcg(uint(int(pos.y/4))^Seed))>>8) / 16777216.0
	a := image0At(pos * pixelScale() / 2)
	b := image1Sample(pos / rectSize() * image1Size())
	return mix(a, b, 0.5)*0.6 + Tint*clamp(glow, 0, 1)*0.3 + vec4(h*0.1)
}
`

// testScene returns a scene of s over a gray frame at scale 2, reading a
// gradient bitmap and a canvas of two rectangles.
func testScene(s *Shader) *scene.Scene {
	const scale = 2
	grad := image.NewRGBA(image.Rect(0, 0, 64, 40))
	for y := range 40 {
		for x := range 64 {
			grad.Set(x, y, color.RGBA{uint8(x * 4), uint8(y * 6), 128, 255})
		}
	}
	canvas := scene.NewCanvas(20, 12)
	canvas.Scene.Ops = []scene.Op{
		{Kind: scene.OpFill, Rect: scene.Rect{W: 10, H: 12}, Color: scene.Color{R: 220, G: 40, B: 40, A: 255}},
		{Kind: scene.OpFill, Rect: scene.Rect{X: 10, W: 10, H: 12}, Color: scene.Color{R: 37, G: 99, B: 235, A: 255}},
	}
	images := [4]*scene.Image{scene.NewImage(grad), canvas}

	r := scene.Rect{X: 8, Y: 6, W: 224, H: 108}
	words := make([]uint32, s.words)
	for i, img := range images {
		if img != nil {
			words[2*i], words[2*i+1] = math.Float32bits(float32(img.W)), math.Float32bits(float32(img.H))
		}
	}
	words[scaleWord-2], words[scaleWord-1] = math.Float32bits(r.W/scale), math.Float32bits(r.H/scale)
	words[scaleWord] = math.Float32bits(scale)
	values := map[string]any{
		"Seed":  uint32(0x3fa1c2d9),
		"Tint":  []float32{1, 0.8, 0.2, 1},
		"Count": 2,
		"Spots": []float32{-20, 0, 18, 25, 10, 12, 0, 0, 1},
		"Turn":  []float32{0.8, 0.6, -0.6, 0.8},
	}
	for name, v := range values {
		u := s.uniforms[name]
		kind := u.t.Scalar()
		if u.t.Main == shaderir.Array {
			kind = u.t.Sub[0].Scalar()
		}
		set(words[u.word:u.word+u.n], kind, name, v)
	}
	sc := &scene.Scene{Width: 240, Height: 120, Clear: scene.Color{R: 240, G: 240, B: 240, A: 255}}
	sc.Ops = append(sc.Ops, scene.Op{Kind: scene.OpEffect, Rect: r, Radii: [4]float32{16, 16, 16, 16}})
	sc.Effects = append(sc.Effects, scene.EffectOp{Effect: s.fx, Uniforms: words, Images: images})
	return sc
}
