//go:build windows

package texture

import "github.com/egoist/mygo/internal/scene"

// SharedTexture identifies a Windows Direct3D 11 shared BGRA8 texture
// (NT handle guarded by an IDXGIKeyedMutex on key 0) with a process-wide
// unique ID for renderer handle caches.
//
// The caller retains ownership of the underlying NT handle and must keep it
// open while any Texture or renderer may still draw it.
type SharedTexture struct {
	handle uintptr
	id     uint64
}

// Share registers a Windows Direct3D 11 BGRA8 NT shared texture handle and
// assigns it a process-wide unique ID shared with all other producers in the
// app (such as ui.Video's Media Foundation engine). Returns nil if handle is 0.
func Share(handle uintptr) *SharedTexture {
	if handle == 0 {
		return nil
	}
	return &SharedTexture{handle: handle, id: scene.NextNativeID()}
}

// ID returns the process-wide unique identifier assigned to this shared texture.
func (s *SharedTexture) ID() uint64 {
	if s == nil {
		return 0
	}
	return s.id
}

type dxgiState struct {
	dxgi        bool
	pendingID   uint64
	pendingRGBA func(dst []byte) []byte
	shownID     uint64
	shownRGBA   func(dst []byte) []byte
}

// PresentDXGI shows a Windows Direct3D 11 shared BGRA8 texture (NT handle with
// IDXGIKeyedMutex key 0, wrapped via Share) in the next frame of the windows
// showing the texture, with zero CPU copy. Safe from any goroutine.
//
// rgba is an optional readback function returning premultiplied RGBA pixels
// (4×w×h bytes) into dst when it has room, used by the CPU renderer, captures
// and Tester frames, smaller mip levels, and backdrop blurs over the texture.
// Passing nil makes the texture GPU-only (those paths read no pixels).
func (t *Texture) PresentDXGI(shared *SharedTexture, w, h int, rgba func(dst []byte) []byte) {
	if shared == nil || shared.handle == 0 || shared.id == 0 || w <= 0 || h <= 0 {
		return
	}
	s := t.s
	s.mu.Lock()
	s.dxgi = true
	s.pending = shared.handle
	s.pendingID = shared.id
	s.pendingRGBA = rgba
	s.w, s.h = w, h
	s.seq++
	watch := s.collectWatchersLocked()
	s.mu.Unlock()
	for _, f := range watch {
		f()
	}
}

func (s *state) clearDXGILocked() {
	s.pendingID = 0
	s.pendingRGBA = nil
}

func (s *state) isDXGILocked() bool { return s.dxgi }

func (s *state) dxgiFrame(key any, invalidate func()) (*scene.Image, bool) {
	s.mu.Lock()
	if !s.dxgi {
		s.mu.Unlock()
		return nil, false
	}
	s.recordWatcherLocked(key, invalidate)
	handle, nativeID, rgba, seq, w, h := s.pending, s.pendingID, s.pendingRGBA, s.seq, s.w, s.h
	s.mu.Unlock()

	if handle == 0 {
		s.img = nil
		s.shown = 0
		s.shownID = 0
		s.shownRGBA = nil
		s.shownSeq = seq
		return nil, true
	}
	if s.img == nil {
		s.img = scene.NewImageRGBA(w, h, nil)
	}
	if seq != s.shownSeq {
		s.shown, s.shownID, s.shownRGBA, s.shownSeq = handle, nativeID, rgba, seq
		s.img.W, s.img.H = w, h
		s.img.Pix = nil
		if rgba != nil {
			fn := rgba
			s.img.Restore = func() []byte { return fn(nil) }
		} else {
			s.img.Restore = nil
		}
		s.img.Surface = &scene.Surface{
			Format:   scene.SurfaceBGRA,
			Native:   handle,
			NativeID: nativeID,
		}
		s.img.Changed()
	}
	return s.img, true
}
