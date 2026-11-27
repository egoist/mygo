package main

import (
	"runtime"
	"syscall"
)

// simulatorArch is the Go architecture of the Mac's simulator, which runs
// apps of its own architecture: arm64 on Apple silicon, even under
// Rosetta, and amd64 on an Intel Mac, which has no hw.optional.arm64.
func simulatorArch() string {
	if runtime.GOARCH == "arm64" {
		return "arm64"
	}
	if v, err := syscall.Sysctl("hw.optional.arm64"); err == nil && v != "" && v[0] == 1 {
		return "arm64"
	}
	return "amd64"
}
