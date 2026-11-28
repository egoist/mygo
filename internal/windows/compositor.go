//go:build windows && (amd64 || arm64)

package windows

import (
	"errors"
	"log"
	"math"
	"time"
	"unsafe"

	"github.com/egoist/mygo/internal/gpu/d3d11/device"
)

// A window with a material behind it has no redirection bitmap
// (window.noRedirect), which hides what GDI and UpdateLayeredWindow draw in
// it and in its child windows, the controls of a hidden title bar among
// them. Those show through DirectComposition instead: a surface of
// premultiplied BGRA on a layered window without a redirection bitmap
// either, which blends over the webview as a layered window does. Native
// UI drawn in memory shows the same way on its surface's window; drawn on
// the GPU, through the renderer's swap chain, on a device of its own
// (d3d11.NewComposed).
//
// The windows share a Direct3D 11 device and a DirectComposition device.
// When the GPU device is removed (a driver update, a GPU reset), which its
// removed event tells, or a draw finds the device invalid, both go, and
// every window's controls draw again with new ones. Controls that could not
// draw try again on a timer of the application window, later after each
// failure in a row.

var (
	procDCompositionCreateDevice = systemDLL("dcomp.dll").NewProc("DCompositionCreateDevice")
	procCreateDXGIFactory1       = systemDLL("dxgi.dll").NewProc("CreateDXGIFactory1")
	procCreateEventW             = kernel32.NewProc("CreateEventW")
	procSetEvent                 = kernel32.NewProc("SetEvent")
	procWaitForSingleObject      = kernel32.NewProc("WaitForSingleObject")
	procSetTimer                 = user32.NewProc("SetTimer")
	procKillTimer                = user32.NewProc("KillTimer")

	iidIDXGIDevice         = guid("54ec77fa-1377-44e6-8c32-88fd5f44c84c")
	iidIDXGIFactory1       = guid("770aae78-f26f-4dba-a829-253c83d1b387")
	iidID3D11Device4       = guid("8992ab71-02e6-4b8d-ba48-b056dcda42c4")
	iidID3D11Texture2D     = guid("6f15aaf2-d208-4e89-9ab4-489535d34f9c")
	iidIDCompositionDevice = guid("c37ea93a-e7aa-450d-b16f-9746cb0407f3")
)

// Vtable indices, from d3d11.h, d3d11_4.h and dcomp.h.
const (
	d3dCtxUpdateSubresource          = 48 // ID3D11DeviceContext
	d3dDevRegisterDeviceRemovedEvent = 65 // ID3D11Device4
	d3dDevUnregisterDeviceRemoved    = 66
	dcompDevCommit                   = 3 // IDCompositionDevice
	dcompDevCreateTargetForHwnd      = 6
	dcompDevCreateVisual             = 7
	dcompDevCreateSurface            = 8
	dcompDevCheckDeviceState         = 26
	dcompTargetSetRoot               = 3  // IDCompositionTarget
	dcompVisualSetContent            = 15 // IDCompositionVisual, whose overloads take a slot each
	dcompSurfaceBeginDraw            = 3  // IDCompositionSurface
	dcompSurfaceEndDraw              = 4

	dxgiFormatB8G8R8A8     = 87
	dxgiAlphaPremultiplied = 1

	wmTimer = 0x0113
	// timerCompose is the timer of the application window that draws
	// again the controls that could not draw (recompose).
	timerCompose = 1
	infinite     = 0xFFFFFFFF
)

// composition is the backend's Direct3D 11 device and DirectComposition
// device, shared by the windows that show through it.
type composition struct {
	d3d, ctx, dcomp uintptr
	software        bool
	// removed is the event that tells the GPU device was removed, which a
	// goroutine waits for (watch), and cookie its registration; removed is
	// 0 without ID3D11Device4 (Windows 10 before 1607).
	removed uintptr
	cookie  uint32
}

// composition returns the device, made on first use, or nil when it cannot
// be made; after a failure it tries again at compRetry at the soonest, a
// second later, then longer after each failure in a row.
func (b *Backend) composition() *composition {
	if b.comp != nil {
		return b.comp
	}
	if time.Now().Before(b.compRetry) {
		return nil
	}
	c, err := newComposition()
	if err != nil {
		log.Printf("mygo: no DirectComposition: %v", err)
		b.compRetry = time.Now().Add(backoff(b.compFails))
		b.compFails++
		return nil
	}
	if c.software {
		log.Print("mygo: DirectComposition draws with WARP, without the GPU")
	}
	c.watch(b.appHwnd)
	b.comp = c
	return c
}

func newComposition() (*composition, error) {
	c := &composition{}
	var err error
	if c.d3d, c.ctx, c.software, err = device.New(); err != nil {
		return nil, err
	}
	if err := procDCompositionCreateDevice.Find(); err != nil {
		c.free()
		return nil, err
	}
	dxgi := queryInterface(c.d3d, &iidIDXGIDevice)
	if dxgi == 0 {
		c.free()
		return nil, hresultError("querying the DXGI device", 0x80004002) // E_NOINTERFACE
	}
	hr, _, _ := procDCompositionCreateDevice.Call(dxgi, uintptr(unsafe.Pointer(&iidIDCompositionDevice)), uintptr(unsafe.Pointer(&c.dcomp)))
	release(dxgi)
	if failed(hr) {
		c.free()
		return nil, hresultError("DCompositionCreateDevice", hr)
	}
	return c, nil
}

// backoff is how long to wait before trying again after fails failures in
// a row: a second, doubling up to a minute.
func backoff(fails int) time.Duration { return min(time.Second<<min(fails, 6), time.Minute) }

// watch has a goroutine wait for the GPU device to be removed and tell the
// application window (deviceRemoved). free signals the event too, which
// ends the goroutine; the main thread closes it.
func (c *composition) watch(appHwnd uintptr) {
	dev := queryInterface(c.d3d, &iidID3D11Device4)
	if dev == 0 {
		return
	}
	defer release(dev)
	event, _, _ := procCreateEventW.Call(0, 0, 0, 0) // auto-reset, not signaled
	if event == 0 {
		return
	}
	if failed(comCall(dev, d3dDevRegisterDeviceRemovedEvent, event, uintptr(unsafe.Pointer(&c.cookie)))) {
		procCloseHandle.Call(event)
		return
	}
	c.removed = event
	go func() {
		procWaitForSingleObject.Call(event, infinite)
		postMessage(appHwnd, wmAppDeviceRemoved, event, 0)
	}()
}

// deviceRemoved handles the message of a goroutine of watch, whose event
// was signaled: by the GPU device, gone, or by free.
func (b *Backend) deviceRemoved(event uintptr) {
	if b.comp != nil && b.comp.removed == event {
		b.loseComposition(errors.New("the GPU device was removed"))
	}
	procCloseHandle.Call(event)
}

// lost reports whether the DirectComposition device is no longer valid, as
// when its GPU device was removed.
func (c *composition) lost() bool {
	var valid int32
	if failed(comCall(c.dcomp, dcompDevCheckDeviceState, uintptr(unsafe.Pointer(&valid)))) {
		return true
	}
	return valid == 0
}

func (c *composition) free() {
	if c.removed != 0 {
		if dev := queryInterface(c.d3d, &iidID3D11Device4); dev != 0 {
			comCall(dev, d3dDevUnregisterDeviceRemoved, uintptr(c.cookie))
			release(dev)
		}
		procSetEvent.Call(c.removed) // ends the goroutine of watch
		c.removed = 0
	}
	for _, p := range []*uintptr{&c.dcomp, &c.ctx, &c.d3d} {
		if *p != 0 {
			release(*p)
			*p = 0
		}
	}
}

// loseComposition drops the device after it failed, as when the GPU was
// reset, and what every window made with it. The controls draw again with
// a new device at once, or later when it fails again in a row.
func (b *Backend) loseComposition(err error) {
	log.Printf("mygo: DirectComposition failed, starting over: %v", err)
	for hwnd, c := range b.captions {
		if hwnd == c.buttons && c.comp != nil {
			c.comp.free()
		}
	}
	for _, s := range b.surfaces {
		if s.comp != nil {
			s.comp.free()
		}
	}
	if b.comp != nil {
		b.comp.free()
		b.comp = nil
	}
	b.compRetry = time.Now()
	if b.compFails > 0 {
		b.compRetry = b.compRetry.Add(backoff(b.compFails - 1))
	}
	b.compFails++
	b.composeLater(b.compRetry)
}

// composeLater has the controls that could not draw draw again at the
// given time, or at an earlier one already set.
func (b *Backend) composeLater(at time.Time) {
	if !b.composeAt.IsZero() && !at.Before(b.composeAt) {
		return
	}
	b.composeAt = at
	ms := max((time.Until(at)+time.Millisecond-1)/time.Millisecond, 0)
	procSetTimer.Call(b.appHwnd, timerCompose, uintptr(ms), 0)
}

// recompose draws again, on timerCompose, the controls and the frames
// drawn in memory that do not show through the device: it was lost, or
// they could not draw.
func (b *Backend) recompose() {
	procKillTimer.Call(b.appHwnd, timerCompose)
	b.composeAt = time.Time{}
	for hwnd, c := range b.captions {
		if hwnd != c.buttons || c.comp == nil || c.comp.dev != nil {
			continue
		}
		if time.Now().Before(c.comp.retry) {
			b.composeLater(c.comp.retry)
			continue
		}
		c.paint()
	}
	// Frames drawn in memory that could not show draw again.
	for _, s := range b.surfaces {
		if s.comp == nil || s.comp.dev != nil {
			continue
		}
		if time.Now().Before(s.comp.retry) {
			b.composeLater(s.comp.retry)
			continue
		}
		s.RequestFrame()
	}
}

// compositor shows a bitmap in one window through DirectComposition.
type compositor struct {
	b                       *Backend
	hwnd                    uintptr
	dev                     *composition // that made target, visual and surface, nil until it draws
	target, visual, surface uintptr
	w, h                    int32
	px                      []uint32 // the pixels to show, kept from one paint to the next
	// radius rounds the window's corners, in its pixels, 0 while it is
	// square. The corners are taken off the pixels themselves (roundPixels):
	// a clip of the visual's geometry edges the arc at the pixel grid, and
	// Windows 10's DWM gives a window square corners whatever
	// WindowOptions.CornerRadius says.
	radius int32
	// retry is when to draw again after drawing failed fails times in a
	// row, with a device that stayed valid.
	retry time.Time
	fails int
}

func newCompositor(b *Backend, hwnd uintptr) *compositor {
	c := &compositor{b: b, hwnd: hwnd}
	if w := b.windows[hwnd]; w != nil {
		c.radius = w.cornerRadius
	}
	return c
}

// pixels returns room for n pixels.
func (c *compositor) pixels(n int) []uint32 {
	if cap(c.px) < n {
		c.px = make([]uint32, n)
	}
	return c.px[:n]
}

// show puts px, top-down rows of premultiplied BGRA w wide, on the window.
// When it cannot, the window draws again later (recompose); a device found
// invalid gives way to a new one (loseComposition).
func (c *compositor) show(px []uint32, w, h int32) {
	if w <= 0 || h <= 0 || len(px) < int(w)*int(h) {
		return
	}
	b := c.b
	dev := b.composition()
	if dev == nil {
		b.composeLater(b.compRetry)
		return
	}
	err := c.draw(dev, px, w, h)
	if err == nil {
		b.compFails, c.fails = 0, 0
		return
	}
	c.free()
	if dev.lost() {
		b.loseComposition(err)
		return
	}
	// The device is fine: this window alone tries again.
	log.Printf("mygo: cannot show a window's pixels through DirectComposition: %v", err)
	c.retry = time.Now().Add(backoff(c.fails))
	c.fails++
	b.composeLater(c.retry)
}

func (c *compositor) draw(dev *composition, px []uint32, w, h int32) error {
	if c.dev != dev {
		c.free()
		if hr := comCall(dev.dcomp, dcompDevCreateTargetForHwnd, c.hwnd, 1, uintptr(unsafe.Pointer(&c.target))); failed(hr) {
			return hresultError("IDCompositionDevice::CreateTargetForHwnd", hr)
		}
		if hr := comCall(dev.dcomp, dcompDevCreateVisual, uintptr(unsafe.Pointer(&c.visual))); failed(hr) {
			return hresultError("IDCompositionDevice::CreateVisual", hr)
		}
		if hr := comCall(c.target, dcompTargetSetRoot, c.visual); failed(hr) {
			return hresultError("IDCompositionTarget::SetRoot", hr)
		}
		c.dev = dev
	}
	if c.surface == 0 || w != c.w || h != c.h {
		release(c.surface)
		c.surface = 0
		if hr := comCall(dev.dcomp, dcompDevCreateSurface, uintptr(w), uintptr(h), dxgiFormatB8G8R8A8, dxgiAlphaPremultiplied,
			uintptr(unsafe.Pointer(&c.surface))); failed(hr) {
			return hresultError("IDCompositionDevice::CreateSurface", hr)
		}
		c.w, c.h = w, h
		if hr := comCall(c.visual, dcompVisualSetContent, c.surface); failed(hr) {
			return hresultError("IDCompositionVisual::SetContent", hr)
		}
	}
	c.roundPixels(px, w, h)
	var tex uintptr
	var offset point
	if hr := comCall(c.surface, dcompSurfaceBeginDraw, 0, uintptr(unsafe.Pointer(&iidID3D11Texture2D)),
		uintptr(unsafe.Pointer(&tex)), uintptr(unsafe.Pointer(&offset))); failed(hr) {
		return hresultError("IDCompositionSurface::BeginDraw", hr)
	}
	// The surface can be part of an atlas: draw at the offset given.
	box := [6]uint32{uint32(offset.X), uint32(offset.Y), 0, uint32(offset.X + w), uint32(offset.Y + h), 1}
	comCall(dev.ctx, d3dCtxUpdateSubresource, tex, 0, uintptr(unsafe.Pointer(&box)), uintptr(unsafe.Pointer(&px[0])), uintptr(w*4), 0)
	release(tex)
	if hr := comCall(c.surface, dcompSurfaceEndDraw); failed(hr) {
		return hresultError("IDCompositionSurface::EndDraw", hr)
	}
	if hr := comCall(dev.dcomp, dcompDevCommit); failed(hr) {
		return hresultError("IDCompositionDevice::Commit", hr)
	}
	return nil
}

// roundPixels takes the corners off px, top-down rows of premultiplied BGRA
// w wide and h high, at the window's radius: every pixel of the four corners
// keeps the part of its alpha that the rounded rectangle covers, and with
// premultiplied alpha the colour goes with it. That leaves the arc smooth —
// a pixel at the edge is partly the window and partly the desktop behind it,
// which the window shows through — and no pixel outside the arc at all.
//
// The arc is drawn here rather than clipped by the visual's geometry because
// DirectComposition's clips are hard-edged: an IDCompositionRectangleClip
// takes the corners of the visual but leaves the arc a stair of whole pixels,
// which is the same jagged edge a window region gives.
func (c *compositor) roundPixels(px []uint32, w, h int32) {
	r := c.radius
	if r <= 0 || w < 2*r || h < 2*r {
		return
	}
	rf := float64(r)
	for _, corner := range [4][2]int32{
		{0, 0}, {w - r, 0}, {0, h - r}, {w - r, h - r},
	} {
		x0, y0 := corner[0], corner[1]
		for y := int32(0); y < r; y++ {
			for x := int32(0); x < r; x++ {
				// How much of this pixel the quarter disc of radius r, its
				// centre at (r, r) of this corner, covers: the distance from
				// that centre, offset by half a pixel for the pixel's middle.
				dx, dy := rf-float64(x)-0.5, rf-float64(y)-0.5
				cover := rf - math.Sqrt(dx*dx+dy*dy)
				if cover <= 0 {
					px[(y0+y)*w+x0+x] = 0
					continue
				}
				if cover >= 1 {
					continue
				}
				p := px[(y0+y)*w+x0+x]
				px[(y0+y)*w+x0+x] = uint32(float64(p>>24)*cover)<<24 |
					uint32(float64((p>>16)&0xFF)*cover)<<16 |
					uint32(float64((p>>8)&0xFF)*cover)<<8 |
					uint32(float64(p&0xFF)*cover)
			}
		}
	}
}

func (c *compositor) free() {
	for _, p := range []*uintptr{&c.surface, &c.visual, &c.target} {
		if *p != 0 {
			release(*p)
			*p = 0
		}
	}
	c.dev, c.w, c.h = nil, 0, 0
}
