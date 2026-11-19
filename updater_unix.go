//go:build unix

package mygo

import "syscall"

// detachedProcess starts a process in a session of its own, out of the
// app's process group, which a terminal or the system ends the app with:
// the process that installs an update outlives the app.
func detachedProcess() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
