//go:build darwin || linux

package pty

import (
	"syscall"
	"testing"
)

// TestMasterBlocks covers the shell hanging up before it prints its
// prompt: Go's os.OpenFile leaves O_NONBLOCK on /dev/ptmx, whose master
// then returns EAGAIN until the program writes. A reader that takes any
// error as the program's end closes the terminal, and the shell gets
// SIGHUP before its prompt (exit 129).
func TestMasterBlocks(t *testing.T) {
	p, err := Start(Options{Path: "/bin/sh", Args: []string{"sh", "-c", "exit 0"}})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	master := p.master
	c, err := master.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var flags uintptr
	var errno syscall.Errno
	if err := c.Control(func(fd uintptr) {
		flags, _, errno = syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_GETFL, 0)
	}); err != nil {
		t.Fatal(err)
	}
	if errno != 0 {
		t.Fatalf("F_GETFL: %v", errno)
	}
	if flags&syscall.O_NONBLOCK != 0 {
		t.Errorf("the master is non-blocking (flags %#x): a read before the program writes returns EAGAIN", flags)
	}
}
