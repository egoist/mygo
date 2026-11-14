//go:build linux && (amd64 || arm64)

package gl

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/egoist/mygo/internal/gpu/gputest"
	"github.com/egoist/mygo/internal/scene"
)

// The shared builder's culling and scissors must preserve every pixel with
// both GL APIs, including nested clip masks and canvases drawn into textures.
func TestGeometryOptimizationsPreservePixels(t *testing.T) {
	type fixture struct {
		name string
		s    *scene.Scene
	}
	var cases []fixture
	for _, c := range gputest.CullCases() {
		cases = append(cases, fixture{"cull/" + c.Name, c.Scene})
	}
	for _, c := range gputest.RingCases() {
		cases = append(cases, fixture{"ring/" + c.Name, c.Scene})
	}
	for i, c := range gputest.ShadowCases() {
		cases = append(cases, fixture{fmt.Sprintf("shadow/%d", i), gputest.ShadowPanelScene(c)})
	}
	for _, c := range gputest.MaskedCases() {
		cases = append(cases, fixture{"masked/" + c.Name, c.Scene})
	}
	cases = append(cases,
		fixture{"transformed", gputest.TransformedScene()},
		fixture{"perspective", gputest.PerspectiveScene()},
		fixture{"translated", gputest.TranslatedScenes()[0]},
		fixture{"translated-between-pixels", gputest.TranslatedScenes()[1]},
		fixture{"masked-clips", gputest.MaskedClipScene()},
		fixture{"corner-clips", gputest.CornerClipScene()},
		fixture{"wide-colors", gputest.WideScene()},
		fixture{"canvases", gputest.CanvasScene()},
	)
	for _, api := range apis {
		t.Run(api.name, func(t *testing.T) {
			for _, c := range cases {
				t.Run(c.name, func(t *testing.T) {
					read := offscreen(t, api.es, c.s.Width, c.s.Height)
					r, err := New()
					if err != nil {
						t.Fatal(err)
					}
					defer r.Release()
					if err := r.Render(c.s); err != nil {
						t.Fatal(err)
					}
					want := read()
					r.b.NoCull, r.b.NoShadowSplit, r.b.NoRingSplit = true, true, true
					for _, canvas := range r.canvases {
						canvas.b.NoCull, canvas.b.NoShadowSplit, canvas.b.NoRingSplit = true, true, true
					}
					// Render every canvas again under the reference policy, rather
					// than comparing against its already optimized cached texture.
					c.s.EachCanvas(func(img *scene.Image) { img.Changed() })
					if err := r.Render(c.s); err != nil {
						t.Fatal(err)
					}
					if got := read(); !bytes.Equal(got, want) {
						t.Fatal("culling or split scissors changed pixels")
					}
				})
			}
		})
	}
}

// TestCornerSplitDrawsTheSamePixels draws scenes with continuous corners
// with a renderer drawing them (ContinuousCorners), large shapes as
// nine-patches whose pieces away from the corners draw with the shader
// without their code (gpu.Builder.SplitCorners), and as whole shapes
// (NoCornerSplit), and requires the same bytes.
func TestCornerSplitDrawsTheSamePixels(t *testing.T) {
	type fixture struct {
		name string
		s    *scene.Scene
	}
	cases := []fixture{{"cards", gputest.CardsScene(1400, 900)}, {"odd cards", gputest.CardsScene(1001, 777)},
		{"continuous", gputest.ContinuousScene()}, {"continuous clips", gputest.ContinuousCornerClipScene()}}
	for _, c := range gputest.CullCases() {
		cases = append(cases, fixture{"cull/" + c.Name, gputest.Continuously(c.Scene)})
	}
	for _, c := range gputest.RingCases() {
		cases = append(cases, fixture{"ring/" + c.Name, gputest.Continuously(c.Scene)})
	}
	for i, c := range gputest.ShadowCases() {
		c.Continuous = true
		cases = append(cases, fixture{fmt.Sprintf("shadow/%d", i), gputest.ShadowPanelScene(c)})
	}
	for _, api := range apis {
		t.Run(api.name, func(t *testing.T) {
			pieces := 0
			for _, c := range cases {
				t.Run(c.name, func(t *testing.T) {
					read := offscreen(t, api.es, c.s.Width, c.s.Height)
					r, err := New()
					if err != nil {
						t.Fatal(err)
					}
					defer r.Release()
					r.ContinuousCorners()
					if err := r.Render(c.s); err != nil {
						t.Fatal(err)
					}
					split, n := read(), len(r.b.Instances)
					r.b.NoCornerSplit = true
					if err := r.Render(c.s); err != nil {
						t.Fatal(err)
					}
					pieces += n - len(r.b.Instances)
					if got := read(); !bytes.Equal(got, split) {
						t.Fatal("nine-patches and whole shapes differ")
					}
				})
			}
			if pieces == 0 {
				t.Error("nothing was drawn in pieces")
			}
		})
	}
}
