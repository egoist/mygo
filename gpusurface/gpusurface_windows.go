//go:build windows

package gpusurface

import (
	"cmp"
	"errors"
	"fmt"
	"sync/atomic"
	"syscall"
	"unsafe"

	"github.com/egoist/mygo/internal/gpu/d3d11/device"
)

const (
	release = 2
	// ID3D11Device
	devCreateBuffer             = 3
	devCreateShaderResourceView = 7
	devCreateRenderTargetView   = 9
	devCreateVertexShader       = 12
	devCreatePixelShader        = 15
	devCreateSamplerState       = 23
	// ID3D11DeviceContext
	ctxPSSetShaderResources   = 8
	ctxPSSetShader            = 9
	ctxPSSetSamplers          = 10
	ctxVSSetShader            = 11
	ctxDraw                   = 13
	ctxVSSetConstantBuffers   = 7
	ctxPSSetConstantBuffers   = 16
	ctxIASetInputLayout       = 17
	ctxIASetPrimitiveTopology = 24
	ctxOMSetRenderTargets     = 33
	ctxRSSetViewports         = 44
	ctxClearRenderTargetView  = 50
	ctxFlush                  = 111
	// ID3D11Texture2D
	texGetDesc = 10
	// DXGI
	dxgiDevGetAdapter      = 7
	adapterGetDesc         = 8
	dxgiDevSetMaxLatency   = 12
	dxgiGetParent          = 6
	factoryMakeWindowAssoc = 8
	factoryCreateSwapChain = 15 // CreateSwapChainForHwnd
	factory5CheckFeature   = 28
	scPresent              = 8
	scGetBuffer            = 9
	scResizeBuffers        = 13

	formatB8G8R8A8Unorm = 87
	formatR8Unorm       = 61
	formatR8G8Unorm     = 49
	formatR16Unorm      = 56
	formatR16G16Unorm   = 35
	formatNV12          = 103
	formatP010          = 104

	swapEffectFlipDiscard = 4
	usageRenderTarget     = 0x20
	swapChainTearing      = 0x800 // DXGI_SWAP_CHAIN_FLAG_ALLOW_TEARING
	presentTearing        = 0x200 // DXGI_PRESENT_ALLOW_TEARING
	mwaNoAltEnter         = 2
	topologyTriangleList  = 4
	dimensionTexture2D    = 4
	dimensionTexture2DArr = 5
	bindConstantBuffer    = 0x4
	usageImmutable        = 1

	// swapChainBuffers: one on screen, one queued for DWM and one drawn,
	// so that a frame replaces the one queued rather than wait for the
	// screen to give its buffer back (as MyGo's uncapped renderer).
	swapChainBuffers = 3
)

type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

var (
	iidIDXGIDevice     = guid{0x54ec77fa, 0x1377, 0x44e6, [8]byte{0x8c, 0x32, 0x88, 0xfd, 0x5f, 0x44, 0xc8, 0x4c}}
	iidIDXGIDevice1    = guid{0x77db970f, 0x6276, 0x48ba, [8]byte{0xba, 0x28, 0x07, 0x01, 0x43, 0xb4, 0x39, 0x2c}}
	iidIDXGIFactory2   = guid{0x50c83a1c, 0xe072, 0x4c48, [8]byte{0x87, 0xb0, 0x36, 0x30, 0xfa, 0x36, 0xa6, 0xd0}}
	iidIDXGIFactory5   = guid{0x7632e1f5, 0xee65, 0x4dca, [8]byte{0x87, 0xfd, 0x84, 0xcd, 0x75, 0xf8, 0x83, 0x8d}}
	iidID3D11Texture2D = guid{0x6f15aaf2, 0xd208, 0x4e89, [8]byte{0x9a, 0xb4, 0x48, 0x95, 0x35, 0xd3, 0x4f, 0x9c}}
)

var (
	kernel32       = syscall.NewLazyDLL(device.SystemDir() + `\kernel32.dll`)
	procQPC        = kernel32.NewProc("QueryPerformanceCounter")
	procQPF        = kernel32.NewProc("QueryPerformanceFrequency")
	procD3DCompile = syscall.NewLazyDLL(device.SystemDir() + `\d3dcompiler_47.dll`).NewProc("D3DCompile")
	qpcFrequency   int64
)

// DirectComposition, for Options.Composite.
const (
	factoryCreateSwapChainComp = 24 // CreateSwapChainForComposition
	alphaModeIgnore            = 3
	// IDCompositionDevice, IDCompositionTarget, IDCompositionVisual
	dcompCommit           = 3
	dcompCreateTarget     = 6 // CreateTargetForHwnd
	dcompCreateVisual     = 7
	dcompTargetSetRoot    = 3
	dcompVisualSetContent = 15 // the overload taking an IUnknown
)

var (
	iidIDCompositionDevice       = guid{0xc37ea93a, 0xe7aa, 0x450d, [8]byte{0xb1, 0x6f, 0x97, 0x46, 0xcb, 0x04, 0x07, 0xf3}}
	procDCompositionCreateDevice = syscall.NewLazyDLL(device.SystemDir() + `\dcomp.dll`).NewProc("DCompositionCreateDevice")
	procGetClientRect            = syscall.NewLazyDLL(device.SystemDir() + `\user32.dll`).NewProc("GetClientRect")
)

func init() { procQPF.Call(uintptr(unsafe.Pointer(&qpcFrequency))) }

// Now returns the time in nanoseconds of the clock Timing uses: the
// performance counter.
func Now() int64 {
	var t int64
	syscall.SyscallN(procQPC.Addr(), uintptr(unsafe.Pointer(&t)))
	return qpcToNs(t)
}

func qpcToNs(t int64) int64 {
	return t/qpcFrequency*1e9 + t%qpcFrequency*1e9/qpcFrequency
}

func ptr(a uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&a)) }

// call calls method i of a COM object; the pointers passed as uintptr stay
// valid until it returns.
//
//go:uintptrescapes
func call(obj uintptr, i int, args ...uintptr) uintptr {
	vtbl := *(*uintptr)(ptr(obj))
	fn := *(*uintptr)(unsafe.Add(ptr(vtbl), i*int(unsafe.Sizeof(uintptr(0)))))
	var a [16]uintptr
	a[0] = obj
	n := copy(a[1:], args)
	r, _, _ := syscall.SyscallN(fn, a[:n+1]...)
	return r
}

// fastCall is call for the hot path, with up to five arguments: call's
// variadic arguments, which go:uintptrescapes moves to the heap, cost an
// allocation a call. The pointers passed must not point into a goroutine
// stack, which may move before the call: only to the heap or C memory.
func fastCall(obj uintptr, i int, a, b, c, d, e uintptr) uintptr {
	vtbl := *(*uintptr)(ptr(obj))
	fn := *(*uintptr)(unsafe.Add(ptr(vtbl), i*int(unsafe.Sizeof(uintptr(0)))))
	r, _, _ := syscall.SyscallN(fn, obj, a, b, c, d, e)
	return r
}

func failed(hr uintptr) bool { return int32(uint32(hr)) < 0 }

func free(obj *uintptr) {
	if *obj != 0 {
		call(*obj, release)
		*obj = 0
	}
}

type swapChainDesc1 struct {
	Width, Height, Format          uint32
	Stereo                         int32
	SampleCount, SampleQuality     uint32
	BufferUsage, BufferCount       uint32
	Scaling, SwapEffect, AlphaMode uint32
	Flags                          uint32
}

type texture2DDesc struct {
	Width, Height, MipLevels, ArraySize, Format uint32
	SampleCount, SampleQuality                  uint32
	Usage, BindFlags, CPUAccessFlags, MiscFlags uint32
}

type srvDesc struct {
	Format, ViewDimension uint32
	MostDetailedMip       uint32
	MipLevels             uint32
	// FirstArraySlice and ArraySize are read for a texture array.
	FirstArraySlice, ArraySize uint32
}

type bufferDesc struct {
	ByteWidth, Usage, BindFlags, CPUAccessFlags, MiscFlags, StructureByteStride uint32
}

type subresourceData struct {
	SysMem                   uintptr
	SysMemPitch, SysMemSlice uint32
}

type samplerDesc struct {
	Filter                       uint32
	AddressU, AddressV, AddressW uint32
	MipLODBias                   float32
	MaxAnisotropy                uint32
	ComparisonFunc               uint32
	BorderColor                  [4]float32
	MinLOD, MaxLOD               float32
}

type viewport struct{ X, Y, W, H, MinDepth, MaxDepth float32 }

// yuvParams converts the video range samples of a frame to full range: Y'
// = (y - YOffset) * YScale, Cb, Cr = (c - COffset) * CScale.
type yuvParams struct{ YOffset, YScale, COffset, CScale float32 }

var (
	params8  = yuvParams{16.0 / 255, 255.0 / 219, 128.0 / 255, 255.0 / 224}
	params10 = yuvParams{64 * 64 / 65535.0, 65535.0 / (876 * 64), 512 * 64 / 65535.0, 65535.0 / (896 * 64)}
)

// uvScale crops the frame: the part of the texture inside the picture's
// visible size (a decoder's textures are aligned past it).
type uvScale struct{ U, V, _, _ float32 }

const shaderSource = `
struct VOut { float4 pos : SV_Position; float2 uv : TEXCOORD0; };
cbuffer Crop : register(b0) { float2 uvScale; };
VOut vs(uint id : SV_VertexID) {
	float2 p = float2((id << 1) & 2, id & 2);
	VOut o;
	o.pos = float4(p * float2(2, -2) + float2(-1, 1), 0, 1);
	o.uv = p * uvScale;
	return o;
}
Texture2D<float4> t0 : register(t0);
Texture2D<float4> t1 : register(t1);
SamplerState smp : register(s0);
cbuffer YUVParams : register(b0) { float yOffset, yScale, cOffset, cScale; };
float4 ps_rgb(VOut i) : SV_Target { return float4(t0.Sample(smp, i.uv).rgb, 1); }
float4 ps_yuv(VOut i) : SV_Target {
	float l = (t0.Sample(smp, i.uv).r - yOffset) * yScale;
	float2 c = (t1.Sample(smp, i.uv).rg - cOffset) * cScale;
	// BT.709
	return float4(l + 1.5748 * c.y, l - 0.1873 * c.x - 0.4681 * c.y, l + 1.8556 * c.x, 1);
}
`

// frameViews is a frame of the pool with views of it: its BGRA, or its
// luma and chroma, of one slice of the texture.
type frameViews struct {
	tex           uintptr
	slice         int
	width, height int
	srv           [2]uintptr
	// visibleW and visibleH are the size of the picture shown.
	visibleW, visibleH int
	format             Format
	params             uintptr // constant buffer
	crop               uintptr // constant buffer of uvScale
}

const wrapCache = 16

// Surface presents frames into a window.
type Surface struct {
	hwnd                 uintptr
	device, ctx, factory uintptr
	swapChain, rtv       uintptr
	vs, psRGB, psYUV     uintptr
	sampler              uintptr
	params8, params10    uintptr
	vp                   viewport
	black                [4]float32
	tearing              bool
	adapterName          string
	width, height        int

	// dcomp, target and visual show the swap chain with Options.Composite.
	dcomp, target, visual uintptr

	cache    [wrapCache]frameViews
	nextSlot int
	noViews  [2]uintptr

	// timings has no Presented: DXGI's frame statistics, read after each
	// present, gave times before the frame was submitted, composed by DWM,
	// in a multiplane overlay and across adapters alike.
	timings     timings
	onPresented atomic.Pointer[func(tag, submitted, presented int64)]
}

// New makes a surface with a swap chain of its own on opts.Window, an
// HWND, which has none unless opts.Composite.
func New(opts Options) (s *Surface, err error) {
	if opts.Window == 0 {
		return nil, errors.New("gpusurface: no window")
	}
	s = &Surface{hwnd: opts.Window}
	s.timings.recs = make([]Timing, opts.Timings)
	defer func() {
		if err != nil {
			s.Close()
			s = nil
		}
	}()
	if opts.Adapter == "" {
		s.device, s.ctx, err = device.NewOnGPU()
	} else {
		s.device, s.ctx, err = newDeviceOn(opts.Adapter)
	}
	if err != nil {
		return nil, err
	}
	var dxgiDev, adapter uintptr
	if failed(call(s.device, 0, uintptr(unsafe.Pointer(&iidIDXGIDevice)), uintptr(unsafe.Pointer(&dxgiDev)))) {
		return nil, errors.New("gpusurface: no DXGI device")
	}
	defer free(&dxgiDev)
	var dxgiDev1 uintptr
	if !failed(call(s.device, 0, uintptr(unsafe.Pointer(&iidIDXGIDevice1)), uintptr(unsafe.Pointer(&dxgiDev1)))) {
		// The CPU queues one frame ahead of the GPU at most.
		call(dxgiDev1, dxgiDevSetMaxLatency, 1)
		free(&dxgiDev1)
	}
	if failed(call(dxgiDev, dxgiDevGetAdapter, uintptr(unsafe.Pointer(&adapter)))) {
		return nil, errors.New("gpusurface: no adapter")
	}
	defer free(&adapter)
	var ad adapterDesc1 // DXGI_ADAPTER_DESC is its start
	call(adapter, adapterGetDesc, uintptr(unsafe.Pointer(&ad)))
	s.adapterName = syscall.UTF16ToString(ad.Description[:])
	if failed(call(adapter, dxgiGetParent, uintptr(unsafe.Pointer(&iidIDXGIFactory2)), uintptr(unsafe.Pointer(&s.factory)))) {
		return nil, errors.New("gpusurface: DXGI 1.2 is required")
	}
	s.tearing = tearingSupported(s.factory)
	desc := swapChainDesc1{Format: formatB8G8R8A8Unorm, SampleCount: 1, BufferUsage: usageRenderTarget,
		BufferCount: swapChainBuffers, SwapEffect: swapEffectFlipDiscard}
	if s.tearing {
		desc.Flags = swapChainTearing
	}
	if opts.Composite {
		if err = s.compose(dxgiDev, desc); err != nil {
			return nil, err
		}
	} else {
		if hr := call(s.factory, factoryCreateSwapChain, s.device, s.hwnd, uintptr(unsafe.Pointer(&desc)), 0, 0, uintptr(unsafe.Pointer(&s.swapChain))); failed(hr) {
			return nil, fmt.Errorf("gpusurface: cannot create the swap chain: %#x", uint32(hr))
		}
		call(s.factory, factoryMakeWindowAssoc, s.hwnd, mwaNoAltEnter)
	}
	if err = s.viewBackBuffer(); err != nil {
		return nil, err
	}
	if err = s.makeShaders(); err != nil {
		return nil, err
	}
	return s, nil
}

// compose makes a swap chain for composition of the window's client size
// and shows it as the root visual of a target on the window that is not
// topmost, on a DirectComposition device of the surface's own, as MyGo
// shows web views under a window's frames. The swap chain's frames show as
// it presents them, with no commit.
func (s *Surface) compose(dxgiDev uintptr, desc swapChainDesc1) error {
	if err := procDCompositionCreateDevice.Find(); err != nil {
		return fmt.Errorf("gpusurface: no DirectComposition: %w", err)
	}
	if hr, _, _ := procDCompositionCreateDevice.Call(dxgiDev, uintptr(unsafe.Pointer(&iidIDCompositionDevice)), uintptr(unsafe.Pointer(&s.dcomp))); failed(hr) {
		return fmt.Errorf("gpusurface: cannot create the DirectComposition device: %#x", uint32(hr))
	}
	desc.Width, desc.Height = s.clientSize()
	desc.AlphaMode = alphaModeIgnore
	hr := call(s.factory, factoryCreateSwapChainComp, s.device, uintptr(unsafe.Pointer(&desc)), 0, uintptr(unsafe.Pointer(&s.swapChain)))
	if failed(hr) && s.tearing {
		s.tearing, desc.Flags = false, 0
		hr = call(s.factory, factoryCreateSwapChainComp, s.device, uintptr(unsafe.Pointer(&desc)), 0, uintptr(unsafe.Pointer(&s.swapChain)))
	}
	if failed(hr) {
		return fmt.Errorf("gpusurface: cannot create the swap chain for composition: %#x", uint32(hr))
	}
	if hr := call(s.dcomp, dcompCreateTarget, s.hwnd, 0, uintptr(unsafe.Pointer(&s.target))); failed(hr) {
		return fmt.Errorf("gpusurface: cannot compose the window: %#x", uint32(hr))
	}
	if hr := call(s.dcomp, dcompCreateVisual, uintptr(unsafe.Pointer(&s.visual))); failed(hr) {
		return fmt.Errorf("gpusurface: cannot create the visual: %#x", uint32(hr))
	}
	if hr := call(s.visual, dcompVisualSetContent, s.swapChain); failed(hr) {
		return fmt.Errorf("gpusurface: cannot show the swap chain in the visual: %#x", uint32(hr))
	}
	if hr := call(s.target, dcompTargetSetRoot, s.visual); failed(hr) {
		return fmt.Errorf("gpusurface: cannot show the visual on the window: %#x", uint32(hr))
	}
	if hr := call(s.dcomp, dcompCommit); failed(hr) {
		return fmt.Errorf("gpusurface: cannot commit the composition: %#x", uint32(hr))
	}
	return nil
}

// clientSize returns the window's client size in pixels, at least 1 x 1.
func (s *Surface) clientSize() (w, h uint32) {
	var r struct{ Left, Top, Right, Bottom int32 }
	procGetClientRect.Call(s.hwnd, uintptr(unsafe.Pointer(&r)))
	return uint32(max(r.Right-r.Left, 1)), uint32(max(r.Bottom-r.Top, 1))
}

// tearingSupported reports whether the system shows frames presented with
// tearing (DXGI 1.5).
func tearingSupported(factory uintptr) bool {
	var f5 uintptr
	if failed(call(factory, 0, uintptr(unsafe.Pointer(&iidIDXGIFactory5)), uintptr(unsafe.Pointer(&f5)))) {
		return false
	}
	defer free(&f5)
	var allow int32
	if failed(call(f5, factory5CheckFeature, 0, uintptr(unsafe.Pointer(&allow)), unsafe.Sizeof(allow))) {
		return false
	}
	return allow != 0
}

func (s *Surface) makeShaders() error {
	for _, sh := range []struct {
		entry, target string
		vtbl          int
		out           *uintptr
	}{{"vs", "vs_4_0", devCreateVertexShader, &s.vs}, {"ps_rgb", "ps_4_0", devCreatePixelShader, &s.psRGB}, {"ps_yuv", "ps_4_0", devCreatePixelShader, &s.psYUV}} {
		code, err := compile(shaderSource, sh.entry, sh.target)
		if err != nil {
			return err
		}
		if failed(call(s.device, sh.vtbl, uintptr(unsafe.Pointer(&code[0])), uintptr(len(code)), 0, uintptr(unsafe.Pointer(sh.out)))) {
			return fmt.Errorf("gpusurface: cannot create the %s shader", sh.entry)
		}
	}
	sd := samplerDesc{Filter: 0x15, AddressU: 3, AddressV: 3, AddressW: 3, MaxLOD: 3.402823466e+38}
	if failed(call(s.device, devCreateSamplerState, uintptr(unsafe.Pointer(&sd)), uintptr(unsafe.Pointer(&s.sampler)))) {
		return errors.New("gpusurface: cannot create the sampler")
	}
	for _, p := range []struct {
		v   *yuvParams
		out *uintptr
	}{{&params8, &s.params8}, {&params10, &s.params10}} {
		bd := bufferDesc{ByteWidth: uint32(unsafe.Sizeof(*p.v)), Usage: usageImmutable, BindFlags: bindConstantBuffer}
		data := subresourceData{SysMem: uintptr(unsafe.Pointer(p.v))}
		if failed(call(s.device, devCreateBuffer, uintptr(unsafe.Pointer(&bd)), uintptr(unsafe.Pointer(&data)), uintptr(unsafe.Pointer(p.out)))) {
			return errors.New("gpusurface: cannot create the constant buffer")
		}
	}
	return nil
}

// compile compiles the function entry of source for target with the
// system's d3dcompiler_47.dll, as MyGo's renderer does.
func compile(source, entry, target string) ([]byte, error) {
	if err := procD3DCompile.Find(); err != nil {
		return nil, fmt.Errorf("gpusurface: no shader compiler: %w", err)
	}
	src := []byte(source)
	e, _ := syscall.BytePtrFromString(entry)
	t, _ := syscall.BytePtrFromString(target)
	const optimize3 = 1 << 15
	var blob, errs uintptr
	hr, _, _ := procD3DCompile.Call(uintptr(unsafe.Pointer(&src[0])), uintptr(len(src)), 0, 0, 0,
		uintptr(unsafe.Pointer(e)), uintptr(unsafe.Pointer(t)), optimize3, 0, uintptr(unsafe.Pointer(&blob)), uintptr(unsafe.Pointer(&errs)))
	if errs != 0 {
		defer free(&errs)
	}
	if failed(hr) || blob == 0 {
		msg := "unknown error"
		if errs != 0 {
			msg = string(blobBytes(errs))
		}
		return nil, fmt.Errorf("gpusurface: cannot compile the %s shader: %s", entry, msg)
	}
	defer free(&blob)
	return append([]byte(nil), blobBytes(blob)...), nil
}

func blobBytes(blob uintptr) []byte {
	const blobGetBufferPointer, blobGetBufferSize = 3, 4
	p, n := call(blob, blobGetBufferPointer), call(blob, blobGetBufferSize)
	return unsafe.Slice((*byte)(ptr(p)), n)
}

// Device returns the surface's ID3D11Device, and Context its immediate
// context: a producer makes its frames on the device and draws them on the
// context, from the thread that presents them.
func (s *Surface) Device() uintptr  { return s.device }
func (s *Surface) Context() uintptr { return s.ctx }

// Queue returns 0: Direct3D 11 has no command queue to share.
func (s *Surface) Queue() uintptr { return 0 }

// Size returns the size of the swap chain's buffers in pixels.
func (s *Surface) Size() (w, h int) { return s.width, s.height }

// SwapChain returns the surface's IDXGISwapChain1, as for its HDR metadata.
func (s *Surface) SwapChain() uintptr { return s.swapChain }

// Tearing reports whether frames present with tearing.
func (s *Surface) Tearing() bool { return s.tearing }

// Present draws the frame in texture, an ID3D11Texture2D of the surface's
// device in BGRA, NV12 or P010 that shaders can read, and presents it at
// once.
func (s *Surface) Present(texture uintptr) error { return s.present(texture, 0, 0, 0, 0) }

// PresentTagged is Present with a tag the OnPresented handler receives
// back for this frame.
func (s *Surface) PresentTagged(texture uintptr, tag int64) error {
	return s.present(texture, 0, 0, 0, tag)
}

// OnPresented sets f to be called once for each frame presented after it,
// with the frame's tag (0 but from PresentTagged), the time Present was
// called and 0 for the time it reached the screen, which DXGI does not
// tell (see Timing). It is called on the presenting thread as the present
// returns. Set it before the first present. A nil f stops the calls.
func (s *Surface) OnPresented(f func(tag, submitted, presented int64)) {
	if f == nil {
		s.onPresented.Store(nil)
		return
	}
	s.onPresented.Store(&f)
}

// PresentSlice is Present for slice of a texture array, as hardware
// decoders write their pictures, showing its top-left width x height: the
// visible picture of a texture aligned past it. Zero width or height shows
// the whole texture. A picture of another shape than the swap chain shows
// whole between black bars.
func (s *Surface) PresentSlice(texture uintptr, slice, width, height int) error {
	return s.present(texture, slice, width, height, 0)
}

func (s *Surface) present(texture uintptr, slice, width, height int, tag int64) error {
	submitted := Now()
	f := s.wrap(texture, slice, width, height)
	if f == nil {
		return ErrFormat
	}
	// Every pointer passed is into s, on the heap (fastCall).
	ctx := s.ctx
	if s.fitViewport(f.visibleW, f.visibleH) {
		// Flip-discard leaves the back buffer undefined: the bars need
		// clearing on every present.
		fastCall(ctx, ctxClearRenderTargetView, s.rtv, uintptr(unsafe.Pointer(&s.black)), 0, 0, 0)
	}
	fastCall(ctx, ctxOMSetRenderTargets, 1, uintptr(unsafe.Pointer(&s.rtv)), 0, 0, 0)
	fastCall(ctx, ctxRSSetViewports, 1, uintptr(unsafe.Pointer(&s.vp)), 0, 0, 0)
	fastCall(ctx, ctxIASetPrimitiveTopology, topologyTriangleList, 0, 0, 0, 0)
	fastCall(ctx, ctxIASetInputLayout, 0, 0, 0, 0, 0)
	fastCall(ctx, ctxVSSetShader, s.vs, 0, 0, 0, 0)
	fastCall(ctx, ctxVSSetConstantBuffers, 0, 1, uintptr(unsafe.Pointer(&f.crop)), 0, 0)
	if f.format == FormatBGRA8 {
		fastCall(ctx, ctxPSSetShader, s.psRGB, 0, 0, 0, 0)
	} else {
		fastCall(ctx, ctxPSSetShader, s.psYUV, 0, 0, 0, 0)
		fastCall(ctx, ctxPSSetConstantBuffers, 0, 1, uintptr(unsafe.Pointer(&f.params)), 0, 0)
	}
	fastCall(ctx, ctxPSSetShaderResources, 0, 2, uintptr(unsafe.Pointer(&f.srv[0])), 0, 0)
	fastCall(ctx, ctxPSSetSamplers, 0, 1, uintptr(unsafe.Pointer(&s.sampler)), 0, 0)
	fastCall(ctx, ctxDraw, 3, 0, 0, 0, 0)
	// The producer may draw into the frame next: it is no longer bound.
	fastCall(ctx, ctxPSSetShaderResources, 0, 2, uintptr(unsafe.Pointer(&s.noViews[0])), 0, 0)
	var flags uintptr
	if s.tearing {
		flags = presentTearing
	}
	if hr := fastCall(s.swapChain, scPresent, 0, flags, 0, 0, 0); failed(hr) {
		return fmt.Errorf("gpusurface: present failed: %#x", uint32(hr))
	}
	if i := s.timings.start(submitted); i >= 0 {
		s.timings.recs[i].Encoded = Now()
		s.timings.resolve(i, 0)
	}
	if f := s.onPresented.Load(); f != nil && *f != nil {
		(*f)(tag, submitted, 0)
	}
	return nil
}

func (s *Surface) wrap(texture uintptr, slice, width, height int) *frameViews {
	for i := range s.cache {
		c := &s.cache[i]
		if c.tex == texture && texture != 0 && c.slice == slice && c.width == width && c.height == height {
			return c
		}
	}
	if texture == 0 {
		return nil
	}
	var td texture2DDesc
	call(texture, texGetDesc, uintptr(unsafe.Pointer(&td)))
	if slice < 0 || uint32(slice) >= max(td.ArraySize, 1) || uint32(width) > td.Width || uint32(height) > td.Height {
		return nil
	}
	f := frameViews{slice: slice, width: width, height: height, visibleW: int(td.Width), visibleH: int(td.Height)}
	if width > 0 && height > 0 {
		f.visibleW, f.visibleH = width, height
	}
	var formats [2]uint32
	switch td.Format {
	case formatB8G8R8A8Unorm:
		f.format, formats = FormatBGRA8, [2]uint32{formatB8G8R8A8Unorm}
	case formatNV12:
		f.format, formats, f.params = FormatNV12, [2]uint32{formatR8Unorm, formatR8G8Unorm}, s.params8
	case formatP010:
		f.format, formats, f.params = FormatP010, [2]uint32{formatR16Unorm, formatR16G16Unorm}, s.params10
	default:
		return nil
	}
	for i, format := range formats {
		if format == 0 {
			continue
		}
		d := srvDesc{Format: format, ViewDimension: dimensionTexture2D, MipLevels: 1}
		if td.ArraySize > 1 {
			d.ViewDimension, d.FirstArraySlice, d.ArraySize = dimensionTexture2DArr, uint32(slice), 1
		}
		if failed(call(s.device, devCreateShaderResourceView, texture, uintptr(unsafe.Pointer(&d)), uintptr(unsafe.Pointer(&f.srv[i])))) {
			free(&f.srv[0])
			free(&f.srv[1])
			return nil
		}
	}
	crop := uvScale{U: 1, V: 1}
	if width > 0 && height > 0 {
		crop = uvScale{U: float32(width) / float32(td.Width), V: float32(height) / float32(td.Height)}
	}
	bd := bufferDesc{ByteWidth: uint32(unsafe.Sizeof(crop)), Usage: usageImmutable, BindFlags: bindConstantBuffer}
	data := subresourceData{SysMem: uintptr(unsafe.Pointer(&crop))}
	if failed(call(s.device, devCreateBuffer, uintptr(unsafe.Pointer(&bd)), uintptr(unsafe.Pointer(&data)), uintptr(unsafe.Pointer(&f.crop)))) {
		free(&f.srv[0])
		free(&f.srv[1])
		return nil
	}
	f.tex = texture
	slot := &s.cache[s.nextSlot]
	s.nextSlot = (s.nextSlot + 1) % wrapCache
	free(&slot.srv[0])
	free(&slot.srv[1])
	free(&slot.crop)
	*slot = f
	return slot
}

// Resize resizes the swap chain's buffers to w x h pixels, as after the
// window's client area changed; zero takes the client area's size. Call it
// from the thread that presents.
func (s *Surface) Resize(w, h int) error {
	if w < 0 || h < 0 {
		return fmt.Errorf("gpusurface: size %dx%d", w, h)
	}
	if s.dcomp != 0 && (w == 0 || h == 0) {
		// A swap chain for composition has no window to take the size of.
		cw, ch := s.clientSize()
		w, h = cmp.Or(w, int(cw)), cmp.Or(h, int(ch))
	}
	// The swap chain resizes only once nothing holds its back buffer.
	fastCall(s.ctx, ctxOMSetRenderTargets, 0, 0, 0, 0, 0)
	free(&s.rtv)
	call(s.ctx, ctxFlush)
	var flags uintptr
	if s.tearing {
		flags = swapChainTearing
	}
	if hr := call(s.swapChain, scResizeBuffers, 0, uintptr(w), uintptr(h), 0, flags); failed(hr) {
		return fmt.Errorf("gpusurface: cannot resize the swap chain: %#x", uint32(hr))
	}
	return s.viewBackBuffer()
}

// viewBackBuffer makes the render target view of the back buffer and takes
// its size.
func (s *Surface) viewBackBuffer() error {
	var back uintptr
	if hr := call(s.swapChain, scGetBuffer, 0, uintptr(unsafe.Pointer(&iidID3D11Texture2D)), uintptr(unsafe.Pointer(&back))); failed(hr) {
		return fmt.Errorf("gpusurface: no back buffer: %#x", uint32(hr))
	}
	var bd texture2DDesc
	call(back, texGetDesc, uintptr(unsafe.Pointer(&bd)))
	hr := call(s.device, devCreateRenderTargetView, back, 0, uintptr(unsafe.Pointer(&s.rtv)))
	free(&back)
	if failed(hr) {
		return fmt.Errorf("gpusurface: cannot view the back buffer: %#x", uint32(hr))
	}
	s.width, s.height = int(bd.Width), int(bd.Height)
	s.vp = viewport{W: float32(bd.Width), H: float32(bd.Height), MaxDepth: 1}
	return nil
}

// fitViewport sets the viewport where a fw x fh picture shows whole in
// the back buffer, and reports whether it leaves bars.
func (s *Surface) fitViewport(fw, fh int) (bars bool) {
	x, y, w, h := FitRect(fw, fh, s.width, s.height)
	s.vp = viewport{X: float32(x), Y: float32(y), W: float32(w), H: float32(h), MaxDepth: 1}
	return w != s.width || h != s.height
}

// Close frees the surface.
func (s *Surface) Close() {
	if s.ctx != 0 {
		call(s.ctx, ctxFlush)
	}
	for i := range s.cache {
		free(&s.cache[i].srv[0])
		free(&s.cache[i].srv[1])
		free(&s.cache[i].crop)
	}
	if s.target != 0 {
		call(s.target, dcompTargetSetRoot, 0)
		call(s.dcomp, dcompCommit)
	}
	for _, p := range []*uintptr{&s.params10, &s.params8, &s.sampler, &s.psYUV, &s.psRGB, &s.vs, &s.rtv, &s.visual, &s.target, &s.dcomp, &s.swapChain, &s.factory, &s.ctx, &s.device} {
		free(p)
	}
}
