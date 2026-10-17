//go:build windows && (amd64 || arm64)

package windows

import (
	"fmt"
	"time"
	"unsafe"
)

// H.264 encoding goes through the H.264 encoder transform Media Foundation
// lists for the machine, the same way playing goes through its decoders.
// The transform takes its pictures as NV12 and gives back a byte stream,
// which is what media's muxer takes.

// vtable slots of the interfaces encoding calls, after the ones mf.go names.
const (
	slotTransformOutputStreamInfo       = 7
	slotTransformAttributes             = 8
	slotTransformGetOutputAvailableType = 14
	slotTransformSetInputType           = 15
	slotTransformSetOutputType          = 16
	slotTransformProcessMessage         = 23
	slotTransformProcessInput           = 24
	slotTransformProcessOutput          = 25

	slotSampleSetSampleTime     = 36
	slotSampleSetSampleDuration = 38
	slotSampleAddBuffer         = 42

	slotBufferSetCurrentLength = 6
)

// What the H.264 encoder is asked for.
var (
	// MFTCategoryVideoEncoder is the encoder category of MFTEnumEx.
	MFTCategoryVideoEncoder = guid("f79eac7d-e545-4387-bdee-d647d7bde42a")
	// FormatNV12 is the uncompressed format the encoder takes: a luma
	// plane and then two rows of colour at a time.
	FormatNV12 = mediaFormat(fourCC("NV12"))
	// iidTransform is IMFTransform, what an encoder's activate makes.
	iidTransform = guid("bf94c121-5b05-4e6f-8000-ba598961414d")
	// iidEventGenerator is IMFMediaEventGenerator, which an encoder that
	// works on threads of its own also has.
	iidEventGenerator = guid("2cd0bd52-bcd5-4b89-b62c-eadc0c031e7d")

	// What the encoder is told about its input and output.
	KeyPixelAspectRatio = guid("c6376a1e-8d0a-4027-be45-6d9a0ad39bb6")
	KeyInterlaceMode    = guid("e2724bb8-e676-4806-b4b2-a8d6efb44ccd")
	KeyMPEG2Profile     = guid("ad76a80b-2d5c-4e0b-b375-64e520137036")

	// MF_TRANSFORM_ASYNC says the transform answers on threads of its
	// own, and MF_TRANSFORM_ASYNC_UNLOCK says an app that drives it
	// synchronously may use it anyway.
	KeyTransformAsync       = guid("f81a699a-649a-497d-8c73-29f8fed6ad7a")
	KeyTransformAsyncUnlock = guid("e5666d6b-3422-4eb6-a421-da7db1f8e207")
)

// The numbers the encoder's calls are made with.
const (
	mftEnumSyncMFT       = 0x1
	mftEnumAsyncMFT      = 0x2
	mftEnumHardwareMFT   = 0x4
	mftEnumSortAndFilter = 0x40

	// interlaceProgressive is MFVideoInterlace_Progressive.
	interlaceProgressive = 2
	// mpeg2ProfileH264Main is the profile every decoder can play.
	mpeg2ProfileH264Main = 77

	// The messages a transform is sent.
	messageCommandDrain         = 1
	messageNotifyBeginStreaming = 0x10000000
	messageNotifyEndStreaming   = 0x10000001
	messageNotifyEndOfStream    = 0x10000002
	messageNotifyStartOfStream  = 0x10000003

	// What an asynchronous encoder says on threads of its own.
	meTransformNeedInput     = 601
	meTransformHaveOutput    = 602
	meTransformDrainComplete = 603

	slotEventGet  = 3
	slotEventType = 33

	// mfEventFlagNoWait asks for an event only if one is already waiting.
	mfEventFlagNoWait = 1

	// What a transform's output stream says about itself.
	outputStreamProvidesSamples = 0x100

	// The results that are part of the way a transform works rather
	// than failures.
	hrNotAccepting     = 0xc00d36b5 // MF_E_NOTACCEPTING
	hrStreamChange     = 0xc00d6d61 // MF_E_TRANSFORM_STREAM_CHANGE
	hrNeedMoreInput    = 0xc00d6d72 // MF_E_TRANSFORM_NEED_MORE_INPUT
	hrAsyncTransform   = 0xc00d6d77 // MF_E_TRANSFORM_ASYNC_LOCKED
	hrNoEvents         = 0xc00d3e80 // MF_E_NO_EVENTS_AVAILABLE
	drainOutputsAtMost = 64
)

// A registerType is MFT_REGISTER_TYPE_INFO: one side of a transform, as a
// major type and a subtype.
type registerType struct {
	MajorType GUID
	Subtype   GUID
}

// An outputStreamInfo is MFT_OUTPUT_STREAM_INFO.
type outputStreamInfo struct {
	Flags     uint32
	Size      uint32
	Alignment uint32
}

// An outputDataBuffer is MFT_OUTPUT_DATA_BUFFER. Windows writes it with the
// two pointers where they land on a 64-bit machine.
type outputDataBuffer struct {
	StreamID uint32
	_        uint32
	Sample   uintptr
	Status   uint32
	_        uint32
	Events   uintptr
}

// An EncoderPacket is one picture the encoder has finished, in the byte
// stream form the muxer writes, with the parameter sets it needs.
type EncoderPacket struct {
	PTS      time.Duration
	Keyframe bool
	Data     []byte
	SPS, PPS [][]byte
}

// H264Encoder encodes pictures with the machine's H.264 encoder.
type H264Encoder struct {
	p        uintptr
	width    int
	height   int
	frameDur time.Duration
	outSize  uint32
	provides bool    // the transform makes its own output samples
	events   uintptr // IMFMediaEventGenerator, when it works on its own threads
	async    bool
	needIn   int
	haveOut  int
	drained  int
	packets  []EncoderPacket
	sps, pps [][]byte
	haveSets bool
	lastPTS  time.Duration
	closed   bool
	err      error
}

// NewH264Encoder opens the machine's H.264 encoder for pictures of this
// size. It says what is missing when the machine cannot make one.
func NewH264Encoder(width, height, bitrate, fps int) (*H264Encoder, error) {
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("windows: a picture of %dx%d has no size", width, height)
	}
	if width%2 != 0 || height%2 != 0 {
		return nil, fmt.Errorf("windows: H.264 cannot encode a picture of %dx%d: both sides must be even", width, height)
	}
	if fps <= 0 {
		fps = 30
	}
	if bitrate <= 0 {
		bitrate = width * height * fps * 3 / 20
	}
	if err := Load(); err != nil {
		return nil, err
	}
	enter()
	in := registerType{MajorType: MajorVideo, Subtype: FormatNV12}
	out := registerType{MajorType: MajorVideo, Subtype: FormatH264}
	var (
		activates *uintptr
		count     uint32
	)
	flags := uintptr(mftEnumSyncMFT | mftEnumAsyncMFT | mftEnumHardwareMFT | mftEnumSortAndFilter)
	hr, _, _ := procMFTEnumEx.Call(
		uintptr(unsafe.Pointer(&MFTCategoryVideoEncoder)),
		flags,
		uintptr(unsafe.Pointer(&in)),
		uintptr(unsafe.Pointer(&out)),
		uintptr(unsafe.Pointer(&activates)),
		uintptr(unsafe.Pointer(&count)),
	)
	if failed(hr) {
		return nil, hresultError("MFTEnumEx", hr)
	}
	if activates == nil || count == 0 {
		return nil, fmt.Errorf("windows: this machine has no H.264 encoder")
	}
	defer procCoTaskMemFree.Call(uintptr(unsafe.Pointer(activates)))
	var firstErr error
	for _, activate := range unsafe.Slice((*uintptr)(unsafe.Pointer(activates)), int(count)) {
		if activate == 0 {
			continue
		}
		enc, err := newH264EncoderFrom(activate, width, height, bitrate, fps)
		release(activate)
		if err == nil {
			return enc, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return nil, fmt.Errorf("windows: this machine has no H.264 encoder")
}

func newH264EncoderFrom(activate uintptr, width, height, bitrate, fps int) (*H264Encoder, error) {
	var p uintptr
	hr := comCall(activate, slotActivateObject, uintptr(unsafe.Pointer(&iidTransform)), uintptr(unsafe.Pointer(&p)))
	if failed(hr) || p == 0 {
		return nil, hresultError("IMFActivate::ActivateObject", hr)
	}
	e := &H264Encoder{p: p, width: width, height: height, frameDur: time.Second / time.Duration(fps)}
	if err := e.setup(bitrate, fps); err != nil {
		release(p)
		return nil, err
	}
	return e, nil
}

// setup tells the transform what it will be given and what it should give
// back, and lets go of the asynchronous way it would rather be driven.
func (e *H264Encoder) setup(bitrate, fps int) error {
	var attrsPtr uintptr
	hr := comCall(e.p, slotTransformAttributes, uintptr(unsafe.Pointer(&attrsPtr)))
	if !failed(hr) && attrsPtr != 0 {
		attrs := Attributes{p: attrsPtr}
		if async, err := attrs.GetUINT32(&KeyTransformAsync); err == nil && async != 0 {
			// An encoder that works on threads of its own is only
			// usable once it is told the app will drive it.
			e.async = true
			if err := attrs.SetUINT32(&KeyTransformAsyncUnlock, 1); err != nil {
				attrs.Release()
				return err
			}
		}
		attrs.Release()
	}

	// MF_MT_FRAME_SIZE is the width in the high half and the height in the low
	// half, the same way MF_MT_FRAME_RATE packs its two numbers.
	size := uint64(uint32(e.width))<<32 | uint64(uint32(e.height))
	rate := uint64(uint32(fps))<<32 | 1
	aspect := uint64(1)<<32 | 1

	out, err := NewMediaType()
	if err != nil {
		return err
	}
	defer out.Release()
	for _, step := range []func() error{
		func() error { return out.SetGUID(&KeyMajorType, &MajorVideo) },
		func() error { return out.SetGUID(&KeySubtype, &FormatH264) },
		func() error { return out.SetUINT64(&KeyFrameSize, size) },
		func() error { return out.SetUINT64(&KeyFrameRate, rate) },
		func() error { return out.SetUINT64(&KeyPixelAspectRatio, aspect) },
		func() error { return out.SetUINT32(&KeyInterlaceMode, interlaceProgressive) },
		func() error { return out.SetUINT32(&KeyAvgBitrate, uint32(bitrate)) },
		func() error { return out.SetUINT32(&KeyMPEG2Profile, mpeg2ProfileH264Main) },
	} {
		if err := step(); err != nil {
			return err
		}
	}
	// The encoder wants to know what it is making before it is told what
	// it will be given.
	if hr := comCall(e.p, slotTransformSetOutputType, 0, out.p, 0); failed(hr) {
		return fmt.Errorf("windows: the H.264 encoder will not give back H.264: %w", hresultError("IMFTransform::SetOutputType", hr))
	}

	in, err := NewMediaType()
	if err != nil {
		return err
	}
	defer in.Release()
	for _, step := range []func() error{
		func() error { return in.SetGUID(&KeyMajorType, &MajorVideo) },
		func() error { return in.SetGUID(&KeySubtype, &FormatNV12) },
		func() error { return in.SetUINT64(&KeyFrameSize, size) },
		func() error { return in.SetUINT64(&KeyFrameRate, rate) },
		func() error { return in.SetUINT64(&KeyPixelAspectRatio, aspect) },
		func() error { return in.SetUINT32(&KeyInterlaceMode, interlaceProgressive) },
	} {
		if err := step(); err != nil {
			return err
		}
	}
	if hr := comCall(e.p, slotTransformSetInputType, 0, in.p, 0); failed(hr) {
		return fmt.Errorf("windows: the H.264 encoder will not take %dx%d pictures: %w", e.width, e.height, hresultError("IMFTransform::SetInputType", hr))
	}

	var info outputStreamInfo
	if hr := comCall(e.p, slotTransformOutputStreamInfo, 0, uintptr(unsafe.Pointer(&info))); failed(hr) {
		return hresultError("IMFTransform::GetOutputStreamInfo", hr)
	}
	e.outSize = info.Size
	e.provides = info.Flags&outputStreamProvidesSamples != 0

	comCall(e.p, slotTransformProcessMessage, messageNotifyBeginStreaming, 0)
	if e.async {
		hr := comCall(e.p, slotQueryInterface, uintptr(unsafe.Pointer(&iidEventGenerator)), uintptr(unsafe.Pointer(&e.events)))
		if failed(hr) || e.events == 0 {
			return hresultError("IMFTransform::QueryInterface(IMFMediaEventGenerator)", hr)
		}
	}
	// The stream only begins, and the encoder only asks for pictures, once
	// it is told the stream has started.
	if hr := comCall(e.p, slotTransformProcessMessage, messageNotifyStartOfStream, 0); failed(hr) {
		return hresultError("IMFTransform::ProcessMessage(START_OF_STREAM)", hr)
	}
	return nil
}

// Encode takes one picture, a red, green, blue and unused byte for each
// pixel, with where it belongs in the file, and gives what comes out to
// Take.
func (e *H264Encoder) Encode(rgba []byte, pts time.Duration) error {
	if e.closed {
		return fmt.Errorf("windows: the H.264 encoder is closed")
	}
	if e.err != nil {
		return e.err
	}
	if len(rgba) < e.width*e.height*4 {
		return fmt.Errorf("windows: a picture of %d bytes is too small for %dx%d", len(rgba), e.width, e.height)
	}
	nv12 := make([]byte, e.width*e.height*3/2)
	nv12FromRGBA(nv12, rgba, e.width, e.height)
	sample, err := makeSample(nv12)
	if err != nil {
		e.err = err
		return err
	}
	defer release(sample)
	if hr := comCall(sample, slotSampleSetSampleTime, uintptr(int64(pts/100))); failed(hr) {
		e.err = hresultError("IMFSample::SetSampleTime", hr)
		return e.err
	}
	if hr := comCall(sample, slotSampleSetSampleDuration, uintptr(int64(e.frameDur/100))); failed(hr) {
		e.err = hresultError("IMFSample::SetSampleDuration", hr)
		return e.err
	}
	if e.async {
		// Give back whatever the encoder finished while the app was away.
		if err := e.pumpEvents(); err != nil {
			e.err = err
			return err
		}
	}
	for try := 0; ; try++ {
		if e.async && e.needIn == 0 {
			if err := e.waitEvent(); err != nil {
				e.err = err
				return err
			}
			if err := e.handleOutputs(); err != nil {
				e.err = err
				return err
			}
			continue
		}
		hr := comCall(e.p, slotTransformProcessInput, 0, sample)
		if !failed(hr) {
			if e.async {
				e.needIn--
			}
			break
		}
		switch uint32(hr) {
		case hrNotAccepting:
			if !e.async || try >= 4 {
				e.err = fmt.Errorf("windows: the H.264 encoder will not take another picture: %w", hresultError("IMFTransform::ProcessInput", hr))
				return e.err
			}
			if err := e.waitEvent(); err != nil {
				e.err = err
				return err
			}
		case hrAsyncTransform:
			e.err = fmt.Errorf("windows: the H.264 encoder only works on threads of its own: %w", hresultError("IMFTransform::ProcessInput", hr))
			return e.err
		default:
			e.err = hresultError("IMFTransform::ProcessInput", hr)
			return e.err
		}
	}
	if e.async {
		// The picture is encoded on the encoder's own threads: take what is
		// ready now and leave the rest to the next picture or to Close.
		if err := e.pumpEvents(); err != nil {
			e.err = err
			return err
		}
	} else {
		e.pullOutputs()
	}
	return e.err
}

// handleOutputs collects the pictures the encoder has said are ready.
func (e *H264Encoder) handleOutputs() error {
	for e.haveOut > 0 {
		e.haveOut--
		e.pullOne()
		if e.err != nil {
			return e.err
		}
	}
	return nil
}

// pumpEvents takes the events the encoder has already sent, without waiting
// for more.
func (e *H264Encoder) pumpEvents() error {
	for {
		more, err := e.pollEvent()
		if err != nil {
			return err
		}
		if !more {
			break
		}
	}
	return e.handleOutputs()
}

// pullOne takes one picture the encoder has finished, and says whether it
// got one. An encoder driven by events gives back one picture for each time
// it says one is ready.
func (e *H264Encoder) pullOne() bool {
	buf := outputDataBuffer{StreamID: 0}
	if !e.provides {
		sample, err := makeSampleOf(int(e.outSize))
		if err != nil {
			e.err = err
			return false
		}
		buf.Sample = sample
	}
	var status uint32
	hr := comCall(e.p, slotTransformProcessOutput, 0, 1, uintptr(unsafe.Pointer(&buf)), uintptr(unsafe.Pointer(&status)))
	switch {
	case uint32(hr) == hrStreamChange:
		// The encoder has more to give back than it first said: it
		// offers a new output type and the app has to take it.
		if buf.Sample != 0 {
			release(buf.Sample)
		}
		if err := e.renegotiateOutput(); err != nil && e.err == nil {
			e.err = err
		}
		// The encoder speaks again once the new output type is in
		// place; the next picture or the drain takes it from there.
		return false
	case uint32(hr) == hrNeedMoreInput:
		if buf.Sample != 0 {
			release(buf.Sample)
		}
		return false
	case failed(hr):
		if buf.Sample != 0 {
			release(buf.Sample)
		}
		if e.err == nil {
			e.err = hresultError("IMFTransform::ProcessOutput", hr)
		}
		return false
	case buf.Sample == 0:
		return false
	}
	e.collect(buf.Sample)
	release(buf.Sample)
	return true
}

// pullOutputs collects everything the encoder has finished so far, for an
// encoder the app drives itself.
func (e *H264Encoder) pullOutputs() {
	for i := 0; i < drainOutputsAtMost && e.pullOne(); i++ {
	}
}

// waitEvent waits for the encoder to say what it wants next, on an encoder
// that works on threads of its own.
func (e *H264Encoder) waitEvent() error {
	if e.events == 0 {
		return fmt.Errorf("windows: the H.264 encoder has no events")
	}
	var event uintptr
	if hr := comCall(e.events, slotEventGet, 0, uintptr(unsafe.Pointer(&event))); failed(hr) {
		return hresultError("IMFMediaEventGenerator::GetEvent", hr)
	}
	if event == 0 {
		return nil
	}
	defer release(event)
	var kind uint32
	if hr := comCall(event, slotEventType, uintptr(unsafe.Pointer(&kind))); failed(hr) {
		return hresultError("IMFMediaEvent::GetType", hr)
	}
	switch kind {
	case meTransformNeedInput:
		e.needIn++
	case meTransformHaveOutput:
		e.haveOut++
	case meTransformDrainComplete:
		e.drained++
	}
	return nil
}

// pollEvent takes one event the encoder has already sent, and says whether
// there was one at all.
func (e *H264Encoder) pollEvent() (bool, error) {
	if e.events == 0 {
		return false, fmt.Errorf("windows: the H.264 encoder has no events")
	}
	var event uintptr
	hr := comCall(e.events, slotEventGet, mfEventFlagNoWait, uintptr(unsafe.Pointer(&event)))
	if uint32(hr) == hrNoEvents {
		return false, nil
	}
	if failed(hr) {
		return false, hresultError("IMFMediaEventGenerator::GetEvent", hr)
	}
	if event == 0 {
		return false, nil
	}
	defer release(event)
	var kind uint32
	if hr := comCall(event, slotEventType, uintptr(unsafe.Pointer(&kind))); failed(hr) {
		return false, hresultError("IMFMediaEvent::GetType", hr)
	}
	switch kind {
	case meTransformNeedInput:
		e.needIn++
	case meTransformHaveOutput:
		e.haveOut++
	case meTransformDrainComplete:
		e.drained++
	}
	return true, nil
}

// renegotiateOutput takes the output type the encoder offers when it turns
// out to give back more than it first said.
func (e *H264Encoder) renegotiateOutput() error {
	for i := 0; i < 8; i++ {
		var mt uintptr
		hr := comCall(e.p, slotTransformGetOutputAvailableType, 0, uintptr(i), uintptr(unsafe.Pointer(&mt)))
		if failed(hr) || mt == 0 {
			break
		}
		hr = comCall(e.p, slotTransformSetOutputType, 0, mt, 0)
		release(mt)
		if failed(hr) {
			continue
		}
		var info outputStreamInfo
		if hr := comCall(e.p, slotTransformOutputStreamInfo, 0, uintptr(unsafe.Pointer(&info))); failed(hr) {
			return hresultError("IMFTransform::GetOutputStreamInfo", hr)
		}
		e.outSize = info.Size
		e.provides = info.Flags&outputStreamProvidesSamples != 0
		return nil
	}
	return fmt.Errorf("windows: the H.264 encoder offered no output it would take back")
}

// collect turns one finished sample into a packet.
func (e *H264Encoder) collect(sample uintptr) {
	s := Sample{p: sample}
	pts, ok := s.Time()
	if !ok {
		pts = e.lastPTS + e.frameDur
	}
	e.lastPTS = pts
	b, err := s.Buffer()
	if err != nil {
		if e.err == nil {
			e.err = err
		}
		return
	}
	ptr, n, err := b.Lock()
	if err != nil {
		b.Release()
		if e.err == nil {
			e.err = err
		}
		return
	}
	data := make([]byte, n)
	copy(data, unsafe.Slice((*byte)(ptr), n))
	b.Unlock()
	b.Release()
	if len(data) == 0 {
		return
	}
	packet := EncoderPacket{PTS: pts, Data: data}
	for _, nalu := range h264NALUs(data) {
		if len(nalu) == 0 {
			continue
		}
		switch nalu[0] & 0x1f {
		case 5:
			packet.Keyframe = true
		case 7:
			if !e.haveSets {
				e.sps = append(e.sps, append([]byte(nil), nalu...))
			}
		case 8:
			if !e.haveSets {
				e.pps = append(e.pps, append([]byte(nil), nalu...))
			}
		}
	}
	if !e.haveSets && len(e.sps) > 0 && len(e.pps) > 0 {
		packet.SPS = e.sps
		packet.PPS = e.pps
		e.haveSets = true
	}
	e.packets = append(e.packets, packet)
}

// Take gives the packets encoded since the last call.
func (e *H264Encoder) Take() []EncoderPacket {
	out := e.packets
	e.packets = nil
	return out
}

// Err says what went wrong, if anything did.
func (e *H264Encoder) Err() error { return e.err }

// Close stops the encoder and gives back what it was using.
func (e *H264Encoder) Close() error {
	if e.closed {
		return e.err
	}
	e.closed = true
	comCall(e.p, slotTransformProcessMessage, messageCommandDrain, 0)
	if e.async && e.events != 0 {
		// The encoder says when it has given back the last picture: wait
		// for that rather than guessing how long it takes.
		for e.drained == 0 {
			if err := e.waitEvent(); err != nil {
				e.stop()
				return e.err
			}
			if err := e.handleOutputs(); err != nil {
				e.stop()
				return e.err
			}
		}
	}
	e.stop()
	return e.err
}

// stop ends the stream and gives up the encoder and its events.
func (e *H264Encoder) stop() {
	comCall(e.p, slotTransformProcessMessage, messageNotifyEndOfStream, 0)
	comCall(e.p, slotTransformProcessMessage, messageNotifyEndStreaming, 0)
	if e.events != 0 {
		release(e.events)
		e.events = 0
	}
	if e.p != 0 {
		release(e.p)
		e.p = 0
	}
}

// makeSample wraps a copy of data in a sample, the way a transform is given
// a picture. makeSampleOf makes an empty sample with room for size bytes,
// the way a transform is given somewhere to put its output.
func makeSample(data []byte) (uintptr, error) {
	sample, err := makeSampleOf(len(data))
	if err != nil {
		return 0, err
	}
	if err := fillSample(sample, data); err != nil {
		release(sample)
		return 0, err
	}
	return sample, nil
}

func makeSampleOf(size int) (uintptr, error) {
	var sample uintptr
	if hr, _, _ := procMFCreateSample.Call(uintptr(unsafe.Pointer(&sample))); failed(hr) {
		return 0, hresultError("MFCreateSample", hr)
	}
	var buffer uintptr
	if hr, _, _ := procMFCreateMemoryBuffer.Call(uintptr(size), uintptr(unsafe.Pointer(&buffer))); failed(hr) {
		release(sample)
		return 0, hresultError("MFCreateMemoryBuffer", hr)
	}
	if hr := comCall(sample, slotSampleAddBuffer, buffer); failed(hr) {
		release(buffer)
		release(sample)
		return 0, hresultError("IMFSample::AddBuffer", hr)
	}
	release(buffer)
	return sample, nil
}

// fillSample copies data into a sample that was made for it.
func fillSample(sample uintptr, data []byte) error {
	s := Sample{p: sample}
	b, err := s.Buffer()
	if err != nil {
		return err
	}
	defer b.Release()
	ptr, n, err := b.LockWrite()
	if err != nil {
		return err
	}
	defer b.Unlock()
	if n < len(data) {
		return fmt.Errorf("windows: a sample of %d bytes is too small for %d", n, len(data))
	}
	copy(unsafe.Slice((*byte)(ptr), n), data)
	if hr := comCall(b.p, slotBufferSetCurrentLength, uintptr(uint32(len(data)))); failed(hr) {
		return hresultError("IMFMediaBuffer::SetCurrentLength", hr)
	}
	return nil
}

// nv12FromRGBA makes the encoder's picture from an app's: a luma byte for
// each pixel, then one colour pair for each two pixels of every second row,
// in the limited range SD televisions use.
func nv12FromRGBA(dst, src []byte, width, height int) {
	uv := dst[width*height:]
	for y := 0; y < height; y++ {
		row := src[y*width*4:]
		luma := dst[y*width:]
		for x := 0; x < width; x++ {
			r := int32(row[x*4])
			g := int32(row[x*4+1])
			b := int32(row[x*4+2])
			luma[x] = clampByte(((66*r + 129*g + 25*b + 128) >> 8) + 16)
			if y%2 == 0 && x%2 == 0 {
				at := (y/2)*width + x
				uv[at] = clampByte(((-38*r - 74*g + 112*b + 128) >> 8) + 128)
				uv[at+1] = clampByte(((112*r - 94*g - 18*b + 128) >> 8) + 128)
			}
		}
	}
}

func clampByte(v int32) byte {
	switch {
	case v < 0:
		return 0
	case v > 255:
		return 255
	}
	return byte(v)
}

// h264NALUs splits a byte stream into its pictures and parameter sets, each
// one without the start code before it.
func h264NALUs(data []byte) [][]byte {
	var out [][]byte
	start := -1
	for i := 0; i+2 < len(data); i++ {
		if data[i] != 0 || data[i+1] != 0 {
			continue
		}
		if data[i+2] == 1 {
			if start >= 0 {
				out = append(out, trimNAL(data[start:i]))
			}
			start = i + 3
			i += 2
		}
	}
	if start >= 0 && start < len(data) {
		out = append(out, trimNAL(data[start:]))
	}
	return out
}

func trimNAL(nalu []byte) []byte {
	for len(nalu) > 0 && nalu[len(nalu)-1] == 0 {
		nalu = nalu[:len(nalu)-1]
	}
	return nalu
}
