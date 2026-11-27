//go:build linux && !android && (amd64 || arm64)

package linux

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// WebKitGTK starts sandboxed web processes in bubblewrap, which makes user
// namespaces. Where the system forbids them, as Ubuntu's AppArmor does for
// apps without a profile that allows them, WebKitGTK aborts the app at its
// first page with "Failed to fully launch dbus-proxy", which does not say
// why. So Init checks first, and Run returns the cause instead.

// sandboxCheck is checkSandbox, which LinkSandboxCheck links: Init is
// linked into every app, and only calls it.
var sandboxCheck func(getenv func(string) string, root string) error

// LinkSandboxCheck links the check of bubblewrap, when the app asks for
// the sandbox.
func LinkSandboxCheck() { sandboxCheck = checkSandbox }

// runBwrap runs bwrap as WebKitGTK does to tell whether it can sandbox
// pages in a container (isInsideUnsupportedContainer): in every namespace
// its sandbox makes. Tests replace it.
var runBwrap = func(bwrap string) ([]byte, error) {
	return exec.Command(bwrap, "--ro-bind", "/", "/", "--proc", "/proc", "--dev", "/dev", "--unshare-all", "true").CombinedOutput()
}

// checkSandbox returns why WebKitGTK cannot start pages in bubblewrap,
// where it would, with getenv the environment and root the file system's
// root.
func checkSandbox(getenv func(string) string, root string) error {
	// WebKitGTK does not run bubblewrap when the user turned the sandbox
	// off, in Flatpak, which has a sandbox of its own, or in Snap.
	if v := getenv("WEBKIT_DISABLE_SANDBOX_THIS_IS_DANGEROUS"); v != "" && v != "0" {
		return nil
	}
	if _, err := os.Stat(filepath.Join(root, ".flatpak-info")); err == nil {
		return nil
	}
	if getenv("SNAP") != "" && getenv("SNAP_NAME") != "" && getenv("SNAP_REVISION") != "" {
		return nil
	}
	// WebKitGTK runs the bwrap it was built with, /usr/bin/bwrap on
	// distributions. Elsewhere (NixOS) the one on PATH is the best guess,
	// and without one WebKitGTK is left to find its own.
	bwrap := ""
	for _, dir := range append([]string{"/usr/bin"}, filepath.SplitList(getenv("PATH"))...) {
		p := filepath.Join(root, dir, "bwrap")
		if fi, err := os.Stat(p); filepath.IsAbs(dir) && err == nil && fi.Mode().IsRegular() {
			bwrap = p
			break
		}
	}
	if bwrap == "" {
		return nil
	}
	out, err := runBwrap(bwrap)
	if err == nil {
		return nil
	}
	why := strings.TrimSpace(string(out))
	if why == "" {
		why = err.Error()
	}
	cause := "the system or the container forbids the user namespaces it makes"
	if b, _ := os.ReadFile(filepath.Join(root, "proc/sys/kernel/apparmor_restrict_unprivileged_userns")); strings.TrimSpace(string(b)) == "1" {
		cause = "AppArmor keeps apps from making user namespaces (kernel.apparmor_restrict_unprivileged_userns=1): install an AppArmor profile for the app that allows userns"
	}
	return fmt.Errorf("mygo: App.EnableSandbox: bubblewrap cannot sandbox pages (%s: %s): %s; see \"Sandboxing web pages on Linux\" in MyGo's docs/app.md", bwrap, why, cause)
}
