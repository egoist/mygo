//go:build linux && (amd64 || arm64)

package audio

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

// Linux plays through PulseAudio's simple API, which PipeWire serves as
// well, or else ALSA's default device. Both block while the device holds
// enough, from the output's goroutine.

const linuxFrames = sampleRate / 100 // per write: 10 ms

var (
	pulseLoad sync.Once
	pulseErr  error

	paSimpleNew   func(server, name *byte, dir int32, dev, stream *byte, spec *paSampleSpec, chmap, attr unsafe.Pointer, err *int32) uintptr
	paSimpleWrite uintptr // int pa_simple_write(pa_simple *, const void *, size_t, int *)
	paSimpleFree  func(s uintptr)
	paStrerror    func(err int32) string

	alsaLoad sync.Once
	alsaErr  error

	sndPcmOpen      func(pcm *uintptr, name *byte, stream int32, mode int32) int32
	sndPcmSetParams func(pcm uintptr, format, access int32, channels, rate uint32, softResample int32, latency uint32) int32
	sndPcmWritei    uintptr // snd_pcm_sframes_t snd_pcm_writei(snd_pcm_t *, const void *, snd_pcm_uframes_t)
	sndPcmRecover   func(pcm uintptr, err int32, silent int32) int32
	sndPcmClose     func(pcm uintptr) int32
	sndStrerror     func(err int32) string
)

type paSampleSpec struct {
	format   int32
	rate     uint32
	channels uint8
}

type paBufferAttr struct {
	maxLength, tLength, preBuf, minReq, fragSize uint32
}

const (
	paStreamPlayback  = 1
	paSampleFloat32LE = 5

	sndPcmStreamPlayback      = 0
	sndPcmFormatFloatLE       = 14
	sndPcmAccessRWInterleaved = 3
)

func cstr(s string) *byte {
	b := append([]byte(s), 0)
	return &b[0]
}

func appName() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Base(exe)
	}
	return "mygo"
}

func openDevice(ahead time.Duration) (device, int, error) {
	d, perr := openPulse(ahead)
	if perr == nil {
		return d, linuxFrames, nil
	}
	d, aerr := openALSA(ahead)
	if aerr == nil {
		return d, linuxFrames, nil
	}
	return nil, 0, errors.Join(perr, aerr)
}

// Typed purego wrappers use reflection and allocate; keep them for setup,
// teardown and recovery only. Writes reuse their escaping SyscallN argument
// arrays instead of allocating a variadic array for every buffer.
type pulseDevice struct {
	s    uintptr
	code int32
	args [4]uintptr
	data *float32
}

func openPulse(ahead time.Duration) (device, error) {
	pulseLoad.Do(func() {
		lib, err := purego.Dlopen("libpulse-simple.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			pulseErr = err
			return
		}
		purego.RegisterLibFunc(&paSimpleNew, lib, "pa_simple_new")
		paSimpleWrite, pulseErr = purego.Dlsym(lib, "pa_simple_write")
		if pulseErr != nil {
			return
		}
		purego.RegisterLibFunc(&paSimpleFree, lib, "pa_simple_free")
		purego.RegisterLibFunc(&paStrerror, lib, "pa_strerror")
	})
	if pulseErr != nil {
		return nil, pulseErr
	}
	spec := paSampleSpec{format: paSampleFloat32LE, rate: sampleRate, channels: channels}
	// ahead, asking for more every 10 ms.
	const frame = channels * 4
	attr := paBufferAttr{
		maxLength: ^uint32(0),
		tLength:   uint32(int64(ahead) * sampleRate / int64(time.Second) * frame),
		preBuf:    ^uint32(0),
		minReq:    linuxFrames * frame,
		fragSize:  ^uint32(0),
	}
	var code int32
	name := appName()
	s := paSimpleNew(nil, cstr(name), paStreamPlayback, nil, cstr(name), &spec, nil, unsafe.Pointer(&attr), &code)
	if s == 0 {
		return nil, fmt.Errorf("audio: PulseAudio: %s", paStrerror(code))
	}
	return &pulseDevice{s: s}, nil
}

func (d *pulseDevice) write(samples []float32) error {
	// Storing the pointer in the device forces the backing array onto the
	// heap: uintptrs in args alone would not keep a movable Go stack safe.
	// PulseAudio borrows both samples and code only until the call returns.
	d.data = &samples[0]
	d.args = [4]uintptr{d.s, uintptr(unsafe.Pointer(d.data)), uintptr(len(samples) * 4), uintptr(unsafe.Pointer(&d.code))}
	r, _, _ := purego.SyscallN(paSimpleWrite, d.args[:]...)
	runtime.KeepAlive(samples)
	d.data = nil
	if int32(r) < 0 {
		return fmt.Errorf("audio: PulseAudio: %s", paStrerror(d.code))
	}
	return nil
}

func (d *pulseDevice) close() { paSimpleFree(d.s) }

type alsaDevice struct {
	pcm  uintptr
	args [3]uintptr
	data *float32
}

func openALSA(ahead time.Duration) (device, error) {
	alsaLoad.Do(func() {
		lib, err := purego.Dlopen("libasound.so.2", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			alsaErr = err
			return
		}
		purego.RegisterLibFunc(&sndPcmOpen, lib, "snd_pcm_open")
		purego.RegisterLibFunc(&sndPcmSetParams, lib, "snd_pcm_set_params")
		sndPcmWritei, alsaErr = purego.Dlsym(lib, "snd_pcm_writei")
		if alsaErr != nil {
			return
		}
		purego.RegisterLibFunc(&sndPcmRecover, lib, "snd_pcm_recover")
		purego.RegisterLibFunc(&sndPcmClose, lib, "snd_pcm_close")
		purego.RegisterLibFunc(&sndStrerror, lib, "snd_strerror")
	})
	if alsaErr != nil {
		return nil, alsaErr
	}
	var pcm uintptr
	if e := sndPcmOpen(&pcm, cstr("default"), sndPcmStreamPlayback, 0); e < 0 {
		return nil, fmt.Errorf("audio: ALSA: %s", sndStrerror(e))
	}
	if e := sndPcmSetParams(pcm, sndPcmFormatFloatLE, sndPcmAccessRWInterleaved, channels, sampleRate, 1, uint32(ahead.Microseconds())); e < 0 {
		sndPcmClose(pcm)
		return nil, fmt.Errorf("audio: ALSA: %s", sndStrerror(e))
	}
	return &alsaDevice{pcm: pcm}, nil
}

func (d *alsaDevice) write(samples []float32) error {
	for len(samples) > 0 {
		// As for PulseAudio, force the buffer to escape before converting its
		// address to uintptr. Keep this slice alive through each call, before
		// shortening it or returning a recovery error; ALSA does not retain it.
		d.data = &samples[0]
		d.args = [3]uintptr{d.pcm, uintptr(unsafe.Pointer(d.data)), uintptr(len(samples) / channels)}
		r, _, _ := purego.SyscallN(sndPcmWritei, d.args[:]...)
		runtime.KeepAlive(samples)
		d.data = nil
		n := int(r) // snd_pcm_sframes_t is a signed C long on amd64 and arm64.
		if n < 0 {
			// An underrun or a suspend, recovered from by preparing the
			// device again.
			if e := sndPcmRecover(d.pcm, int32(n), 1); e < 0 {
				return fmt.Errorf("audio: ALSA: %s", sndStrerror(e))
			}
			continue
		}
		samples = samples[n*channels:]
	}
	return nil
}

func (d *alsaDevice) close() { sndPcmClose(d.pcm) }
