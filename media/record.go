package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// A RecordOption is one choice about a recording.
type RecordOption func(*recordOptions)

type recordOptions struct {
	codec     Codec
	container Container
	bitrate   int
	fps       int
}

func newRecordOptions() recordOptions {
	return recordOptions{codec: CodecH264, container: ContainerMP4, fps: 30}
}

// WithRecordCodec chooses the codec a recording is written in. H.264 is the
// one this version writes; a codec the platform cannot encode fails when
// the recording starts, naming it, rather than writing something else.
func WithRecordCodec(codec Codec) RecordOption {
	return func(o *recordOptions) { o.codec = codec }
}

// WithRecordContainer chooses the container a recording is written in. MP4
// is the default, and the one every platform's player reads.
func WithRecordContainer(container Container) RecordOption {
	return func(o *recordOptions) { o.container = container }
}

// WithRecordBitrate asks for a bitrate in bits per second, which 0 leaves
// to the platform's own choice for the picture's size and frame rate.
func WithRecordBitrate(bits int) RecordOption {
	return func(o *recordOptions) { o.bitrate = bits }
}

// WithRecordFrameRate says how many pictures a second the recording expects,
// which the encoder uses for its keyframes and for the duration of the last
// picture. Pictures with other spacing are recorded as they come.
func WithRecordFrameRate(fps int) RecordOption {
	return func(o *recordOptions) { o.fps = fps }
}

// A Recorder writes the pictures of a [Source] into a file, encoded by the
// platform's own encoder, so that recording costs no bundled codec. It is
// what a camera preview or a screen recording saves: pull a frame, hand it
// over, release it.
//
// A recorder is not safe for use from several goroutines at once: one
// goroutine writes, the way one goroutine pulls from a source.
type Recorder struct {
	mu sync.Mutex

	mux  Muxer
	opts recordOptions
	enc  videoEncoder

	// width and height are the picture size, which the first frame says.
	width, height int

	// startPTS is the first frame's own time: a camera or a screen
	// stamps pictures with the machine's clock, and a recording starts at
	// zero.
	startPTS time.Duration
	started  bool

	track     int
	haveTrack bool

	// queue holds the encoded pictures waiting to be written: one waits
	// for the next so that its duration is the time to the next picture.
	queue []EncodedPacket

	closed bool
	err    error
}

// NewRecorder makes a recorder that writes a container to w. The pictures
// come after, with [Recorder.WriteFrame], which gives the size.
func NewRecorder(w io.Writer, opts ...RecordOption) (*Recorder, error) {
	if w == nil {
		return nil, fmt.Errorf("media: recording to nothing")
	}
	o := newRecordOptions()
	for _, opt := range opts {
		opt(&o)
	}
	if o.fps <= 0 {
		o.fps = 30
	}
	mux, err := NewMuxer(w, o.container)
	if err != nil {
		return nil, err
	}
	return &Recorder{mux: mux, opts: o}, nil
}

// WriteFrame encodes one picture and writes it.
//
// The recorder copies what it needs, so the frame may be released as soon
// as this returns. The first frame fixes the picture size: a frame of
// another size is a mistake, not a recording of two sizes.
func (r *Recorder) WriteFrame(f *Frame) error {
	if f == nil {
		return fmt.Errorf("media: recording no picture")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		if r.err != nil {
			return r.err
		}
		return fmt.Errorf("media: recording with a closed recorder")
	}
	if r.err != nil {
		return r.err
	}
	if r.enc == nil {
		return r.begin(f)
	}
	return r.write(f)
}

// begin makes the encoder from the first frame's size, and writes the frame.
func (r *Recorder) begin(f *Frame) error {
	if f.Width <= 0 || f.Height <= 0 {
		return fmt.Errorf("media: a picture of %dx%d cannot be recorded", f.Width, f.Height)
	}
	enc, err := newVideoEncoder(r.opts.codec, f.Width, f.Height, r.opts.bitrate, r.opts.fps)
	if err != nil {
		return err
	}
	r.enc = enc
	r.width, r.height = f.Width, f.Height
	return r.write(f)
}

// write hands one frame to the encoder and writes what it finished. The
// mutex is held.
func (r *Recorder) write(f *Frame) error {
	if f.Width != r.width || f.Height != r.height {
		return fmt.Errorf("media: a picture of %dx%d in a recording of %dx%d", f.Width, f.Height, r.width, r.height)
	}
	if !r.started {
		r.startPTS = f.PTS
		r.started = true
	}
	frame := *f
	frame.PTS = f.PTS - r.startPTS
	if err := r.enc.encode(&frame); err != nil {
		r.err = err
		return err
	}
	if err := r.addSamples(r.enc.take()); err != nil {
		r.err = err
		return err
	}
	return nil
}

// addSamples writes the samples the encoder finished. The mutex is held.
func (r *Recorder) addSamples(samples []encodedSample) error {
	for _, s := range samples {
		if s.cfg != nil && !r.haveTrack {
			track, err := r.mux.AddTrack(*s.cfg)
			if err != nil {
				return err
			}
			r.track = track
			r.haveTrack = true
		}
		s.packet.Track = r.track
		r.queue = append(r.queue, s.packet)
	}
	return r.flush(false)
}

// flush writes the queued pictures, holding the last one back until its
// duration is known from the picture after it, unless all is set. The mutex
// is held.
func (r *Recorder) flush(all bool) error {
	for len(r.queue) > 0 {
		if r.haveTrack {
			p := r.queue[0]
			if len(r.queue) > 1 {
				p.Duration = r.queue[1].PTS - p.PTS
				if p.Duration <= 0 {
					p.Duration = r.frameDuration()
				}
			} else if all {
				p.Duration = r.frameDuration()
			} else {
				return nil
			}
			if err := r.mux.WritePacket(p); err != nil {
				return err
			}
		}
		r.queue = r.queue[1:]
	}
	return nil
}

// frameDuration is how long a picture lasts when the next one does not say:
// the recorder's frame rate.
func (r *Recorder) frameDuration() time.Duration {
	return time.Second / time.Duration(r.opts.fps)
}

// Close finishes the pictures still being encoded, writes what is left and
// finishes the file. Closing twice is fine.
func (r *Recorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return r.err
	}
	r.closed = true
	if r.enc != nil {
		if err := r.enc.close(); err != nil && r.err == nil {
			r.err = err
		}
		if err := r.addSamples(r.enc.take()); err != nil && r.err == nil {
			r.err = err
		}
		if err := r.flush(true); err != nil && r.err == nil {
			r.err = err
		}
	}
	r.queue = nil
	if err := r.mux.Close(); err != nil && r.err == nil {
		r.err = err
	}
	return r.err
}

// Record reads a source and writes it to a new file at path, encoded by the
// platform's own encoder, until the source ends or ctx is done. It returns
// nil at the end of the source, and leaves a file that plays up to the last
// picture written even when it stops on a failure.
//
// It does not close src: the source belongs to the caller, which may record
// it again.
func Record(ctx context.Context, path string, src Source, opts ...RecordOption) (err error) {
	if src == nil {
		return fmt.Errorf("media: recording nothing")
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	rec, err := NewRecorder(file, opts...)
	if err != nil {
		file.Close()
		return err
	}
	defer func() {
		if cerr := rec.Close(); cerr != nil && err == nil {
			err = cerr
		}
		if cerr := file.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	for {
		frame, ferr := src.NextFrame(ctx)
		if ferr != nil {
			// The end of the source is the end of the recording, and
			// so is being asked to stop.
			if errors.Is(ferr, ErrEnd) || ctx.Err() != nil {
				return nil
			}
			return ferr
		}
		werr := rec.WriteFrame(frame)
		frame.Release()
		if werr != nil {
			return werr
		}
	}
}
