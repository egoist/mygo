//go:build darwin

package media

import (
	"fmt"
	"sync"

	darwin "github.com/egoist/mygo/media/internal/darwin"
)

// newH264Encoder encodes pictures through VideoToolbox, the system's own
// H.264 encoder, so that a recording costs no bundled codec and uses the
// hardware that plays everything else on the machine.
func newH264Encoder(width, height, bitrate, fps int) (videoEncoder, error) {
	if err := darwin.Load(); err != nil {
		return nil, unsupported("AVFoundation: %v", err)
	}
	e := &darwinH264{width: width, height: height}
	enc, err := darwin.NewH264Encoder(width, height, bitrate, fps, e.gotPacket)
	if err != nil {
		return nil, err
	}
	e.enc = enc
	return e, nil
}

// A darwinH264 is a videoEncoder over VideoToolbox. VideoToolbox hands each
// picture to gotPacket, on its own thread, in the order the pictures were
// handed over.
type darwinH264 struct {
	mu sync.Mutex

	enc           *darwin.H264Encoder
	width, height int

	samples []encodedSample
	cfg     *CodecConfig

	// bgra is the encoder's copy of the last picture: VideoToolbox reads
	// BGRA, and a frame is RGBA.
	bgra []byte
}

// gotPacket keeps a sample VideoToolbox finished.
func (e *darwinH264) gotPacket(p darwin.H264Packet) {
	sample := encodedSample{packet: EncodedPacket{
		PTS:      p.PTS,
		Keyframe: p.Keyframe,
		Data:     p.Data,
	}}
	if p.SPS != nil && p.PPS != nil {
		extra, err := avcCFromSPSPPS(p.SPS, p.PPS)
		if err == nil {
			e.mu.Lock()
			if e.cfg == nil {
				e.cfg = &CodecConfig{
					Codec:  CodecH264,
					Extra:  extra,
					Width:  e.width,
					Height: e.height,
				}
			}
			e.mu.Unlock()
		}
	}
	e.mu.Lock()
	if e.cfg != nil {
		sample.cfg = e.cfg
	}
	e.samples = append(e.samples, sample)
	e.mu.Unlock()
}

// encode copies a frame into the order VideoToolbox reads and hands it
// over.
func (e *darwinH264) encode(f *Frame) error {
	if f.Format != PixelRGBA {
		return unsupported("recording a picture that is %s", f.Format)
	}
	if f.Width != e.width || f.Height != e.height {
		return fmt.Errorf("media: a picture of %dx%d in a recording of %dx%d", f.Width, f.Height, e.width, e.height)
	}
	if len(f.Planes) == 0 || f.Planes[0].Stride < f.Width*4 {
		return fmt.Errorf("media: a picture of %dx%d with no pixels", f.Width, f.Height)
	}
	n := e.width * e.height * 4
	if cap(e.bgra) < n {
		e.bgra = make([]byte, n)
	}
	bgra := e.bgra[:n]
	pix := f.Planes[0].Pix
	for i := 0; i < n; i += 4 {
		bgra[i] = pix[i+2]
		bgra[i+1] = pix[i+1]
		bgra[i+2] = pix[i]
		bgra[i+3] = pix[i+3]
	}
	return e.enc.Encode(bgra, f.PTS)
}

// take gives up the samples encoded so far.
func (e *darwinH264) take() []encodedSample {
	e.mu.Lock()
	samples := e.samples
	e.samples = nil
	e.mu.Unlock()
	return samples
}

// close finishes the pictures still being encoded, and gives up the
// encoder.
func (e *darwinH264) close() error {
	err := e.enc.Close()
	return err
}
