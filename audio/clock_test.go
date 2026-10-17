package audio

import (
	"testing"
	"time"
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
