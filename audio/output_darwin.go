//go:build !ios

package audio

import (
	"fmt"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

// macOS plays through an Audio Queue on the default output, which follows
// it when it changes.

type audioStreamBasicDescription struct {
	sampleRate       float64
	formatID         uint32
	formatFlags      uint32
	bytesPerPacket   uint32
	framesPerPacket  uint32
	bytesPerFrame    uint32
	channelsPerFrame uint32
	bitsPerChannel   uint32
	reserved         uint32
}

type audioQueueBuffer struct {
	audioDataBytesCapacity uint32
	audioData              unsafe.Pointer
	audioDataByteSize      uint32
	userData               uintptr
	packetDescCapacity     uint32
	packetDescs            uintptr
	packetDescCount        uint32
}

const (
	kAudioFormatLinearPCM   = 0x6C70636D // 'lpcm'
	kAudioFormatFlagIsFloat = 1
	aqFrames                = 512 // per buffer: 10.7 ms, as many as hold ahead
	dispatchTimeNow         = 0
)

var (
	aqLoad sync.Once
	aqErr  error

	audioQueueNewOutput      func(format *audioStreamBasicDescription, callback uintptr, user uintptr, runLoop, runLoopMode uintptr, flags uint32, out *uintptr) int32
	audioQueueAllocateBuffer func(q uintptr, size uint32, out **audioQueueBuffer) int32
	audioQueueStart          func(q uintptr, at uintptr) int32
	audioQueueStop           func(q uintptr, immediate bool) int32
	audioQueueDispose        func(q uintptr, immediate bool) int32
	dispatchSemaphoreCreate  func(value int) uintptr
	dispatchRelease          func(obj uintptr)

	// Called on every buffer, through a cCaller.
	audioQueueEnqueueBuffer uintptr
	dispatchSemaphoreWait   uintptr
	dispatchTime            uintptr

	dispatchSemaphoreSignal uintptr // every queue's callback

	// Core Audio's, for the output's latency; nil where it didn't load.
	audioObjectGetPropertyData func(obj uint32, addr *audioObjectPropertyAddress, qualSize uint32, qual unsafe.Pointer, size *uint32, data unsafe.Pointer) int32
)

type audioObjectPropertyAddress struct{ selector, scope, element uint32 }

const (
	kAudioObjectSystemObject                  = 1
	kAudioHardwarePropertyDefaultOutputDevice = 0x644f7574 // 'dOut'
	kAudioObjectPropertyScopeGlobal           = 0x676c6f62 // 'glob'
	kAudioObjectPropertyScopeOutput           = 0x6f757470 // 'outp'
	kAudioDevicePropertyLatency               = 0x6c746e63 // 'ltnc', a stream's as well
	kAudioDevicePropertySafetyOffset          = 0x73616674 // 'saft'
	kAudioDevicePropertyBufferFrameSize       = 0x6673697a // 'fsiz'
	kAudioDevicePropertyNominalSampleRate     = 0x6e737274 // 'nsrt'
	kAudioDevicePropertyStreams               = 0x73746d23 // 'stm#'
)

func loadAudioToolbox() error {
	aqLoad.Do(func() {
		lib, err := purego.Dlopen("/System/Library/Frameworks/AudioToolbox.framework/AudioToolbox", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			aqErr = err
			return
		}
		purego.RegisterLibFunc(&audioQueueNewOutput, lib, "AudioQueueNewOutput")
		purego.RegisterLibFunc(&audioQueueAllocateBuffer, lib, "AudioQueueAllocateBuffer")
		purego.RegisterLibFunc(&audioQueueStart, lib, "AudioQueueStart")
		purego.RegisterLibFunc(&audioQueueStop, lib, "AudioQueueStop")
		purego.RegisterLibFunc(&audioQueueDispose, lib, "AudioQueueDispose")
		purego.RegisterLibFunc(&dispatchSemaphoreCreate, purego.RTLD_DEFAULT, "dispatch_semaphore_create")
		purego.RegisterLibFunc(&dispatchRelease, purego.RTLD_DEFAULT, "dispatch_release")
		audioQueueEnqueueBuffer = cFunc(lib, "AudioQueueEnqueueBuffer")
		dispatchSemaphoreWait = cFunc(purego.RTLD_DEFAULT, "dispatch_semaphore_wait")
		dispatchSemaphoreSignal = cFunc(purego.RTLD_DEFAULT, "dispatch_semaphore_signal")
		dispatchTime = cFunc(purego.RTLD_DEFAULT, "dispatch_time")
		if ca, err := purego.Dlopen("/System/Library/Frameworks/CoreAudio.framework/CoreAudio", purego.RTLD_LAZY|purego.RTLD_GLOBAL); err == nil {
			purego.RegisterLibFunc(&audioObjectGetPropertyData, ca, "AudioObjectGetPropertyData")
		}
	})
	return aqErr
}

// deviceLatency returns how long the default output, which the queue
// plays on, takes from a frame the queue gives it to the speakers: its
// I/O buffer, safety offset, and its own and its stream's latency, as
// Core Audio reports them, more over Bluetooth or AirPlay. It returns 0
// where it can't tell.
func deviceLatency() time.Duration {
	if audioObjectGetPropertyData == nil {
		return 0
	}
	get := func(obj, selector, scope uint32, data unsafe.Pointer, size uint32) bool {
		addr := audioObjectPropertyAddress{selector, scope, 0}
		return audioObjectGetPropertyData(obj, &addr, 0, nil, &size, data) == 0
	}
	var dev uint32
	var rate float64
	if !get(kAudioObjectSystemObject, kAudioHardwarePropertyDefaultOutputDevice, kAudioObjectPropertyScopeGlobal, unsafe.Pointer(&dev), 4) || dev == 0 ||
		!get(dev, kAudioDevicePropertyNominalSampleRate, kAudioObjectPropertyScopeGlobal, unsafe.Pointer(&rate), 8) || rate <= 0 {
		return 0
	}
	var total int64
	for _, sel := range []uint32{kAudioDevicePropertyLatency, kAudioDevicePropertySafetyOffset, kAudioDevicePropertyBufferFrameSize} {
		var n uint32
		if get(dev, sel, kAudioObjectPropertyScopeOutput, unsafe.Pointer(&n), 4) {
			total += int64(n)
		}
	}
	var streams [8]uint32
	var n uint32
	if get(dev, kAudioDevicePropertyStreams, kAudioObjectPropertyScopeOutput, unsafe.Pointer(&streams), uint32(unsafe.Sizeof(streams))) && streams[0] != 0 &&
		get(streams[0], kAudioDevicePropertyLatency, kAudioObjectPropertyScopeGlobal, unsafe.Pointer(&n), 4) {
		total += int64(n)
	}
	return frames(total, int(rate))
}

// An aqDevice is an Audio Queue whose callback is
// dispatch_semaphore_signal, its semaphore the user data (C on arm64 and
// x86-64 ignores the queue and buffer passed after it), so no Go runs on
// the queue's thread. The queue plays buffers in order: each signal frees
// the oldest.
type aqDevice struct {
	q       uintptr
	sema    uintptr
	bufs    []*audioQueueBuffer
	next    int // the buffer written next, the oldest enqueued once all are
	queued  int // buffers enqueued before the queue started
	started bool
	c       cCaller
	lag     time.Duration // from a write to the speakers
}

func openDevice(ahead time.Duration) (device, int, error) {
	if err := loadAudioToolbox(); err != nil {
		return nil, 0, err
	}
	desc := audioStreamBasicDescription{
		sampleRate:       sampleRate,
		formatID:         kAudioFormatLinearPCM,
		formatFlags:      kAudioFormatFlagIsFloat,
		bytesPerPacket:   channels * 4,
		framesPerPacket:  1,
		bytesPerFrame:    channels * 4,
		channelsPerFrame: channels,
		bitsPerChannel:   32,
	}
	n := max(2, int((int64(ahead)*sampleRate/int64(time.Second)+aqFrames-1)/aqFrames))
	// Fails only for a negative value.
	d := &aqDevice{sema: dispatchSemaphoreCreate(0), bufs: make([]*audioQueueBuffer, n)}
	if st := audioQueueNewOutput(&desc, dispatchSemaphoreSignal, d.sema, 0, 0, 0, &d.q); st != 0 {
		dispatchRelease(d.sema)
		return nil, 0, fmt.Errorf("audio: AudioQueueNewOutput failed (OSStatus %d)", st)
	}
	for i := range d.bufs {
		if st := audioQueueAllocateBuffer(d.q, aqFrames*channels*4, &d.bufs[i]); st != 0 {
			d.close()
			return nil, 0, fmt.Errorf("audio: AudioQueueAllocateBuffer failed (OSStatus %d)", st)
		}
	}
	d.lag = frames(int64(n*aqFrames), sampleRate) + deviceLatency()
	return d, aqFrames, nil
}

func (d *aqDevice) delay() time.Duration { return d.lag }

func (d *aqDevice) write(samples []float32) error {
	if d.started {
		// Wait for the oldest buffer to play.
		deadline := d.c.call(dispatchTime, dispatchTimeNow, uintptr(time.Second))
		if d.c.call(dispatchSemaphoreWait, d.sema, deadline) != 0 {
			// The queue stopped taking buffers, as when its device went.
			return fmt.Errorf("audio: the audio queue stopped")
		}
	}
	b := d.bufs[d.next]
	d.next = (d.next + 1) % len(d.bufs)
	copy(unsafe.Slice((*float32)(b.audioData), len(samples)), samples)
	b.audioDataByteSize = uint32(len(samples) * 4)
	// b is C memory, as a cCaller needs.
	if st := int32(d.c.call(audioQueueEnqueueBuffer, d.q, uintptr(unsafe.Pointer(b)), 0, 0)); st != 0 {
		return fmt.Errorf("audio: AudioQueueEnqueueBuffer failed (OSStatus %d)", st)
	}
	if !d.started {
		// Start once every buffer holds something, not to begin with an
		// underrun.
		if d.queued++; d.queued == len(d.bufs) {
			if st := audioQueueStart(d.q, 0); st != 0 {
				return fmt.Errorf("audio: AudioQueueStart failed (OSStatus %d)", st)
			}
			d.started = true
		}
	}
	return nil
}

func (d *aqDevice) close() {
	// Synchronous: no callback runs once it returns.
	audioQueueStop(d.q, true)
	audioQueueDispose(d.q, true)
	// Never below its initial value, which dispatch_release requires.
	dispatchRelease(d.sema)
}
