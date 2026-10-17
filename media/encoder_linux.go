//go:build linux && (amd64 || arm64)

package media

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/egoist/mygo/media/internal/linux"
)

// encodeIdlePulls is how many pulls that came back empty a closing encoder
// waits for the end of the stream before it takes the pipeline down itself: a
// pipeline that will not say it ended must still let go.
const encodeIdlePulls = 50

// A gstH264 is a videoEncoder over GStreamer: media pushes pictures into an
// appsrc, the machine's own encoder (x264enc, or whatever the machine has that
// speaks the same caps) compresses them, and a goroutine of the encoder's own
// reads the access units back from an appsink.
type gstH264 struct {
	pipe linux.Element
	src  linux.AppSrc
	sink linux.AppSink
	bus  *linux.Bus

	width, height int
	done          chan struct{}

	mu       sync.Mutex
	samples  []encodedSample
	cfg      *CodecConfig
	lastPTS  time.Duration
	closed   bool
	err      error
	released bool
}

// newH264Encoder starts the encoder's pipeline: pictures in as packed RGBA,
// H.264 access units out with start codes, in the form a muxer writes.
func newH264Encoder(width, height, bitrate, fps int) (videoEncoder, error) {
	if err := linux.Load(); err != nil {
		return nil, &MissingComponentError{
			Component: "GStreamer",
			Feature:   FeatureEncode,
			Hint: "install it, for example on Debian or Ubuntu: " +
				"apt install libgstreamer1.0-0 gstreamer1.0-plugins-base",
		}
	}
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("media: a picture of %dx%d cannot be encoded", width, height)
	}
	if fps <= 0 {
		fps = 30
	}
	if bitrate <= 0 {
		bitrate = width * height * fps * 3 / 20
	}
	kilobits := bitrate / 1000
	if kilobits <= 0 {
		kilobits = 1
	}
	description := fmt.Sprintf(
		"appsrc name=picture is-live=true format=time "+
			"! video/x-raw,format=RGBA,width=%d,height=%d,framerate=%d/1 "+
			"! videoconvert ! video/x-raw,format=I420 "+
			"! x264enc tune=zerolatency speed-preset=ultrafast bitrate=%d key-int-max=%d "+
			"! video/x-h264,stream-format=byte-stream,alignment=au "+
			"! appsink name=encoded sync=false",
		width, height, fps, kilobits, fps)
	pipe, err := linux.NewPipelineFrom(description)
	if err != nil {
		return nil, encodeError(err)
	}
	src := pipe.ByName("picture")
	sink := pipe.ByName("encoded")
	if !src.Valid() || !sink.Valid() {
		if sink.Valid() {
			sink.Release()
		}
		if src.Valid() {
			src.Release()
		}
		pipe.Release()
		return nil, errors.New("media: the encoder pipeline is missing an end")
	}
	e := &gstH264{
		pipe:   pipe,
		src:    linux.AppSrc{Element: src},
		sink:   linux.AppSink{Element: sink},
		bus:    pipe.Bus(),
		width:  width,
		height: height,
		done:   make(chan struct{}),
	}
	if pipe.SetState(linux.StatePlaying) == linux.StateChangeFailure {
		err := e.busError()
		e.release()
		if err == nil {
			err = errors.New("media: the encoder pipeline would not start")
		}
		return nil, encodeError(err)
	}
	go e.run()
	return e, nil
}

// run reads the encoded pictures until the pipeline ends, breaks, or the
// encoder is closed.
func (e *gstH264) run() {
	defer close(e.done)
	idle := 0
	for {
		sample := e.sink.PullSample(gstPullWait)
		if sample == nil {
			if e.stopping() {
				if e.sink.IsEOS() || idle >= encodeIdlePulls {
					return
				}
				idle++
				continue
			}
			if e.sink.IsEOS() {
				return
			}
			if err := e.busError(); err != nil {
				e.fail(err)
				return
			}
			continue
		}
		idle = 0
		err := e.keepSample(sample)
		sample.Release()
		if err != nil {
			e.fail(err)
			return
		}
	}
}

// keepSample keeps one encoded picture, and says what the track is when the
// encoder first gave its parameter sets.
func (e *gstH264) keepSample(sample *linux.Sample) error {
	m, err := sample.Map()
	if err != nil {
		return encodeError(err)
	}
	data := make([]byte, len(m.Data))
	copy(data, m.Data)
	m.Unmap()

	e.mu.Lock()
	pts, ok := sample.StreamTime()
	if !ok {
		pts = e.lastPTS
	}
	e.lastPTS = pts
	e.mu.Unlock()

	keyframe := false
	var sps, pps [][]byte
	nalus, err := nalusOf(data)
	if err != nil {
		return fmt.Errorf("media: the encoder gave a picture without start codes")
	}
	for _, nalu := range nalus {
		switch nalu[0] & 0x1f {
		case 5: // an IDR picture: decoding can start here
			keyframe = true
		case 7:
			sps = append(sps, nalu)
		case 8:
			pps = append(pps, nalu)
		}
	}
	e.mu.Lock()
	if e.cfg == nil && len(sps) > 0 && len(pps) > 0 {
		if extra, err := avcCFromSPSPPS(sps, pps); err == nil {
			e.cfg = &CodecConfig{
				Codec:  CodecH264,
				Extra:  extra,
				Width:  e.width,
				Height: e.height,
			}
		}
	}
	s := encodedSample{packet: EncodedPacket{PTS: pts, DTS: pts, Keyframe: keyframe, Data: data}}
	if e.cfg != nil {
		s.cfg = e.cfg
	}
	e.samples = append(e.samples, s)
	e.mu.Unlock()
	return nil
}

// encode pushes one frame's pixels into the pipeline.
func (e *gstH264) encode(f *Frame) error {
	if f.Format != PixelRGBA {
		return unsupported("recording a picture that is %s", f.Format)
	}
	if f.Width != e.width || f.Height != e.height {
		return fmt.Errorf("media: a picture of %dx%d in a recording of %dx%d", f.Width, f.Height, e.width, e.height)
	}
	if len(f.Planes) == 0 || f.Planes[0].Stride < f.Width*4 || len(f.Planes[0].Pix) < f.Planes[0].Stride*f.Height {
		return fmt.Errorf("media: a picture of %dx%d with no pixels", f.Width, f.Height)
	}
	e.mu.Lock()
	closed, err := e.closed, e.err
	e.mu.Unlock()
	if closed {
		return errors.New("media: encoding with a closed encoder")
	}
	if err != nil {
		return err
	}
	// The frame is packed RGBA at its own stride: the pipeline was told
	// the width and height, and a frame of a taller stride is copied row
	// by row.
	var pixels []byte
	p := f.Planes[0]
	if p.Stride == f.Width*4 {
		pixels = p.Pix[:f.Width*f.Height*4]
	} else {
		pixels = make([]byte, f.Width*f.Height*4)
		for y := 0; y < f.Height; y++ {
			copy(pixels[y*f.Width*4:], p.Pix[y*p.Stride:y*p.Stride+f.Width*4])
		}
	}
	duration := f.Duration
	if duration <= 0 {
		duration = time.Second / 30
	}
	if err := e.src.Push(pixels, f.PTS, duration); err != nil {
		werr := encodeError(err)
		e.fail(werr)
		return werr
	}
	return nil
}

// take gives up the samples encoded since the last call.
func (e *gstH264) take() []encodedSample {
	e.mu.Lock()
	defer e.mu.Unlock()
	samples := e.samples
	e.samples = nil
	return samples
}

// close ends the stream, reads what is left, and gives up the pipeline.
func (e *gstH264) close() error {
	e.mu.Lock()
	if e.closed {
		err := e.err
		e.mu.Unlock()
		return err
	}
	e.closed = true
	e.mu.Unlock()
	_ = e.src.EndOfStream()
	select {
	case <-e.done:
	case <-time.After(5 * time.Second):
		// The pipeline will not say it ended: take it down, which is
		// what lets the reading goroutine go.
		e.pipe.SetState(linux.StateNull)
		<-e.done
	}
	e.pipe.SetState(linux.StateNull)
	e.release()
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.err
}

// stopping reports whether the encoder is closing, which a pull that came
// back empty looks at.
func (e *gstH264) stopping() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.closed
}

// fail keeps the first failure the encoder met.
func (e *gstH264) fail(err error) {
	if err == nil {
		return
	}
	e.mu.Lock()
	if e.err == nil {
		e.err = err
	}
	e.mu.Unlock()
}

// busError is what the pipeline said went wrong, when it said anything.
func (e *gstH264) busError() error {
	if e.bus == nil {
		return nil
	}
	message := e.bus.Pop(linux.MessageError, 0)
	if message == nil {
		return nil
	}
	defer message.Release()
	return encodeError(message.PipelineError())
}

// release drops what the encoder holds of the pipeline.
func (e *gstH264) release() {
	e.mu.Lock()
	if e.released {
		e.mu.Unlock()
		return
	}
	e.released = true
	e.mu.Unlock()
	e.sink.Element.Release()
	e.src.Element.Release()
	if e.bus != nil {
		e.bus.Release()
		e.bus = nil
	}
	e.pipe.Release()
}

// encodeError turns what GStreamer said about encoding into what media
// promises: a machine missing a plugin is a component that is missing.
func encodeError(err error) error {
	if err == nil {
		return nil
	}
	var missing *linux.MissingError
	if errors.As(err, &missing) {
		return &MissingComponentError{Component: missing.Element, Feature: FeatureEncode, Hint: encodeHint}
	}
	var failure *linux.PipelineError
	if errors.As(err, &failure) {
		if what := missingElement(failure.Message); what != "" {
			return &MissingComponentError{Component: what, Feature: FeatureEncode, Hint: encodeHint}
		}
		return fmt.Errorf("media: encoding: %s", failure.Message)
	}
	if what := missingElement(err.Error()); what != "" {
		return &MissingComponentError{Component: what, Feature: FeatureEncode, Hint: encodeHint}
	}
	return err
}

// encodeHint is what a person can do about a missing encoder. It is advice,
// not a contract: a distribution names its packages its own way.
const encodeHint = "install the GStreamer encoder plugins, for example on Debian or Ubuntu: " +
	"apt install gstreamer1.0-plugins-ugly (x264enc), or gstreamer1.0-plugins-good for other encoders"
