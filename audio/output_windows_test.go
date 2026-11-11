//go:build windows && (amd64 || arm64)

package audio

import (
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// Callbacks are allocated once; each fake interface routes through its owner.
var (
	wasapiTestClientVTable = [7]uintptr{6: syscall.NewCallback(wasapiTestGetCurrentPadding)}
	wasapiTestRenderVTable = [5]uintptr{
		3: syscall.NewCallback(wasapiTestGetBuffer),
		4: syscall.NewCallback(wasapiTestReleaseBuffer),
	}
)

type wasapiTestInterface struct {
	vtable unsafe.Pointer
	owner  *wasapiTestOutput
}

type wasapiTestOutput struct {
	client, render wasapiTestInterface
	buffer         [8]float32
	padding        uint32
	hr             [3]uintptr
	calls          [3]int
	step           int
	badOrder       bool
	getFrames      uintptr
	releaseFrames  uintptr
	releaseFlags   uintptr
}

func wasapiTestGetCurrentPadding(this, out uintptr) uintptr {
	f := (*wasapiTestInterface)(native(this)).owner
	f.calls[0]++
	f.badOrder = f.badOrder || f.step != 0
	f.step = 1
	*(*uint32)(native(out)) = f.padding
	return f.hr[0]
}

func wasapiTestGetBuffer(this, frames, out uintptr) uintptr {
	f := (*wasapiTestInterface)(native(this)).owner
	f.calls[1]++
	f.badOrder = f.badOrder || f.step != 1
	f.step = 2
	f.getFrames = frames
	*(*unsafe.Pointer)(native(out)) = unsafe.Pointer(&f.buffer[0])
	return f.hr[1]
}

func wasapiTestReleaseBuffer(this, frames, flags uintptr) uintptr {
	f := (*wasapiTestInterface)(native(this)).owner
	f.calls[2]++
	f.badOrder = f.badOrder || f.step != 2
	f.step = 0
	f.releaseFrames, f.releaseFlags = frames, flags
	return f.hr[2]
}

func newWASAPITestOutput(t *testing.T) (*wasapiDevice, *wasapiTestOutput) {
	t.Helper()
	f := &wasapiTestOutput{}
	f.client = wasapiTestInterface{unsafe.Pointer(&wasapiTestClientVTable), f}
	f.render = wasapiTestInterface{unsafe.Pointer(&wasapiTestRenderVTable), f}
	// The device holds native addresses, not Go pointers. Keep their owner
	// alive until cleanup, without registering callbacks for each fixture.
	t.Cleanup(func() { runtime.KeepAlive(f) })
	return &wasapiDevice{
		client: uintptr(unsafe.Pointer(&f.client)),
		render: uintptr(unsafe.Pointer(&f.render)),
		frames: 8,
		// Disable endpoint polling, regardless of how long the test takes.
		checked: time.Date(9999, time.January, 1, 0, 0, 0, 0, time.UTC),
	}, f
}

func TestWASAPIWrite(t *testing.T) {
	d, f := newWASAPITestOutput(t)
	// Exactly enough space remains for these four stereo frames.
	f.padding = 4
	samples := []float32{0.25, -0.25, 0.5, -0.5, 0.75, -0.75, 1, -1}
	if err := d.write(samples); err != nil {
		t.Fatal(err)
	}
	if f.buffer != [8]float32(samples) {
		t.Errorf("buffer = %v, want %v", f.buffer, samples)
	}
	if f.calls != [3]int{1, 1, 1} || f.badOrder || f.step != 0 {
		t.Errorf("calls = %v, bad order = %v, final step = %d", f.calls, f.badOrder, f.step)
	}
	if f.getFrames != 4 || f.releaseFrames != 4 || f.releaseFlags != 0 {
		t.Errorf("GetBuffer frames = %d, ReleaseBuffer frames = %d, flags = %d", f.getFrames, f.releaseFrames, f.releaseFlags)
	}
}

func TestWASAPIWriteAllocations(t *testing.T) {
	d, f := newWASAPITestOutput(t)
	samples := []float32{0.25, -0.25, 0.5, -0.5, 0.75, -0.75, 1, -1}
	var err error
	if n := testing.AllocsPerRun(1000, func() { err = d.write(samples) }); n != 0 {
		t.Errorf("write allocated %v times, want zero", n)
	}
	if err != nil {
		t.Fatal(err)
	}
	// AllocsPerRun also performs one warm-up call.
	if f.calls != [3]int{1001, 1001, 1001} || f.badOrder || f.step != 0 {
		t.Errorf("calls = %v, bad order = %v, final step = %d", f.calls, f.badOrder, f.step)
	}
}

func TestWASAPIWriteErrors(t *testing.T) {
	for i, method := range []string{
		"IAudioClient.GetCurrentPadding",
		"IAudioRenderClient.GetBuffer",
		"IAudioRenderClient.ReleaseBuffer",
	} {
		t.Run(method, func(t *testing.T) {
			d, f := newWASAPITestOutput(t)
			const hr = 0x88890004 // AUDCLNT_E_DEVICE_INVALIDATED
			f.hr[i] = hr
			err := d.write([]float32{0.5, -0.5})
			if err == nil || !strings.Contains(err.Error(), method+" failed (HRESULT 0x88890004)") {
				t.Fatalf("write error = %v, want %s HRESULT %#x", err, method, hr)
			}
			var want [3]int
			for j := 0; j <= i; j++ {
				want[j] = 1
			}
			if f.calls != want || f.badOrder {
				t.Errorf("calls = %v, want %v; bad order = %v", f.calls, want, f.badOrder)
			}
		})
	}
}
