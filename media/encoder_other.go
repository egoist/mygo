//go:build !darwin && !(linux && (amd64 || arm64)) && !(windows && (amd64 || arm64))

package media

// newH264Encoder is what a platform without an encoder wired up yet
// answers: it says what it cannot do rather than recording nothing.
func newH264Encoder(width, height, bitrate, fps int) (videoEncoder, error) {
	return nil, unsupported("encoding H.264 on this platform yet")
}
