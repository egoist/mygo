//go:build windows && (amd64 || arm64)

package media

import (
	"fmt"
	"sync"

	"github.com/egoist/mygo/media/internal/windows"
)

// newH264Encoder encodes pictures through Media Foundation, the system's own
// H.264 encoder, so that a recording costs no bundled codec and uses the
// hardware that plays everything else on the machine.
func newH264Encoder(width, height, bitrate, fps int) (videoEncoder, error) {
	if err := windows.Load(); err != nil {
		return nil, &MissingComponentError{
			Component: "Media Foundation",
			Feature:   FeatureEncode,
			Hint:      "Media Foundation comes with Windows; an N edition needs the Media Feature Pack",
		}
	}
	enc, err := windows.NewH264Encoder(width, height, bitrate, fps)
	if err != nil {
		return nil, unsupported("encoding H.264: %v", err)
	}
	return &winH264{enc: enc, width: width, height: height}, nil
}

// A winH264 is a videoEncoder over a Media Foundation encoder transform.
type winH264 struct {
	mu sync.Mutex

	enc           *windows.H264Encoder
	width, height int

	samples []encodedSample
	cfg     *CodecConfig
	closed  bool
	err     error
}

// encode hands one picture to the encoder and keeps whatever it finished.
func (e *winH264) encode(f *Frame) error {
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
		return fmt.Errorf("media: encoding with a closed encoder")
	}
	if err != nil {
		return err
	}
	// The encoder takes packed red, green, blue and unused rows: a frame
	// of a taller stride is copied row by row.
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
	if err := e.enc.Encode(pixels, f.PTS); err != nil {
		e.mu.Lock()
		e.err = err
		e.mu.Unlock()
		return err
	}
	e.keep(e.enc.Take())
	return nil
}

// keep turns finished packets into samples the recorder can write.
func (e *winH264) keep(packets []windows.EncoderPacket) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, p := range packets {
		sample := encodedSample{packet: EncodedPacket{
			PTS:      p.PTS,
			Keyframe: p.Keyframe,
			Data:     p.Data,
		}}
		if e.cfg == nil && len(p.SPS) > 0 && len(p.PPS) > 0 {
			if extra, err := avcCFromSPSPPS(p.SPS, p.PPS); err == nil {
				e.cfg = &CodecConfig{
					Codec:  CodecH264,
					Extra:  extra,
					Width:  e.width,
					Height: e.height,
				}
			}
		}
		if e.cfg != nil {
			sample.cfg = e.cfg
		}
		e.samples = append(e.samples, sample)
	}
}

func (e *winH264) take() []encodedSample {
	e.mu.Lock()
	samples := e.samples
	e.samples = nil
	e.mu.Unlock()
	return samples
}

func (e *winH264) close() error {
	e.mu.Lock()
	if e.closed {
		err := e.err
		e.mu.Unlock()
		return err
	}
	e.closed = true
	e.mu.Unlock()
	// The encoder may still be working on pictures: what it finishes
	// before it stops is still worth keeping.
	err := e.enc.Close()
	e.keep(e.enc.Take())
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.err == nil {
		e.err = err
	}
	return e.err
}
