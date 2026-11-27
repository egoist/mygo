//go:build darwin && !ios

package mygo

import "github.com/egoist/mygo/internal/darwin"

// peerUID returns the user of the process at the other end of the Unix
// socket fd.
func peerUID(fd int) (int, error) { return darwin.PeerUID(fd) }
