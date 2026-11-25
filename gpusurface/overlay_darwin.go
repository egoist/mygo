//go:build darwin && !ios

package gpusurface

import (
	"errors"
	"fmt"
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// The overlay draws in the frame's own render pass, after the frame: one
// instanced quad a rect, blended over it. It adds no layer, so the frame's
// layer stays the only one composited in direct mode.

// overlayRing is how many vertex buffers the overlay cycles through. With
// two drawables, the GPU is done with a frame's buffer once the drawable
// two frames later was handed out, so a buffer is never written while the
// GPU reads it.
const overlayRing = 3

const overlayShaderSource = `
struct ORect { packed_float4 rect; float radius; packed_float4 color; packed_float4 uv; };
struct OOut {
	float4 pos [[position]];
	float2 local;
	float2 uv;
	float2 halfSize [[flat]];
	float radius [[flat]];
	float4 color [[flat]];
	float glyph [[flat]];
};
vertex OOut overlay_vs(uint vid [[vertex_id]], uint iid [[instance_id]],
		const device ORect *rects [[buffer(0)]], constant float2 &size [[buffer(1)]]) {
	ORect r = rects[iid];
	float2 c = float2(vid & 1, vid >> 1);
	float2 wh = float2(r.rect[2], r.rect[3]);
	float2 px = float2(r.rect[0], r.rect[1]) + c * wh;
	float4 uv = float4(r.uv);
	OOut o;
	o.pos = float4(px / size * float2(2, -2) + float2(-1, 1), 0, 1);
	o.local = (c - 0.5) * wh;
	o.halfSize = 0.5 * wh;
	o.radius = min(r.radius, 0.5 * min(wh.x, wh.y));
	o.color = float4(r.color);
	o.uv = mix(uv.xy, uv.zw, c);
	o.glyph = any(uv != 0) ? 1.0 : 0.0;
	return o;
}
constexpr sampler atlasSmp(filter::linear, address::clamp_to_edge);
static float4 overlay_color(OOut in, texture2d<float> atlas) {
	float a;
	if (in.glyph > 0.5) {
		a = atlas.sample(atlasSmp, in.uv).r;
	} else {
		float2 q = abs(in.local) - in.halfSize + in.radius;
		float sdf = length(max(q, 0.0)) + min(max(q.x, q.y), 0.0) - in.radius;
		a = clamp(0.5 - sdf, 0.0, 1.0);
	}
	a *= in.color.a;
	return float4(in.color.rgb * a, a);
}
fragment float4 overlay_fs(OOut in [[stage_in]], texture2d<float> atlas [[texture(0)]]) {
	return overlay_color(in, atlas);
}
// overlay_fs_pq is overlay_fs on an HDR10 drawable: the sRGB colour, made
// linear, moved to BT.2020 primaries and set at 203 nits for white (ITU-R
// BT.2408 reference white), then PQ encoded.
fragment float4 overlay_fs_pq(OOut in [[stage_in]], texture2d<float> atlas [[texture(0)]]) {
	float4 c = overlay_color(in, atlas);
	if (c.a <= 0.0) {
		return c;
	}
	float3 srgb = c.rgb / c.a;
	float3 lin = select(pow((srgb + 0.055) / 1.055, 2.4), srgb / 12.92, srgb <= 0.04045);
	float3x3 to2020 = float3x3(float3(0.6274, 0.0691, 0.0164), float3(0.3293, 0.9195, 0.0880), float3(0.0433, 0.0114, 0.8956));
	float3 y = clamp(to2020 * lin * (203.0 / 10000.0), 0.0, 1.0);
	float3 ym = pow(y, 0.1593017578125);
	float3 pq = pow((0.8359375 + 18.8515625 * ym) / (1.0 + 18.6875 * ym), 78.84375);
	return float4(pq * c.a, c.a);
}
`

var selContents = objc.RegisterName("contents")

func ptr(a uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&a)) }

func msg4(obj uintptr, sel objc.SEL, a, b, c, d uintptr) uintptr {
	r, _, _ := syscall6(lib.msgSend, obj, uintptr(sel), a, b, c, d)
	return r
}

func (s *Surface) makeOverlay() error {
	size := uintptr(MaxOverlayRects) * unsafe.Sizeof(OverlayRect{})
	for i := range s.overlayBufs {
		const storageModeShared = 0
		b := send(s.device, "newBufferWithLength:options:", size, uintptr(storageModeShared))
		if b == 0 {
			return errors.New("gpusurface: cannot make the overlay buffers")
		}
		s.overlayBufs[i] = b
		s.overlayMem[i] = ptr(msg(b, selContents))
	}
	s.atlasAlign = int(send(s.device, "minimumLinearTextureAlignmentForPixelFormat:", uintptr(pixelFormatR8Unorm)))
	return s.SetOverlayAtlas([]byte{0}, 1, 1)
}

// SetOverlay sets the rects drawn over every frame presented after it, at
// most MaxOverlayRects; none hides the overlay. It copies them, allocates
// nothing and never waits for a present. Call it from any goroutine.
func (s *Surface) SetOverlay(rects []OverlayRect) { s.overlay.set(rects) }

// SetOverlayAtlas sets the coverage the overlay's glyph rects read: w x h
// bytes, a row after another, 255 covered. Call it from any goroutine,
// seldom: on a change of scale. It makes Metal objects only, which take
// any thread, and swaps them in under a lock the present takes too.
func (s *Surface) SetOverlayAtlas(pix []byte, w, h int) error {
	if w <= 0 || h <= 0 || w > 16384 || h > 16384 || len(pix) < w*h {
		return fmt.Errorf("gpusurface: overlay atlas %dx%d from %d bytes", w, h, len(pix))
	}
	align := max(s.atlasAlign, 1)
	stride := (w + align - 1) / align * align
	rows := make([]byte, stride*h)
	for y := range h {
		copy(rows[y*stride:], pix[y*w:(y+1)*w])
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool, _, _ := purego.SyscallN(lib.poolPush)
	defer purego.SyscallN(lib.poolPop, pool)
	const storageModeShared = 0
	buf := send(s.device, "newBufferWithBytes:length:options:", uintptr(unsafe.Pointer(&rows[0])), uintptr(len(rows)), uintptr(storageModeShared))
	runtime.KeepAlive(rows)
	if buf == 0 {
		return errors.New("gpusurface: cannot make the overlay atlas")
	}
	d := send(class("MTLTextureDescriptor"), "texture2DDescriptorWithPixelFormat:width:height:mipmapped:", uintptr(pixelFormatR8Unorm), uintptr(w), uintptr(h), false)
	msg1(d, selSetUsage, textureUsageShaderRead)
	send(d, "setStorageMode:", uintptr(storageModeShared))
	tex := send(buf, "newTextureWithDescriptor:offset:bytesPerRow:", d, uintptr(0), uintptr(stride))
	if tex == 0 {
		release(&buf)
		return errors.New("gpusurface: cannot make the overlay atlas texture")
	}
	// A command buffer retains the atlas it was given, so the old one may
	// go once no present can still pick it up.
	s.atlasMu.Lock()
	oldTex, oldBuf := s.atlas, s.atlasBuf
	s.atlas, s.atlasBuf = tex, buf
	s.atlasMu.Unlock()
	release(&oldTex)
	release(&oldBuf)
	return nil
}

func (s *Surface) drawOverlay(enc uintptr, rects []OverlayRect, dw, dh uintptr) {
	i := s.overlayFrame % overlayRing
	s.overlayFrame++
	copy(unsafe.Slice((*OverlayRect)(s.overlayMem[i]), MaxOverlayRects), rects)
	s.overlaySize = [2]float32{float32(dw), float32(dh)}
	pipe := s.overlayPipe
	if s.hdr.Load() {
		pipe = s.overlayPQPipe
	}
	msg1(enc, selSetPipeline, pipe)
	msg3(enc, selSetVertexBuffer, s.overlayBufs[i], 0, 0)
	msg3(enc, selSetVertexBytes, uintptr(unsafe.Pointer(&s.overlaySize)), unsafe.Sizeof(s.overlaySize), 1)
	s.atlasMu.Lock()
	msg2(enc, selSetFragmentTex, s.atlas, 0)
	s.atlasMu.Unlock()
	msg4(enc, selDrawInstanced, primitiveTriangleStrip, 0, 4, uintptr(len(rects)))
}

func (s *Surface) closeOverlay() {
	for i := range s.overlayBufs {
		release(&s.overlayBufs[i])
		s.overlayMem[i] = nil
	}
	s.atlasMu.Lock()
	release(&s.atlas)
	release(&s.atlasBuf)
	s.atlasMu.Unlock()
}
