//go:build linux && !android && (amd64 || arm64)

package linux

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

// fakeBwrap makes runBwrap answer as bwrap does on a host where it can
// (ok) or cannot make its namespaces, and counts its runs.
func fakeBwrap(t *testing.T, ok bool) *[]string {
	t.Helper()
	var runs []string
	previous := runBwrap
	runBwrap = func(bwrap string) ([]byte, error) {
		runs = append(runs, bwrap)
		if ok {
			return nil, nil
		}
		return []byte("bwrap: setting up uid map: Permission denied\n"), errors.New("exit status 1")
	}
	t.Cleanup(func() { runBwrap = previous })
	return &runs
}

// sandboxHost is a file system's root with these files.
func sandboxHost(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		p := filepath.Join(root, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestCheckSandbox(t *testing.T) {
	ubuntu := map[string]string{"usr/bin/bwrap": "", "proc/sys/kernel/apparmor_restrict_unprivileged_userns": "1\n"}
	with := func(files map[string]string, more ...string) map[string]string {
		f := map[string]string{}
		for k, v := range files {
			f[k] = v
		}
		for i := 0; i < len(more); i += 2 {
			f[more[i]] = more[i+1]
		}
		return f
	}
	for _, c := range []struct {
		name   string
		env    map[string]string
		files  map[string]string
		bwrap  bool   // bwrap can make its namespaces
		ran    string // the bwrap run, from root
		reason string // in the error, "" for none
	}{
		{"bwrap works", nil, ubuntu, true, "/usr/bin/bwrap", ""},
		{"Ubuntu's AppArmor", nil, ubuntu, false, "/usr/bin/bwrap", "kernel.apparmor_restrict_unprivileged_userns=1"},
		{"AppArmor allows userns", nil, with(ubuntu, "proc/sys/kernel/apparmor_restrict_unprivileged_userns", "0\n"), false, "/usr/bin/bwrap", "forbids the user namespaces"},
		{"no AppArmor", nil, map[string]string{"usr/bin/bwrap": ""}, false, "/usr/bin/bwrap", "forbids the user namespaces"},
		{"bwrap on PATH", map[string]string{"PATH": "relative:/run/current-system/sw/bin"}, map[string]string{"relative/bwrap": "", "run/current-system/sw/bin/bwrap": ""}, false, "/run/current-system/sw/bin/bwrap", "forbids the user namespaces"},
		{"no bwrap", map[string]string{"PATH": "/bin"}, map[string]string{"proc/sys/kernel/apparmor_restrict_unprivileged_userns": "1\n"}, false, "", ""},
		{"Flatpak", nil, with(ubuntu, ".flatpak-info", ""), false, "", ""},
		{"Snap", map[string]string{"SNAP": "/snap/app/1", "SNAP_NAME": "app", "SNAP_REVISION": "1"}, ubuntu, false, "", ""},
		{"SNAP alone", map[string]string{"SNAP": "/snap/app/1"}, ubuntu, false, "/usr/bin/bwrap", "kernel.apparmor_restrict_unprivileged_userns=1"},
		{"the user turned the sandbox off", map[string]string{"WEBKIT_DISABLE_SANDBOX_THIS_IS_DANGEROUS": "1"}, ubuntu, false, "", ""},
		{"WEBKIT_DISABLE_SANDBOX_THIS_IS_DANGEROUS=0", map[string]string{"WEBKIT_DISABLE_SANDBOX_THIS_IS_DANGEROUS": "0"}, ubuntu, false, "/usr/bin/bwrap", "kernel.apparmor_restrict_unprivileged_userns=1"},
	} {
		t.Run(c.name, func(t *testing.T) {
			runs := fakeBwrap(t, c.bwrap)
			root := sandboxHost(t, c.files)
			err := checkSandbox(func(k string) string { return c.env[k] }, root)
			var want []string
			if c.ran != "" {
				want = []string{filepath.Join(root, c.ran)}
			}
			if strings.Join(*runs, " ") != strings.Join(want, " ") {
				t.Errorf("ran bwrap %q, want %q", *runs, want)
			}
			switch {
			case c.reason == "" && err != nil:
				t.Errorf("checkSandbox = %v, want nil", err)
			case c.reason != "" && (err == nil || !strings.Contains(err.Error(), c.reason) || !strings.Contains(err.Error(), "setting up uid map: Permission denied")):
				t.Errorf("checkSandbox = %v, want an error with bwrap's output and %q", err, c.reason)
			}
		})
	}
}

// TestInitSandboxCheck: an app that asked for the sandbox where bubblewrap
// cannot make its namespaces gets the cause from Run, before GTK or
// WebKitGTK is loaded, so no page starts unsandboxed or aborts the app.
func TestInitSandboxCheck(t *testing.T) {
	previous, backend := sandboxCheck, theBackend
	defer func() { sandboxCheck, theBackend = previous, backend }()
	runs := fakeBwrap(t, false)
	root := sandboxHost(t, map[string]string{"usr/bin/bwrap": "", "proc/sys/kernel/apparmor_restrict_unprivileged_userns": "1\n"})
	sandboxCheck = func(func(string) string, string) error {
		return checkSandbox(func(string) string { return "" }, root)
	}
	loaded := libGLib != 0
	err := New().Init(nil, platform.AppOptions{Name: "test", Sandbox: true})
	if err == nil || !strings.Contains(err.Error(), "AppArmor profile for the app that allows userns") {
		t.Fatalf("Init = %v, want bubblewrap's error", err)
	}
	if len(*runs) != 1 {
		t.Errorf("Init ran bwrap %d times, want 1", len(*runs))
	}
	if !loaded && libGLib != 0 {
		t.Error("Init loaded GLib before it failed")
	}
}
