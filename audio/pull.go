package audio

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// An Output plays what an app mixes itself. fill is called on the output's
// thread for every period the device takes, with dst holding interleaved
// stereo float32 samples at 48 kHz: the app writes what it has mixed, the
// output sends it on. Nothing is decoded or mixed with other sounds, so
// an app with its own engine — its own volume, equalizer, crossfades and
// playback position — hears its changes as soon as the device's buffer
// lets them, and Delay says how long that is, to time lyrics against.
//
// Unlike the players' output, which opens while something plays and
// closes five seconds after, an Output opens when it is started and stays
// until it is stopped. Where no output opens, as on a machine with no
// audio device, fill still runs on time and the samples go unheard, as
// players do, and audio.Err says why.
//
// One audio session serves a phone's app and one AudioContext its page,
// so an Output and players at once is not what those hosts hold yet: an
// app that pulls plays no sound through a Player, and one that plays
// sounds starts no Output.
//
// Every method is safe to call from any goroutine. fill runs only on the
// output's thread; it may take as long as mixing takes, and the output
// waits for it before the next period.
type Output struct {
	fill  func(dst []float32)
	ahead time.Duration

	mu     sync.Mutex
	run    *pullRun // the loop running now, or nil
	closed bool     // after Close, which starts nothing again

	delay atomic.Int64 // nanoseconds, as Delay reports
}

// OutputOptions say how an Output gives the app's samples to the device.
// The zero options are those of NewOutput without them.
type OutputOptions struct {
	// Buffer is how long the device holds the app's samples before they
	// are heard: LatencyInteractive's 40 ms by default, as the players'
	// output mixes ahead. It is what Delay reports, plus the wait for the
	// device to take a period.
	Buffer time.Duration
}

// NewOutput returns an output that pulls what fill mixes, period by
// period. Start it to open the device, Stop to close it, Close for good.
//
//	fill := func(dst []float32) {
//		mix(dst) // interleaved stereo at 48 kHz
//	}
//	out := audio.NewOutput(fill)
//	out.Start()
//	defer out.Close()
//
//	out.Delay() // what fill's samples wait before they are heard
func NewOutput(fill func(dst []float32), options ...OutputOptions) *Output {
	if len(options) > 1 {
		panic("audio: NewOutput takes at most one OutputOptions")
	}
	o := &Output{fill: fill, ahead: LatencyInteractive.ahead()}
	if len(options) == 1 && options[0].Buffer > 0 {
		o.ahead = options[0].Buffer
	}
	o.delay.Store(int64(o.ahead))
	return o
}

// Start opens the device and begins pulling fill, period by period. It
// does nothing while the output runs or after Close, as Player.Play does
// on a closed player.
func (o *Output) Start() {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed || o.run != nil {
		return
	}
	r := &pullRun{quit: make(chan struct{}), done: make(chan struct{})}
	o.run = r
	o.delay.Store(int64(o.ahead))
	go o.loop(r)
}

// Stop closes the device and returns once fill runs no more: the period
// in flight finishes first. The output starts again with Start.
func (o *Output) Stop() {
	if o == nil {
		return
	}
	o.mu.Lock()
	r := o.run
	o.mu.Unlock()
	if r == nil {
		return
	}
	r.stop()
	o.mu.Lock()
	if o.run == r {
		o.run = nil
	}
	o.mu.Unlock()
}

// Close stops the output for good: it closes the device and starts
// nothing again, as Player.Close does.
func (o *Output) Close() {
	if o == nil {
		return
	}
	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		return
	}
	o.closed = true
	o.mu.Unlock()
	o.Stop()
}

// Delay returns what fill's samples wait before they are heard: the
// buffer the device holds (OutputOptions' Buffer), and the wait for it
// to take the period just mixed. It holds through fill, at what the last
// period measured.
func (o *Output) Delay() time.Duration {
	if o == nil {
		return 0
	}
	return time.Duration(o.delay.Load())
}

// pullRun is one run of the output's loop: the signal to end it, and
// the signal that it has ended.
type pullRun struct {
	quit chan struct{}
	done chan struct{}
	once sync.Once
}

func (r *pullRun) stop() {
	r.once.Do(func() { close(r.quit) })
	<-r.done
}

// loop opens the device and pulls fill into it until stopped or the
// device fails.
func (o *Output) loop(r *pullRun) {
	ahead := o.ahead
	dev, n, err := openOutput(ahead)
	if err != nil {
		setErr(err)
		dev, n = newNullDevice(true), nullFrames
	} else {
		setErr(nil)
	}
	buf := make([]float32, n*channels)
	defer func() {
		dev.close()
		o.mu.Lock()
		if o.run == r {
			o.run = nil
		}
		o.mu.Unlock()
		close(r.done)
	}()
	for {
		select {
		case <-r.quit:
			return
		default:
		}
		if o.fill != nil {
			o.fill(buf)
		} else {
			clear(buf)
		}
		took := time.Now()
		if err := dev.write(buf); err != nil {
			// Lost the device: the run ends, and Start opens it again.
			return
		}
		// write blocks in C, keeping the P, which the runtime would take
		// back every 10 ms, waking more often after.
		runtime.Gosched()
		// fill's samples waited for the device to take them, and then
		// wait out the buffer it holds.
		o.delay.Store(int64(ahead + time.Since(took)))
	}
}
