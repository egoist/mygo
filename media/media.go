// Package media plays and captures pictures and sound: files, cameras and
// screens, decoded by the platform's own media stack, without cgo.
//
// # What the platform does and what MyGo does
//
// The platform decodes: macOS reads media through AVFoundation, Windows
// through Media Foundation, Linux through GStreamer. Each of them hands out
// pictures and sound as timed samples, and MyGo keeps them together: it owns
// the clock, drops pictures whose time has passed, and plays the sound
// through [github.com/egoist/mygo/audio], so that one volume, one device and
// one mixer serve an app's music and its video alike.
//
// Nothing here is built into an app that does not import it: the package
// stands on its own rather than going through the framework's platform
// interface, whose every method every app links.
//
// # Pictures
//
// A [Source] gives [Frame]s, one at a time, pulled: [Source.NextFrame]
// blocks until the next one is ready and the caller owns what it returns
// until [Frame.Release]. Camera and screen sources, which the platform
// drives, keep a small queue and drop the oldest pictures rather than
// slow the platform's own thread down.
//
// # Sound
//
// [Open] returns a [Player], which plays a file's sound itself and hands its
// pictures out as they are due, keeping them to the sound's clock: a
// picture is not shown before the sound at its time has been heard. With no
// sound track the player keeps a clock of its own.
//
// # Formats
//
// A frame says what it holds: its [PixelFormat] and the planes it is stored
// in. The platforms are asked for one converted, packed plane of 8-bit
// colour, which is what the self-drawn UI draws, so a video frame's memory
// goes to [github.com/egoist/mygo/ui] and to the GPU without another copy.
// YUV frames straight from a decoder, which the GPU converts, are what a
// later version adds.
//
// # What is not here
//
// Nothing is bundled: MyGo neither ships a media engine nor the codecs an
// operating system may leave out, and it grants no codec or patent licence.
// What a machine can play is the machine's operating system, distribution
// and installed components, which [Capabilities] reports on. A source that
// needs something the platform does not have fails with an error that
// [errors.Is] matches against [errors.ErrUnsupported], naming what was
// missing.
package media

import (
	"context"
	"errors"
	"iter"
	"time"
)

// A Source gives pictures over time: a file being decoded, a camera, a
// screen. Its methods are safe to call from any goroutine, though one
// goroutine should pull from it at a time.
type Source interface {
	// NextFrame returns the next picture, which the caller owns and gives
	// back with [Frame.Release], or a failure. It blocks while the
	// picture is not ready yet, and returns [ErrEnd] once there are no
	// more rather than blocking further.
	NextFrame(ctx context.Context) (*Frame, error)

	// Close stops the source, giving up what it holds, including the
	// frames it gave out that were not released. Closing twice is fine.
	Close() error
}

// ErrEnd is what [Source.NextFrame] returns at the end of a source: a file
// played out, a camera taken away. It is an end, not a failure.
var ErrEnd = errors.New("media: end of source")

// A Seekable source can move to another point in its timeline.
type Seekable interface {
	// Seek moves to t from the start: the frames after it are the ones at
	// t or later. It returns [errors.ErrUnsupported] where the source
	// cannot seek, as a live camera cannot.
	Seek(t time.Duration) error
}

// A TrackKind is what a track carries.
type TrackKind uint8

const (
	TrackOther TrackKind = iota
	TrackVideo
	TrackAudio
)

// A Track is one stream in a source, as the container describes it.
type Track struct {
	// Index is the track's position in the source, which
	// [TrackSource.SelectTracks] takes.
	Index int
	Kind  TrackKind
	Codec Codec

	Width, Height        int // video, in pixels
	SampleRate, Channels int // audio

	// Duration is the track's length, or 0 when unknown.
	Duration time.Duration
	// Language is the track's language tag, or empty.
	Language string
}

// A TrackSource has tracks to choose from.
type TrackSource interface {
	// Tracks returns the source's tracks as the container lists them.
	Tracks() []Track
	// SelectTracks picks the video and audio track to use by their index
	// in Tracks, or -1 for none: no picture track, or no sound. It
	// returns [errors.ErrUnsupported] for a source with nothing to choose,
	// as a camera has.
	SelectTracks(video, audio int) error
}

// A DurationSource knows how long it is.
type DurationSource interface {
	// Duration returns the source's length, or 0 when unknown, as it is
	// for a live source.
	Duration() time.Duration
}

// Frames returns the source's pictures for a range loop, which stops at the
// end of the source or at the first failure.
//
//	for f, err := range media.Frames(ctx, src) {
//		if err != nil {
//			return err
//		}
//		draw(f)
//		f.Release()
//	}
func Frames(ctx context.Context, s Source) iter.Seq2[*Frame, error] {
	return func(yield func(*Frame, error) bool) {
		for {
			f, err := s.NextFrame(ctx)
			if err != nil {
				if !errors.Is(err, ErrEnd) {
					yield(nil, err)
				}
				return
			}
			if !yield(f, nil) {
				f.Release()
				return
			}
		}
	}
}

// Handle calls fn for each of the source's pictures, on the goroutine that
// calls it, until the source ends, ctx is done, or fn returns false. The
// frame fn is given is the source's for as long as fn runs: fn must copy
// what it keeps, as [Frame.Image]'s pixels are, rather than hold the frame.
func Handle(ctx context.Context, s Source, fn func(*Frame) bool) error {
	for f, err := range Frames(ctx, s) {
		if err != nil {
			return err
		}
		keep := fn(f)
		f.Release()
		if !keep {
			return nil
		}
	}
	return nil
}
