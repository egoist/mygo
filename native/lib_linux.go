package native

import (
	"os"

	"github.com/ebitengine/purego"
)

// Linux runs a module's Swift implementation under go test and go run,
// built by mygo generate with the Swift toolchain for Linux; apps for
// Linux do not ship modules yet. Swift's main actor is the main thread,
// the process's first.
const (
	libcPath     = "libc.so.6"
	isMainSymbol = "gettid"
)

func mainThread() bool {
	r, _, _ := purego.SyscallN(libc.isMain)
	return int(r) == os.Getpid()
}

// packagedUnsupported is true: apps for Linux ship no module yet, so a
// packaged app has none (errors.ErrUnsupported).
const packagedUnsupported = true

// olderMacOS is false off macOS: a module's minimum is macOS's.
func olderMacOS(string) bool { return false }

// releaseSurface has no surfaces to release off macOS.
func releaseSurface(uintptr) {}
