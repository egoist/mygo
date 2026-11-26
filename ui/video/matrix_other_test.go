//go:build !linux

package video

// matrixMissing is none: only GStreamer's elements vary by what is
// installed.
func matrixMissing([]string) []string { return nil }
