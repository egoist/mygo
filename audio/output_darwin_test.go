package audio

import (
	"strings"
	"testing"
	"testing/synctest"
	"time"
	"unsafe"
)

// Buffer identity comes from the callback, not the enqueue order. These
// tests stub AudioToolbox, so they do not need a real output device.
func TestAudioQueueReturnedBuffers(t *testing.T) {
	oldEnqueue := audioQueueEnqueueBuffer
	t.Cleanup(func() { audioQueueEnqueueBuffer = oldEnqueue })
	a, b := [4]float32{}, [4]float32{}
	first := &audioQueueBuffer{audioData: unsafe.Pointer(&a[0])}
	second := &audioQueueBuffer{audioData: unsafe.Pointer(&b[0])}
	d := &aqDevice{q: 1, free: make(chan *audioQueueBuffer, 2), started: true}
	d.timeout = time.NewTimer(time.Second)
	d.timeout.Stop()
	t.Cleanup(func() { d.timeout.Stop() })
	d.free <- second
	d.free <- first
	var got [2]*audioQueueBuffer
	n := 0
	audioQueueEnqueueBuffer = func(q uintptr, buf *audioQueueBuffer, count uint32, descs uintptr) int32 {
		if q != d.q || count != 0 || descs != 0 {
			t.Error("unexpected enqueue arguments")
		}
		got[n] = buf
		n++
		return 0
	}
	samples := []float32{0.25, -0.25, 0.5, -0.5}
	for range 2 {
		if err := d.write(samples); err != nil {
			t.Fatal(err)
		}
	}
	if got != [2]*audioQueueBuffer{second, first} {
		t.Fatalf("enqueued buffers = %v, want returned order", got)
	}
	if a != [4]float32(samples) || b != a || first.audioDataByteSize != 16 || second.audioDataByteSize != 16 {
		t.Fatalf("buffer contents or sizes differ: %v, %v", a, b)
	}
}

func TestAudioQueueWriteAllocations(t *testing.T) {
	oldEnqueue := audioQueueEnqueueBuffer
	t.Cleanup(func() { audioQueueEnqueueBuffer = oldEnqueue })
	data := [2]float32{}
	buf := &audioQueueBuffer{audioData: unsafe.Pointer(&data[0])}
	d := &aqDevice{q: 1, free: make(chan *audioQueueBuffer, 1), started: true}
	d.timeout = time.NewTimer(time.Second)
	d.timeout.Stop()
	t.Cleanup(func() { d.timeout.Stop() })
	audioQueueEnqueueBuffer = func(uintptr, *audioQueueBuffer, uint32, uintptr) int32 {
		d.free <- buf
		return 0
	}
	d.free <- buf
	var err error
	samples := []float32{0.5, -0.5}
	if n := testing.AllocsPerRun(100, func() { err = d.write(samples) }); n != 0 {
		t.Errorf("ready-buffer write allocated %v times", n)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func TestAudioQueueWaitingWriteAllocations(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		oldEnqueue := audioQueueEnqueueBuffer
		defer func() { audioQueueEnqueueBuffer = oldEnqueue }()
		audioQueueEnqueueBuffer = func(uintptr, *audioQueueBuffer, uint32, uintptr) int32 { return 0 }
		data := [2]float32{}
		buf := &audioQueueBuffer{audioData: unsafe.Pointer(&data[0])}
		d := &aqDevice{q: 1, free: make(chan *audioQueueBuffer), started: true}
		d.timeout = time.NewTimer(time.Second)
		d.timeout.Stop()
		defer d.timeout.Stop()
		requests := make(chan struct{})
		defer close(requests)
		go func() {
			for range requests {
				// No buffer is returned until write is durably blocked in
				// its slow path. Reuse this goroutine outside the measurement.
				synctest.Wait()
				d.free <- buf
			}
		}()
		samples := []float32{0.5, -0.5}
		var err error
		if n := testing.AllocsPerRun(100, func() {
			requests <- struct{}{}
			if e := d.write(samples); e != nil {
				err = e
			}
		}); n != 0 {
			t.Errorf("waiting write allocated %v times", n)
		}
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestAudioQueueWaitReusesTimer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		oldEnqueue := audioQueueEnqueueBuffer
		defer func() { audioQueueEnqueueBuffer = oldEnqueue }()
		audioQueueEnqueueBuffer = func(uintptr, *audioQueueBuffer, uint32, uintptr) int32 { return 0 }
		data := [2]float32{}
		buf := &audioQueueBuffer{audioData: unsafe.Pointer(&data[0])}
		d := &aqDevice{q: 1, free: make(chan *audioQueueBuffer), started: true}
		d.timeout = time.NewTimer(time.Second)
		d.timeout.Stop()
		defer d.timeout.Stop()
		samples := []float32{0.5, -0.5}
		// Only the bubble's clock advances: no slow CI runner can make a
		// correct callback miss its deadline.
		if err := d.write(samples); err == nil || !strings.Contains(err.Error(), "queue stopped") {
			t.Fatalf("write without a callback = %v", err)
		}
		for range 2 {
			var err error
			go func() { err = d.write(samples) }()
			synctest.Wait() // write is parked waiting for the callback
			d.free <- buf
			synctest.Wait()
			if err != nil {
				t.Fatal(err)
			}
		}
	})
}

func TestAudioQueueCloseWithoutTimer(t *testing.T) {
	oldStop, oldDispose := audioQueueStop, audioQueueDispose
	t.Cleanup(func() { audioQueueStop, audioQueueDispose = oldStop, oldDispose })
	stopped, disposed := false, false
	audioQueueStop = func(q uintptr, immediate bool) int32 {
		stopped = q == 1 && immediate
		return 0
	}
	audioQueueDispose = func(q uintptr, immediate bool) int32 {
		disposed = q == 1 && immediate
		return 0
	}
	(&aqDevice{q: 1}).close()
	if !stopped || !disposed {
		t.Fatalf("close: stopped = %v, disposed = %v", stopped, disposed)
	}
}

func TestAudioQueueEnqueueError(t *testing.T) {
	oldEnqueue := audioQueueEnqueueBuffer
	t.Cleanup(func() { audioQueueEnqueueBuffer = oldEnqueue })
	data := [2]float32{}
	d := &aqDevice{q: 1, free: make(chan *audioQueueBuffer, 1), started: true}
	d.free <- &audioQueueBuffer{audioData: unsafe.Pointer(&data[0])}
	audioQueueEnqueueBuffer = func(uintptr, *audioQueueBuffer, uint32, uintptr) int32 { return -50 }
	if err := d.write([]float32{0, 0}); err == nil || !strings.Contains(err.Error(), "OSStatus -50") {
		t.Fatalf("write error = %v, want OSStatus -50", err)
	}
}
