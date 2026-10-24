package media

import (
	"fmt"
)

// An encodedSample is one encoded picture and, the first time the encoder
// knows them, the decoder's parameter sets for the picture's track. A
// recorder takes them from the encoder in order and writes them out;
// nothing outside this package sees an encodedSample.
type encodedSample struct {
	packet EncodedPacket
	// cfg is set on the samples the encoder hands over before it has
	// said what its track's configuration is: nil once the recorder has
	// added the track.
	cfg *CodecConfig
}

// A videoEncoder turns the frames of a [Source] into H.264 access units
// with the platform's own encoder: VideoToolbox on a Mac, and what the
// platform offers elsewhere.
//
// encode hands one frame over and returns once the encoder has the
// picture: the frame may be released then, and the samples come out later.
// take returns the samples finished since the last call, in order. close
// finishes what is still being encoded and returns the first failure.
type videoEncoder interface {
	encode(f *Frame) error
	take() []encodedSample
	close() error
}

// newVideoEncoder makes an encoder of pictures of a size, at a bitrate in
// bits per second and a frame rate, which 0 leaves to the platform. A
// codec the platform cannot encode gives an error that matches
// [errors.ErrUnsupported].
func newVideoEncoder(codec Codec, width, height, bitrate, fps int) (videoEncoder, error) {
	switch codec {
	case CodecH264:
		return newH264Encoder(width, height, bitrate, fps)
	}
	return nil, unsupported("encoding %s", codec)
}

// avcCFromSPSPPS builds the avcC configuration record that a file's H.264
// track carries, from the parameter sets an encoder produced. It is the
// length-prefixed form [CodecConfig.Extra] holds, so that a muxer can put
// it in the file as it is.
func avcCFromSPSPPS(sps, pps [][]byte) ([]byte, error) {
	if len(sps) == 0 || len(sps[0]) < 4 {
		return nil, fmt.Errorf("media: an H.264 encoder gave no SPS")
	}
	if len(pps) == 0 {
		return nil, fmt.Errorf("media: an H.264 encoder gave no PPS")
	}
	extra := []byte{1, sps[0][1], sps[0][2], sps[0][3], 0xff, 0xe0 | byte(len(sps))}
	for _, set := range sps {
		extra = append(extra, byte(len(set)>>8), byte(len(set)))
		extra = append(extra, set...)
	}
	extra = append(extra, byte(len(pps)))
	for _, set := range pps {
		extra = append(extra, byte(len(set)>>8), byte(len(set)))
		extra = append(extra, set...)
	}
	return extra, nil
}
