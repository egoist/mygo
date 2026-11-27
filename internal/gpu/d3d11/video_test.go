//go:build windows

package d3d11

import (
	"runtime"
	"sync/atomic"
	"syscall"
	"testing"
	"unsafe"

	"github.com/egoist/mygo/internal/gpu/d3d11/device"
	"github.com/egoist/mygo/internal/gpu/gputest"
	"github.com/egoist/mygo/internal/scene"
	nativetex "github.com/egoist/mygo/internal/texture"
)

// TestDrawsVideoAsTheCPURenderer draws frames of video in NV12 from their
// planes in memory, which the shader converts as the CPU renderer does,
// then frames in BGRA that another device drew into textures it shares,
// as Media Foundation does, which the renderer reads where they are.
func TestDrawsVideoAsTheCPURenderer(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	s := gputest.VideoScene()
	r, err := New(hiddenWindow(t, s.Width, s.Height))
	if err != nil {
		t.Skip("no Direct3D 11:", err)
	}
	defer r.Release()
	for range 2 {
		for _, op := range s.Ops {
			op.Image.Changed()
		}
		if err := r.draw(s); err != nil {
			t.Fatal(err)
		}
		pix, stride := r.readBack(t)
		gputest.Compare(t, "d3d11-video", pix, stride, s)
	}
	for _, op := range s.Ops {
		if tex := r.images[op.Image.ID()]; tex == nil || tex.chroma == nil {
			t.Fatal("a frame of video drew from RGBA, not its planes")
		}
	}

	// The frames converted to BGRA, in textures of another device.
	dev, ctx, _, err := device.New()
	if err != nil {
		t.Fatal(err)
	}
	defer free(&dev)
	defer free(&ctx)
	var shared []uintptr
	defer func() {
		for _, h := range shared {
			syscall.CloseHandle(syscall.Handle(h))
		}
	}()
	done := map[*scene.Image]bool{}
	for i, op := range s.Ops {
		img := op.Image
		if done[img] {
			continue
		}
		done[img] = true
		rgba := img.Surface.RGBA(nil, img.W, img.H)
		bgra := make([]byte, len(rgba))
		for k := 0; k < len(rgba); k += 4 {
			bgra[k], bgra[k+1], bgra[k+2], bgra[k+3] = rgba[k+2], rgba[k+1], rgba[k], rgba[k+3]
		}
		h := newSharedTexture(t, dev, ctx, img.W, img.H, bgra)
		shared = append(shared, h)
		img.Surface = &scene.Surface{Format: scene.SurfaceBGRA, Native: h, NativeID: uint64(i + 1)}
		img.Restore = func() []byte { return rgba }
		img.Pix = nil
		img.Changed()
	}
	for range 3 {
		if err := r.draw(s); err != nil {
			t.Fatal(err)
		}
		pix, stride := r.readBack(t)
		gputest.Compare(t, "d3d11-video-shared", pix, stride, s)
	}
	if len(r.video.shared) != len(shared) {
		t.Fatalf("the renderer opened %d shared textures, not %d", len(r.video.shared), len(shared))
	}
	if len(r.video.acquired) != 0 {
		t.Fatal("the frame kept the keyed mutexes of the textures it read")
	}
}

// TestDrawsNativeTextureDXGI verifies that native.Texture (internal/texture)
// can present a D3D11 shared BGRA texture from a non-UI goroutine, wake its
// watchers, draw through the D3D11 renderer with zero CPU copy, and clear back
// to empty.
func TestDrawsNativeTextureDXGI(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	const w, h = 64, 64
	r, err := New(hiddenWindow(t, w, h))
	if err != nil {
		t.Skip("no Direct3D 11:", err)
	}
	defer r.Release()

	dev, ctx, _, err := device.New()
	if err != nil {
		t.Fatal(err)
	}
	defer free(&dev)
	defer free(&ctx)

	// Solid BGRA color (B=0x20, G=0x80, R=0xe0, A=0xff).
	bgra := make([]byte, w*h*4)
	for i := 0; i < len(bgra); i += 4 {
		bgra[i], bgra[i+1], bgra[i+2], bgra[i+3] = 0x20, 0x80, 0xe0, 0xff
	}
	handle := newSharedTexture(t, dev, ctx, w, h, bgra)
	defer syscall.CloseHandle(syscall.Handle(handle))

	tex := nativetex.New()
	key := new(int)
	var woke atomic.Int32
	if img := tex.Frame(key, func() { woke.Add(1) }); img != nil {
		t.Fatal("expected nil frame before PresentDXGI")
	}

	done := make(chan struct{})
	go func() {
		tex.PresentDXGI(handle, 1, w, h)
		close(done)
	}()
	<-done

	if got := woke.Load(); got != 1 {
		t.Fatalf("PresentDXGI woke watcher %d times, want 1", got)
	}
	if sw, sh := tex.Size(); sw != w || sh != h {
		t.Fatalf("Size = %vx%v, want %dx%d", sw, sh, w, h)
	}

	img := tex.Frame(key, func() { woke.Add(1) })
	if img == nil {
		t.Fatal("expected non-nil frame after PresentDXGI")
	}
	s := &scene.Scene{
		Width:  w,
		Height: h,
		Clear:  scene.Color{R: 0, G: 0, B: 0, A: 255},
		Ops: []scene.Op{{
			Kind:  scene.OpImage,
			Rect:  scene.Rect{W: w, H: h},
			Image: img,
			Src:   scene.Rect{W: w, H: h},
		}},
	}
	if err := r.draw(s); err != nil {
		t.Fatal(err)
	}
	pix, stride := r.readBack(t)
	off := (h/2)*stride + (w/2)*4
	rGot, gGot, bGot, aGot := pix[off], pix[off+1], pix[off+2], pix[off+3]
	if absDiff(rGot, 0xe0) > 2 || absDiff(gGot, 0x80) > 2 || absDiff(bGot, 0x20) > 2 || aGot != 0xff {
		t.Fatalf("center pixel = RGBA(%#x,%#x,%#x,%#x), want RGBA(0xe0,0x80,0x20,0xff)", rGot, gGot, bGot, aGot)
	}
	if len(r.video.shared) != 1 {
		t.Fatalf("renderer opened %d shared textures, want 1", len(r.video.shared))
	}
	if len(r.video.acquired) != 0 {
		t.Fatal("renderer kept keyed mutex acquired after draw")
	}

	cleared := make(chan struct{})
	go func() {
		tex.Clear()
		close(cleared)
	}()
	<-cleared

	if got := woke.Load(); got != 2 {
		t.Fatalf("Clear woke watcher %d times total, want 2", got)
	}
	if sw, sh := tex.Size(); sw != 0 || sh != 0 {
		t.Fatalf("Size after Clear = %vx%v, want 0x0", sw, sh)
	}
	if img := tex.Frame(key, func() { woke.Add(1) }); img != nil {
		t.Fatalf("expected nil frame after Clear, got %+v", img)
	}
}

func absDiff(a, b byte) byte {
	if a > b {
		return a - b
	}
	return b - a
}

// newSharedTexture returns the NT handle of a w×h texture in BGRA of dev with
// a keyed mutex, holding pix, which dev wrote under the mutex.
func newSharedTexture(t *testing.T, dev, ctx uintptr, w, h int, pix []byte) uintptr {
	const (
		miscSharedKeyedMutex = 0x100
		miscSharedNTHandle   = 0x800
		sharedRead           = 0x80000000
		sharedWrite          = 1
		createSharedHandle   = 13 // IDXGIResource1
	)
	iidIDXGIResource1 := guid{0x30961379, 0x4609, 0x4a41, [8]byte{0x99, 0x8e, 0x54, 0xfe, 0x56, 0x7e, 0xe0, 0xc1}}
	desc := texture2DDesc{Width: uint32(w), Height: uint32(h), MipLevels: 1, ArraySize: 1, Format: formatB8G8R8A8Unorm, SampleCount: 1,
		Usage: usageDefault, BindFlags: bindShaderResource, MiscFlags: miscSharedKeyedMutex | miscSharedNTHandle}
	var tex, mutex, res uintptr
	if hr := call(dev, devCreateTexture2D, uintptr(unsafe.Pointer(&desc)), 0, uintptr(unsafe.Pointer(&tex))); failed(hr) {
		t.Fatalf("cannot create a shared texture: %#x", uint32(hr))
	}
	defer free(&tex)
	if failed(call(tex, 0, uintptr(unsafe.Pointer(&iidIDXGIKeyedMutex)), uintptr(unsafe.Pointer(&mutex)))) {
		t.Fatal("no keyed mutex")
	}
	defer free(&mutex)
	call(mutex, keyedAcquireSync, 0, 1000)
	call(ctx, ctxUpdateSubresource, tex, 0, 0, uintptr(unsafe.Pointer(&pix[0])), uintptr(4*w), 0)
	call(mutex, keyedReleaseSync, 0)
	call(ctx, ctxFlush)
	if failed(call(tex, 0, uintptr(unsafe.Pointer(&iidIDXGIResource1)), uintptr(unsafe.Pointer(&res)))) {
		t.Fatal("no IDXGIResource1")
	}
	defer free(&res)
	var handle uintptr
	if hr := call(res, createSharedHandle, 0, sharedRead|sharedWrite, 0, uintptr(unsafe.Pointer(&handle))); failed(hr) {
		t.Fatalf("cannot share the texture: %#x", uint32(hr))
	}
	return handle
}
