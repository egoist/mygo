//go:build linux && (amd64 || arm64)

package audio

import (
	"runtime"

	"testing"
	"unsafe"

	"github.com/ebitengine/purego"
)

// Register once per signature, routing by the device handle, not per fixture.
var (
	linuxTestPulseWrite = purego.NewCallback(linuxTestWritePulse)
	linuxTestALSAWrite  = purego.NewCallback(linuxTestWriteALSA)
)

type linuxTestWrite struct {
	data   *float32
	frames uintptr
}

type linuxTestOutput struct {
	allocationOnly bool
	pulseResult    uintptr
	pulseCode      int32
	pulseBytes     uintptr
	pulseData      [8]float32
	results        []int
	writes         [8]linuxTestWrite
	calls          int
}

func linuxTestWritePulse(f *linuxTestOutput, data *float32, bytes uintptr, code *int32) uintptr {
	f.calls++
	f.pulseBytes = bytes
	copy(f.pulseData[:], unsafe.Slice(data, int(bytes/4)))
	*code = f.pulseCode
	return f.pulseResult
}

func linuxTestWriteALSA(f *linuxTestOutput, data *float32, frames uintptr) uintptr {
	if f.allocationOnly {
		f.calls++
		return frames
	}
	f.writes[f.calls] = linuxTestWrite{data, frames}
	n := int(frames)
	if f.calls < len(f.results) {
		n = f.results[f.calls]
	}
	f.calls++
	return uintptr(n)
}

func newLinuxTestOutput(t *testing.T) (*pulseDevice, *alsaDevice, *linuxTestOutput) {
	t.Helper()
	oldPulse, oldALSA := paSimpleWrite, sndPcmWritei
	oldPulseError, oldALSAError, oldRecover := paStrerror, sndStrerror, sndPcmRecover
	t.Cleanup(func() {
		paSimpleWrite, sndPcmWritei = oldPulse, oldALSA
		paStrerror, sndStrerror, sndPcmRecover = oldPulseError, oldALSAError, oldRecover
	})
	paSimpleWrite, sndPcmWritei = linuxTestPulseWrite, linuxTestALSAWrite
	f := &linuxTestOutput{}
	// Production handles are native addresses; keep this Go owner alive even
	// though the device only holds its address as a uintptr.
	t.Cleanup(func() { runtime.KeepAlive(f) })
	handle := uintptr(unsafe.Pointer(f))
	return &pulseDevice{s: handle}, &alsaDevice{pcm: handle}, f
}

func TestPulseWrite(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "success"
		if fail {
			name = "error"
		}
		t.Run(name, func(t *testing.T) {
			d, _, f := newLinuxTestOutput(t)
			if fail {
				// C int returns need not sign-extend the upper 32 bits.
				f.pulseResult, f.pulseCode = uintptr(^uint32(0)), 17
			}
			errorCalls := 0
			paStrerror = func(code int32) string {
				errorCalls++
				if code != 17 {
					t.Errorf("error code = %d, want 17", code)
				}
				return "pulse write failed"
			}
			samples := []float32{0.25, -0.25, 0.5, -0.5, 0.75, -0.75, 1, -1}
			err := d.write(samples)
			if fail {
				if err == nil || err.Error() != "audio: PulseAudio: pulse write failed" || errorCalls != 1 {
					t.Fatalf("write error = %v, strerror calls = %d", err, errorCalls)
				}
			} else if err != nil || errorCalls != 0 {
				t.Fatalf("write error = %v, strerror calls = %d", err, errorCalls)
			}
			if f.calls != 1 || f.pulseBytes != uintptr(len(samples)*4) || f.pulseData != [8]float32(samples) {
				t.Errorf("calls = %d, bytes = %d, data = %v", f.calls, f.pulseBytes, f.pulseData)
			}
			if d.data != nil {
				t.Error("write retained the sample buffer")
			}
		})
	}
}

func TestALSAWrite(t *testing.T) {
	for _, tt := range []struct {
		name       string
		results    []int
		recoverErr int32
		wantError  bool
	}{
		{name: "success", results: []int{4}},
		{name: "partial", results: []int{1, 1, 2}},
		{name: "underrun", results: []int{-32, 4}},
		{name: "suspend after partial", results: []int{1, -86, 3}},
		{name: "recovery error", results: []int{1, -32}, recoverErr: -5, wantError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, d, f := newLinuxTestOutput(t)
			f.results = tt.results
			recoverCalls, errorCalls := 0, 0
			sndPcmRecover = func(pcm uintptr, code, silent int32) int32 {
				recoverCalls++
				if pcm != d.pcm || code != int32(tt.results[f.calls-1]) || silent != 1 {
					t.Errorf("unexpected recover arguments: %#x, %d, %d", pcm, code, silent)
				}
				return tt.recoverErr
			}
			sndStrerror = func(code int32) string {
				errorCalls++
				if code != tt.recoverErr {
					t.Errorf("error code = %d, want %d", code, tt.recoverErr)
				}
				return "alsa recovery failed"
			}
			samples := []float32{0.25, -0.25, 0.5, -0.5, 0.75, -0.75, 1, -1}
			err := d.write(samples)
			if tt.wantError {
				if err == nil || err.Error() != "audio: ALSA: alsa recovery failed" || errorCalls != 1 {
					t.Fatalf("write error = %v, strerror calls = %d", err, errorCalls)
				}
			} else if err != nil || errorCalls != 0 {
				t.Fatalf("write error = %v, strerror calls = %d", err, errorCalls)
			}
			if f.calls != len(tt.results) {
				t.Fatalf("write calls = %d, want %d", f.calls, len(tt.results))
			}
			offset, wantRecover := 0, 0
			for i, n := range tt.results {
				want := linuxTestWrite{&samples[offset*channels], uintptr(len(samples)/channels - offset)}
				if f.writes[i] != want {
					t.Errorf("write %d = %v, want %v", i, f.writes[i], want)
				}
				if n < 0 {
					wantRecover++
				} else {
					offset += n
				}
			}
			if recoverCalls != wantRecover {
				t.Errorf("recover calls = %d, want %d", recoverCalls, wantRecover)
			}
			if d.data != nil {
				t.Error("write retained the sample buffer")
			}
		})
	}
}

// purego's Go callback bridge itself allocates through reflection. Compare
// writes with direct calls using preallocated argument arrays to exclude that
// test bridge overhead and guard against extra allocations in the production
// wrapper. This does not measure zero allocations on a real Linux audio device.
func TestLinuxWriteAllocations(t *testing.T) {
	pulse, alsa, f := newLinuxTestOutput(t)
	f.allocationOnly = true
	samples := make([]float32, linuxFrames*channels)
	pulseArgs := [4]uintptr{pulse.s, uintptr(unsafe.Pointer(&samples[0])), uintptr(len(samples) * 4), uintptr(unsafe.Pointer(&pulse.code))}
	alsaArgs := [3]uintptr{alsa.pcm, uintptr(unsafe.Pointer(&samples[0])), uintptr(len(samples) / channels)}
	for _, tt := range []struct {
		name string
		d    device
		fn   uintptr
		args []uintptr
		want uintptr
	}{
		{"PulseAudio", pulse, linuxTestPulseWrite, pulseArgs[:], 0},
		{"ALSA", alsa, linuxTestALSAWrite, alsaArgs[:], linuxFrames},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f.calls = 0
			const runs = 1000
			var result uintptr
			baseline := testing.AllocsPerRun(runs, func() {
				result, _, _ = purego.SyscallN(tt.fn, tt.args...)
			})
			if result != tt.want {
				t.Fatalf("direct call result = %d, want %d", result, tt.want)
			}
			var err error
			if n := testing.AllocsPerRun(runs, func() { err = tt.d.write(samples) }); n != baseline {
				t.Errorf("write allocated %v times, callback bridge baseline = %v", n, baseline)
			}
			if err != nil {
				t.Fatal(err)
			}
			// Each measurement includes one warm-up call.
			if f.calls != 2*(runs+1) {
				t.Errorf("callback calls = %d, want %d", f.calls, 2*(runs+1))
			}
		})
	}
	runtime.KeepAlive(samples)
}
