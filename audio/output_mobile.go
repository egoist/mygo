//go:build ios || android

package audio

import (
	"time"
	"unsafe"

	"github.com/egoist/mygo/internal/mobile"
)

// Phones play through their hosts: AVAudioEngine on iOS, AudioTrack on Android.
// Writes block on this goroutine's thread, in the host, as the output fills.

// The host says how long a write waits before it is heard: the writes it
// holds, and on iOS the route's latency after them.
type mobileDevice struct{ lag time.Duration }

// mobileFrames is how many frames go in a write: 10 ms.
const mobileFrames = sampleRate / 100

func openDevice(ahead time.Duration) (device, int, error) {
	lag, err := mobile.AudioOpen(sampleRate, int(ahead/time.Millisecond))
	if err != nil {
		return nil, 0, err
	}
	return mobileDevice{lag}, mobileFrames, nil
}

func (d mobileDevice) delay() time.Duration { return d.lag }

func (mobileDevice) write(samples []float32) error {
	if len(samples) == 0 {
		return nil
	}
	b := unsafe.Slice((*byte)(unsafe.Pointer(&samples[0])), len(samples)*4)
	return mobile.AudioWrite(b)
}

func (mobileDevice) close() { mobile.AudioClose() }
