//go:build !js

package mygo

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/internal/wait"
)

// secondInstance is a helper process of the single-instance tests (see
// TestMain for what its modes do). Every line it prints is sent on lines
// and collected; what it logs goes to logs; exit waits for its output to
// end and the process too.
type secondInstance struct {
	t     *testing.T
	name  string
	cmd   *exec.Cmd
	lines chan string
	logs  chan string
	read  chan struct{} // the output ended
	mu    sync.Mutex
	got   []string
}

// startSecondInstance starts a helper in the given mode, with an
// identifier of its own (empty for this process's), a file whose
// appearance lets it go on and the witness lock of the first instance
// (see the tests).
func startSecondInstance(t *testing.T, name, mode, identifier, goFile, witness string) *secondInstance {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	env := append(os.Environ(),
		"MYGO_TEST_SECOND_INSTANCE="+mode,
		// However long the production wait may be, a helper passes its
		// threshold at once: what the helper does past it is the point.
		"MYGO_TEST_WAIT=1ms")
	if identifier != "" {
		env = append(env, "MYGO_TEST_IDENTIFIER="+identifier)
	}
	if goFile != "" {
		env = append(env, "MYGO_TEST_GO_FILE="+goFile)
	}
	if witness != "" {
		env = append(env, "MYGO_TEST_WITNESS="+witness)
	}
	cmd.Env = env
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	errOut, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	s := &secondInstance{t: t, name: name, cmd: cmd, lines: make(chan string, 16), logs: make(chan string, 16), read: make(chan struct{})}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		s.cmd.Process.Kill()
		s.cmd.Wait()
	})
	go func() {
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			s.mu.Lock()
			s.got = append(s.got, sc.Text())
			s.mu.Unlock()
			s.lines <- sc.Text()
		}
		close(s.read)
	}()
	go func() {
		sc := bufio.NewScanner(errOut)
		for sc.Scan() {
			s.logs <- sc.Text()
		}
	}()
	return s
}

// line waits until the helper prints line.
func (s *secondInstance) line(line string) {
	s.t.Helper()
	for {
		select {
		case got := <-s.lines:
			if got == line {
				return
			}
		case <-time.After(wait.Guard):
			s.t.Fatalf("%s never printed %q (printed %q)", s.name, line, s.output())
		}
	}
}

// output is everything the helper printed so far.
func (s *secondInstance) output() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.got...)
}

// exit waits for the helper's output to end and the process with it,
// reporting either failure.
func (s *secondInstance) exit() {
	s.t.Helper()
	select {
	case <-s.read:
	case <-time.After(wait.Guard):
		s.t.Fatalf("%s never finished its output (printed %q)", s.name, s.output())
	}
	if err := s.cmd.Wait(); err != nil {
		s.t.Fatalf("%s exited with %v (printed %q)", s.name, err, s.output())
	}
}

// saidWait waits until the helper says on its log that it waits for the
// holder of the lock, which it does once past its wait threshold (1 ms
// here) — while the first instance is still alive, since nothing lets it
// go but this test. A helper that locks instead runs beside a live
// instance, the bug of two instances at once (#1906): whatever its log
// said, failing here names it.
func (s *secondInstance) saidWait() {
	s.t.Helper()
	for {
		select {
		case l := <-s.logs:
			if strings.Contains(l, "mygo: waiting for the other instance") {
				return
			}
		case l := <-s.lines:
			if l == "locked" {
				s.t.Fatalf("%s locked instead of waiting for the first instance (printed %q)", s.name, s.output())
			}
		case <-time.After(wait.Guard):
			s.t.Fatalf("%s neither waits nor locks (printed %q)", s.name, s.output())
		}
	}
}

// witnessHeldBy reports whether the helper of the first instance still
// lives, by the witness lock it holds until its process ends: a lock the
// system frees only with the process, checked with the same call the
// single instance lock itself uses. The second helper asks it right
// after getting the lock — the line after "locked" is its answer — so a
// first instance still alive then is the bug of two instances at once,
// and no scheduling of the test process can reorder the two (#1906).
func (s *secondInstance) witnessHeldBy(first *secondInstance) {
	s.t.Helper()
	s.line("locked")
	select {
	case got := <-s.lines:
		if got != "witness-free" {
			s.t.Fatalf("%s locked while %s still lived (then %q, after %q)", s.name, first.name, got, s.output())
		}
	case <-time.After(wait.Guard):
		s.t.Fatalf("%s never said whether %s lives (printed %q)", s.name, first.name, s.output())
	}
}

// TestSingleInstanceOSLock checks that an instance holds the OS lock next
// to its socket: another process cannot take that lock, however long the
// instance lives on past the quit that released its listener (#1906).
func TestSingleInstanceOSLock(t *testing.T) {
	if !App.RequestSingleInstanceLock() {
		t.Fatal("the first instance did not get the lock")
	}
	// The helper only asks whether the lock file of this process's
	// instance is held.
	other := startSecondInstance(t, "other", "probe", "", "", "")
	defer other.exit()
	select {
	case got := <-other.lines:
		if got != "held" {
			t.Fatalf("another process found the lock %q", got)
		}
	case <-time.After(wait.Guard):
		t.Fatal("the helper never answered")
	}
}

// TestSingleInstanceWaitsForExit checks that a launch while the first
// instance is quitting — its listener released, as OnQuit does, while the
// process goes on finishing what App.Run left — waits for that process to
// end before taking over, however long that takes, instead of running
// beside its shutdown (#1906).
func TestSingleInstanceWaitsForExit(t *testing.T) {
	dir := t.TempDir()
	goFile, witness := filepath.Join(dir, "go"), filepath.Join(dir, "witness")
	first := startSecondInstance(t, "first", "run", "si-exit", goFile, witness)
	first.line("locked")
	first.line("released") // the listener is gone, the process is not
	second := startSecondInstance(t, "second", "start", "si-exit", "", witness)
	second.line("starting")
	// The first instance still lives, and the second says it waits for
	// it, past its wait threshold: it must not run beside it, however
	// long that takes (#1906). Only then is the first let go.
	second.saidWait()
	if err := os.WriteFile(goFile, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	first.exit()
	second.witnessHeldBy(first)
	second.exit()
}

// TestSingleInstanceOneWinner checks that two launches at the same moment
// cannot both win: while one holds the OS lock without a socket yet, the
// other waits instead of taking the socket over, past the wait it says
// (#1906).
func TestSingleInstanceOneWinner(t *testing.T) {
	dir := t.TempDir()
	goFile, witness := filepath.Join(dir, "go"), filepath.Join(dir, "witness")
	first := startSecondInstance(t, "first", "hold", "si-race", goFile, witness)
	first.line("locked") // it holds the OS lock, and no socket yet
	second := startSecondInstance(t, "second", "start", "si-race", "", witness)
	second.line("starting")
	second.saidWait()
	if err := os.WriteFile(goFile, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	first.exit()
	second.witnessHeldBy(first)
	second.exit()
}
