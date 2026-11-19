//go:build !unix && !js

package mygo

import "syscall"

// detachedProcess returns nil: only macOS installs an update in a process
// of its own.
func detachedProcess() *syscall.SysProcAttr { return nil }

// inheritNoFiles does nothing: only macOS installs an update in a process
// of its own.
func inheritNoFiles() {}
