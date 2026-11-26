package audio

import (
	"slices"
	"time"
)

// An Output plays what an app computes itself, as the system needs it: an
// app with an audio engine of its own, which mixes, sets volumes, equalizes
// and crossfades, and keeps its own position, gives it each period's frames
// just before the device takes them, a known Delay ahead of the speakers,
// rather than decoding ahead into a Player's ring.
//
//	out := audio.NewOutput(func(dst []float32) {
//		engine.Render(dst) // interleaved stereo at 48 kHz
//	})
//	out.Start()
//	...
//	heard := engine.Rendered() - out.Delay() // what plays now
//
// It plays through the same output as the players: its frames are added
// to theirs, then clipped to [-1, 1]. audio.SetVolume doesn't apply to it,
// nor does audio.SetLatency while it runs (see SetLatency).
type Output struct {
	fill func(dst []float32)
	buf  []float32 // what fill fills, the output goroutine's

	// Under mu.
	latency time.Duration
	running bool
	closed  bool
}

// The latencies an Output can ask for: the device takes 10 ms at a time,
// and holds two of those at least.
const (
	minOutputLatency = 20 * time.Millisecond
	maxOutputLatency = time.Second
)

// outputs are the Outputs started, under mu.
var outputs []*Output

// NewOutput returns a stopped output that calls fill for each period the
// system's output takes, on the output's goroutine, with dst, interleaved
// stereo float32 samples in [-1, 1] at 48 kHz (left, right), cleared to
// silence. fill writes the period's samples into dst, which is valid only
// during the call and holds the same whole number of frames from call to
// call while the device stays open: 10 ms on most platforms, 512 frames on
// macOS, 1024 in browsers. It is late if it takes longer than that, as
// when it blocks, allocates a lot, or waits for a lock the app holds for
// long; the device then plays silence. NaNs are played as silence. A panic
// in fill is not recovered: it ends the program, as a Decoder's does.
func NewOutput(fill func(dst []float32)) *Output {
	if fill == nil {
		panic("audio: NewOutput takes a fill function")
	}
	return &Output{fill: fill, latency: LatencyInteractive.ahead()}
}

// SetLatency sets how much the device holds ahead of what is heard, from
// 20 ms to 1 s; 40 ms, the default, is LatencyInteractive's. Less reacts
// sooner and runs dry sooner when the system keeps the app waiting. While
// Outputs run the device holds the least of their latencies, whatever
// audio.SetLatency says, and an output already open opens again when that
// changes, which drops what it held. Delay says what the device does hold.
func (o *Output) SetLatency(d time.Duration) *Output {
	mu.Lock()
	defer mu.Unlock()
	o.latency = min(max(d, minOutputLatency), maxOutputLatency)
	return o
}

// Latency returns the latency SetLatency set.
func (o *Output) Latency() time.Duration {
	mu.Lock()
	defer mu.Unlock()
	return o.latency
}

// Start starts calling fill, opening the system's output if it is closed.
// It does nothing once the output is closed. Where no output can open,
// fill is still called on time, unheard, and audio.Err says why. The output
// runs until Stop or Close, even once the app holds it no more.
func (o *Output) Start() {
	mu.Lock()
	defer mu.Unlock()
	if o.closed || o.running {
		return
	}
	o.running = true
	outputs = append(outputs, o)
	wakeOutput()
}

// Stop stops calling fill: no call starts after it returns, though one
// already under way on the output's goroutine finishes. Start goes on
// again. Stop may be called from fill.
func (o *Output) Stop() {
	mu.Lock()
	defer mu.Unlock()
	o.stopLocked()
}

func (o *Output) stopLocked() {
	if !o.running {
		return
	}
	o.running = false
	outputs = slices.DeleteFunc(outputs, func(x *Output) bool { return x == o })
}

// Running reports whether the output calls fill: between Start and Stop.
func (o *Output) Running() bool {
	mu.Lock()
	defer mu.Unlock()
	return o.running
}

// Close stops the output for good, as Stop does. It may be called from
// fill.
func (o *Output) Close() {
	mu.Lock()
	defer mu.Unlock()
	o.stopLocked()
	o.closed = true
}

// Delay returns how long a frame that fill gave waits from fill's return
// until it is heard: what the device holds, and the system's latency after
// it where it says (see the docs for each platform's). A running engine's
// position heard is the position it rendered to less Delay. Before the
// output starts, and while the device opens, it is the latency asked for;
// where no device opens, it is 0.
func (o *Output) Delay() time.Duration {
	mu.Lock()
	defer mu.Unlock()
	if !o.running || lag < 0 {
		return o.latency
	}
	return lag
}

// begin reports whether fill is to be called for the next period.
func (o *Output) begin() bool {
	mu.Lock()
	defer mu.Unlock()
	return o.running
}

// fillOutputs adds what the Outputs running give to buf, which the players
// were mixed into, and reports whether any runs. fills is the output
// goroutine's, reused from period to period so that filling allocates
// nothing.
func fillOutputs(buf []float32, fills *[]*Output) bool {
	mu.Lock()
	*fills = append((*fills)[:0], outputs...)
	mu.Unlock()
	if len(*fills) == 0 {
		return false
	}
	for _, o := range *fills {
		// Unlocked: fill may call any function of the package.
		if !o.begin() {
			continue
		}
		if len(o.buf) != len(buf) {
			o.buf = make([]float32, len(buf)) // as the device opens
		} else {
			clear(o.buf)
		}
		o.fill(o.buf)
		for i, s := range o.buf {
			if s == s { // not NaN
				buf[i] += s
			}
		}
	}
	for i, s := range buf {
		buf[i] = min(max(s, -1), 1)
	}
	clear(*fills)
	return true
}
