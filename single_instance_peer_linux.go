package mygo

import "syscall"

// peerUID returns the user of the process at the other end of the Unix
// socket fd.
func peerUID(fd int) (int, error) {
	cred, err := syscall.GetsockoptUcred(fd, syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	if err != nil {
		return -1, err
	}
	return int(cred.Uid), nil
}
