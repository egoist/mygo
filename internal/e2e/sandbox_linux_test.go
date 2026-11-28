//go:build linux && !android && (amd64 || arm64)

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/wait"
)

// TestSandbox runs page tests again in an app that called
// App.EnableSandbox, whose web processes WebKitGTK starts in bubblewrap:
// pages of the default session, of persistent and ephemeral ones, from a
// custom scheme, a file and a server still load and answer the app.
func TestSandbox(t *testing.T) {
	if os.Getenv("MYGO_E2E_SANDBOX") != "" {
		t.Skip("in the sandboxed app")
	}
	if os.Getenv("WEBKIT_DISABLE_SANDBOX_THIS_IS_DANGEROUS") != "" {
		t.Skip("WEBKIT_DISABLE_SANDBOX_THIS_IS_DANGEROUS turns WebKit's sandbox off")
	}
	// WebKitGTK aborts the app where bubblewrap cannot make its
	// namespaces, which this probe, as WebKit's own in containers, tells.
	// It is approximate: WebKit runs the bwrap it was built with
	// (/usr/bin/bwrap on distributions), the probe the one on PATH.
	if out, err := exec.Command("bwrap", "--ro-bind", "/", "/", "--proc", "/proc", "--dev", "/dev", "--unshare-all", "true").CombinedOutput(); err != nil {
		t.Skipf("bubblewrap cannot run here: %v %s", err, out)
	}
	cmd := exec.CommandContext(wait.Context(t), os.Args[0], "-test.run=^(TestSandboxedPages|TestIPCAndProtocol|TestSessions)$", "-test.v")
	cmd.Env = append(os.Environ(), "MYGO_E2E_SANDBOX=1")
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "--- PASS: TestSandboxedPages") {
		t.Fatalf("the sandboxed app: %v\n%s", err, out)
	}
}

// TestSandboxedPages, in TestSandbox's app: the web processes of each
// session run in bubblewrap, and a file's page loads there.
func TestSandboxedPages(t *testing.T) {
	if os.Getenv("MYGO_E2E_SANDBOX") == "" {
		t.Skip("TestSandbox runs it")
	}
	file := filepath.Join(t.TempDir(), "page.html")
	if err := os.WriteFile(file, []byte("<!doctype html><title>from a file</title>"), 0o644); err != nil {
		t.Fatal(err)
	}
	persistent := mygo.NewSession("e2e-sandbox")
	t.Cleanup(func() { _ = persistent.ClearData(mygo.SiteDataAll) })
	for _, s := range []struct {
		name    string
		session *mygo.Session
	}{
		{"default", nil},
		{"persistent", persistent},
		{"ephemeral", mygo.NewEphemeralSession()},
	} {
		t.Run(s.name, func(t *testing.T) {
			w := newWindow(t, mygo.WindowOptions{Hidden: true, Page: mygo.PageOptions{Session: s.session}})
			if err := w.Page().LoadFile(file); err != nil {
				t.Fatal(err)
			}
			waitFor(t, w, "document.title === 'from a file'")
			in, out := webProcesses(t)
			if in == 0 || out != 0 {
				t.Errorf("%d web processes of the app in bubblewrap, %d outside", in, out)
			}
		})
	}
}

// webProcesses counts the WebKitWebProcess processes this process started
// that run under bwrap, and those that do not.
func webProcesses(t *testing.T) (in, out int) {
	t.Helper()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		t.Fatal(err)
	}
	parent := map[int]int{}
	program := map[int]string{}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		stat, err := os.ReadFile(filepath.Join("/proc", e.Name(), "stat"))
		if err != nil {
			continue
		}
		// pid (comm) state ppid ...: comm may hold spaces.
		fields := strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+1:]))
		if len(fields) < 2 {
			continue
		}
		parent[pid], _ = strconv.Atoi(fields[1])
		cmdline, _ := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		name, _, _ := strings.Cut(string(cmdline), "\x00")
		program[pid] = filepath.Base(name)
	}
	self := os.Getpid()
	for pid, name := range program {
		if name != "WebKitWebProcess" {
			continue
		}
		bwrap := false
		for p := parent[pid]; p > 1; p = parent[p] {
			if program[p] == "bwrap" {
				bwrap = true
			}
			if p == self {
				if bwrap {
					in++
				} else {
					out++
				}
				break
			}
		}
	}
	return in, out
}
