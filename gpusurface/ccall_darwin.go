//go:build darwin && !ios

package gpusurface

import (
	"syscall"
	_ "unsafe"
)

// syscall6 calls the C function fn with up to six integer arguments.
// purego.SyscallN allocates its variadic arguments on the Go heap, once a
// call; the runtime's syscall6, which golang.org/x/sys/unix links to the
// same way, allocates nothing.
//
//go:linkname syscall6 syscall.syscall6
func syscall6(fn, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2 uintptr, err syscall.Errno)
