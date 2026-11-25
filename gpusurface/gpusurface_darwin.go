//go:build darwin && !ios

package gpusurface

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// Every call on the hot path goes through syscall6 (ccall_darwin.go) with
// integer arguments, which allocates nothing; objc.Send and functions made with
// purego.RegisterFunc box their arguments and are kept to setup and to the
// presented handler, which runs on Core Animation's thread.

const (
	pixelFormatR8Unorm      = 10
	pixelFormatR16Unorm     = 20
	pixelFormatRG8Unorm     = 30
	pixelFormatRG16Unorm    = 60
	pixelFormatBGRA8Unorm   = 80
	pixelFormatBGR10A2Unorm = 94
	textureUsageShaderRead  = 1
	loadActionDontCare      = 0
	loadActionClear         = 2
	storeActionStore        = 1
	primitiveTriangleStrip  = 4

	// IOSurface pixel formats (CoreVideo's four-character codes).
	fourccBGRA = 0x42475241 // 'BGRA'
	fourcc420v = 0x34323076 // '420v', NV12 video range
	fourccx420 = 0x78343230 // 'x420', P010 video range
	fourccl10r = 0x6c313072 // 'l10r', 10-bit RGB

	viewWidthSizable   = 2           // NSViewWidthSizable
	viewHeightSizable  = 16          // NSViewHeightSizable
	layerWidthSizable  = 2           // kCALayerWidthSizable
	layerHeightSizable = 16          // kCALayerHeightSizable
	windowBelow        = ^uintptr(0) // NSWindowBelow, -1
)

type cgSize struct{ W, H float64 }
type cgRect struct{ X, Y, W, H float64 }

var lib struct {
	once sync.Once
	err  error

	msgSend, poolPush, poolPop, machAbsoluteTime uintptr
	ioWidth, ioHeight, ioFormat                  uintptr
	ioPlaneWidth, ioPlaneHeight                  uintptr
	numer, denom                                 uint64

	// pthreadMainNP, dispatchSyncF, mainQueue (dispatch_get_main_queue())
	// and mainCallback are onMain's.
	pthreadMainNP, dispatchSyncF, mainQueue, mainCallback uintptr

	createDevice func() uintptr
	msgSetSize   func(obj uintptr, sel objc.SEL, s cgSize)
	msgRect      func(obj uintptr, sel objc.SEL) cgRect
	msgSetRect   func(obj uintptr, sel objc.SEL, r cgRect)
	msgFloat     func(obj uintptr, sel objc.SEL) float64
	msgSetFloat  func(obj uintptr, sel objc.SEL, v float64)
	msgInitRect  func(obj uintptr, sel objc.SEL, r cgRect) uintptr

	presented objc.Block
}

var (
	selNextDrawable     = objc.RegisterName("nextDrawable")
	selCommandBuffer    = objc.RegisterName("commandBuffer")
	selTexture          = objc.RegisterName("texture")
	selSetTexture       = objc.RegisterName("setTexture:")
	selRenderEncoder    = objc.RegisterName("renderCommandEncoderWithDescriptor:")
	selSetPipeline      = objc.RegisterName("setRenderPipelineState:")
	selSetFragmentTex   = objc.RegisterName("setFragmentTexture:atIndex:")
	selSetFragmentBytes = objc.RegisterName("setFragmentBytes:length:atIndex:")
	selDraw             = objc.RegisterName("drawPrimitives:vertexStart:vertexCount:")
	selEndEncoding      = objc.RegisterName("endEncoding")
	selDrawableID       = objc.RegisterName("drawableID")
	selAddPresented     = objc.RegisterName("addPresentedHandler:")
	selPresentDrawable  = objc.RegisterName("presentDrawable:")
	selCommit           = objc.RegisterName("commit")
	selPresentedTime    = objc.RegisterName("presentedTime")
	selLayer            = objc.RegisterName("layer")
	selRelease          = objc.RegisterName("release")
	selNewTexture       = objc.RegisterName("newTextureWithDescriptor:iosurface:plane:")
	selSetUsage         = objc.RegisterName("setUsage:")
	selWidth            = objc.RegisterName("width")
	selHeight           = objc.RegisterName("height")
	selSetLoadAction    = objc.RegisterName("setLoadAction:")
	selSetVertexBytes   = objc.RegisterName("setVertexBytes:length:atIndex:")
	selSetVertexBuffer  = objc.RegisterName("setVertexBuffer:offset:atIndex:")
	selDrawInstanced    = objc.RegisterName("drawPrimitives:vertexStart:vertexCount:instanceCount:")
)

func load() error {
	lib.once.Do(func() {
		open := func(path string) uintptr {
			h, err := purego.Dlopen(path, purego.RTLD_GLOBAL|purego.RTLD_NOW)
			if err != nil && lib.err == nil {
				lib.err = fmt.Errorf("gpusurface: %w", err)
			}
			return h
		}
		objcLib := open("/usr/lib/libobjc.A.dylib")
		metal := open("/System/Library/Frameworks/Metal.framework/Metal")
		open("/System/Library/Frameworks/QuartzCore.framework/QuartzCore")
		io := open("/System/Library/Frameworks/IOSurface.framework/IOSurface")
		system := open("/usr/lib/libSystem.B.dylib")
		if lib.err != nil {
			return
		}
		sym := func(h uintptr, name string) uintptr {
			p, err := purego.Dlsym(h, name)
			if err != nil && lib.err == nil {
				lib.err = fmt.Errorf("gpusurface: %w", err)
			}
			return p
		}
		lib.msgSend = sym(objcLib, "objc_msgSend")
		lib.poolPush = sym(objcLib, "objc_autoreleasePoolPush")
		lib.poolPop = sym(objcLib, "objc_autoreleasePoolPop")
		lib.ioWidth = sym(io, "IOSurfaceGetWidth")
		lib.ioHeight = sym(io, "IOSurfaceGetHeight")
		lib.ioFormat = sym(io, "IOSurfaceGetPixelFormat")
		lib.ioPlaneWidth = sym(io, "IOSurfaceGetWidthOfPlane")
		lib.ioPlaneHeight = sym(io, "IOSurfaceGetHeightOfPlane")
		lib.machAbsoluteTime = sym(system, "mach_absolute_time")
		lib.pthreadMainNP = sym(system, "pthread_main_np")
		lib.dispatchSyncF = sym(system, "dispatch_sync_f")
		lib.mainQueue = sym(system, "_dispatch_main_q")
		timebase := sym(system, "mach_timebase_info")
		create := sym(metal, "MTLCreateSystemDefaultDevice")
		if lib.err != nil {
			return
		}
		var tb struct{ numer, denom uint32 }
		purego.SyscallN(timebase, uintptr(unsafe.Pointer(&tb)))
		lib.numer, lib.denom = uint64(tb.numer), uint64(tb.denom)
		purego.RegisterFunc(&lib.createDevice, create)
		purego.RegisterFunc(&lib.msgSetSize, lib.msgSend)
		purego.RegisterFunc(&lib.msgRect, lib.msgSend)
		purego.RegisterFunc(&lib.msgSetRect, lib.msgSend)
		purego.RegisterFunc(&lib.msgFloat, lib.msgSend)
		purego.RegisterFunc(&lib.msgSetFloat, lib.msgSend)
		purego.RegisterFunc(&lib.msgInitRect, lib.msgSend)
		// One block for every drawable of every surface: purego callbacks
		// are never freed.
		lib.presented = objc.NewBlock(onPresented)
		lib.mainCallback = purego.NewCallback(runMainCall)
	})
	return lib.err
}

// Now returns the time in nanoseconds of the clock Timing uses: the host's
// mach time, which Core Animation's presentedTime counts in seconds.
func Now() int64 {
	t, _, _ := syscall6(lib.machAbsoluteTime, 0, 0, 0, 0, 0, 0)
	return int64(uint64(t) * lib.numer / lib.denom)
}

func msg(obj uintptr, sel objc.SEL) uintptr {
	r, _, _ := syscall6(lib.msgSend, obj, uintptr(sel), 0, 0, 0, 0)
	return r
}

func msg1(obj uintptr, sel objc.SEL, a uintptr) uintptr {
	r, _, _ := syscall6(lib.msgSend, obj, uintptr(sel), a, 0, 0, 0)
	return r
}

func msg2(obj uintptr, sel objc.SEL, a, b uintptr) uintptr {
	r, _, _ := syscall6(lib.msgSend, obj, uintptr(sel), a, b, 0, 0)
	return r
}

func msg3(obj uintptr, sel objc.SEL, a, b, c uintptr) uintptr {
	r, _, _ := syscall6(lib.msgSend, obj, uintptr(sel), a, b, c, 0)
	return r
}

func send(obj uintptr, sel string, args ...any) uintptr {
	return uintptr(objc.ID(obj).Send(objc.RegisterName(sel), args...))
}

func class(name string) uintptr { return uintptr(objc.GetClass(name)) }

func release(obj *uintptr) {
	if *obj != 0 {
		msg(*obj, selRelease)
		*obj = 0
	}
}

// yuvParams converts the video range samples of a frame to full range: Y'
// = (y - YOffset) * YScale, Cb, Cr = (c - COffset) * CScale.
type yuvParams struct{ YOffset, YScale, COffset, CScale float32 }

var (
	params8  = yuvParams{16.0 / 255, 255.0 / 219, 128.0 / 255, 255.0 / 224}
	params10 = yuvParams{64 * 64 / 65535.0, 65535.0 / (876 * 64), 512 * 64 / 65535.0, 65535.0 / (896 * 64)}
)

// frameTextures is a frame of the pool wrapped as textures: its BGRA, or
// its luma and chroma.
type frameTextures struct {
	surface uintptr
	tex     [2]uintptr
	format  Format
	params  *yuvParams
	// width and height are the frame's size in pixels.
	width, height uintptr
}

// wrapCache bounds the frames held wrapped: a decoder's pool is smaller.
const wrapCache = 16

// pendingRing bounds the drawables between Present and their presented
// handler.
const pendingRing = 64

type pending struct {
	drawableID int64 // atomic
	index      int64 // atomic: the Timing's index, or -1
	tag        int64 // atomic
	submitted  int64 // atomic
}

// Surface presents frames into a window.
type Surface struct {
	device, queue        uintptr
	window               uintptr
	layer                uintptr
	host                 uintptr // the view hosting layer, with Composite
	pass, attachment     uintptr
	pixelFormat          uintptr
	rgbPipeline, yuvPipe uintptr
	// yuv2020Pipe and overlayPQPipe draw while hdr is on; sdrSpace is the
	// layer's colour space before, retained, which SetHDR10(false) gives
	// back.
	yuv2020Pipe, overlayPQPipe uintptr
	hdr                        atomic.Bool
	sdrSpace                   uintptr
	cache                      [wrapCache]frameTextures
	nextSlot                   int
	pending                    [pendingRing]pending
	timings                    timings
	onPresented                atomic.Pointer[func(tag, submitted, presented int64)]
	scale                      float64

	// size is the drawables' size, width << 32 | height. sizeMu orders
	// the changes of it and of direct mode; viewW and viewH are the size
	// Resize last asked for, which direct mode keeps for when it ends.
	size         atomic.Uint64
	sizeMu       sync.Mutex
	viewW, viewH int
	direct       atomic.Bool
	hidden       []hiddenLayer // main thread

	// fit places the frame in the drawable, scale x, y then offset x, y in
	// clip space, for the vertex shader; the presenting thread's.
	fit [4]float32

	overlay      overlayRects
	overlayPipe  uintptr
	overlayBufs  [overlayRing]uintptr
	overlayMem   [overlayRing]unsafe.Pointer
	overlayFrame int
	overlaySize  [2]float32
	atlasMu      sync.Mutex
	atlas        uintptr
	atlasBuf     uintptr
	atlasAlign   int
}

var surfaces sync.Map // layer → *Surface, for the presented handler

const shaderSource = `
#include <metal_stdlib>
using namespace metal;
struct VOut { float4 pos [[position]]; float2 uv; };
// A quad over the drawable, scaled and moved by fit (scale x, y, offset x,
// y in clip space) to where the frame goes.
vertex VOut vs(uint vid [[vertex_id]], constant float4 &fit [[buffer(0)]]) {
	float2 p = float2(vid & 1, vid >> 1);
	VOut o;
	o.pos = float4((p * float2(2, -2) + float2(-1, 1)) * fit.xy + fit.zw, 0, 1);
	o.uv = p;
	return o;
}
constexpr sampler smp(filter::linear, address::clamp_to_edge);
fragment float4 fs_rgb(VOut in [[stage_in]], texture2d<float> t [[texture(0)]]) {
	return float4(t.sample(smp, in.uv).rgb, 1);
}
struct YUVParams { float yOffset, yScale, cOffset, cScale; };
fragment float4 fs_yuv(VOut in [[stage_in]], texture2d<float> y [[texture(0)]], texture2d<float> c [[texture(1)]],
		constant YUVParams &p [[buffer(0)]]) {
	float l = (y.sample(smp, in.uv).r - p.yOffset) * p.yScale;
	float2 uv = (c.sample(smp, in.uv).rg - p.cOffset) * p.cScale;
	// BT.709
	return float4(l + 1.5748 * uv.y, l - 0.1873 * uv.x - 0.4681 * uv.y, l + 1.8556 * uv.x, 1);
}
fragment float4 fs_yuv2020(VOut in [[stage_in]], texture2d<float> y [[texture(0)]], texture2d<float> c [[texture(1)]],
		constant YUVParams &p [[buffer(0)]]) {
	float l = (y.sample(smp, in.uv).r - p.yOffset) * p.yScale;
	float2 uv = (c.sample(smp, in.uv).rg - p.cOffset) * p.cScale;
	// BT.2020 non-constant luminance; the result stays PQ encoded.
	return float4(l + 1.4746 * uv.y, l - 0.16455 * uv.x - 0.57135 * uv.y, l + 1.8814 * uv.x, 1);
}
` + overlayShaderSource

// New makes a surface filling opts.Window, an NSWindow*. It may be called
// from any goroutine: its AppKit work runs on the main thread, which must
// be running its run loop then unless New is called on it. Present may
// then be called from one other thread.
func New(opts Options) (s *Surface, err error) {
	if err := load(); err != nil {
		return nil, err
	}
	if opts.Window == 0 {
		return nil, errors.New("gpusurface: no window")
	}
	onMain(func() { s, err = newOnMain(opts) })
	return s, err
}

func newOnMain(opts Options) (s *Surface, err error) {
	s = &Surface{window: opts.Window, pixelFormat: pixelFormatBGRA8Unorm, fit: [4]float32{1, 1, 0, 0}}
	if opts.Drawable10Bit || opts.HDR10 {
		s.pixelFormat = pixelFormatBGR10A2Unorm
	}
	s.overlay.init()
	s.timings.recs = make([]Timing, opts.Timings)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool, _, _ := purego.SyscallN(lib.poolPush)
	defer purego.SyscallN(lib.poolPop, pool)
	// A failed New returns a nil surface: close the one being built.
	built := s
	defer func() {
		if err != nil {
			built.Close()
			s = nil
		}
	}()
	if s.device = lib.createDevice(); s.device == 0 {
		return nil, errors.New("gpusurface: no Metal device")
	}
	s.queue = send(s.device, "newCommandQueue")
	if err = s.makePipelines(); err != nil {
		return nil, err
	}
	if err = s.makeOverlay(); err != nil {
		return nil, err
	}
	s.pass = send(send(class("MTLRenderPassDescriptor"), "renderPassDescriptor"), "retain")
	s.attachment = send(send(s.pass, "colorAttachments"), "objectAtIndexedSubscript:", uintptr(0))
	send(s.attachment, "setLoadAction:", uintptr(loadActionDontCare))
	send(s.attachment, "setStoreAction:", uintptr(storeActionStore))

	view := send(opts.Window, "contentView")
	scale := lib.msgFloat(opts.Window, objc.RegisterName("backingScaleFactor"))
	bounds := lib.msgRect(view, objc.RegisterName("bounds"))
	viewBounds := bounds
	bounds.X, bounds.Y = 0, 0

	layer := send(send(class("CAMetalLayer"), "alloc"), "init")
	send(layer, "setDevice:", s.device)
	send(layer, "setPixelFormat:", s.pixelFormat)
	send(layer, "setFramebufferOnly:", true)
	// Two drawables: one on screen, one drawn. A third would only queue a
	// frame behind the one shown, a refresh of latency.
	send(layer, "setMaximumDrawableCount:", uintptr(2))
	// Drawables present once drawn, not at the next refresh.
	send(layer, "setDisplaySyncEnabled:", false)
	send(layer, "setPresentsWithTransaction:", false)
	send(layer, "setOpaque:", true)
	if opts.Composite {
		// The host view follows the window, and the layer it: the frame
		// changes on the main thread, the drawable size in Resize.
		send(layer, "setAutoresizingMask:", uintptr(layerWidthSizable|layerHeightSizable))
	}
	lib.msgSetRect(layer, objc.RegisterName("setFrame:"), bounds)
	lib.msgSetFloat(layer, objc.RegisterName("setContentsScale:"), scale)
	s.scale = scale
	s.viewW, s.viewH = int(bounds.W*scale), int(bounds.H*scale)
	s.setDrawableSize(layer, s.viewW, s.viewH)
	if opts.Composite {
		// A view of its own at the back of the content view, as MyGo puts
		// web views under its surface view: AppKit keeps the layers of a
		// view's subviews in their order. The view hosts the layer itself,
		// with no layer of its own above it, which direct mode needs to be
		// the only thing composited.
		s.host = lib.msgInitRect(send(class("NSView"), "alloc"), objc.RegisterName("initWithFrame:"), viewBounds)
		send(s.host, "setAutoresizingMask:", uintptr(viewWidthSizable|viewHeightSizable))
		send(s.host, "setLayer:", layer)
		send(s.host, "setWantsLayer:", true)
		send(view, "addSubview:positioned:relativeTo:", s.host, windowBelow, uintptr(0))
	} else {
		send(view, "setWantsLayer:", true)
		send(send(view, "layer"), "addSublayer:", layer)
	}
	send(class("CATransaction"), "flush")
	s.layer = layer
	s.sdrSpace = retainSpace(send(layer, "colorspace"))
	if opts.HDR10 {
		if err = s.SetHDR10(true); err != nil {
			return nil, err
		}
	}
	surfaces.Store(layer, s)
	return s, nil
}

func (s *Surface) makePipelines() error {
	var errObj uintptr
	src := send(send(class("NSString"), "alloc"), "initWithUTF8String:", shaderSource+"\x00")
	defer release(&src)
	libObj := send(s.device, "newLibraryWithSource:options:error:", src, uintptr(0), uintptr(unsafe.Pointer(&errObj)))
	if libObj == 0 {
		return fmt.Errorf("gpusurface: cannot compile the shaders: %s", describe(errObj))
	}
	defer release(&libObj)
	pipeline := func(vs, fs string, blend bool) (uintptr, error) {
		desc := send(send(class("MTLRenderPipelineDescriptor"), "alloc"), "init")
		defer release(&desc)
		vfn := send(libObj, "newFunctionWithName:", nsString(vs))
		ffn := send(libObj, "newFunctionWithName:", nsString(fs))
		defer release(&vfn)
		defer release(&ffn)
		send(desc, "setVertexFunction:", vfn)
		send(desc, "setFragmentFunction:", ffn)
		att := send(send(desc, "colorAttachments"), "objectAtIndexedSubscript:", uintptr(0))
		send(att, "setPixelFormat:", s.pixelFormat)
		if blend {
			// The fragment shader premultiplies.
			const one, oneMinusSourceAlpha = 1, 5
			send(att, "setBlendingEnabled:", true)
			send(att, "setSourceRGBBlendFactor:", uintptr(one))
			send(att, "setSourceAlphaBlendFactor:", uintptr(one))
			send(att, "setDestinationRGBBlendFactor:", uintptr(oneMinusSourceAlpha))
			send(att, "setDestinationAlphaBlendFactor:", uintptr(oneMinusSourceAlpha))
		}
		var e uintptr
		p := send(s.device, "newRenderPipelineStateWithDescriptor:error:", desc, uintptr(unsafe.Pointer(&e)))
		if p == 0 {
			return 0, fmt.Errorf("gpusurface: cannot make the %s pipeline: %s", fs, describe(e))
		}
		return p, nil
	}
	var err error
	if s.rgbPipeline, err = pipeline("vs", "fs_rgb", false); err != nil {
		return err
	}
	if s.yuvPipe, err = pipeline("vs", "fs_yuv", false); err != nil {
		return err
	}
	if s.overlayPipe, err = pipeline("overlay_vs", "overlay_fs", true); err != nil {
		return err
	}
	if s.pixelFormat != pixelFormatBGR10A2Unorm {
		return nil
	}
	if s.yuv2020Pipe, err = pipeline("vs", "fs_yuv2020", false); err != nil {
		return err
	}
	s.overlayPQPipe, err = pipeline("overlay_vs", "overlay_fs_pq", true)
	return err
}

func nsString(s string) uintptr {
	return send(class("NSString"), "stringWithUTF8String:", s+"\x00")
}

func describe(err uintptr) string {
	if err == 0 {
		return "unknown error"
	}
	return objc.Send[string](objc.ID(send(err, "localizedDescription")), objc.RegisterName("UTF8String"))
}

// Device returns the surface's MTLDevice, and Queue its MTLCommandQueue:
// a producer drawing frames on the GPU commits on the queue, so that the
// surface reads them after its writes.
func (s *Surface) Device() uintptr { return s.device }
func (s *Surface) Queue() uintptr  { return s.queue }

// Context and SwapChain return 0: they are Direct3D's, on Windows.
func (s *Surface) Context() uintptr   { return 0 }
func (s *Surface) SwapChain() uintptr { return 0 }

// Tearing reports false: tearing is DXGI's, on Windows. The drawables
// present with display sync off all the same.
func (s *Surface) Tearing() bool { return false }

// AdapterName returns the name of the Metal device the surface draws on.
func (s *Surface) AdapterName() string {
	if s.device == 0 {
		return ""
	}
	return objc.Send[string](objc.ID(send(s.device, "name")), objc.RegisterName("UTF8String"))
}

// Size returns the size of the drawables in pixels.
func (s *Surface) Size() (w, h int) {
	v := s.size.Load()
	return int(v >> 32), int(uint32(v))
}

// Scale returns the window's backing scale factor when the surface was
// made: pixels a point.
func (s *Surface) Scale() float64 { return s.scale }

func (s *Surface) setDrawableSize(layer uintptr, w, h int) {
	lib.msgSetSize(layer, objc.RegisterName("setDrawableSize:"), cgSize{float64(w), float64(h)})
	s.size.Store(uint64(w)<<32 | uint64(uint32(h)))
}

// Resize makes the layer w x h pixels, as after the window's content view
// changed size. Call it from the thread that presents, between presents.
// In direct mode the drawables keep the screen's size, and take w x h once
// it ends.
func (s *Surface) Resize(w, h int) error {
	if w <= 0 || h <= 0 {
		return fmt.Errorf("gpusurface: size %dx%d", w, h)
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool, _, _ := purego.SyscallN(lib.poolPush)
	defer purego.SyscallN(lib.poolPop, pool)
	s.sizeMu.Lock()
	defer s.sizeMu.Unlock()
	s.viewW, s.viewH = w, h
	if s.direct.Load() {
		return nil
	}
	if s.host == 0 {
		lib.msgSetRect(s.layer, objc.RegisterName("setFrame:"), cgRect{W: float64(w) / s.scale, H: float64(h) / s.scale})
	}
	s.setDrawableSize(s.layer, w, h)
	send(class("CATransaction"), "flush")
	return nil
}

// errNoDrawable is Present's error when the layer gave no drawable in time.
var errNoDrawable = errors.New("gpusurface: no drawable")

// OnPresented sets f to be called, on Core Animation's thread, once for
// each frame given to PresentTagged after it: with the frame's tag, the
// time Present was called and the time it reached the screen (0 when a
// newer frame replaced it first), on the clock Now reads. Set it before
// the first present.
func (s *Surface) OnPresented(f func(tag, submitted, presented int64)) {
	s.onPresented.Store(&f)
}

// Present draws the frame in surface, an IOSurfaceRef in BGRA ('BGRA'),
// NV12 ('420v') or P010 ('x420'), and presents it at once. A frame of
// another shape than the drawable shows whole between black bars.
func (s *Surface) Present(surface uintptr) error { return s.PresentTagged(surface, 0) }

// PresentSlice is Present for an IOSurface, which has no slices and no
// visible size apart from its own: slice, width and height must be 0, or
// it returns ErrUnsupported.
func (s *Surface) PresentSlice(surface uintptr, slice, width, height int) error {
	if slice != 0 || width != 0 || height != 0 {
		return ErrUnsupported
	}
	return s.PresentTagged(surface, 0)
}

// PresentTagged is Present with a tag the OnPresented handler receives
// back for this frame.
func (s *Surface) PresentTagged(surface uintptr, tag int64) error {
	submitted := Now()
	// The autorelease pool belongs to the thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	f := s.wrap(surface)
	if f == nil {
		return ErrFormat
	}
	pool, _, _ := syscall6(lib.poolPush, 0, 0, 0, 0, 0, 0)
	d := msg(s.layer, selNextDrawable)
	if d == 0 {
		syscall6(lib.poolPop, pool, 0, 0, 0, 0, 0)
		return errNoDrawable
	}
	cb := msg(s.queue, selCommandBuffer)
	target := msg(d, selTexture)
	dw, dh := msg(target, selWidth), msg(target, selHeight)
	load := uintptr(loadActionDontCare)
	if s.fitFrame(f.width, f.height, dw, dh) {
		load = loadActionClear
	}
	msg1(s.attachment, selSetLoadAction, load)
	msg1(s.attachment, selSetTexture, target)
	enc := msg1(cb, selRenderEncoder, s.pass)
	msg3(enc, selSetVertexBytes, uintptr(unsafe.Pointer(&s.fit)), unsafe.Sizeof(s.fit), 0)
	if f.format == FormatBGRA8 || f.format == FormatBGR10A2 {
		msg1(enc, selSetPipeline, s.rgbPipeline)
		msg2(enc, selSetFragmentTex, f.tex[0], 0)
	} else {
		yuv := s.yuvPipe
		if s.hdr.Load() {
			yuv = s.yuv2020Pipe
		}
		msg1(enc, selSetPipeline, yuv)
		msg2(enc, selSetFragmentTex, f.tex[0], 0)
		msg2(enc, selSetFragmentTex, f.tex[1], 1)
		msg3(enc, selSetFragmentBytes, uintptr(unsafe.Pointer(f.params)), unsafe.Sizeof(*f.params), 0)
	}
	msg3(enc, selDraw, primitiveTriangleStrip, 0, 4)
	if rects := s.overlay.latest(); len(rects) > 0 {
		s.drawOverlay(enc, rects, dw, dh)
	}
	msg(enc, selEndEncoding)
	i := s.timings.start(submitted)
	if i >= 0 || s.onPresented.Load() != nil {
		id := int64(msg(d, selDrawableID))
		p := &s.pending[id%pendingRing]
		atomic.StoreInt64(&p.index, int64(i))
		atomic.StoreInt64(&p.tag, tag)
		atomic.StoreInt64(&p.submitted, submitted)
		atomic.StoreInt64(&p.drawableID, id)
		msg1(d, selAddPresented, uintptr(lib.presented))
	}
	msg1(cb, selPresentDrawable, d)
	msg(cb, selCommit)
	syscall6(lib.poolPop, pool, 0, 0, 0, 0, 0)
	if i >= 0 {
		s.timings.recs[i].Encoded = Now()
	}
	return nil
}

// fitFrame places a fw x fh frame whole in the dw x dh drawable and
// reports whether it leaves bars to clear.
func (s *Surface) fitFrame(fw, fh, dw, dh uintptr) (bars bool) {
	s.fit, bars = fitRect(fw, fh, dw, dh)
	return bars
}

// onPresented is the presented handler of the drawables of every surface,
// on Core Animation's thread.
func onPresented(_ objc.Block, d objc.ID) {
	layer := msg(uintptr(d), selLayer)
	v, ok := surfaces.Load(layer)
	if !ok {
		return
	}
	s := v.(*Surface)
	id := int64(msg(uintptr(d), selDrawableID))
	p := &s.pending[id%pendingRing]
	if atomic.LoadInt64(&p.drawableID) != id {
		return
	}
	at := objc.Send[float64](d, selPresentedTime)
	var presented int64
	if at > 0 {
		presented = int64(at * 1e9)
	}
	s.timings.resolve(int(atomic.LoadInt64(&p.index)), presented)
	if f := s.onPresented.Load(); f != nil {
		(*f)(atomic.LoadInt64(&p.tag), atomic.LoadInt64(&p.submitted), presented)
	}
}

// wrap returns the textures of the frame in surface, wrapping it on its
// first present.
func (s *Surface) wrap(surface uintptr) *frameTextures {
	for i := range s.cache {
		if s.cache[i].surface == surface && surface != 0 {
			return &s.cache[i]
		}
	}
	if surface == 0 {
		return nil
	}
	fourcc, _, _ := purego.SyscallN(lib.ioFormat, surface)
	var f frameTextures
	switch fourcc {
	case fourccBGRA:
		f.format = FormatBGRA8
		f.tex[0] = s.planeTexture(surface, -1, pixelFormatBGRA8Unorm)
	case fourccl10r:
		f.format = FormatBGR10A2
		f.tex[0] = s.planeTexture(surface, -1, pixelFormatBGR10A2Unorm)
	case fourcc420v:
		f.format, f.params = FormatNV12, &params8
		f.tex[0] = s.planeTexture(surface, 0, pixelFormatR8Unorm)
		f.tex[1] = s.planeTexture(surface, 1, pixelFormatRG8Unorm)
	case fourccx420:
		f.format, f.params = FormatP010, &params10
		f.tex[0] = s.planeTexture(surface, 0, pixelFormatR16Unorm)
		f.tex[1] = s.planeTexture(surface, 1, pixelFormatRG16Unorm)
	default:
		return nil
	}
	if f.tex[0] == 0 || (f.params != nil && f.tex[1] == 0) {
		release(&f.tex[0])
		release(&f.tex[1])
		return nil
	}
	f.surface = surface
	f.width, _, _ = purego.SyscallN(lib.ioWidth, surface)
	f.height, _, _ = purego.SyscallN(lib.ioHeight, surface)
	slot := &s.cache[s.nextSlot]
	s.nextSlot = (s.nextSlot + 1) % wrapCache
	// Command buffers in flight retain the textures they read.
	release(&slot.tex[0])
	release(&slot.tex[1])
	*slot = f
	return slot
}

// planeTexture wraps a plane of surface, or the whole of it for plane -1,
// as a texture of the device, with no copy.
func (s *Surface) planeTexture(surface uintptr, plane int, format uintptr) uintptr {
	var w, h uintptr
	if plane < 0 {
		w, _, _ = purego.SyscallN(lib.ioWidth, surface)
		h, _, _ = purego.SyscallN(lib.ioHeight, surface)
		plane = 0
	} else {
		w, _, _ = purego.SyscallN(lib.ioPlaneWidth, surface, uintptr(plane))
		h, _, _ = purego.SyscallN(lib.ioPlaneHeight, surface, uintptr(plane))
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool, _, _ := purego.SyscallN(lib.poolPush)
	defer purego.SyscallN(lib.poolPop, pool)
	d := send(class("MTLTextureDescriptor"), "texture2DDescriptorWithPixelFormat:width:height:mipmapped:", format, w, h, false)
	msg1(d, selSetUsage, textureUsageShaderRead)
	return msg3(s.device, selNewTexture, d, surface, uintptr(plane))
}

// done reports whether every frame recorded has its Presented.
func (t *timings) done() bool { return t.resolved.Load() >= t.next.Load() }

// Close waits for the frames presented to reach the screen, a second at
// most, and frees the surface, once no other call on it is under way. It
// may be called from any goroutine: it takes the surface's view and layer
// out of the window on the main thread (see New), and frees the rest on
// the caller's.
func (s *Surface) Close() {
	deadline := time.Now().Add(time.Second)
	for !s.timings.done() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	onMain(s.detach)
	for i := range s.cache {
		release(&s.cache[i].tex[0])
		release(&s.cache[i].tex[1])
	}
	s.closeOverlay()
	for _, p := range []*uintptr{&s.pass, &s.rgbPipeline, &s.yuvPipe, &s.overlayPipe, &s.yuv2020Pipe, &s.overlayPQPipe, &s.queue, &s.device} {
		release(p)
	}
	releaseSpace(&s.sdrSpace)
}

// detach ends direct mode and takes the surface's layer and view out of
// the window, on the main thread.
func (s *Surface) detach() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool, _, _ := purego.SyscallN(lib.poolPush)
	defer purego.SyscallN(lib.poolPop, pool)
	if s.direct.Load() {
		s.setDirect(false)
	}
	if s.layer != 0 {
		surfaces.Delete(s.layer)
		send(s.layer, "removeFromSuperlayer")
	}
	if s.host != 0 {
		send(s.host, "removeFromSuperview")
	}
	release(&s.host)
	release(&s.layer)
}
