//go:build windows

package texture

import (
	"bytes"
	"testing"

	"github.com/egoist/mygo/internal/scene"
)

func TestPresentDXGIAndFrame(t *testing.T) {
	if Share(0) != nil {
		t.Fatal("Share(0) should return nil")
	}

	tex := New()
	key := new(int)
	if img := tex.Frame(key, func() {}); img != nil {
		t.Fatal("expected nil frame before any PresentDXGI")
	}

	asked := 0
	tex.Frame(key, func() { asked++ })

	const handle1, handle2 = uintptr(0x1001), uintptr(0x1002)
	s1 := Share(handle1)
	s2 := Share(handle2)
	if s1 == nil || s2 == nil || s1.ID() == 0 || s2.ID() == 0 || s1.ID() == s2.ID() {
		t.Fatalf("expected distinct non-zero IDs: s1=%+v s2=%+v", s1, s2)
	}

	wantRGBA := []byte{0xe0, 0x80, 0x20, 0xff}
	tex.PresentDXGI(s1, 1920, 1080, func(dst []byte) []byte { return append(dst[:0], wantRGBA...) })
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
	if img.Surface.Format != scene.SurfaceBGRA || img.Surface.Native != handle1 || img.Surface.NativeID != s1.ID() {
		t.Fatalf("unexpected surface: %+v", img.Surface)
	}
	if img.Restore == nil || !bytes.Equal(img.Restore(), wantRGBA) {
		t.Fatalf("Restore() = %x, want %x", img.Restore(), wantRGBA)
	}
	v1 := img.Version()

	// Next frame without rgba callback makes it GPU-only (Restore == nil),
	// updates handle, NativeID, size, and bumps version.
	tex.PresentDXGI(s2, 2560, 1440, nil)
	img2 := tex.Frame(key, func() {})
	if img2 == nil || img2.Version() == v1 || img2.W != 2560 || img2.H != 1440 {
		t.Fatalf("second frame did not update: %+v (v1=%d)", img2, v1)
	}
	if img2.Surface == nil || img2.Surface.Native != handle2 || img2.Surface.NativeID != s2.ID() {
		t.Fatalf("second surface unexpected: %+v", img2.Surface)
	}
	if img2.Restore != nil {
		t.Fatal("expected nil Restore when rgba callback is nil")
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
	for range 2 * staleAfter {
		tex.PresentDXGI(s1, 640, 360, nil)
	}
	if asked > staleAfter || len(tex.s.watch) != 0 {
		t.Fatalf("stale watcher asked %d times, watch len=%d", asked, len(tex.s.watch))
	}
}
