//go:build linux && (amd64 || arm64)

package audio

import (
	"sync"

	"github.com/ebitengine/purego"
)

// The output's own position, from the server or the device: both keep the
// frames they hold and will say how many, which is what the mixer's read
// position runs ahead by. Loaded apart from the playback symbols so that a
// library without them still plays.

var (
	clockLoad sync.Once

	paSimpleGetLatency func(s uintptr, code *int32) uint64
	sndPcmDelay        func(pcm uintptr, frames *int64) int32
)

func loadClock() {
	clockLoad.Do(func() {
		if lib, err := purego.Dlopen("libpulse-simple.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL); err == nil {
			if fn, err := purego.Dlsym(lib, "pa_simple_get_latency"); err == nil {
				purego.RegisterFunc(&paSimpleGetLatency, fn)
			}
		}
		if lib, err := purego.Dlopen("libasound.so.2", purego.RTLD_NOW|purego.RTLD_GLOBAL); err == nil {
			if fn, err := purego.Dlsym(lib, "snd_pcm_delay"); err == nil {
				purego.RegisterFunc(&sndPcmDelay, fn)
			}
		}
	})
}

// lag returns the frames PulseAudio holds, which its latency report gives
// in microseconds. PipeWire's PulseAudio server answers the same way.
func (d *pulseDevice) lag() (int64, bool) {
	loadClock()
	if paSimpleGetLatency == nil {
		return 0, false
	}
	var code int32
	usec := paSimpleGetLatency(d.s, &code)
	if code < 0 {
		return 0, false
	}
	return int64(usec) * sampleRate / 1e6, true
}

// lag returns the frames ALSA holds: the device's own delay, less than what
// was written whenever it is behind.
func (d *alsaDevice) lag() (int64, bool) {
	loadClock()
	if sndPcmDelay == nil {
		return 0, false
	}
	var delay int64
	if e := sndPcmDelay(d.pcm, &delay); e < 0 {
		return 0, false
	}
	return max(delay, 0), true
}
