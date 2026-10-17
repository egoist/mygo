//go:build windows && (amd64 || arm64)

package audio

import "unsafe"

// lag returns the frames in the endpoint's buffer, which WASAPI has taken
// but not played yet.
func (d *wasapiDevice) lag() (int64, bool) {
	if d.client == 0 {
		return 0, false
	}
	var padding uint32
	if hr := comCall(d.client, 6, uintptr(unsafe.Pointer(&padding))); failed(hr) {
		return 0, false
	}
	return int64(padding), true
}
