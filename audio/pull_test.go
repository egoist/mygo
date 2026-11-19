package audio

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/internal/wait"
)

// pullDevice takes every period at once and keeps the last one.
type pullDevice struct {
	mu     sync.Mutex
	opens  *atomic.Int64
	writes int
	last   []float32
	length int
}

func (d *pullDevice) write(samples []float32) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.writes++
	d.length = len(samples)
	d.last = append(d.last[:0], samples...)
	return nil
}

func (d *pullDevice) close() {}

// fakePull makes openOutput hand out a pullDevice of n frames a period,
// or fail with err, and puts the real one back when the test ends.
func fakePull(t *testing.T, n int, err error) *pullDevice {
	t.Helper()
	d := &pullDevice{opens: new(atomic.Int64)}
	openOutput = func(time.Duration) (device, int, error) {
		if err != nil {
			return nil, 0, err
		}
		d.opens.Add(1)
		return d, n, nil
	}
	t.Cleanup(func() { openOutput = openDevice })
	return d
}

// An Output pulls fill's frames into the device, period by period, and
// fill sees the period whole.
func TestOutputPullsFillIntoTheDevice(t *testing.T) {
	const period = 480 // 10 ms
	d := fakePull(t, period, nil)
	var fills atomic.Int64
	var lengths atomic.Int64
	out := NewOutput(func(dst []float32) {
		n := fills.Add(1)
		lengths.Store(int64(len(dst)))
		for i := range dst {
			dst[i] = float32(n % 7)
		}
	})
	out.Start()
	wait.Until(t, "three periods", func() bool { return fills.Load() >= 3 })
	out.Stop()
	if got, want := lengths.Load(), int64(period*channels); got != want {
		t.Errorf("fill saw %d samples a period, want %d", got, want)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.writes == 0 || d.length != period*channels {
		t.Fatalf("the device took %d samples of %d", d.length, d.writes)
	}
	want := float32(fills.Load() % 7)
	for i, s := range d.last {
		if s != want {
			t.Fatalf("the last period's sample %d = %v, want fill %d's %v", i, s, fills.Load(), want)
		}
	}
	if got := out.Delay(); got < LatencyInteractive.ahead() {
		t.Errorf("Delay = %v, want at least the buffer %v", got, LatencyInteractive.ahead())
	}
}

// An Output's buffer is the option's, and Delay reports at least it.
func TestOutputBufferOption(t *testing.T) {
	fakePull(t, 480, nil)
	var fills atomic.Int64
	out := NewOutput(func(dst []float32) { fills.Add(1) }, OutputOptions{Buffer: 100 * time.Millisecond})
	out.Start()
	wait.Until(t, "a period", func() bool { return fills.Load() >= 1 })
	out.Stop()
	if got, want := out.Delay(), 100*time.Millisecond; got < want {
		t.Errorf("Delay = %v, want at least the buffer %v", got, want)
	}
}

// Start does nothing while the output runs, Stop closes the device and
// fill runs no more, and Start after Close starts nothing.
func TestOutputStopAndClose(t *testing.T) {
	d := fakePull(t, 480, nil)
	var fills atomic.Int64
	out := NewOutput(func(dst []float32) { fills.Add(1) })
	out.Start()
	out.Start() // a second Start while running opens nothing
	wait.Until(t, "a period", func() bool { return fills.Load() >= 1 })
	if got := d.opens.Load(); got != 1 {
		t.Fatalf("the device opened %d times, want 1", got)
	}
	out.Stop()
	seen := fills.Load()
	for range 100 {
		if fills.Load() != seen {
			t.Fatalf("fill ran %d times after Stop, want still %d", fills.Load(), seen)
		}
	}
	out.Start()
	wait.Until(t, "a period after Start", func() bool { return fills.Load() > seen })
	if got := d.opens.Load(); got != 2 {
		t.Errorf("the device opened %d times, want 2", got)
	}
	out.Close()
	seen = fills.Load()
	out.Start() // closed: nothing starts
	out.Stop()
	if fills.Load() != seen {
		t.Errorf("fill ran %d times after Close, want still %d", fills.Load(), seen)
	}
}

// Where no device opens, fill still runs, on time through the null
// device, and Err says why, as for the players' output.
func TestOutputWithoutDeviceKeepsTimeAndSaysWhy(t *testing.T) {
	fakePull(t, 0, errors.New("audio: no device on this machine"))
	var fills atomic.Int64
	out := NewOutput(func(dst []float32) { fills.Add(1) })
	setErr(nil)
	out.Start()
	wait.Until(t, "a period", func() bool { return fills.Load() >= 1 })
	wait.Until(t, "the error", func() bool { return Err() != nil })
	out.Stop()
	if Err() == nil {
		t.Error("Err says nothing, want why no device opened")
	}
}

// No fill means silence, as a player with nothing to play.
func TestOutputWithoutFillIsSilence(t *testing.T) {
	d := fakePull(t, 64, nil)
	out := NewOutput(nil)
	out.Start()
	wait.Until(t, "a period", func() bool {
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.writes >= 1
	})
	out.Stop()
	d.mu.Lock()
	defer d.mu.Unlock()
	for i, s := range d.last {
		if s != 0 {
			t.Fatalf("sample %d = %v, want silence", i, s)
		}
	}
}

// NewOutput takes at most one OutputOptions.
func TestOutputTwoOptionsPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != "audio: NewOutput takes at most one OutputOptions" {
			t.Errorf("recovered %v", r)
		}
	}()
	NewOutput(nil, OutputOptions{}, OutputOptions{})
}

// Delay is what the samples wait: at least the buffer, over what the
// device took to accept a period.
func TestOutputDelayGrowsWithTheBuffer(t *testing.T) {
	fakePull(t, 480, nil)
	for _, buffer := range []time.Duration{10 * time.Millisecond, 250 * time.Millisecond} {
		var fills atomic.Int64
		out := NewOutput(func(dst []float32) { fills.Add(1) }, OutputOptions{Buffer: buffer})
		out.Start()
		wait.Until(t, "a period", func() bool { return fills.Load() >= 1 })
		got := out.Delay()
		out.Stop()
		if got < buffer {
			t.Errorf("a buffer of %v: Delay = %v, want at least it", buffer, got)
		}
	}
}
