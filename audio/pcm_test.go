package audio

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/internal/wait"
)

// A fakeDevice takes what it is given a little at a time, as a device
// waiting for room does, and keeps it.
type fakeDevice struct {
	mu      sync.Mutex
	periods [][]float32
}

func (d *fakeDevice) write(samples []float32) error {
	time.Sleep(time.Millisecond)
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.periods) < 4096 {
		d.periods = append(d.periods, append([]float32(nil), samples...))
	}
	return nil
}

func (d *fakeDevice) close() {}

// writes returns how many periods the device took.
func (d *fakeDevice) writes() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.periods)
}

// find returns the first period ok accepts, or nil.
func (d *fakeDevice) find(ok func([]float32) bool) []float32 {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, p := range d.periods {
		if ok(p) {
			return p
		}
	}
	return nil
}

// waitWrites waits for the device to take n more periods.
func (d *fakeDevice) waitWrites(t *testing.T, n int) {
	t.Helper()
	w := d.writes() + n
	wait.Until(t, "the device to take more", func() bool { return d.writes() >= w })
}

// A lateDevice says how long what it takes waits before it is heard.
type lateDevice struct {
	fakeDevice
	lag time.Duration
}

func (d *lateDevice) delay() time.Duration { return d.lag }

// runOutput runs the output goroutine for the test, on the devices open
// returns, and waits for it to close the device once the test ends.
func runOutput(t *testing.T, open func(ahead time.Duration) (device, int, error)) {
	t.Helper()
	setup(t)
	closed := func() bool {
		mu.Lock()
		defer mu.Unlock()
		return lag < 0
	}
	// What a test before left open closes, testMode set.
	wait.Until(t, "the device to close", closed)
	openOutput = open
	mu.Lock()
	testMode = false
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		testMode = true
		for _, o := range outputs {
			o.running = false
		}
		outputs = nil
		mu.Unlock()
		wait.Until(t, "the device to close", closed)
		openOutput = openDevice
	})
}

// openOn returns an open function for runOutput that opens d, and sends
// on opened how far ahead it was asked to hold.
func openOn(d device, opened chan<- time.Duration) func(time.Duration) (device, int, error) {
	return func(ahead time.Duration) (device, int, error) {
		if opened != nil {
			opened <- ahead
		}
		return d, nullFrames, nil
	}
}

// What fill gives is what the device takes, period after period.
func TestOutputReachesDevice(t *testing.T) {
	d := &fakeDevice{}
	opened := make(chan time.Duration, 8)
	runOutput(t, openOn(d, opened))
	var size atomic.Int64
	k := 0 // the output goroutine's
	o := NewOutput(func(dst []float32) {
		size.Store(int64(len(dst)))
		for i := 0; i < len(dst); i += 2 {
			dst[i] = float32(k%1000) / 1000
			dst[i+1] = -dst[i]
			k++
		}
	})
	defer o.Close()
	o.Start()
	if got := wait.For(t, "the device to open", opened); got != 40*time.Millisecond {
		t.Errorf("opened %v ahead, want 40ms", got)
	}
	d.waitWrites(t, 10)
	if got := size.Load(); got != nullFrames*channels {
		t.Errorf("fill got %d samples, want %d", got, nullFrames*channels)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	f := 0
	for _, p := range d.periods[:10] {
		for i := 0; i < len(p); i += 2 {
			if want := float32(f%1000) / 1000; p[i] != want || p[i+1] != -want {
				t.Fatalf("frame %d = %v, %v, want %v, %v", f, p[i], p[i+1], want, -want)
			}
			f++
		}
	}
}

// An Output plays over the players, clipped, and its NaNs are silence.
func TestOutputMixesWithPlayers(t *testing.T) {
	d := &fakeDevice{}
	runOutput(t, openOn(d, nil))
	samples := make([]float32, sampleRate)
	for i := range samples {
		samples[i] = 0.25
	}
	p := NewSound(sampleRate, 1, samples).NewPlayer().SetLoop(true)
	defer p.Close()
	nan := float32(0)
	nan /= nan
	o := NewOutput(func(dst []float32) {
		for i := 0; i < len(dst); i += 2 {
			dst[i], dst[i+1] = 0.9, nan
		}
	})
	defer o.Close()
	p.Play()
	o.Start()
	both := func(period []float32) bool {
		for i := 0; i < len(period); i += 2 {
			if period[i] != 1 || !near(period[i+1], 0.25) {
				return false
			}
		}
		return true
	}
	wait.Until(t, "the output over the player", func() bool { return d.find(both) != nil })
}

// Delay is what the device says it holds, or the latency asked for until
// it says.
func TestOutputDelay(t *testing.T) {
	d := &lateDevice{lag: 123 * time.Millisecond}
	opened := make(chan time.Duration, 8)
	runOutput(t, openOn(d, opened))
	o := NewOutput(func([]float32) {})
	defer o.Close()
	if got := o.Delay(); got != 40*time.Millisecond {
		t.Errorf("Delay before Start = %v, want the default 40ms", got)
	}
	for _, c := range []struct{ set, want time.Duration }{
		{time.Millisecond, 20 * time.Millisecond},
		{time.Hour, time.Second},
		{25 * time.Millisecond, 25 * time.Millisecond},
	} {
		if got := o.SetLatency(c.set).Latency(); got != c.want {
			t.Errorf("SetLatency(%v): Latency = %v, want %v", c.set, got, c.want)
		}
	}
	if got := o.Delay(); got != 25*time.Millisecond {
		t.Errorf("Delay = %v, want the latency, 25ms", got)
	}
	o.Start()
	if got := wait.For(t, "the device to open", opened); got != 25*time.Millisecond {
		t.Errorf("opened %v ahead, want 25ms", got)
	}
	wait.Until(t, "the device's delay", func() bool { return o.Delay() == 123*time.Millisecond })
	o.Stop()
	if got := o.Delay(); got != 25*time.Millisecond {
		t.Errorf("Delay once stopped = %v, want the latency, 25ms", got)
	}
}

// A device that says nothing of its delay holds what it was asked to.
func TestOutputDelayIsLatency(t *testing.T) {
	d := &fakeDevice{}
	runOutput(t, openOn(d, nil))
	o := NewOutput(func([]float32) {}).SetLatency(30 * time.Millisecond)
	defer o.Close()
	o.Start()
	d.waitWrites(t, 2)
	if got := o.Delay(); got != 30*time.Millisecond {
		t.Errorf("Delay = %v, want 30ms", got)
	}
}

// Where no device opens, fill is still called, its frames heard at once,
// and Err says why.
func TestOutputWithoutDevice(t *testing.T) {
	failure := errors.New("no device here")
	runOutput(t, func(time.Duration) (device, int, error) { return nil, 0, failure })
	var calls atomic.Int64
	o := NewOutput(func([]float32) { calls.Add(1) })
	defer o.Close()
	o.Start()
	wait.Until(t, "fill to be called", func() bool { return calls.Load() >= 3 })
	if got := o.Delay(); got != 0 {
		t.Errorf("Delay = %v, want 0", got)
	}
	if Err() != failure {
		t.Errorf("Err = %v", Err())
	}
}

// The device holds the least latency of the Outputs running, and opens
// again as that changes.
func TestOutputLatencyOpensDevice(t *testing.T) {
	d := &fakeDevice{}
	opened := make(chan time.Duration, 8)
	runOutput(t, openOn(d, opened))
	slow := NewOutput(func([]float32) {}).SetLatency(60 * time.Millisecond)
	fast := NewOutput(func([]float32) {}).SetLatency(30 * time.Millisecond)
	defer slow.Close()
	defer fast.Close()
	slow.Start()
	if got := wait.For(t, "the device to open", opened); got != 60*time.Millisecond {
		t.Errorf("opened %v ahead, want 60ms", got)
	}
	fast.Start()
	if got := wait.For(t, "the device to open again", opened); got != 30*time.Millisecond {
		t.Errorf("opened again %v ahead, want 30ms", got)
	}
	fast.Stop()
	if got := wait.For(t, "the device to open again", opened); got != 60*time.Millisecond {
		t.Errorf("opened again %v ahead, want 60ms", got)
	}
}

// fill is called between Start and Stop, again after Start, and never
// once closed.
func TestOutputStartStop(t *testing.T) {
	d := &fakeDevice{}
	runOutput(t, openOn(d, nil))
	var calls atomic.Int64
	o := NewOutput(func([]float32) { calls.Add(1) })
	o.Start()
	wait.Until(t, "fill to be called", func() bool { return calls.Load() >= 3 })
	// No call starts after Stop, but one under way.
	stopped := func(what string) {
		t.Helper()
		n := calls.Load()
		d.waitWrites(t, 3)
		if got := calls.Load(); got > n+1 {
			t.Errorf("%s: fill called %d times more", what, got-n)
		}
	}
	o.Stop()
	if o.Running() {
		t.Error("Running after Stop")
	}
	stopped("after Stop")
	o.Start()
	n := calls.Load()
	wait.Until(t, "fill to be called again", func() bool { return calls.Load() >= n+3 })
	o.Close()
	stopped("after Close")
	o.Start()
	if o.Running() {
		t.Error("Running after Start once closed")
	}
	stopped("after Start once closed")
}

// fill may stop its output.
func TestOutputStopsFromFill(t *testing.T) {
	d := &fakeDevice{}
	runOutput(t, openOn(d, nil))
	var calls atomic.Int64
	var o *Output
	o = NewOutput(func([]float32) {
		if calls.Add(1) == 3 {
			o.Stop()
		}
	})
	defer o.Close()
	o.Start()
	wait.Until(t, "fill to stop it", func() bool { return !o.Running() })
	d.waitWrites(t, 3)
	if got := calls.Load(); got != 3 {
		t.Errorf("fill called %d times, want 3", got)
	}
}

// Filling allocates nothing once running.
func TestOutputFillDoesNotAllocate(t *testing.T) {
	setup(t)
	o := NewOutput(func(dst []float32) {
		for i := range dst {
			dst[i] = 0.1
		}
	})
	defer o.Close()
	o.Start() // testMode: no device
	buf := make([]float32, 512*channels)
	var fills []*Output
	fillOutputs(buf, &fills)
	if n := testing.AllocsPerRun(200, func() { fillOutputs(buf, &fills) }); n != 0 {
		t.Errorf("fillOutputs allocated %v times a call", n)
	}
}

func TestNewOutputTakesFill(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("NewOutput(nil) did not panic")
		}
	}()
	NewOutput(nil)
}
