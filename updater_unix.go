//go:build unix

package mygo

import (
	"os"
	"strconv"
	"syscall"
)

// detachedProcess starts a process in a session of its own, out of the
// app's process group, which a terminal or the system ends the app with:
// the process that installs an update outlives the app.
func detachedProcess() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

// inheritNoFiles marks every file of the app above its standard streams
// close-on-exec, before the app starts the process that installs an
// update. Go opens its own so, but AppKit and other native code need not:
// the process, which outlives the app, would hold their sockets, locks and
// files open. The process gets its standard streams explicitly.
func inheritNoFiles() {
	entries, err := os.ReadDir("/dev/fd")
	if err != nil {
		return
	}
	for _, e := range entries {
		if fd, err := strconv.Atoi(e.Name()); err == nil && fd > 2 {
			syscall.CloseOnExec(fd)
		}
	}
}
