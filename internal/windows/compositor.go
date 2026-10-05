//go:build windows && (amd64 || arm64)

package windows

import (
	"log"
	"time"
	"unsafe"

	"github.com/egoist/mygo/internal/gpu/d3d11"
)

// A window with a material behind it has no redirection bitmap (see
// window.styles), which hides what GDI and UpdateLayeredWindow draw in its
// child windows, the controls of a hidden title bar among them. Those show
// through DirectComposition instead: a surface of premultiplied BGRA on a
// layered window without a redirection bitmap either, which blends over
// the webview as a layered window does.

var (
	procDCompositionCreateDevice = systemDLL("dcomp.dll").NewProc("DCompositionCreateDevice")

	iidIDXGIDevice         = guid("54ec77fa-1377-44e6-8c32-88fd5f44c84c")
	iidIDXGISurface        = guid("cafcb56c-6ac3-4889-bf47-9e23bbd260ec")
	iidID3D11Texture2D     = guid("6f15aaf2-d208-4e89-9ab4-489535d34f9c")
	iidIDCompositionDevice = guid("c37ea93a-e7aa-450d-b16f-9746cb0407f3")
)

// Vtable indices, from d3d11.h and dcomp.h.
const (
	d3dCtxUpdateSubresource     = 48 // ID3D11DeviceContext
	dcompDevCommit              = 3  // IDCompositionDevice
	dcompDevCreateTargetForHwnd = 6
	dcompDevCreateVisual        = 7
	dcompDevCreateSurface       = 8
	dcompTargetSetRoot          = 3  // IDCompositionTarget
	dcompVisualSetContent       = 15 // IDCompositionVisual, whose overloads take a slot each
	dcompSurfaceBeginDraw       = 3  // IDCompositionSurface
	dcompSurfaceEndDraw         = 4

	dxgiFormatB8G8R8A8     = 87
	dxgiAlphaPremultiplied = 1
)

// composition is the backend's Direct3D 11 device and DirectComposition
// device, shared by the windows that show through it.
type composition struct {
	d3d, ctx, dcomp uintptr
	software        bool
}

// composition returns the device, made on first use, or nil when it cannot
// be made; after a failure it tries again a second later at the soonest.
func (b *Backend) composition() *composition {
	if b.comp != nil {
		return b.comp
	}
	if time.Now().Before(b.compRetry) {
		return nil
	}
	c := &composition{}
	var err error
	c.d3d, c.ctx, c.software, err = d3d11.NewDevice()
	if err == nil {
		if dxgi := queryInterface(c.d3d, &iidIDXGIDevice); dxgi != 0 {
			if err = procDCompositionCreateDevice.Find(); err == nil {
				hr, _, _ := procDCompositionCreateDevice.Call(dxgi, uintptr(unsafe.Pointer(&iidIDCompositionDevice)), uintptr(unsafe.Pointer(&c.dcomp)))
				if failed(hr) {
					err = hresultError("DCompositionCreateDevice", hr)
				}
			}
			release(dxgi)
		} else {
			err = hresultError("querying the DXGI device", 0x80004002) // E_NOINTERFACE
		}
	}
	if err != nil {
		c.free()
		b.compRetry = time.Now().Add(time.Second)
		log.Printf("mygo: no DirectComposition: %v", err)
		return nil
	}
	if c.software {
		log.Print("mygo: DirectComposition draws with WARP, without the GPU")
	}
	b.comp = c
	return c
}

func (c *composition) free() {
	for _, p := range []*uintptr{&c.dcomp, &c.ctx, &c.d3d} {
		if *p != 0 {
			release(*p)
			*p = 0
		}
	}
}

// loseComposition drops the device after it failed, as when the GPU was
// reset, and what was made with it; the next draw makes them again.
func (b *Backend) loseComposition(err error) {
	log.Printf("mygo: DirectComposition failed, starting over: %v", err)
	for _, c := range b.captions {
		if c.comp != nil {
			c.comp.free()
		}
	}
	if b.comp != nil {
		b.comp.free()
		b.comp = nil
	}
}

// compositor shows a bitmap in one window through DirectComposition.
type compositor struct {
	b                       *Backend
	hwnd                    uintptr
	dev                     *composition // that made target, visual and surface
	target, visual, surface uintptr
	w, h                    int32
}

func newCompositor(b *Backend, hwnd uintptr) *compositor {
	return &compositor{b: b, hwnd: hwnd}
}

// show puts px, top-down rows of premultiplied BGRA w wide, on the window,
// and reports whether it did. A failed device gives way to a new one at
// once, in case the GPU is back already or WARP can draw.
func (c *compositor) show(px []uint32, w, h int32) bool {
	if w <= 0 || h <= 0 || len(px) < int(w*h) {
		return false
	}
	for try := 0; try < 2; try++ {
		dev := c.b.composition()
		if dev == nil {
			return false
		}
		err := c.draw(dev, px, w, h)
		if err == nil {
			return true
		}
		c.b.loseComposition(err)
	}
	return false
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
		comCall(c.target, dcompTargetSetRoot, c.visual)
		c.dev = dev
	}
	if c.surface == 0 || w != c.w || h != c.h {
		if c.surface != 0 {
			release(c.surface)
			c.surface = 0
		}
		if hr := comCall(dev.dcomp, dcompDevCreateSurface, uintptr(w), uintptr(h), dxgiFormatB8G8R8A8, dxgiAlphaPremultiplied,
			uintptr(unsafe.Pointer(&c.surface))); failed(hr) {
			return hresultError("IDCompositionDevice::CreateSurface", hr)
		}
		c.w, c.h = w, h
		comCall(c.visual, dcompVisualSetContent, c.surface)
	}
	var dxgiSurface uintptr
	var offset point
	if hr := comCall(c.surface, dcompSurfaceBeginDraw, 0, uintptr(unsafe.Pointer(&iidIDXGISurface)),
		uintptr(unsafe.Pointer(&dxgiSurface)), uintptr(unsafe.Pointer(&offset))); failed(hr) {
		return hresultError("IDCompositionSurface::BeginDraw", hr)
	}
	if tex := queryInterface(dxgiSurface, &iidID3D11Texture2D); tex != 0 {
		// The surface can be part of an atlas: draw at the offset given.
		box := [6]uint32{uint32(offset.X), uint32(offset.Y), 0, uint32(offset.X + w), uint32(offset.Y + h), 1}
		comCall(dev.ctx, d3dCtxUpdateSubresource, tex, 0, uintptr(unsafe.Pointer(&box)), uintptr(unsafe.Pointer(&px[0])), uintptr(w*4), 0)
		release(tex)
	}
	release(dxgiSurface)
	if hr := comCall(c.surface, dcompSurfaceEndDraw); failed(hr) {
		return hresultError("IDCompositionSurface::EndDraw", hr)
	}
	if hr := comCall(dev.dcomp, dcompDevCommit); failed(hr) {
		return hresultError("IDCompositionDevice::Commit", hr)
	}
	return nil
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
