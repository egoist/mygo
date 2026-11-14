package ui

import (
	"runtime"
	"testing"

	"github.com/egoist/mygo/internal/scene"
)

// cornersGPU is a GPU renderer that draws continuous corners only when
// asked to, as Direct3D's and OpenGL's.
type cornersGPU struct {
	testGPU
	asked int
}

func (g *cornersGPU) ContinuousCorners() { g.asked++ }

// resetCorners restores the corners of the app, as they are by default,
// once t ends.
func resetCorners(t *testing.T) {
	t.Cleanup(func() { continuousCorners, gpuCorners = runtime.GOOS == "darwin", nil })
}

// continuousFills reports how many of the rounded fills of the last frame
// of tt have continuous corners, and how many are rounded.
func continuousFills(tt *Tester) (continuous, rounded int) {
	for _, op := range tt.h.last.Ops {
		if op.Kind == scene.OpFill && op.Radii != [4]float32{} {
			rounded++
			if op.Continuous {
				continuous++
			}
		}
	}
	return continuous, rounded
}

// TestContinuousCornersByDefault checks that rounded corners are continuous
// on macOS only, unless the app says otherwise, and that no GPU renderer is
// asked to draw them then.
func TestContinuousCornersByDefault(t *testing.T) {
	if want := runtime.GOOS == "darwin"; continuousCorners != want {
		t.Fatalf("continuous corners by default: %v, want %v", continuousCorners, want)
	}
	if gpuCorners != nil {
		t.Fatal("GPU renderers are asked for continuous corners by default")
	}
	tt := coreNewTester(func(c *context) {
		coreBox(c).Size(80, 40).Radius(16).Background(RGB(37, 99, 235))
	}, 200, 100)
	cont, rounded := continuousFills(tt)
	if rounded == 0 {
		t.Fatal("no rounded fill was painted")
	}
	want := 0
	if runtime.GOOS == "darwin" {
		want = rounded
	}
	if cont != want {
		t.Fatalf("%d of %d rounded fills are continuous on %s, want %d", cont, rounded, runtime.GOOS, want)
	}

	g := &cornersGPU{}
	_, _, frame := gpuHost(t, func() (gpuRenderer, error) { return g, nil })
	frame()
	if g.frames != 1 || g.asked != 0 {
		t.Fatalf("by default: %d frames, asked for continuous corners %d times", g.frames, g.asked)
	}
}

// TestSetContinuousCorners checks that the app's choice paints rounded
// corners continuous, or circular on macOS, and has the GPU renderers of
// windows made before and after draw continuous ones.
func TestSetContinuousCorners(t *testing.T) {
	resetCorners(t)
	view := func(c *context) {
		coreBox(c).Size(80, 40).Radius(16).Background(RGB(37, 99, 235))
		coreBox(c).Size(80, 40).Radius(4, 20, 0, 8).Background(RGB(220, 40, 40))
	}

	g := &cornersGPU{}
	_, _, frame := gpuHost(t, func() (gpuRenderer, error) { return g, nil })
	frame()
	SetContinuousCorners(true)
	if cont, rounded := continuousFills(coreNewTester(view, 200, 100)); rounded == 0 || cont != rounded {
		t.Fatalf("on: %d of %d rounded fills are continuous", cont, rounded)
	}
	frame()
	if g.asked == 0 {
		t.Fatal("the renderer of a window made before was not asked for continuous corners")
	}
	later := &cornersGPU{}
	_, _, frame = gpuHost(t, func() (gpuRenderer, error) { return later, nil })
	frame()
	if later.asked == 0 || later.frames != 1 {
		t.Fatalf("the renderer of a window made after: asked %d times, %d frames", later.asked, later.frames)
	}

	SetContinuousCorners(false)
	if cont, rounded := continuousFills(coreNewTester(view, 200, 100)); rounded == 0 || cont != 0 {
		t.Fatalf("off: %d of %d rounded fills are continuous", cont, rounded)
	}
}
