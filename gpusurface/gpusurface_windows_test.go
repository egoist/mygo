//go:build windows

package gpusurface

import (
	"runtime"
	"syscall"
	"testing"
	"unsafe"

	"github.com/egoist/mygo/internal/wait"
)

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	gdi32                = syscall.NewLazyDLL("gdi32.dll")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procSetWindowPos     = user32.NewProc("SetWindowPos")
	procClientToScreen   = user32.NewProc("ClientToScreen")
	procPeekMessageW     = user32.NewProc("PeekMessageW")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procGetDC            = user32.NewProc("GetDC")
	procReleaseDC        = user32.NewProc("ReleaseDC")
	procGetPixel         = gdi32.NewProc("GetPixel")
	procSetProcessDpiCtx = user32.NewProc("SetProcessDpiAwarenessContext")
	procDwmFlush         = syscall.NewLazyDLL("dwmapi.dll").NewProc("DwmFlush")
)

// The tests are per-monitor DPI aware, as MyGo's apps are: the windows'
// sizes, their client points and the screen's pixels are then all in
// physical pixels, on a scaled display too.
func init() {
	if procSetProcessDpiCtx.Find() == nil {
		const perMonitorAwareV2 = ^uintptr(0) - 3 // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2
		procSetProcessDpiCtx.Call(perMonitorAwareV2)
	}
}

// testWindow makes a topmost w x h pixel window on the calling thread,
// which the test keeps locked.
func testWindow(t *testing.T, w, h int) uintptr {
	const wsPopup, wsVisible, wsExTopmost, wsExToolWindow, wsExNoActivate = 0x80000000, 0x10000000, 0x8, 0x80, 0x08000000
	cls, _ := syscall.UTF16PtrFromString("STATIC")
	hwnd, _, err := procCreateWindowExW.Call(wsExTopmost|wsExToolWindow|wsExNoActivate, uintptr(unsafe.Pointer(cls)), 0,
		wsPopup|wsVisible, 40, 40, uintptr(w), uintptr(h), 0, 0, 0, 0)
	if hwnd == 0 {
		t.Fatalf("CreateWindowExW: %v", err)
	}
	t.Cleanup(func() { procDestroyWindow.Call(hwnd) })
	return hwnd
}

// pump dispatches the messages waiting for the thread's windows.
func pump() {
	var msg [48]byte
	for {
		if r, _, _ := procPeekMessageW.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0, 1); r == 0 {
			return
		}
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg[0])))
	}
}

// waitPixel waits until the screen shows want, as 0x00BBGGRR, at the
// client point x, y of hwnd: until DWM composed what was presented, which
// takes a refresh or two on a desktop with a GPU and longer on a busy
// runner drawing with WARP. The window's messages are pumped meanwhile.
func waitPixel(t *testing.T, s *Surface, hwnd uintptr, x, y int32, want uint32, what string) {
	t.Helper()
	last := ^uint32(0)
	if !wait.Poll(func() bool {
		pump()
		procDwmFlush.Call()
		last = screenPixel(hwnd, x, y)
		return last == want
	}) {
		t.Fatalf("%s: the screen still shows %#06x at (%d, %d), want %#06x (on %s)", what, last, x, y, want, s.AdapterName())
	}
}

func clientSize(hwnd uintptr) (w, h int32) {
	var r struct{ Left, Top, Right, Bottom int32 }
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	return r.Right - r.Left, r.Bottom - r.Top
}

// solid makes a BGRA texture of the surface's device filled with bgra.
func solid(t *testing.T, s *Surface, w, h int, bgra uint32) uintptr {
	const devCreateTexture2D, usageDefault, bindShaderResource = 5, 0, 0x8
	pixels := make([]uint32, w*h)
	for i := range pixels {
		pixels[i] = bgra
	}
	td := texture2DDesc{Width: uint32(w), Height: uint32(h), MipLevels: 1, ArraySize: 1, Format: formatB8G8R8A8Unorm,
		SampleCount: 1, Usage: usageDefault, BindFlags: bindShaderResource}
	data := subresourceData{SysMem: uintptr(unsafe.Pointer(&pixels[0])), SysMemPitch: uint32(w * 4)}
	var tex uintptr
	if hr := call(s.device, devCreateTexture2D, uintptr(unsafe.Pointer(&td)), uintptr(unsafe.Pointer(&data)), uintptr(unsafe.Pointer(&tex))); failed(hr) {
		t.Fatalf("CreateTexture2D: %#x", uint32(hr))
	}
	runtime.KeepAlive(pixels)
	t.Cleanup(func() { free(&tex) })
	return tex
}

// screenPixel reads the desktop's pixel at the client point x, y of hwnd,
// as 0x00BBGGRR.
func screenPixel(hwnd uintptr, x, y int32) uint32 {
	p := struct{ X, Y int32 }{x, y}
	procClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(&p)))
	dc, _, _ := procGetDC.Call(0)
	defer procReleaseDC.Call(0, dc)
	c, _, _ := procGetPixel.Call(dc, uintptr(p.X), uintptr(p.Y))
	return uint32(c)
}

// TestComposite shows frames through a DirectComposition target that is
// not topmost: the swap chain takes the client size, follows a resize, and
// what it presents reaches the screen.
func TestComposite(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hwnd := testWindow(t, 320, 180)
	s, err := New(Options{Window: hwnd, Composite: true})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if w, h := s.Size(); w != 320 || h != 180 {
		t.Fatalf("surface %dx%d, want the client size 320x180", w, h)
	}
	if err := s.Present(solid(t, s, 64, 64, 0xff00ff00)); err != nil {
		t.Fatal(err)
	}
	waitPixel(t, s, hwnd, 160, 90, 0x0000ff00, "green at the surface's centre")

	const swpNoMove, swpNoZOrder, swpNoActivate = 0x2, 0x4, 0x10
	procSetWindowPos.Call(hwnd, 0, 0, 0, 480, 270, swpNoMove|swpNoZOrder|swpNoActivate)
	wait.Until(t, "the window at 480x270", func() bool {
		pump()
		w, h := clientSize(hwnd)
		return w == 480 && h == 270
	})
	if err := s.Resize(0, 0); err != nil {
		t.Fatal(err)
	}
	if w, h := s.Size(); w != 480 || h != 270 {
		t.Fatalf("surface %dx%d after resize, want 480x270", w, h)
	}
	if err := s.Present(solid(t, s, 64, 36, 0xffff0000)); err != nil {
		t.Fatal(err)
	}
	waitPixel(t, s, hwnd, 400, 220, 0x000000ff, "red after the resize")
	t.Logf("on %s, tearing %v", s.AdapterName(), s.Tearing())
}

// TestCompositeKeepsAspect: a square picture in a 16:9 swap chain shows
// whole between black bars, not stretched over the window.
func TestCompositeKeepsAspect(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hwnd := testWindow(t, 320, 180)
	s, err := New(Options{Window: hwnd, Composite: true})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Present(solid(t, s, 64, 64, 0xff00ff00)); err != nil {
		t.Fatal(err)
	}
	waitPixel(t, s, hwnd, 160, 90, 0x0000ff00, "green at the centre")
	for _, x := range []int32{10, 309} {
		if c := screenPixel(hwnd, x, 90); c != 0 {
			t.Fatalf("screen shows %#06x at x %d, want the black pillar", c, x)
		}
	}
}

func TestFitViewport(t *testing.T) {
	s := &Surface{width: 5120, height: 1440}
	if !s.fitViewport(1920, 1080) || s.vp != (viewport{X: 1280, W: 2560, H: 1440, MaxDepth: 1}) {
		t.Fatalf("16:9 in 32:9: viewport %+v", s.vp)
	}
	if s.fitViewport(2560, 720) || s.vp != (viewport{W: 5120, H: 1440, MaxDepth: 1}) {
		t.Fatalf("32:9 in 32:9: viewport %+v", s.vp)
	}
}

// TestOnPresented: each present after OnPresented calls it back with the
// frame's tag and its submit time, and 0 for the time it reached the
// screen, which DXGI does not tell.
func TestOnPresented(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hwnd := testWindow(t, 64, 64)
	s, err := New(Options{Window: hwnd, Composite: true})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	type call struct{ tag, submitted, presented int64 }
	var calls []call
	s.OnPresented(func(tag, submitted, presented int64) { calls = append(calls, call{tag, submitted, presented}) })
	frame := solid(t, s, 16, 16, 0xff0000ff)
	before := Now()
	if err := s.PresentTagged(frame, 7); err != nil {
		t.Fatal(err)
	}
	if err := s.Present(frame); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0].tag != 7 || calls[1].tag != 0 || calls[0].submitted < before || calls[1].submitted < calls[0].submitted || calls[0].presented != 0 {
		t.Fatalf("OnPresented calls %+v after PresentTagged(7) and Present at %d", calls, before)
	}
}
