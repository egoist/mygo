//go:build !darwin && !(windows && (amd64 || arm64)) && !(linux && (amd64 || arm64))

package media

// Platforms other than macOS, until their own media stacks are wired up:
// everything compiles, nothing plays, and asking says what is missing rather
// than failing without a word.
//
// Windows joins through Media Foundation and Linux through GStreamer; the
// browser build can read media with the WebCodecs and media element APIs,
// which is a surface of its own.

// openSource opens nothing here.
func openSource(target string, o options) (Source, error) {
	return nil, unsupported("no media on this platform yet")
}

// platformCapabilities reports nothing.
func platformCapabilities() Caps { return Caps{} }
