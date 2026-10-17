//go:build windows && (amd64 || arm64)

package windows

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// A transform that exposes the drain protocol without depending on which
// hardware encoder the machine happens to choose. Callbacks are shared.
type drainTransform struct {
	vtbl                     uintptr
	messages                 []uintptr
	outputs, waits, releases int
	drainHR                  uintptr
}

var drainTestOnce sync.Once
var drainTestVtable [26]uintptr

func newDrainTransform() *drainTransform {
	drainTestOnce.Do(func() {
		drainTestVtable[slotRelease] = syscall.NewCallback(func(this uintptr) uintptr {
			d := (*drainTransform)(native(this))
			d.releases++
			return 0
		})
		drainTestVtable[slotTransformProcessMessage] = syscall.NewCallback(func(this, message, param uintptr) uintptr {
			d := (*drainTransform)(native(this))
			d.messages = append(d.messages, message)
			if message == messageCommandDrain {
				return d.drainHR
			}
			return 0
		})
		drainTestVtable[slotTransformProcessOutput] = syscall.NewCallback(func(this, flags, count, buffers, status uintptr) uintptr {
			d := (*drainTransform)(native(this))
			d.outputs++
			return hrNeedMoreInput
		})
		drainTestVtable[slotEventGet] = syscall.NewCallback(func(this, flags, event uintptr) uintptr {
			d := (*drainTransform)(native(this))
			d.waits++
			return 0x80004005
		})
	})
	return &drainTransform{vtbl: uintptr(unsafe.Pointer(&drainTestVtable[0]))}
}

// Short streams must survive encoder look-ahead, even when no output was
// available before Close. Log the native stages for remote ARM64 diagnostics.
func TestH264EncoderKeepsShortStreams(t *testing.T) {
	for _, frames := range []int{1, 5, 90} {
		t.Run(fmt.Sprint(frames), func(t *testing.T) {
			t.Log("opening encoder")
			e, err := NewH264Encoder(160, 120, 500000, 30)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { e.Close() })
			t.Logf("opened encoder (async=%v)", e.async)
			pixels := make([]byte, 160*120*4)
			packets := 0
			for i := 0; i < frames; i++ {
				t.Logf("encoding frame %d", i)
				if err := e.Encode(pixels, time.Duration(i)*time.Second/30); err != nil {
					t.Fatal(err)
				}
				packets += len(e.Take())
			}
			t.Logf("draining encoder, %d packets before Close", packets)
			if err := e.Close(); err != nil {
				t.Fatal(err)
			}
			packets += len(e.Take())
			if packets != frames {
				t.Fatalf("encoded %d frames, received %d packets", frames, packets)
			}
		})
	}
}

func TestSynchronousEncoderDrainsBeforeReleasing(t *testing.T) {
	d := newDrainTransform()
	defer runtime.KeepAlive(d)
	e := &H264Encoder{p: uintptr(unsafe.Pointer(d)), provides: true}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	if d.outputs == 0 {
		t.Fatal("Close discarded buffered output without calling ProcessOutput")
	}
	if len(d.messages) < 2 || d.messages[0] != messageNotifyEndOfStream || d.messages[1] != messageCommandDrain {
		t.Fatalf("messages = %v; want END_OF_STREAM then DRAIN", d.messages)
	}
	if d.releases != 1 {
		t.Fatalf("releases = %d; want 1", d.releases)
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	if d.releases != 1 {
		t.Fatal("Close released the transform twice")
	}
}

func TestEncoderDoesNotWaitAfterRejectedDrain(t *testing.T) {
	d := newDrainTransform()
	d.drainHR = 0x80004005
	defer runtime.KeepAlive(d)
	addr := uintptr(unsafe.Pointer(d))
	e := &H264Encoder{p: addr, events: addr, async: true}
	if err := e.Close(); err == nil {
		t.Fatal("Close lost the drain failure")
	}
	if d.waits != 0 {
		t.Fatal("Close waited for an event after DRAIN failed")
	}
	if d.releases != 2 {
		t.Fatalf("releases = %d; want both interfaces released", d.releases)
	}
}

func TestEncoderReportsDrainEventFailure(t *testing.T) {
	d := newDrainTransform()
	defer runtime.KeepAlive(d)
	addr := uintptr(unsafe.Pointer(d))
	e := &H264Encoder{p: addr, events: addr, async: true}
	if err := e.Close(); err == nil {
		t.Fatal("Close lost the GetEvent failure")
	}
	if d.waits != 1 {
		t.Fatalf("GetEvent calls = %d; want 1", d.waits)
	}
	if d.releases != 2 {
		t.Fatalf("releases = %d; want 2", d.releases)
	}
}
