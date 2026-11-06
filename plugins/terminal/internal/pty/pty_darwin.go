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
	// macOS has open(2) leave O_NONBLOCK off /dev/ptmx, but Go's
	// os.OpenFile sets it, as it does for every file the runtime opens,
	// and os.File.Read does not retry the EAGAIN that a pty master then
	// returns before the program has written anything. Clear it, so that
	// a read waits for output rather than ending the terminal.
	if err := setBlocking(master); err != nil {
		master.Close()
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

// setBlocking clears O_NONBLOCK on f, so that reads wait for output
// instead of returning EAGAIN. macOS reports ENOTTY for F_SETFL on a pty
// master, but applies it, so what the flag reads as afterwards decides.
func setBlocking(f *os.File) error {
	c, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var flags uintptr
	var errno syscall.Errno
	if err := c.Control(func(fd uintptr) {
		flags, _, _ = syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_GETFL, 0)
		syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_SETFL, flags&^uintptr(syscall.O_NONBLOCK))
		flags, _, errno = syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_GETFL, 0)
	}); err != nil {
		return err
	}
	if errno != 0 {
		return errno
	}
	if flags&syscall.O_NONBLOCK != 0 {
		return fmt.Errorf("pty: O_NONBLOCK (%#x) left on the master", flags)
	}
	return nil
}
