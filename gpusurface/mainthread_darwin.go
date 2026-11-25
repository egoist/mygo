//go:build darwin && !ios

package gpusurface

import (
	"sync"

	"github.com/ebitengine/purego"
)

// AppKit takes windows and views on the main thread only. The surface
// does its AppKit work (New, Close, SetDirect, ScreenSize) through onMain,
// so that they may be called from any goroutine.

// mainCall is the call onMain hands the main thread, one at a time.
var mainCall struct {
	mu sync.Mutex
	fn func()
}

// runMainCall is the function dispatch_sync_f runs on the main thread:
// one callback for the process, as purego's are never freed.
func runMainCall(uintptr) { mainCall.fn() }

func isMainThread() bool {
	r, _, _ := syscall6(lib.pthreadMainNP, 0, 0, 0, 0, 0, 0)
	return r != 0
}

// onMain runs fn on the main thread and returns once it ran: at once on
// the main thread, else through the main dispatch queue, which the main
// thread's run loop drains (as a MyGo app's does while it runs). A
// goroutine calling it must not be one the main thread waits for.
func onMain(fn func()) {
	if isMainThread() {
		fn()
		return
	}
	mainCall.mu.Lock()
	defer mainCall.mu.Unlock()
	mainCall.fn = fn
	purego.SyscallN(lib.dispatchSyncF, lib.mainQueue, 0, lib.mainCallback)
	mainCall.fn = nil
}
