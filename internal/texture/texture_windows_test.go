//go:build windows

package texture

import (
	"testing"

	"github.com/egoist/mygo/internal/scene"
)

func TestPresentDXGIAndFrame(t *testing.T) {
	tex := New()
	key := new(int)
	if img := tex.Frame(key, func() {}); img != nil {
		t.Fatal("expected nil frame before any PresentDXGI")
	}

	asked := 0
	tex.Frame(key, func() { asked++ })

	const handle1, id1 = uintptr(0x1001), uint64(1)
	tex.PresentDXGI(handle1, id1, 1920, 1080)
	if asked != 1 {
		t.Fatalf("PresentDXGI asked for %d frames, want 1", asked)
	}
	if w, h := tex.Size(); w != 1920 || h != 1080 {
		t.Fatalf("Size = %v×%v, want 1920×1080", w, h)
	}

	img := tex.Frame(key, func() { asked++ })
	if img == nil || img.W != 1920 || img.H != 1080 || img.Surface == nil {
		t.Fatalf("unexpected frame: %+v", img)
	}
	if img.Surface.Format != scene.SurfaceBGRA || img.Surface.Native != handle1 || img.Surface.NativeID != id1 {
		t.Fatalf("unexpected surface: %+v", img.Surface)
	}
	v1 := img.Version()

	// Next frame updates handle, nativeID, size, and bumps version.
	const handle2, id2 = uintptr(0x1002), uint64(2)
	tex.PresentDXGI(handle2, id2, 2560, 1440)
	img2 := tex.Frame(key, func() {})
	if img2 == nil || img2.Version() == v1 || img2.W != 2560 || img2.H != 1440 {
		t.Fatalf("second frame did not update: %+v (v1=%d)", img2, v1)
	}
	if img2.Surface == nil || img2.Surface.Native != handle2 || img2.Surface.NativeID != id2 {
		t.Fatalf("second surface unexpected: %+v", img2.Surface)
	}

	// Clear resets texture to empty.
	tex.Clear()
	if w, h := tex.Size(); w != 0 || h != 0 {
		t.Fatalf("Size after Clear = %v×%v, want 0×0", w, h)
	}
	if cleared := tex.Frame(key, func() {}); cleared != nil {
		t.Fatalf("expected nil frame after Clear, got %+v", cleared)
	}

	// A window that stops drawing the texture stops being invalidated.
	asked = 0
	tex.Frame(key, func() { asked++ })
	for i := range 2 * staleAfter {
		tex.PresentDXGI(handle1, uint64(10+i), 640, 360)
	}
	if asked > staleAfter || len(tex.s.watch) != 0 {
		t.Fatalf("stale watcher asked %d times, watch len=%d", asked, len(tex.s.watch))
	}
}
