//go:build linux && (amd64 || arm64)

package linux

import (
	"syscall"
	"unsafe"

	"github.com/ebitengine/purego"
)

// GTK runs its main loop inside one long purego call, so the Go runtime
// sees the main thread as an M in a syscall that holds a P. Each time GTK
// calls back into Go (an onMain hop, an event, a frame), the callback
// takes a P back and wakes sysmon from its deep sleep. sysmon then polls
// every 20 µs, backing off to 10 ms, until it retakes the P of the
// main thread 10 ms later: 40 to 60 wakeups of an otherwise idle process
// for each callback.
//
// installPoll gives the main context a poll function in Go that hands the
// P off before it waits (entersyscallblock), as the runtime does for its
// own blocking calls. While GTK waits every P is idle and sysmon sleeps
// until the next callback, which costs it a few wakeups instead of tens.
//
// GLib hands a poll function its timeout in whole milliseconds, rounded
// up, where its own poll waits to the nanosecond (ppoll, gmain.c). Frames
// at 120 Hz and above come late by that rounding, and some 10% of them are
// lost at 165 Hz. So the first short wait gives the main context back
// GLib's own poll while frames follow each other, and a one-shot timeout
// puts glibPoll back a quarter of a second later; if the app still
// animates, the next short wait takes it out again.

//go:linkname entersyscallblock runtime.entersyscallblock
func entersyscallblock()

//go:linkname exitsyscall runtime.exitsyscall
func exitsyscall()

// blockingPPoll is ppoll(2) with the P handed off for the wait. It must not
// grow its stack or allocate between entersyscallblock and exitsyscall.
//
//go:nosplit
func blockingPPoll(fds, nfds uintptr, ts *syscall.Timespec) (uintptr, syscall.Errno) {
	entersyscallblock()
	r, _, e := syscall.RawSyscall6(syscall.SYS_PPOLL, fds, nfds, uintptr(unsafe.Pointer(ts)), 0, 0, 0)
	exitsyscall()
	return r, e
}

// glibPoll is a GPollFunc: GPollFD has the layout of struct pollfd, and the
// timeout is in milliseconds, negative for none.
func glibPoll(fds ptr, nfds uint32, timeout int32) int32 {
	var r uintptr
	var e syscall.Errno
	switch {
	case timeout == 0:
		// A check that does not wait keeps the P.
		r, _, e = syscall.RawSyscall6(syscall.SYS_PPOLL, uintptr(fds), uintptr(nfds), uintptr(unsafe.Pointer(&syscall.Timespec{})), 0, 0, 0)
	case timeout > 0 && timeout < handoffWait:
		// A short wait, such as the next frame's while the app animates:
		// GLib polls itself from the next iteration on. This wait keeps
		// the P as an ordinary syscall does: sysmon leaves it alone for
		// 10 ms while other Ps are idle, and a handoff would start an M.
		usePoll(false)
		ts := syscall.NsecToTimespec(int64(timeout) * 1e6)
		r, _, e = syscall.Syscall6(syscall.SYS_PPOLL, uintptr(fds), uintptr(nfds), uintptr(unsafe.Pointer(&ts)), 0, 0, 0)
	default:
		var ts *syscall.Timespec
		if timeout > 0 {
			t := syscall.NsecToTimespec(int64(timeout) * 1e6)
			ts = &t
		}
		r, e = blockingPPoll(uintptr(fds), uintptr(nfds), ts)
	}
	switch {
	case e == syscall.EINTR:
		// GLib reads errno, which a raw syscall does not set. Report no
		// ready descriptors, and GLib polls again.
		return 0
	case e != 0:
		return -1
	}
	return int32(r)
}

// handoffWait is the shortest wait, in milliseconds, for which glibPoll
// hands the P off: sysmon retakes a P that has been in a syscall for 10 ms
// anyway, waking up every 20 µs to 10 ms until then.
const handoffWait = 10

// pollBackAfter is how long, in milliseconds, GLib polls itself after a
// short wait before glibPoll takes over again.
const pollBackAfter = 250

var (
	pollCallback, pollBackCallback uintptr
	setPollFunc                    func(ctx ptr, fn uintptr)
	// pollInstalled tells that the default main context polls through
	// glibPoll, pollBackArmed that a timeout will put it back.
	pollInstalled, pollBackArmed bool
)

// installPoll sets glibPoll on the default main context, which only the
// main thread iterates.
func installPoll() {
	if !bind(libGLib, &setPollFunc, "g_main_context_set_poll_func") {
		return
	}
	pollCallback = purego.NewCallback(glibPoll)
	pollBackCallback = purego.NewCallback(func(data ptr) bool {
		pollBackArmed = false
		usePoll(true)
		return false
	})
	usePoll(true)
}

// usePoll sets glibPoll on the default main context, or gives it back
// GLib's own poll and has glibPoll set again after pollBackAfter. GLib
// reads the poll function before it unlocks the context to poll, so this
// may run within glibPoll.
func usePoll(on bool) {
	if on == pollInstalled || setPollFunc == nil {
		return
	}
	pollInstalled = on
	if on {
		setPollFunc(0, pollCallback)
		return
	}
	setPollFunc(0, 0)
	if !pollBackArmed {
		pollBackArmed = true
		gTimeoutAdd(pollBackAfter, pollBackCallback, 0)
	}
}
