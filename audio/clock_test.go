package audio

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/internal/wait"
)

// clockTestDevice is a device that plays nothing and reports how much it
// says it still holds, as a real one does.
type clockTestDevice struct {
	held   int64
	report bool
}

func (clockTestDevice) write([]float32) error { return nil }
func (clockTestDevice) close()                {}
func (d clockTestDevice) lag() (int64, bool)  { return d.held, d.report }

func TestPlayedLeavesOutWhatTheDeviceHolds(t *testing.T) {
	setup(t)
	t.Cleanup(func() {
		setDevice(nil)
		produced.Store(0)
	})

	s := NewSound(sampleRate, channels, make([]float32, sampleRate*channels))
	p := s.NewPlayer()
	// One second in at the mixer's read position, as a played second is.
	p.v.reset(sampleRate)
	produced.Store(2 * sampleRate)

	if got := p.Played(); got != time.Second {
		t.Fatalf("a device reporting nothing: Played = %v, want 1s", got)
	}
	if got := OutputClock().Position(); got != 2*time.Second {
		t.Fatalf("a device reporting nothing: clock = %v, want 2s", got)
	}

	// Half a second of what the device took is still to be heard.
	setDevice(clockTestDevice{held: sampleRate / 2, report: true})
	if got := p.Played(); got != time.Second/2 {
		t.Fatalf("half a second held: Played = %v, want 500ms", got)
	}
	if got := OutputClock().Position(); got != 3*time.Second/2 {
		t.Fatalf("half a second held: clock = %v, want 1.5s", got)
	}
	// The read position is where the mixer is, not the device.
	if got := p.Position(); got != time.Second {
		t.Fatalf("half a second held: Position = %v, want 1s", got)
	}

	// More held than produced, as just after a device opened: not behind 0.
	setDevice(clockTestDevice{held: 10 * sampleRate, report: true})
	if got := p.Played(); got != 0 {
		t.Fatalf("more held than produced: Played = %v, want 0", got)
	}
}

// A device whose lag holds until the test lets it go, so that the test can
// look at the output while the clock reads the device's position.
type blockingLagDevice struct {
	entered chan struct{} // closed once the position is being read
	release chan struct{} // the read returns when this is closed
}

func (*blockingLagDevice) write([]float32) error { return nil }
func (*blockingLagDevice) close()                {}
func (d *blockingLagDevice) lag() (int64, bool) {
	close(d.entered)
	<-d.release
	return 0, true
}

// A device whose close the test can pause, to see whether the clock still
// queries a device once the output has started closing it.
type closingDevice struct {
	closing chan struct{} // closed when close begins
	release chan struct{} // close returns when this is closed
	began   atomic.Bool
	queried atomic.Bool
}

func (*closingDevice) write([]float32) error { return nil }
func (d *closingDevice) close() {
	d.began.Store(true)
	close(d.closing)
	<-d.release
}
func (d *closingDevice) lag() (int64, bool) {
	if d.began.Load() {
		d.queried.Store(true)
	}
	return 0, true
}

// The clock never queries a device the output has begun closing: the
// output forgets the device before it closes it, so the native handle that
// close releases is not read by a query afterwards.
func TestClockNeverQueriesAClosingDevice(t *testing.T) {
	setup(t)
	d := &closingDevice{closing: make(chan struct{}), release: make(chan struct{})}
	setDevice(d)
	t.Cleanup(func() {
		setDevice(nil)
		produced.Store(0)
	})

	done := make(chan struct{})
	go func() {
		stopDevice(d)
		close(done)
	}()
	wait.For(t, "the output to start closing the device", d.closing)

	OutputClock().Position()
	if d.queried.Load() {
		close(d.release)
		<-done
		t.Fatal("the clock queried a device that was closing")
	}
	close(d.release)
	<-done
}

// The clock holds the device while it reads its position: closing the
// device takes deviceMu first, so it waits for a read in flight instead of
// releasing the native handle under it.
func TestClockHoldsDeviceWhileReadingIt(t *testing.T) {
	setup(t)
	d := &blockingLagDevice{entered: make(chan struct{}), release: make(chan struct{})}
	setDevice(d)
	t.Cleanup(func() {
		setDevice(nil)
		produced.Store(0)
	})

	done := make(chan struct{})
	go func() {
		OutputClock().Position()
		close(done)
	}()
	wait.For(t, "the clock to read the device's position", d.entered)

	// stopDevice forgets the device under deviceMu and closes it after, so
	// while the position is read the device must not be free to forget.
	if deviceMu.TryLock() {
		deviceMu.Unlock()
		close(d.release)
		<-done
		t.Fatal("the device could be closed while the clock read its position")
	}
	close(d.release)
	<-done
}
