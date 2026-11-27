package ui

import (
	"errors"
	"image"
	"slices"
	"testing"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/surface"
)

// gpuDamageSurface is a test surface told what each frame drawn on the
// GPU changed, as Linux's GtkGLArea is, which repaints only that.
type gpuDamageSurface struct {
	testSurface
	told []string
}

func (s *gpuDamageSurface) GPUDamage(damage []image.Rectangle, whole bool) {
	switch {
	case whole:
		s.told = append(s.told, "whole")
	case len(damage) == 0:
		s.told = append(s.told, "nothing")
	default:
		s.told = append(s.told, damage[0].String())
	}
}

// redrawGPU is a GPU renderer that tells what its last frame redrew.
type redrawGPU struct {
	testGPU
	rects []image.Rectangle
	whole bool
}

func (g *redrawGPU) Redrawn() ([]image.Rectangle, bool) { return g.rects, g.whole }

// TestGPUDamageToldTheSurface checks that a surface repainting what GPU
// frames changed hears what each redrew: the renderer's damage, all of
// it from a renderer that cannot tell, and nothing of a frame drawn in
// memory, which the surface shows by itself.
func TestGPUDamageToldTheSurface(t *testing.T) {
	t.Setenv("MYGO_GPU", "")
	var g gpuRenderer
	newGPU = func(platform.SurfaceNative) (gpuRenderer, error) {
		if g == nil {
			return nil, errors.New("no device")
		}
		return g, nil
	}
	t.Cleanup(func() { newGPU = newGPURenderer })
	s := &gpuDamageSurface{}
	h := &windowHost{conn: &surface.Conn{Surface: s}}
	h.rt = newRuntime(func(c *context) { coreText(c, "Hello") }, h)
	frame := func() { h.event(platform.SurfaceEvent{Kind: platform.SurfaceFrame}) }

	r := &redrawGPU{whole: true}
	g = r
	frame()
	r.rects, r.whole = []image.Rectangle{image.Rect(10, 20, 30, 40)}, false
	frame()
	r.rects = nil
	frame()
	if want := []string{"whole", "(10,20)-(30,40)", "nothing"}; !slices.Equal(s.told, want) {
		t.Fatalf("told %q, want %q", s.told, want)
	}

	// A renderer that cannot tell what it redrew: all of it.
	s.told = nil
	g = &testGPU{}
	h.gpu.Release()
	h.gpu, h.gpuTried = nil, false
	frame()
	if want := []string{"whole"}; !slices.Equal(s.told, want) {
		t.Fatalf("without Redrawn: told %q, want %q", s.told, want)
	}

	// Frames drawn in memory are shown by PresentPixels.
	s.told = nil
	g = nil
	h.gpu.Release()
	h.gpu, h.gpuTried = nil, false
	frame()
	if len(s.told) != 0 || s.pixels != 1 {
		t.Fatalf("in memory: told %q, %d frames in memory", s.told, s.pixels)
	}
}
