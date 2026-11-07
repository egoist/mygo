//go:build linux && (amd64 || arm64)

package linux

import (
	"syscall"
	"testing"
	"unsafe"
)

// pollFD is struct pollfd, which GPollFD matches.
type pollFD struct {
	fd      int32
	events  int16
	revents int16
}

const pollIn = 0x1

// TestGLibPoll drives glibPoll on a pipe as GLib would: each case runs many
// times and counts the results, so none depends on how long a wait took.
func TestGLibPoll(t *testing.T) {
	var p [2]int
	if err := syscall.Pipe2(p[:], syscall.O_CLOEXEC); err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(p[0])
	defer syscall.Close(p[1])
	fd := pollFD{fd: int32(p[0]), events: pollIn}
	poll := func(timeout int32) int32 {
		fd.revents = 0
		return glibPoll(ptr(unsafe.Pointer(&fd)), 1, timeout)
	}
	const runs = 50
	count := func(timeout int32, want int32) int {
		n := 0
		for range runs {
			if poll(timeout) == want {
				n++
			}
		}
		return n
	}

	// An empty pipe: a check without a wait, a short wait that keeps the
	// P and a wait that hands it off all find nothing.
	for _, timeout := range []int32{0, 1, handoffWait} {
		if n := count(timeout, 0); n != runs {
			t.Errorf("empty pipe, timeout %d ms: %d of %d polls returned 0", timeout, n, runs)
		}
	}

	// A byte in the pipe: every poll, waiting or not, finds the one ready
	// descriptor and reports it in revents.
	if _, err := syscall.Write(p[1], []byte{1}); err != nil {
		t.Fatal(err)
	}
	for _, timeout := range []int32{0, 1, handoffWait, -1} {
		if n := count(timeout, 1); n != runs {
			t.Errorf("ready pipe, timeout %d: %d of %d polls returned 1", timeout, n, runs)
		}
		if fd.revents&pollIn == 0 {
			t.Errorf("ready pipe, timeout %d: revents %#x, want POLLIN", timeout, fd.revents)
		}
	}

	// An address ppoll cannot read is an error, which GLib sees as -1.
	if r := glibPoll(1, 1, 1); r != -1 {
		t.Errorf("unreadable fds: glibPoll returned %d, want -1", r)
	}
}
