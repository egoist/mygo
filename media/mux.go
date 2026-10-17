package media

import "io"

// A Muxer writes encoded packets into one container, track by track, in
// the order they are written.
//
// It is the other half of an [EncodedPacket]: an encoder says what the
// sound and pictures are, a muxer packs them into a file, and the same
// packet can be written once to a file and once to the network.
//
// The packets of a track must come in decoding order, starting with a
// keyframe, with the times the track is played at. Tracks must be added
// before the first packet is written. A muxer is not safe for use from
// several goroutines at once: one goroutine writes, the way one goroutine
// reads a [Source].
type Muxer interface {
	// AddTrack declares a track from what decodes it and returns the
	// track's number, which its packets carry in [EncodedPacket.Track].
	AddTrack(cfg CodecConfig) (int, error)

	// WritePacket writes one compressed sample.
	WritePacket(p EncodedPacket) error

	// Close writes what is left, finishes the file and gives up the
	// muxer. Closing twice is fine.
	Close() error
}

// NewMuxer returns a muxer for a container, writing to w.
//
// A container this version cannot write fails with an error that
// [errors.Is] matches against [errors.ErrUnsupported], naming it, rather
// than writing something that only looks like it.
func NewMuxer(w io.Writer, container Container) (Muxer, error) {
	switch container {
	case ContainerMP4, ContainerFragMP4:
		return newMP4Muxer(w), nil
	}
	return nil, unsupported("writing %s", container)
}

// muxers holds the containers this package can write, so that
// [Capabilities] and [NewMuxer] agree.
var muxers = []Container{ContainerMP4, ContainerFragMP4}

// CanMux reports whether the container can be written.
func CanMux(container Container) bool { return has(muxers, container) }
