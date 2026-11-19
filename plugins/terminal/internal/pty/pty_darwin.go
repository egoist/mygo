//go:build !ios

package pty

import (
	"bytes"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// The ioctls of grantpt, unlockpt and ptsname on macOS (sys/ttycom.h).
const (
	tiocptygrant = 0x20007454 // _IO('t', 84)
	tiocptyunlk  = 0x20007452 // _IO('t', 82)
	tiocptygname = 0x40807453 // _IOC(IOC_OUT, 't', 83, 128)
)

func openMaster() (*os.File, string, error) {
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, "", fmt.Errorf("pty: %w", err)
	}
	var name [128]byte
	for _, step := range []struct {
		req uintptr
		arg unsafe.Pointer
	}{{tiocptygrant, nil}, {tiocptyunlk, nil}, {tiocptygname, unsafe.Pointer(&name[0])}} {
		if err := ioctl(master, step.req, step.arg); err != nil {
			master.Close()
			return nil, "", fmt.Errorf("pty: %w", err)
		}
	}
	n := bytes.IndexByte(name[:], 0)
	if n < 0 {
		n = len(name)
	}
	return master, string(name[:n]), nil
}

// waitReadable waits until the master has output or hangs up, for a read
// that came back with EAGAIN because the os package could not make the
// descriptor one its poller waits on. It returns errClosed after Close.
//
// It calls select itself: libc's select refuses descriptors from
// FD_SETSIZE (1024) on, which an app with many files open reaches, and the
// kernel takes sets of any size.
func (p *PTY) waitReadable() error {
	return p.await(func(master, wake int) (bool, error) {
		set := make([]uint32, max(master, wake)/32+1)
		for {
			// select rewrites the set, and leaves it undefined on EINTR.
			clear(set)
			set[master/32] |= 1 << (master % 32)
			set[wake/32] |= 1 << (wake % 32)
			_, _, errno := syscall.Syscall6(syscall.SYS_SELECT, uintptr(max(master, wake)+1), uintptr(unsafe.Pointer(&set[0])), 0, 0, 0, 0)
			if errno == syscall.EINTR {
				continue
			}
			if errno != 0 {
				return false, errno
			}
			return set[wake/32]&(1<<(wake%32)) != 0, nil
		}
	})
}
