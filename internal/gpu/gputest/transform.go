package gputest

import "github.com/egoist/mygo/internal/scene"

// TransformedScene exercises all primitives and effects under affine
// composition, with nested transformed clips and an inline glyph transform.
func TransformedScene() *scene.Scene {
	s := Scene()
	s.Clear = scene.Color{R: 100, G: 120, B: 160, A: 80}
	m := scene.Translation(25, 20).Mul(scene.Rotation(4)).Mul(scene.Scaling(0.8, 0.9))
	for i := range s.Ops {
		s.Ops[i].Transform = m
	}
	if len(s.Glyphs) > 0 {
		s.Glyphs[0].Transform = scene.Rotation(15)
	}
	return s
}
