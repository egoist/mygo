package native

import (
	"os"

	"github.com/ebitengine/purego"
)

// Android's C library is bionic's libc, which holds malloc, free and
// gettid as well: the main thread's is the process's.
const (
	libcPath     = "libc.so"
	isMainSymbol = "gettid"
)

func mainThread() bool {
	r, _, _ := purego.SyscallN(libc.isMain)
	return int(r) == os.Getpid()
}

// olderMacOS is false off macOS: a module's minimum is macOS's.
func olderMacOS(string) bool { return false }

// releaseSurface has no surfaces to release off macOS.
func releaseSurface(uintptr) {}
