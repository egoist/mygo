//go:build (darwin && !ios) || (linux && !android)

package pty

import (
	"errors"
	"io"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/egoist/mygo/internal/wait"
)

// highMaster moves p's master to a descriptor past FD_SETSIZE (1024), as an
// app with many files open gets, and makes it one the os package does not
// poll, in non-blocking mode: a read with nothing to read then comes back
// with EAGAIN and waits in waitReadable.
func highMaster(t *testing.T, p *PTY) {
	t.Helper()
	old := p.master
	c, err := old.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var fd uintptr
	var errno syscall.Errno
	c.Control(func(m uintptr) {
		fd, _, errno = syscall.Syscall(syscall.SYS_FCNTL, m, syscall.F_DUPFD_CLOEXEC, 1500)
	})
	if errno != 0 {
		t.Fatal(errno)
	}
	old.Close()
	// The os package polls only a descriptor that it gets non-blocking.
	if err := syscall.SetNonblock(int(fd), false); err != nil {
		t.Fatal(err)
	}
	p.master = os.NewFile(fd, "ptmx")
	if err := syscall.SetNonblock(int(fd), true); err != nil {
		t.Fatal(err)
	}
	if _, err := p.master.Read(make([]byte, 1)); !errors.Is(err, syscall.EAGAIN) {
		t.Fatalf("a read with nothing to read = %v, want EAGAIN", err)
	}
}

func TestHighMasterReadsAndEnds(t *testing.T) {
	p, err := Start(Options{Path: "/bin/sh", Args: []string{"sh", "-c", "read line; echo got $line"}, Env: []string{"TERM=dumb"}})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Kill()
	highMaster(t, p)

	// The process waits for a line, so there is no output yet to wait for.
	waited := make(chan error, 1)
	go func() { waited <- p.waitReadable() }()
	if _, err := p.Write([]byte("typed\r")); err != nil {
		t.Fatal(err)
	}
	if err := wait.For(t, "the wait for output to end", waited); err != nil {
		t.Fatalf("waitReadable = %v", err)
	}

	// What it writes reaches the reader, and the read ends when it exits,
	// though the terminal stays open.
	var out strings.Builder
	read := make(chan error, 1)
	go func() {
		_, err := io.Copy(&out, p)
		read <- err
	}()
	if err := wait.For(t, "the end of the output", read); err != nil {
		t.Fatalf("read = %v", err)
	}
	if !strings.Contains(out.String(), "got typed\r\n") {
		t.Errorf("output %q lacks %q", out.String(), "got typed\r\n")
	}
}

func TestHighMasterClose(t *testing.T) {
	p, err := Start(Options{Path: "/bin/sh", Args: []string{"sh", "-c", "sleep 30"}})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Kill()
	highMaster(t, p)

	waited := make(chan error, 1)
	go func() { waited <- p.waitReadable() }()
	closed := make(chan error, 1)
	go func() { closed <- p.Close() }()
	wait.For(t, "Close to return", closed)
	if err := wait.For(t, "the wait for output to end", waited); err != errClosed {
		t.Fatalf("waitReadable = %v, want errClosed", err)
	}
	if _, err := p.Read(make([]byte, 64)); err != io.EOF {
		t.Fatalf("read after Close = %v, want EOF", err)
	}
	// Closing the terminal hangs up the process.
	wait.For(t, "the process to exit", p.Exited())
}
