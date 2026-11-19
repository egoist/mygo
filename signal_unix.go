//go:build unix

package mygo

import (
	"os"
	"os/signal"
	"syscall"
)

// quitOnSignals makes SIGINT and SIGTERM quit the application like Quit, so
// quit listeners run (and can cancel). A second signal exits right away,
// once the process that installs a staged update has started, which the
// app otherwise starts as it finishes: by this call, or by finish if it
// took the update first, which the signal waits for at most 2 s and then
// takes as started.
func quitOnSignals() {
	ch := make(chan os.Signal, 1)
	for _, s := range []os.Signal{syscall.SIGINT, syscall.SIGTERM} {
		// Keep signals ignored by the parent, e.g. SIGINT for background jobs.
		if !signal.Ignored(s) {
			signal.Notify(ch, s)
		}
	}
	go func() {
		<-ch
		App.Quit()
		s := <-ch
		installStaged(false, nil)
		os.Exit(128 + int(s.(syscall.Signal)))
	}()
}
