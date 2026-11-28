//go:build unix && !linux && !(darwin && !ios) && !freebsd

package mygo

import "errors"

// peerUID would return the user of the process at the other end of the
// Unix socket fd, which is not asked for here: only the folder of the
// socket keeps other users out.
func peerUID(int) (int, error) { return -1, errors.ErrUnsupported }
