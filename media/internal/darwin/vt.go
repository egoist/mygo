//go:build darwin

package darwin

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

// VideoToolbox is the system's own H.264 encoder: no cgo, no bundled codec,
// and the same hardware encoder the rest of the system uses.

// CMVideoCodecTypeH264 is 'avc1'.
const CMVideoCodecTypeH264 = 0x61766331

// The types Core Foundation numbers: a UTF-8 C string, a 32-bit integer.
const (
	cfStringEncodingUTF8 = 0x08000100
	cfNumberSInt32       = 3
)

var (
	vtCompressionSessionCreate         func(alloc uintptr, width, height int32, codec uint32, encoderSpec, sourceAttrs, compressedAlloc, callback, refcon uintptr, out *uintptr) int32
	vtSessionSetProperty               func(session, key, value uintptr) int32
	vtCompressionSessionEncodeFrame    func(session, image uintptr, pts, duration CMTime, frameProperties, sourceRefcon uintptr, infoFlagsOut *uint32) int32
	vtCompressionSessionCompleteFrames func(session uintptr, until CMTime) int32
	vtCompressionSessionInvalidate     func(session uintptr)

	cvPixelBufferCreateWithBytes func(alloc uintptr, width, height int, format uint32, base unsafe.Pointer, bytesPerRow int, releaseCallback, releaseRefcon, attrs uintptr, out *uintptr) int32

	cfStringCreateWithCString   func(alloc uintptr, s *byte, encoding uint32) uintptr
	cfNumberCreate              func(alloc uintptr, kind int32, value unsafe.Pointer) uintptr
	cfArrayGetCount             func(a uintptr) int
	cfArrayGetValueAtIndex      func(a uintptr, i int) uintptr
	cfDictionaryGetValue        func(d, key uintptr) uintptr
	cfBooleanGetValue           func(b uintptr) uint8
	cmSampleBufferAttachments   func(sb uintptr, createIfNecessary uint8) uintptr
	cmFormatDescriptionH264Sets func(fd uintptr, index int, dataOut *unsafe.Pointer, sizeOut *int, countOut *int, headerLenOut *int32) int32
)

func bindVideoToolbox(lib uintptr) error {
	bind := func(name string, fn any) error {
		addr, err := purego.Dlsym(lib, name)
		if err != nil {
			return err
		}
		purego.RegisterFunc(fn, addr)
		return nil
	}
	for _, b := range []struct {
		name string
		fn   any
	}{
		{"VTCompressionSessionCreate", &vtCompressionSessionCreate},
		{"VTSessionSetProperty", &vtSessionSetProperty},
		{"VTCompressionSessionEncodeFrame", &vtCompressionSessionEncodeFrame},
		{"VTCompressionSessionCompleteFrames", &vtCompressionSessionCompleteFrames},
		{"VTCompressionSessionInvalidate", &vtCompressionSessionInvalidate},
		{"CVPixelBufferCreateWithBytes", &cvPixelBufferCreateWithBytes},
		{"CFStringCreateWithCString", &cfStringCreateWithCString},
		{"CFNumberCreate", &cfNumberCreate},
		{"CFArrayGetCount", &cfArrayGetCount},
		{"CFArrayGetValueAtIndex", &cfArrayGetValueAtIndex},
		{"CFDictionaryGetValue", &cfDictionaryGetValue},
		{"CFBooleanGetValue", &cfBooleanGetValue},
		{"CMSampleBufferGetSampleAttachmentsArray", &cmSampleBufferAttachments},
		{"CMVideoFormatDescriptionGetH264ParameterSetAtIndex", &cmFormatDescriptionH264Sets},
	} {
		if err := bind(b.name, b.fn); err != nil {
			return fmt.Errorf("media: VideoToolbox: %w", err)
		}
	}
	return nil
}

// CMTimeOf makes a CoreMedia time from a Go duration.
func CMTimeOf(d time.Duration) CMTime { return timeOf(d) }

// CFString makes a Core Foundation string, which the caller releases.
func CFString(s string) uintptr {
	return cfStringCreateWithCString(0, cstr(s), cfStringEncodingUTF8)
}

// CFNumber makes a Core Foundation number, which the caller releases.
func CFNumber(n int32) uintptr {
	return cfNumberCreate(0, cfNumberSInt32, unsafe.Pointer(&n))
}

// Boolean returns the value of a Core Foundation boolean.
func Boolean(p uintptr) bool { return cfBooleanGetValue(p) != 0 }

// True returns Core Foundation's true, which the caller must not release.
func True() uintptr { return Global("kCFBooleanTrue") }

// An H264Packet is one encoded picture, in the form a muxer writes: one
// access unit with start codes, and the parameter sets the decoder needs,
// which the encoder gives with the first packet.
type H264Packet struct {
	PTS      time.Duration
	Keyframe bool
	Data     []byte
	// SPS and PPS are the decoder's parameter sets: nil, except with the
	// encoder's first packet.
	SPS, PPS [][]byte
}

// An H264Encoder encodes pictures through VideoToolbox.
type H264Encoder struct {
	mu       sync.Mutex
	session  uintptr
	width    int
	height   int
	callback uintptr

	onPacket func(H264Packet)

	// packets are the encoded pictures not taken yet, in decode order.
	packets []H264Packet

	// pending keeps the pixels of each picture handed over alive until
	// VideoToolbox has finished with them: each is keyed by the number
	// passed as the frame's reference, which comes back with the encoded
	// picture.
	pending map[uint64][]byte
	nextID  atomic.Uint64

	// sets is the last format description's parameter sets, and format
	// the description they came from: a stream usually keeps one, and
	// extracting them for every picture would be work for nothing.
	sets      [][]byte
	pps       [][]byte
	format    uintptr
	firstDone bool

	closed bool
	err    error
}

// A route sends a sample buffer to the encoder that asked for it: the
// callback is one function for every encoder, and this is how it finds the
// one the picture belongs to.
var (
	routes    sync.Map // number -> *H264Encoder
	nextRoute atomic.Uint64
)

var encodeCallback = purego.NewCallback(func(refcon, frameRefcon uintptr, status int32, flags uint32, sampleBuffer uintptr) {
	v, ok := routes.Load(uint64(refcon))
	if !ok {
		return
	}
	v.(*H264Encoder).gotSample(uint64(frameRefcon), status, flags, sampleBuffer)
})

// NewH264Encoder makes an encoder of pictures of a size, at a bitrate and
// frame rate. onPacket is called from VideoToolbox's own thread with each
// encoded picture, in order.
func NewH264Encoder(width, height, bitrate, fps int, onPacket func(H264Packet)) (*H264Encoder, error) {
	if err := Load(); err != nil {
		return nil, err
	}
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("media: a picture of %dx%d cannot be encoded", width, height)
	}
	if width%2 != 0 || height%2 != 0 {
		return nil, fmt.Errorf("media: H.264 needs even sides, and this picture is %dx%d", width, height)
	}
	if fps <= 0 {
		fps = 30
	}
	if bitrate <= 0 {
		bitrate = width * height * fps * 3 / 20
	}
	e := &H264Encoder{width: width, height: height, onPacket: onPacket, pending: make(map[uint64][]byte)}
	e.callback = encodeCallback
	route := nextRoute.Add(1)
	var session uintptr
	var err error
	WithPool(func() {
		status := vtCompressionSessionCreate(0, int32(width), int32(height), CMVideoCodecTypeH264, 0, 0, 0, e.callback, uintptr(route), &session)
		if status != 0 {
			err = fmt.Errorf("media: creating the H.264 encoder: VideoToolbox status %d", status)
		}
	})
	if err != nil {
		return nil, err
	}
	e.session = session
	if err := e.setProperties(bitrate, fps); err != nil {
		CFReleaseItem(session)
		return nil, err
	}
	routes.Store(route, e)
	return e, nil
}

// setProperties asks for real-time encoding into H.264, with a keyframe
// once a second so that a recording can be played from the middle.
func (e *H264Encoder) setProperties(bitrate, fps int) error {
	set := func(name string, value uintptr) error {
		key := Global(name)
		status := vtSessionSetProperty(e.session, key, value)
		if status != 0 {
			return fmt.Errorf("media: telling VideoToolbox %s: status %d", name, status)
		}
		return nil
	}
	setBool := func(name string, on bool) error {
		v := True()
		if !on {
			v = Global("kCFBooleanFalse")
		}
		return set(name, v)
	}
	if err := setBool("kVTCompressionPropertyKey_RealTime", true); err != nil {
		return err
	}
	if err := setBool("kVTCompressionPropertyKey_AllowFrameReordering", false); err != nil {
		return err
	}
	level := Global("kVTProfileLevel_H264_High_AutoLevel")
	if err := set("kVTCompressionPropertyKey_ProfileLevel", level); err != nil {
		return err
	}
	number := func(n int32) (uintptr, func()) {
		v := CFNumber(n)
		return v, func() { CFReleaseItem(v) }
	}
	avg, freeAvg := number(int32(bitrate))
	defer freeAvg()
	if err := set("kVTCompressionPropertyKey_AverageBitRate", avg); err != nil {
		return err
	}
	interval, freeInterval := number(int32(fps))
	defer freeInterval()
	if err := set("kVTCompressionPropertyKey_MaxKeyFrameInterval", interval); err != nil {
		return err
	}
	if err := set("kVTCompressionPropertyKey_ExpectedFrameRate", interval); err != nil {
		return err
	}
	return nil
}

// Encode hands one picture over, as BGRA bytes, width*height*4 of them,
// with the time it belongs at. The encoder keeps the picture until it has
// encoded it; the caller may reuse bgra as soon as this returns.
func (e *H264Encoder) Encode(bgra []byte, pts time.Duration) error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return errors.New("media: encoding with a closed encoder")
	}
	if e.err != nil {
		err := e.err
		e.mu.Unlock()
		return err
	}
	if len(bgra) < e.width*e.height*4 {
		e.mu.Unlock()
		return fmt.Errorf("media: a picture of %d bytes for %dx%d", len(bgra), e.width, e.height)
	}
	id := e.nextID.Add(1)
	pixels := make([]byte, e.width*e.height*4)
	copy(pixels, bgra)
	e.pending[id] = pixels
	e.mu.Unlock()

	var pb uintptr
	status := cvPixelBufferCreateWithBytes(0, e.width, e.height, PixelFormat32BGRA, unsafe.Pointer(&pixels[0]), e.width*4, 0, 0, 0, &pb)
	if status != 0 {
		e.drop(id)
		return fmt.Errorf("media: holding a picture for the encoder: status %d", status)
	}
	defer CFReleaseItem(pb)
	status = vtCompressionSessionEncodeFrame(e.session, pb, CMTimeOf(pts), CMTime{}, 0, uintptr(id), nil)
	if status != 0 {
		e.drop(id)
		return fmt.Errorf("media: encoding a picture: VideoToolbox status %d", status)
	}
	return nil
}

// drop forgets the pixels of a picture the encoder will not hear about.
func (e *H264Encoder) drop(id uint64) {
	e.mu.Lock()
	delete(e.pending, id)
	e.mu.Unlock()
}

// gotSample is the encoder's callback: VideoToolbox calls it, on its own
// thread, with each finished picture.
func (e *H264Encoder) gotSample(frameRefcon uint64, status int32, flags uint32, sampleBuffer uintptr) {
	e.mu.Lock()
	delete(e.pending, frameRefcon)
	e.mu.Unlock()
	if status != 0 {
		e.mu.Lock()
		if e.err == nil {
			e.err = fmt.Errorf("media: VideoToolbox failed to encode a picture: status %d", status)
		}
		e.mu.Unlock()
		return
	}
	// The flags are bits of VTEncodeInfoFlags: a picture is marked
	// asynchronous, and dropped is the bit after that one.
	const frameDropped = 1 << 1 // kVTEncodeInfo_FrameDropped
	if sampleBuffer == 0 || flags&frameDropped != 0 {
		return
	}
	p, err := e.packetOf(sampleBuffer)
	if err != nil {
		e.mu.Lock()
		if e.err == nil {
			e.err = err
		}
		e.mu.Unlock()
		return
	}
	e.mu.Lock()
	var cb func(H264Packet)
	if e.onPacket != nil {
		cb = e.onPacket
	} else {
		e.packets = append(e.packets, p)
	}
	e.mu.Unlock()
	if cb != nil {
		cb(p)
	}
}

// packetOf turns a finished sample buffer into a packet, and the first one
// also says the parameter sets.
func (e *H264Encoder) packetOf(sampleBuffer uintptr) (H264Packet, error) {
	bb := cmSampleBufferData(sampleBuffer)
	n := cmBlockBufferLength(bb)
	if n <= 0 {
		return H264Packet{}, errors.New("media: VideoToolbox gave an empty picture")
	}
	data := make([]byte, n)
	if cmBlockBufferCopy(bb, 0, n, unsafe.Pointer(&data[0])) != 0 {
		return H264Packet{}, errors.New("media: copying an encoded picture")
	}
	p := H264Packet{
		PTS:      Time(cmSampleBufferPTS(sampleBuffer)),
		Keyframe: e.isKeyframe(sampleBuffer),
		Data:     avccToStartCodes(data),
	}
	format := cmSampleBufferFormat(sampleBuffer)
	e.mu.Lock()
	first := !e.firstDone
	if format != e.format {
		e.format = format
		e.sets, e.pps = h264ParameterSets(format)
	}
	sets, pps := e.sets, e.pps
	if !e.firstDone && len(sets) > 0 && len(pps) > 0 {
		e.firstDone = true
	} else {
		sets, pps = nil, nil
	}
	e.mu.Unlock()
	if first {
		p.SPS, p.PPS = sets, pps
	}
	return p, nil
}

// isKeyframe reports whether a picture can be decoded on its own: the sample
// attachments say so by not being marked as a non-sync sample.
func (e *H264Encoder) isKeyframe(sampleBuffer uintptr) bool {
	attachments := cmSampleBufferAttachments(sampleBuffer, 1)
	if attachments == 0 {
		return true
	}
	if cfArrayGetCount(attachments) == 0 {
		return true
	}
	notSync := Global("kCMSampleAttachmentKey_NotSync")
	for i := 0; i < cfArrayGetCount(attachments); i++ {
		d := cfArrayGetValueAtIndex(attachments, i)
		if d != 0 && cfDictionaryGetValue(d, notSync) != 0 {
			return false
		}
	}
	return true
}

// h264ParameterSets reads a format description's SPS and PPS.
func h264ParameterSets(format uintptr) ([][]byte, [][]byte) {
	if format == 0 {
		return nil, nil
	}
	var count int
	if cmFormatDescriptionH264Sets(format, 0, nil, nil, &count, nil) != 0 || count <= 0 {
		return nil, nil
	}
	var sets [][]byte
	for i := 0; i < count; i++ {
		var (
			p   unsafe.Pointer
			n   int
			hdr int32
		)
		if cmFormatDescriptionH264Sets(format, i, &p, &n, nil, &hdr) != 0 || p == nil || n <= 0 {
			return nil, nil
		}
		// The first set is the SPS, the second the PPS: H.264 parameter
		// sets come in that order.
		nalu := make([]byte, n)
		copy(nalu, unsafe.Slice((*byte)(p), n))
		if i == 0 {
			sets = append(sets, nalu)
		} else if i == 1 {
			sets = append(sets, nalu)
		}
	}
	if len(sets) < 2 {
		return nil, nil
	}
	return sets[:1], sets[1:2]
}

// avccToStartCodes turns the length-prefixed NALUs of a sample buffer into
// the start-code form packets travel in.
func avccToStartCodes(data []byte) []byte {
	out := make([]byte, 0, len(data)+4)
	at := 0
	for at+4 <= len(data) {
		n := int(data[at])<<24 | int(data[at+1])<<16 | int(data[at+2])<<8 | int(data[at+3])
		at += 4
		if n <= 0 || at+n > len(data) {
			break
		}
		out = append(out, 0, 0, 0, 1)
		out = append(out, data[at:at+n]...)
		at += n
	}
	if len(out) == 0 {
		// Not a length-prefixed stream after all: take it as it came.
		return data
	}
	return out
}

// Take returns the pictures encoded since the last call, in order.
func (e *H264Encoder) Take() []H264Packet {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.packets) == 0 {
		return nil
	}
	out := e.packets
	e.packets = nil
	return out
}

// Err returns the first failure the encoder met, if it did.
func (e *H264Encoder) Err() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.err
}

// Close finishes the pictures still being encoded, and gives up the
// encoder. It waits for every picture handed over to be encoded.
func (e *H264Encoder) Close() error {
	e.mu.Lock()
	if e.closed {
		err := e.err
		e.mu.Unlock()
		return err
	}
	e.closed = true
	session := e.session
	e.mu.Unlock()
	defer func() {
		vtCompressionSessionInvalidate(session)
		CFReleaseItem(session)
	}()
	if status := vtCompressionSessionCompleteFrames(session, CMTime{}); status != 0 {
		e.mu.Lock()
		if e.err == nil {
			e.err = fmt.Errorf("media: finishing the encoder: VideoToolbox status %d", status)
		}
		err := e.err
		e.mu.Unlock()
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.err
}
