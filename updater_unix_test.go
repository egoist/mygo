//go:build unix

package mygo

import (
	"os/exec"
	"runtime"
	"slices"
	"strconv"
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

// The process that installs the update inherits no file of the app, as
// one that native code opened without close-on-exec.
func TestUpdateSwapInheritsNoFiles(t *testing.T) {
	fd, err := syscall.Open("/dev/null", syscall.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(fd)
	open := func() bool {
		return exec.Command("/bin/sh", "-c", "[ -e /dev/fd/"+strconv.Itoa(fd)+" ]").Run() == nil
	}
	if !open() {
		t.Skip("a child inherits no file without close-on-exec here")
	}
	inheritNoFiles()
	if open() {
		t.Error("a child inherits the file")
	}
}

// An Install without App.Run, which swaps at once and waits for the swap,
// leaves the program's files as they are: its later children inherit
// them as before.
func TestUpdateWithoutRunKeepsFiles(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("only macOS stages updates")
	}
	fd, err := syscall.Open("/dev/null", syscall.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(fd)
	open := func() bool {
		return exec.Command("/bin/sh", "-c", "[ -e /dev/fd/"+strconv.Itoa(fd)+" ]").Run() == nil
	}
	if !open() {
		t.Skip("a child inherits no file without close-on-exec here")
	}
	fakeSwap(t, false)
	withoutRun(t)
	p := stagedApp(t)
	stage(t, p)
	if got := mainOf(p.target); got != "v2" {
		t.Errorf("once Install returned main = %q", got)
	}
	if !open() {
		t.Error("Install marked the program's file close-on-exec")
	}
}

// The swap reads lsappinfo's output as it is: an app found by its bundle
// is listed in the line that finding it by its pid prints, and a bundle
// that runs nothing prints no line, and succeeds. It needs a desktop
// session, with Finder, and skips without one.
func TestLsappinfoContract(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("lsappinfo is macOS's")
	}
	out, err := exec.Command("/usr/bin/pgrep", "-x", "Finder").Output()
	pid := strings.Fields(string(out))
	if err != nil || len(pid) == 0 {
		t.Skip("Finder does not run: no desktop session")
	}
	find := func(what string) string {
		t.Helper()
		out, err := exec.Command("/usr/bin/lsappinfo", "find", what).Output()
		if err != nil {
			t.Fatalf("lsappinfo find %s: %v", what, err)
		}
		return strings.TrimSpace(string(out))
	}
	// Finder by its pid tells whether this process reaches the Launch
	// Services of a desktop session, as an ssh session may not.
	byPid, err := exec.Command("/usr/bin/lsappinfo", "find", "pid="+pid[0]).Output()
	if err != nil || len(strings.TrimSpace(string(byPid))) == 0 {
		t.Skipf("no Launch Services session here: lsappinfo finds no Finder (%v)", err)
	}
	byBundle := find("bundlepath=/System/Library/CoreServices/Finder.app")
	if byBundle == "" {
		t.Fatal("Finder is not found by its bundle")
	}
	if line := strings.TrimSpace(string(byPid)); !slices.Contains(strings.Split(byBundle, "\n"), line) {
		t.Errorf("found by its pid %q, and by its bundle %q", line, byBundle)
	}
	if got := find("bundlepath=/nonexistent.app"); got != "" {
		t.Errorf("a bundle that runs nothing: %q", got)
	}
}
