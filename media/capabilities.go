package media

import (
	"errors"
	"fmt"
	"strings"
)

// A Codec is one way of encoding pictures or sound.
type Codec string

const (
	CodecH264   Codec = "h264"
	CodecHEVC   Codec = "hevc"
	CodecAV1    Codec = "av1"
	CodecVP8    Codec = "vp8"
	CodecVP9    Codec = "vp9"
	CodecMJPEG  Codec = "mjpeg"
	CodecProRes Codec = "prores"
	CodecAAC    Codec = "aac"
	CodecOpus   Codec = "opus"
	CodecVorbis Codec = "vorbis"
	CodecMP3    Codec = "mp3"
	CodecFLAC   Codec = "flac"
	CodecPCM    Codec = "pcm"
)

// Video reports whether the codec carries pictures.
func (c Codec) Video() bool {
	switch c {
	case CodecH264, CodecHEVC, CodecAV1, CodecVP8, CodecVP9, CodecMJPEG, CodecProRes:
		return true
	}
	return false
}

// A Container is a file format that holds tracks.
type Container string

const (
	ContainerMP4      Container = "mp4"
	ContainerFragMP4  Container = "fmp4"
	ContainerWebM     Container = "webm"
	ContainerMatroska Container = "matroska"
	ContainerOgg      Container = "ogg"
	ContainerWAV      Container = "wav"
	ContainerMP3      Container = "mp3"
	ContainerFLAC     Container = "flac"
)

// A Feature is something media can do on this machine.
type Feature string

const (
	// FeaturePlayback is reading and playing media.
	FeaturePlayback Feature = "playback"
	// FeatureDecode is decoding a codec, rather than only passing it
	// through.
	FeatureDecode Feature = "decode"
	// FeatureEncode is encoding a codec, as a recorder does.
	FeatureEncode Feature = "encode"
	// FeatureCamera is capturing from a camera.
	FeatureCamera Feature = "camera"
	// FeatureScreenCapture is capturing a screen.
	FeatureScreenCapture Feature = "screen-capture"
	// FeatureMicrophone is capturing sound.
	FeatureMicrophone Feature = "microphone"
)

// Caps says what the machine's own media stack can do now, which is
// the platform's answer rather than a promise of MyGo's: a codec listed as
// decodable is one the platform has a decoder for, and a machine where
// nothing is installed reports nothing.
//
// It is the current state of the machine, so an app that needs a particular
// codec should check rather than assume, and explain what to install when
// something is missing rather than fail without a word.
type Caps struct {
	// Decoders are the codecs media can decode.
	Decoders []Codec
	// Encoders are the codecs media can encode.
	Encoders []Codec
	// Containers are the container formats media can read.
	Containers []Container
	// Features are what else media can do here.
	Features []Feature
}

// Capabilities returns what the machine's media stack reports now.
func Capabilities() Caps { return platformCapabilities() }

// CanDecode reports whether the codec can be decoded.
func (c Caps) CanDecode(codec Codec) bool { return has(c.Decoders, codec) }

// CanEncode reports whether the codec can be encoded.
func (c Caps) CanEncode(codec Codec) bool { return has(c.Encoders, codec) }

// CanRead reports whether the container can be read.
func (c Caps) CanRead(container Container) bool { return has(c.Containers, container) }

// Supports reports whether the feature is available.
func (c Caps) Supports(f Feature) bool { return has(c.Features, f) }

// String describes what is available, one line's worth, for a log or an
// error.
func (c Caps) String() string {
	var b strings.Builder
	write := func(name string, values []string) {
		if len(values) == 0 {
			return
		}
		if b.Len() > 0 {
			b.WriteString("; ")
		}
		b.WriteString(name)
		b.WriteString(": ")
		b.WriteString(strings.Join(values, ", "))
	}
	write("containers", strings2(len(c.Containers), func(i int) string { return string(c.Containers[i]) }))
	write("decoders", strings2(len(c.Decoders), func(i int) string { return string(c.Decoders[i]) }))
	write("encoders", strings2(len(c.Encoders), func(i int) string { return string(c.Encoders[i]) }))
	write("features", strings2(len(c.Features), func(i int) string { return string(c.Features[i]) }))
	if b.Len() == 0 {
		return "nothing"
	}
	return b.String()
}

func has[T comparable](list []T, want T) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func strings2(n int, at func(int) string) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = at(i)
	}
	return out
}

// A MissingComponentError is what a source returns when the platform's media
// stack does not have what playing it needs: a codec, a container, a plugin.
// It matches [errors.ErrUnsupported], so an app can tell "this machine
// cannot" from "this file is broken".
type MissingComponentError struct {
	// Component names what is missing as the platform calls it: an
	// element, a decoder, a codec.
	Component string
	// Feature is what was being done with it.
	Feature Feature
	// Hint is what a person might do about it, when MyGo knows: a
	// distribution's package name, a setting. It is not a contract.
	Hint string
}

func (e *MissingComponentError) Error() string {
	var b strings.Builder
	b.WriteString("media: ")
	b.WriteString(string(e.Feature))
	b.WriteString(" needs ")
	b.WriteString(e.Component)
	if e.Hint != "" {
		b.WriteString(" (")
		b.WriteString(e.Hint)
		b.WriteString(")")
	}
	return b.String()
}

// Unwrap makes the error match [errors.ErrUnsupported].
func (e *MissingComponentError) Unwrap() error { return errors.ErrUnsupported }

// unsupported makes an error for something the platform cannot do at all,
// which matches [errors.ErrUnsupported] as well.
func unsupported(format string, args ...any) error {
	return fmt.Errorf("media: "+format+": %w", append(args, errors.ErrUnsupported)...)
}
