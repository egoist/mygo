//go:build darwin

package metal

import (
	"strings"
	"sync"
	"unsafe"

	"github.com/egoist/mygo/internal/gpu"
)

var polylineOnce sync.Once
var polylineProgram *shaderEntry

// EnablePolyline registers the compact upload when StrokePolyline first draws
// on the GPU. The existing shader worker owns the program for the device's
// lifetime. Frames use the original upload until it is ready, without waiting.
func EnablePolyline() {
	polylineOnce.Do(func() {
		src := strings.NewReplacer(
			"struct PSOut {", polylineLayout+"\nstruct PSOut {",
			"uint iid [[instance_id]],", "uint iid [[instance_id]], uint base [[base_instance]],",
			"const device Inst *insts", "const device CompactInst *insts",
			"Inst i = insts[iid];", "Inst i = compactDecode(insts[iid + base]);",
			"Inst i = insts[v.inst];", "Inst i = compactDecode(insts[v.inst]);",
			"o.inst = iid;", "o.inst = iid + base;",
			"fragment PSOut ps(", "fragment PSOut shader_ps(",
		).Replace(shaderSource)
		PrecompileShader(src)
		shaders.mu.Lock()
		polylineProgram = shaders.byKey[src]
		shaders.mu.Unlock()
		instanceUpload = uploadPolylines
	})
}

// Each batch keeps its original 224-byte address range in the frame slot.
// Compact records occupy its first half. The vertex's instance_id includes
// base_instance; adding base_instance once more indexes the same byte address
// at a 112-byte stride. Other batches still read ordinary 224-byte records.
// No batch offsets, Builder instances, slot capacities or draw calls change.
type polylineInstance struct {
	Rect, Radii, Color, Clip, Transform0, Transform1, Params [4]float32
}

func uploadPolylines(r *Renderer, d *drawState, dst []gpu.Instance) bool {
	e := polylineProgram
	if e == nil || r.device != shaders.device || !r.canSpecialize || r.source != "" || r.cur.format != pixelFormatBGRA8Unorm || !d.b.Classes || d.b.NoClasses || d.b.GeneralClasses&(1<<gpu.ClassGeneral) != 0 {
		return false
	}
	select {
	case <-e.done:
	default:
		return false
	}
	if e.pipe == 0 {
		return false
	}
	any := false
	for j, b := range d.b.Batches {
		if d.pipes[j] == 0 || b.Class != gpu.ClassGeneral || b.Joined != 0 || b.ClipMask != 0 || b.Effect != nil || b.Shader != nil || b.Hole || b.Opaque || b.Image != 0 {
			continue
		}
		eligible := compactPolyline(&d.b.Instances[b.Start+b.Count-1])
		for i := b.Start; eligible && i < b.Start+b.Count-1; i++ {
			eligible = compactPolyline(&d.b.Instances[i])
		}
		if eligible {
			d.pipes[j] = e.pipe
			any = true
		}
	}
	if !any {
		return false
	}
	for j, b := range d.b.Batches {
		src := d.b.Instances[b.Start : b.Start+b.Count]
		if d.pipes[j] != e.pipe {
			copy(dst[b.Start:b.Start+b.Count], src)
			continue
		}
		packed := unsafe.Slice((*polylineInstance)(unsafe.Pointer(&dst[b.Start])), b.Count)
		for i := range src {
			s := &src[i]
			packed[i] = polylineInstance{s.Rect, s.Radii, s.Color, s.Clip, s.Transform0, s.Transform1, s.Params}
		}
	}
	return true
}

func zeroPolylineField(v *[4]float32) bool {
	// Omitted fields must be bitwise zero, including the sign of zero.
	return *(*[4]uint32)(unsafe.Pointer(v)) == [4]uint32{}
}

func compactPolyline(i *gpu.Instance) bool {
	return i.Params == [4]float32{0, 0, 0, 1} && i.Color[3] == 1 &&
		i.Transform0[3] == 1 && i.Transform1[3] == 0 &&
		i.Rect[0] == -i.Rect[3]/2 && i.Rect[1] == i.Rect[0] &&
		i.Radii == [4]float32{i.Rect[3] / 2, i.Rect[3] / 2, i.Rect[3] / 2, i.Rect[3] / 2} &&
		zeroPolylineField(&i.Inner) && zeroPolylineField(&i.Color2) && zeroPolylineField(&i.Border) &&
		zeroPolylineField(&i.Grad) && zeroPolylineField(&i.UV) && zeroPolylineField(&i.ClipRadii) && zeroPolylineField(&i.Transform2)
}

// Only the input loads change. Keep the complete vertex/fragment mathematics,
// interface, function constants and blending of shader.metal. Params must stay
// dynamic: replacing even its known values with literals changes AA rounding.
const polylineLayout = `
struct CompactInst {
float4 rect;
float4 radii;
float4 color;
float4 clip;
float4 transform0;
float4 transform1;
float4 params;
};
Inst compactDecode(CompactInst c) { Inst i;
i.rect = c.rect;
i.radii = c.radii;
i.inner = float4(0.0f);
i.color = c.color;
i.color2 = float4(0.0f);
i.border = float4(0.0f);
i.grad = float4(0.0f);
i.uv = float4(0.0f);
i.clip = c.clip;
i.clipRadii = float4(0.0f);
i.params = c.params;
i.transform0 = c.transform0;
i.transform1 = c.transform1;
i.transform2 = float4(0.0f);
return i; }
`
