//go:build unix

package mygo

import (
	"strings"
	"syscall"
	"testing"

	"github.com/egoist/mygo/internal/wait"
)

// The process that installs the update runs in a session of its own and
// ignores the signals that end the app: a terminal's, the system's, or one
// sent to its own process group.
func TestUpdateSwapSurvivesSignals(t *testing.T) {
	logPath, opens := fakeSwap(t, false)
	p := stagedApp(t)
	app := fakeApp(t)
	swap, err := startSwap(p, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	wait.Until(t, "the swap to wait for the app", func() bool { return strings.Contains(readLog(logPath), "once process") })
	if pgid, err := syscall.Getpgid(swap.Process.Pid); err != nil || pgid != swap.Process.Pid {
		t.Errorf("the swap's process group is %d (%v), not its own", pgid, err)
	}
	for _, sig := range []syscall.Signal{syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT} {
		if err := syscall.Kill(-swap.Process.Pid, sig); err != nil {
			t.Fatal(err)
		}
	}
	exitApp(app)
	if err := swap.Wait(); err != nil {
		t.Fatalf("the swap ended with %v:\n%s", err, readLog(logPath))
	}
	if got := mainOf(p.target); got != "v2" {
		t.Errorf("after the app exited main = %q", got)
	}
	if got := opens(); len(got) != 1 {
		t.Errorf("opened the app %q", got)
	}
}
