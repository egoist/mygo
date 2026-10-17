package audio

import (
	"sync"
	"sync/atomic"
	"time"
)

// A Clock reports how far the output has been played: the position on the
// output timeline, counting from when the device opened. It is the clock to
// pace pictures against, rather than [Player.Position], which says how far
// the mixer has read and so runs ahead by whatever the device still holds.
type Clock interface {
	// Position returns how much of the output has been played.
	Position() time.Duration
}

// OutputClock returns the clock of the audio output. Its position counts
// from the moment the output opened, and stands still while nothing plays.
//
// Where the platform cannot say how much it still holds, Position is the
// mixer's read position, up to one output buffer ahead of what is heard,
// and [Player.Played] runs that far ahead too. The macOS Audio Queue,
// WASAPI, PulseAudio and ALSA outputs all report it; the browser one
// cannot, as its worklet keeps its own position.
func OutputClock() Clock { return outputClock{} }

type outputClock struct{}

func (outputClock) Position() time.Duration {
	return frames(playedFrames(), sampleRate)
}

// produced is how many output frames the mixer has handed to the device
// since it opened.
var produced atomic.Int64

var (
	deviceMu  sync.Mutex
	deviceCur device // the output's device, for its own position report
)

// A deviceLag is a device that can say how many frames it still holds,
// which it has taken but not played yet.
type deviceLag interface {
	lag() (frames int64, ok bool)
}

// setDevice records the device the output plays into, or nil when it
// closes. The caller is the output goroutine.
func setDevice(d device) {
	deviceMu.Lock()
	deviceCur = d
	deviceMu.Unlock()
}

// playedFrames returns how many output frames the device has played: what
// the mixer handed it, less what it says it still holds.
//
// The device is asked under deviceMu: stopDevice, which closes it, takes
// the same lock to forget it, so a device's close releases its native
// handle only after a read in flight has returned.
func playedFrames() int64 {
	n := produced.Load()
	deviceMu.Lock()
	defer deviceMu.Unlock()
	if l, ok := deviceCur.(deviceLag); ok {
		if held, ok := l.lag(); ok {
			return max(n-held, 0)
		}
	}
	return n
}

// playedLag returns the output frames the device has taken but not played.
func playedLag() int64 {
	return max(produced.Load()-playedFrames(), 0)
}

// Played returns how much of the source has reached the device, against
// Position's how far the mixer has read. Pace pictures against this, or
// against [OutputClock]: sound that has not been heard yet is not time the
// pictures may run ahead into.
func (p *Player) Played() time.Duration {
	mu.Lock()
	defer mu.Unlock()
	return time.Duration(p.v.played() / p.v.rate * float64(time.Second))
}

// played returns the source frame the device has played.
func (v *voice) played() float64 {
	// The mixer reads ahead of the device by the output frames the device
	// still holds, which are the voice's own frames at its rate.
	step := v.rate / sampleRate * v.speed
	return max(v.position()-float64(playedLag())*step, 0)
}
