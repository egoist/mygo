//go:build !android

package pty

import (
	"fmt"
	"os"
	"strconv"
	"syscall"
	"unsafe"
)

func openMaster() (*os.File, string, error) {
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, "", fmt.Errorf("pty: %w", err)
	}
	var unlock int32
	if err := ioctl(master, syscall.TIOCSPTLCK, unsafe.Pointer(&unlock)); err != nil {
		master.Close()
		return nil, "", fmt.Errorf("pty: unlocking: %w", err)
	}
	var n uint32
	if err := ioctl(master, syscall.TIOCGPTN, unsafe.Pointer(&n)); err != nil {
		master.Close()
		return nil, "", fmt.Errorf("pty: %w", err)
	}
	return master, "/dev/pts/" + strconv.Itoa(int(n)), nil
}

// waitReadable waits until the master has output or hangs up, for a read
// that came back with EAGAIN because the os package could not make the
// descriptor one its poller waits on. It returns errClosed after Close.
//
// It polls, which unlike select takes descriptors from FD_SETSIZE (1024)
// on, which an app with many files open reaches.
func (p *PTY) waitReadable() error {
	return p.await(func(master, wake int) (bool, error) {
		type pollFd struct {
			fd              int32
			events, revents int16
		}
		const pollIn = 0x1
		fds := [2]pollFd{{fd: int32(master), events: pollIn}, {fd: int32(wake), events: pollIn}}
		for {
			_, _, errno := syscall.Syscall6(syscall.SYS_PPOLL, uintptr(unsafe.Pointer(&fds[0])), uintptr(len(fds)), 0, 0, 0, 0)
			if errno == syscall.EINTR {
				continue
			}
			if errno != 0 {
				return false, errno
			}
			return fds[1].revents != 0, nil
		}
	})
}
