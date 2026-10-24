package media

import "time"

// An EncodedPacket is one compressed sample in its codec's own form, with
// the times it belongs at. It is what an encoder hands over and what a
// muxer, a recorder or a streamer writes: the same packet, written once, can
// go to a file and to the network.
//
// The form of Data and of the [CodecConfig] that goes with it is fixed per
// codec, so that a packet from any platform can be written by any packager:
//
//   - H.264 and HEVC: one access unit with start codes, as the platform's
//     encoders and the network expect. The parameter sets travel in the
//     config's Extra, in the length-prefixed form a file's avcC or hvcC
//     box holds.
//   - AAC: one raw access unit without its AudioSpecificConfig, which
//     travels in the config's Extra alongside the encoder's priming
//     frames.
//   - Opus: one packet; the config's Extra holds the pre-skip to discard.
//   - VP8, VP9 and AV1: one frame; the config's Extra holds the codec's
//     configuration record, including VP9's colour information.
type EncodedPacket struct {
	// Track is the track the packet belongs to.
	Track int
	// PTS is when the packet is presented, DTS when it must be decoded,
	// from the start of the media, and Duration how long it lasts. DTS is
	// always explicit: zero is the start, not an unspecified value. Encoders
	// without frame reordering set DTS to PTS.
	PTS, DTS, Duration time.Duration
	// Keyframe reports whether decoding can start here.
	Keyframe bool
	// Data holds the compressed sample.
	Data []byte
}

// A CodecConfig is what a decoder needs before the first packet of a track:
// the codec, and the setup data that is not in the packets themselves.
type CodecConfig struct {
	Codec Codec

	// Extra holds the codec's configuration record: the parameter sets of
	// H.264 and HEVC, an AAC stream's AudioSpecificConfig, the vpcC or
	// av1C of VP8, VP9 and AV1, the pre-skip of Opus.
	Extra []byte

	// Width and Height are the picture size, for a video track.
	Width, Height int
	// SampleRate and Channels are the sound's, for an audio track.
	SampleRate, Channels int
}
