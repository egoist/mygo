//go:build (darwin && !ios) || (linux && !android)

package pty

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"unsafe"
)

type sys struct {
	master *os.File
	cmd    *exec.Cmd
	// wakeR and wakeW are a pipe that Close closes the write end of, to
	// end a read's wait for output (waitReadable).
	wakeR, wakeW *os.File
}

// errClosed is what waitReadable returns once Close was called.
var errClosed = errors.New("pty: closed while waiting")

func (p *PTY) start(o Options) (err error) {
	wakeR, wakeW, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("pty: %w", err)
	}
	defer func() {
		if err != nil {
			wakeR.Close()
			wakeW.Close()
		}
	}()
	master, slaveName, err := openMaster()
	if err != nil {
		return err
	}
	slave, err := os.OpenFile(slaveName, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		return fmt.Errorf("pty: opening %s: %w", slaveName, err)
	}
	defer slave.Close() // the child has its own copies
	if err := setSize(master, o.Cols, o.Rows, o.Width, o.Height); err != nil {
		master.Close()
		return err
	}
	cmd := &exec.Cmd{Path: o.Path, Args: o.Args, Dir: o.Dir, Env: o.Env, Stdin: slave, Stdout: slave, Stderr: slave}
	// A session of its own, whose controlling terminal is the slave (the
	// child's descriptor 0), so that the terminal's signals reach the
	// process group in the foreground.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		master.Close()
		return err
	}
	p.master, p.cmd = master, cmd
	p.wakeR, p.wakeW = wakeR, wakeW
	return nil
}

func (p *PTY) wait() (int, error) {
	err := p.cmd.Wait()
	if e, ok := errors.AsType[*exec.ExitError](err); ok {
		if ws, ok := e.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal()), nil
		}
		return e.ExitCode(), nil
	}
	return 0, err
}

// Read reads the output of the process; it returns io.EOF once no process
// has the terminal open any more, or after Close.
func (p *PTY) Read(b []byte) (int, error) {
	for {
		n, err := p.master.Read(b)
		if n > 0 {
			return n, nil
		}
		if err == nil {
			return 0, nil
		}
		// Nothing to read yet, which a read on a master that the os package
		// cannot wait on says with EAGAIN: that is not the end of the
		// program. A reader that takes it for the end closes the master,
		// and the kernel then hangs the program up with SIGHUP, before it
		// runs a single command.
		if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EINTR) {
			if werr := p.waitReadable(); werr == errClosed {
				return 0, io.EOF
			} else if werr != nil {
				return 0, werr
			}
			continue
		}
		// Linux reports EIO when the last process with the terminal
		// open closes it.
		if errors.Is(err, syscall.EIO) || errors.Is(err, os.ErrClosed) {
			err = io.EOF
		}
		return 0, err
	}
}

// Write types b into the terminal.
func (p *PTY) Write(b []byte) (int, error) {
	n, err := p.master.Write(b)
	if errors.Is(err, os.ErrClosed) {
		err = ErrClosed
	}
	return n, err
}

// Resize sets the size of the terminal, which sends the process SIGWINCH.
func (p *PTY) Resize(cols, rows, width, height int) error {
	return setSize(p.master, cols, rows, width, height)
}

// ClearScreen does nothing: the terminal has no screen of its own to
// clear, unlike ConPTY's console.
func (p *PTY) ClearScreen() error { return nil }

// Pid returns the process's ID.
func (p *PTY) Pid() int { return p.cmd.Process.Pid }

// Foreground returns the ID of the process group in the foreground of
// the terminal, 0 when it can't tell.
func (p *PTY) Foreground() int {
	var pgrp int32
	if ioctl(p.master, syscall.TIOCGPGRP, unsafe.Pointer(&pgrp)) != nil {
		return 0
	}
	return int(pgrp)
}

// Close closes the terminal, which hangs up the processes that have it:
// they get SIGHUP. It does not wait for them to exit.
func (p *PTY) Close() error {
	// First end a wait for output, which keeps the master open meanwhile.
	p.wakeW.Close()
	err := p.master.Close()
	p.wakeR.Close()
	return err
}

// await runs wait with the descriptors of the master and of the wake pipe's
// read end, which stay open meanwhile: Close cannot free their numbers for
// another file to take while wait watches them. wait reports whether the
// wake pipe ended it, which means Close was called.
func (p *PTY) await(wait func(master, wake int) (woken bool, err error)) error {
	mc, err := p.master.SyscallConn()
	if err != nil {
		return errClosed
	}
	wc, err := p.wakeR.SyscallConn()
	if err != nil {
		return errClosed
	}
	woken := true
	var werr error
	if err := mc.Read(func(m uintptr) bool {
		wc.Control(func(w uintptr) { woken, werr = wait(int(m), int(w)) })
		return true
	}); err != nil || woken {
		return errClosed
	}
	return werr
}

// Kill kills the process and those of its process group, which it leads.
func (p *PTY) Kill() error {
	if err := syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL); err == nil {
		return nil
	}
	return p.cmd.Process.Kill()
}

func setSize(f *os.File, cols, rows, width, height int) error {
	ws := struct{ row, col, xpixel, ypixel uint16 }{clamp(rows), clamp(cols), clamp(width), clamp(height)}
	return ioctl(f, syscall.TIOCSWINSZ, unsafe.Pointer(&ws))
}

func clamp(v int) uint16 { return uint16(max(0, min(v, 0xFFFF))) }

// ioctl calls ioctl on f with a pointer, which it converts in the system
// call, so that what it points to stays put meanwhile.
func ioctl(f *os.File, req uintptr, arg unsafe.Pointer) error {
	c, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var errno syscall.Errno
	err = c.Control(func(fd uintptr) {
		_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg))
	})
	if err != nil {
		return err
	}
	if errno != 0 {
		return errno
	}
	return nil
}
