//go:build darwin

package metal

import (
	"bytes"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"testing"
	"unsafe"

	"github.com/egoist/mygo/internal/gpu"
	"github.com/egoist/mygo/internal/scene"
	"github.com/egoist/mygo/internal/wait"
)

func compactTestRenderer(t *testing.T) *Renderer {
	t.Helper()
	r, err := newRenderer()
	if err != nil {
		unavailable(t, err)
	}
	t.Cleanup(r.Release)
	needSpecialized(t, r)
	EnablePolyline()
	if polylineProgram == nil {
		t.Fatal("no compact shader")
	}
	wait.For(t, "the compact shader", polylineProgram.done)
	if polylineProgram.pipe == 0 {
		t.Fatalf("no compact pipeline: %v", polylineProgram.err)
	}
	old := instanceUpload
	instanceUpload = uploadPolylines
	t.Cleanup(func() { instanceUpload = old })
	return r
}

func TestCompactPolylineGeometry(t *testing.T) {
	r := compactTestRenderer(t)
	for seed := range uint64(60) {
		rng := rand.New(rand.NewPCG(seed, 71))
		s := &scene.Scene{Width: 320, Height: 240, Clear: scene.Color{R: 19, G: 23, B: 31, A: 255}}
		for range 360 {
			ax, ay := rng.Float32()*360-20, rng.Float32()*280-20
			dx, dy := rng.Float32()*80-40, rng.Float32()*80-40
			length := float32(math.Hypot(float64(dx), float64(dy)))
			dx, dy = dx/length, dy/length
			w := .25 + rng.Float32()*24
			m := scene.Matrix(dx, dy, -dy, dx, ax, ay)
			if seed%2 == 1 {
				m = scene.Matrix(.9, .17, -.13, 1.1, 4, 7).Mul(m)
			}
			s.Ops = append(s.Ops, scene.Op{Kind: scene.OpFill,
				Rect: scene.Rect{X: -w / 2, Y: -w / 2, W: length + w, H: w}, Radii: scene.Uniform(w / 2),
				Color:   scene.Color{R: uint8(rng.Uint32()), G: uint8(rng.Uint32()), B: uint8(rng.Uint32()), A: 255},
				Opacity: 1, Transform: m})
		}
		instanceUpload = nil
		want := renderRaw(t, r, s, false)
		instanceUpload = uploadPolylines
		got := renderRaw(t, r, s, false)
		if n, _ := compactDrawn(r); n == 0 {
			t.Fatalf("seed %d: no compact geometry", seed)
		}
		if n := differing(got, want); n != 0 {
			for i := range got {
				if got[i] != want[i] {
					t.Fatalf("seed %d: %d bytes differ, first at (%d,%d)c%d: %d != %d", seed, n, i/4%s.Width, i/4/s.Width, i%4, got[i], want[i])
				}
			}
		}
	}
}

func compactScene(n int) *scene.Scene {
	s := &scene.Scene{Width: 640, Height: 480, Clear: scene.Color{R: 19, G: 23, B: 31, A: 255}}
	// Ordinary fills before and between the long runs force nonzero base
	// instances and transitions between both layouts in the same buffer.
	fill := scene.Op{Kind: scene.OpFill, Rect: scene.Rect{X: 3, Y: 3, W: 15, H: 15}, Color: scene.Color{R: 180, A: 255}, Opacity: 1}
	s.Ops = append(s.Ops, fill)
	for j := range 2 {
		for i := range n {
			ax, bx := 30+float32(i)*280/float32(n), 30+float32(i+1)*280/float32(n)
			ay, by := 100+float32(j)*200+70*float32(math.Sin(float64(i)*.37)), 100+float32(j)*200+70*float32(math.Sin(float64(i+1)*.37))
			dx, dy := bx-ax, by-ay
			length := float32(math.Hypot(float64(dx), float64(dy)))
			dx, dy = dx/length, dy/length
			s.Ops = append(s.Ops, scene.Op{Kind: scene.OpFill, Rect: scene.Rect{X: -1.625, Y: -1.625, W: length + 3.25, H: 3.25}, Radii: scene.Uniform(1.625), Color: scene.Color{R: 20, G: 82, B: 224, A: 255}, Opacity: 1, Transform: scene.Matrix(dx, dy, -dy, dx, ax, ay)})
		}
		fill.Rect.X += 380
		s.Ops = append(s.Ops, fill)
	}
	return s
}

func compactDrawn(r *Renderer) (count int, nonzero bool) {
	for j, b := range r.b.Batches {
		if r.drawState.pipes[j] == polylineProgram.pipe {
			count += b.Count
			nonzero = nonzero || b.Start > 0
		}
	}
	return
}

func TestCompactPolylineMixedBuffer(t *testing.T) {
	if unsafe.Sizeof(polylineInstance{}) != 112 || gpu.InstanceSize != 224 {
		t.Fatal("instance layout changed")
	}
	r := compactTestRenderer(t)
	s := compactScene(720)
	ops := slices.Clone(s.Ops)
	instanceUpload = nil
	want := renderRaw(t, r, s, false)
	instances := slices.Clone(r.b.Instances)
	instanceUpload = uploadPolylines
	got := renderRaw(t, r, s, false)
	if !bytes.Equal(got, want) {
		t.Fatalf("%d mixed-layout bytes differ", differing(got, want))
	}
	if n, nonzero := compactDrawn(r); n != 1440 || !nonzero {
		t.Fatalf("compact count=%d, nonzero base=%v", n, nonzero)
	}
	if !slices.Equal(ops, s.Ops) || !slices.Equal(instances, r.b.Instances) {
		t.Fatal("upload changed source geometry")
	}
	// Padding belongs to the slot but no compact shader may read it.
	for j, b := range r.b.Batches {
		if r.drawState.pipes[j] != polylineProgram.pipe {
			continue
		}
		p := unsafe.Slice((*byte)(ptr(send(r.instBufs[r.inUse], "contents"))), r.instCaps[r.inUse]*224)
		for i := b.Start*224 + b.Count*112; i < (b.Start+b.Count)*224; i++ {
			p[i] = 0xff
		}
	}
	// drawBuilt uses the prepared buffer, without another upload overwriting
	// the poison. Wait before release, just as an ordinary offscreen frame.
	pool(func() {
		tex := r.newTexture(s.Width, s.Height, pixelFormatBGRA8Unorm, usageRenderTarget|usageShaderRead, nil, 0)
		defer release(&tex)
		cb := send(r.queue, "commandBuffer")
		if err := r.drawBuilt(cb, s, tex, &r.drawState, nil); err != nil {
			t.Error(err)
			return
		}
		blit := send(cb, "blitCommandEncoder")
		send(blit, "synchronizeResource:", tex)
		send(blit, "endEncoding")
		send(cb, "commit")
		send(cb, "waitUntilCompleted")
		pix := make([]byte, s.Width*s.Height*4)
		msgGetBytes(tex, sel("getBytes:bytesPerRow:fromRegion:mipmapLevel:"), unsafe.Pointer(&pix[0]), uint(s.Width*4), mtlRegion{W: uint(s.Width), H: uint(s.Height), D: 1}, 0)
		if !bytes.Equal(pix, want) {
			t.Fatal("compact shaders read padding or another batch")
		}
	})
}

func TestCompactPolylineFallback(t *testing.T) {
	r := compactTestRenderer(t)
	s := compactScene(720)
	for _, kind := range []string{"signed-zero-clip", "opacity", "perspective", "legacy", "pending", "failed", "float16"} {
		t.Run(kind, func(t *testing.T) {
			copyScene := *s
			copyScene.Ops = slices.Clone(s.Ops)
			saved, modern := polylineProgram, r.canSpecialize
			defer func() { polylineProgram = saved; r.canSpecialize = modern }()
			switch kind {
			case "signed-zero-clip":
				copyScene.Ops = slices.Insert(copyScene.Ops, 0, scene.Op{Kind: scene.OpPushClip, Rect: scene.Rect{X: 20.25, Y: 10.5, W: 580, H: 440}, Continuous: true})
				copyScene.Ops = append(copyScene.Ops, scene.Op{Kind: scene.OpPopClip})
			case "opacity":
				for i := range copyScene.Ops {
					copyScene.Ops[i].Opacity = .5
				}
			case "perspective":
				for i := range copyScene.Ops {
					copyScene.Ops[i].Transform = scene.Projective(1, .1, -.1, 1, 3, 7, .0004, .0002).Mul(copyScene.Ops[i].Transform)
				}
			case "legacy":
				r.canSpecialize = false
			case "pending":
				polylineProgram = &shaderEntry{done: make(chan struct{})}
				defer close(polylineProgram.done)
			case "failed":
				polylineProgram = &shaderEntry{done: make(chan struct{})}
				close(polylineProgram.done)
			}
			instanceUpload = nil
			want := renderRaw(t, r, &copyScene, kind == "float16")
			instanceUpload = uploadPolylines
			var got []byte
			done := make(chan struct{})
			go func() { defer close(done); got = renderRaw(t, r, &copyScene, kind == "float16") }()
			wait.For(t, "fallback without waiting for shader compilation", done)
			if !bytes.Equal(got, want) {
				t.Fatal("fallback changed pixels")
			}
			if polylineProgram.pipe != 0 {
				if n, _ := compactDrawn(r); n != 0 {
					t.Fatalf("fallback packed %d instances", n)
				}
			}
		})
	}
}

func TestCompactPolylinePartialFrames(t *testing.T) {
	r := compactTestRenderer(t)
	s := compactScene(720)
	n := partialFrames(t, r, s, false, 18, func(step int) string {
		instanceUpload = uploadPolylines
		if step >= 4 && step < 8 {
			instanceUpload = nil
		}
		s.Ops[180].Transform.X += .125
		return fmt.Sprintf("compact upload interval %d", step/4)
	})
	if n == 0 {
		t.Fatal("no partial frame exercised")
	}
}

func TestCompactPolylineSlots(t *testing.T) {
	ref := compactTestRenderer(t)
	const frames = 8
	var scenes [frames]*scene.Scene
	var want [frames][]byte
	for i := range scenes {
		s := compactScene(720 + 180*i)
		s.Width += 20 * i
		s.Height += 10 * i
		canvas := scene.NewCanvas(s.Width, s.Height)
		*canvas.Scene = *compactScene(720)
		s.Ops = append(s.Ops, scene.Op{Kind: scene.OpImage, Rect: scene.Rect{X: 320, Y: 100, W: 260, H: 200},
			Image: canvas, Src: scene.Rect{W: float32(canvas.W), H: float32(canvas.H)}})
		scenes[i] = s
		instanceUpload = nil
		want[i] = renderRaw(t, ref, s, false)
	}
	r, err := newRenderer()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Release()
	instanceUpload = uploadPolylines
	var targets [frames]id
	pool(func() {
		defer func() {
			for i := range targets {
				release(&targets[i])
			}
		}()
		for i, s := range scenes {
			if i == 4 {
				// The idle path releases every slot, then lazily recreates it.
				r.waitLast()
				r.releaseTextures()
			}
			targets[i] = r.newTexture(s.Width, s.Height, pixelFormatBGRA8Unorm, usageRenderTarget|usageShaderRead, nil, 0)
			cb, err := r.encode(s, targets[i])
			if err != nil {
				t.Error(err)
				return
			}
			if n, _ := compactDrawn(r); n == 0 {
				t.Error("no compact frame")
			}
			fastCanvas := false
			for _, c := range r.canvases {
				fastCanvas = fastCanvas || slices.Contains(c.pipes, polylineProgram.pipe)
			}
			if !fastCanvas {
				t.Error("no compact canvas")
			}
			send(cb, "commit")
			r.track(cb)
			// Caller and Builder storage may change immediately after encoding.
			clear(s.Ops)
			clear(r.b.Instances)
		}
		// Release waits for the last two frames; their textures remain owned
		// here and can be read using another renderer on the same device.
		r.Release()
		for i, tex := range targets {
			s := scenes[i]
			cb := send(ref.queue, "commandBuffer")
			blit := send(cb, "blitCommandEncoder")
			send(blit, "synchronizeResource:", tex)
			send(blit, "endEncoding")
			send(cb, "commit")
			send(cb, "waitUntilCompleted")
			got := make([]byte, s.Width*s.Height*4)
			msgGetBytes(tex, sel("getBytes:bytesPerRow:fromRegion:mipmapLevel:"), unsafe.Pointer(&got[0]), uint(s.Width*4), mtlRegion{W: uint(s.Width), H: uint(s.Height), D: 1}, 0)
			if !bytes.Equal(got, want[i]) {
				t.Errorf("frame %d: %d bytes differ", i, differing(got, want[i]))
			}
		}
	})
	// The shared compact program outlives a renderer's destruction.
	next := compactTestRenderer(t)
	s := compactScene(720)
	instanceUpload = nil
	expected := renderRaw(t, next, s, false)
	instanceUpload = uploadPolylines
	if !bytes.Equal(renderRaw(t, next, s, false), expected) {
		t.Fatal("recreated renderer differs")
	}
}
