package audio

import (
	"errors"
	"runtime"
	"sync"
	"time"
)

// A device is the system's audio output, opened by openDevice to hold
// about ahead of what is heard.
type device interface {
	// write plays interleaved stereo frames after those written before,
	// waiting while the device holds enough already. An error closes the
	// device, and the output opens it again.
	write(samples []float32) error
	close()
}

// A delayer is a device that knows how long a frame written waits before
// it is heard, more exactly than what it was asked to hold: the output
// asks after every write.
type delayer interface {
	delay() time.Duration
}

// idleClose is how long the output stays open with nothing playing.
const idleClose = 5 * time.Second

var (
	openOutput = openDevice // tests replace it

	wake     = make(chan struct{}, 1)
	runOnce  sync.Once
	testMode bool // tests mix by hand
)

// wakeOutput starts the output, or tells it a voice started. The caller
// holds mu.
func wakeOutput() {
	if testMode {
		return
	}
	runOnce.Do(func() { go run() })
	select {
	case wake <- struct{}{}:
	default:
	}
}

// opening reports whether anything plays, for the output to open the
// device, and how far ahead it is to hold: the least latency of the
// Outputs started, or else SetLatency's. Until the device says, a frame
// written waits that long.
func opening() (ahead time.Duration, ok bool) {
	mu.Lock()
	defer mu.Unlock()
	if testMode || len(voices) == 0 && len(outputs) == 0 {
		return 0, false
	}
	ahead = aheadLocked()
	lag = ahead
	return ahead, true
}

// aheadLocked returns how far ahead the device is to hold. The caller
// holds mu.
func aheadLocked() time.Duration {
	if len(outputs) == 0 {
		return latency.ahead()
	}
	a := outputs[0].latency
	for _, o := range outputs[1:] {
		a = min(a, o.latency)
	}
	return a
}

// mixFor mixes into buf for a device open to hold ahead, and reports
// whether anything plays; ok is false, and it mixes nothing, once tests
// mix by hand or the latency changed.
func mixFor(buf []float32, ahead time.Duration) (busy, ok bool) {
	mu.Lock()
	defer mu.Unlock()
	if testMode || aheadLocked() != ahead {
		return false, false
	}
	return mixLocked(buf), true
}

// lag is how long a frame written to the device waits before it is
// heard, or -1 while none is open, under mu.
var lag time.Duration = -1

func setLag(d time.Duration) {
	mu.Lock()
	defer mu.Unlock()
	lag = d
}

// run opens the device while anything plays, and mixes into it.
func run() {
	var fills []*Output // the Outputs filled in a period
	for {
		ahead, ok := opening()
		for !ok {
			<-wake
			ahead, ok = opening()
		}
		dev, n, err := openOutput(ahead)
		var dl delayer
		if err != nil {
			setErr(err)
			dev, n = newNullDevice(err != errUnsupported), nullFrames
			// It holds nothing: what it takes is heard as it takes it.
			setHeld(0)
			setLag(0)
		} else {
			setErr(nil)
			setHeld(int64(ahead * sampleRate / time.Second))
			dl, _ = dev.(delayer)
		}
		buf := make([]float32, n*channels)
		last := time.Duration(-1) // the lag the device said last
		idle := time.Duration(0)
		per := frames(int64(n), sampleRate)
		for {
			busy, ok := mixFor(buf, ahead)
			if !ok {
				break
			}
			if fillOutputs(buf, &fills) {
				busy = true
			}
			// write blocks in C, keeping the P, which the runtime would
			// take back every 10 ms, waking more often after.
			runtime.Gosched()
			if err := dev.write(buf); err != nil {
				// Lost, or a null device trying the real one again.
				break
			}
			if dl != nil {
				if d := dl.delay(); d > 0 && d != last {
					setLag(d)
					last = d
				}
			}
			if busy {
				idle = 0
			} else if idle += per; idle >= idleClose {
				break
			}
		}
		dev.close()
		setLag(-1)
	}
}

var errUnsupported = errors.New("audio: no audio output on this platform")

// nullFrames is how many frames a null device takes at a time: 10 ms.
const nullFrames = sampleRate / 100

// A nullDevice plays nothing, on time: players still end when they would
// have. One standing in for a device that failed to open fails after a
// while, so that the output tries the device again.
type nullDevice struct {
	next  time.Time
	retry time.Time
	// now and sleep are its clock, which tests replace.
	now   func() time.Time
	sleep func(time.Duration)
}

func newNullDevice(retry bool) *nullDevice {
	d := &nullDevice{next: time.Now(), now: time.Now, sleep: time.Sleep}
	if retry {
		d.retry = d.next.Add(2 * time.Second)
	}
	return d
}

var errRetry = errors.New("audio: try the device again")

func (d *nullDevice) write(samples []float32) error {
	now := d.now()
	if !d.retry.IsZero() && now.After(d.retry) {
		return errRetry
	}
	if now.Sub(d.next) > 100*time.Millisecond {
		// Behind, as after a sleep: don't hurry to catch up.
		d.next = now
	}
	d.next = d.next.Add(frames(int64(len(samples)/channels), sampleRate))
	d.sleep(d.next.Sub(d.now()))
	return nil
}

func (d *nullDevice) close() {}
