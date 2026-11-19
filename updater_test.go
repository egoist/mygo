//go:build !js

package mygo

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/internal/update"
	"github.com/egoist/mygo/internal/wait"
)

// updateServer serves a signed update whose app holds main with content,
// and with deltaFrom, a delta of it from that version of installedApp.
// requests counts the requests of each file.
func updateServer(t *testing.T, version, content, deltaFrom string) (srv *httptest.Server, key string, m update.Manifest, requests func(string) int) {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(nil)
	src := t.TempDir()
	app := "app"
	if runtime.GOOS == "darwin" {
		app = "Test.app"
	}
	main := filepath.Join(src, app, "main")
	if err := os.MkdirAll(filepath.Dir(main), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(main, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, app, "resource"), []byte("unchanged"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, app, "code"), newCode, 0o644); err != nil {
		t.Fatal(err)
	}
	entries := []string{app}
	if runtime.GOOS != "darwin" {
		// Elsewhere the archive holds the files of the app directory.
		src, entries = filepath.Join(src, app), []string{"code", "main", "resource"}
	}
	var archive bytes.Buffer
	if err := update.WriteArchive(&archive, src, entries); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(archive.Bytes())
	m = update.Manifest{Version: version, Notes: "- Faster", Date: "2026-09-27T10:00:00Z", Size: int64(archive.Len()), Signature: update.Sign(priv, sum[:])}
	var mu sync.Mutex
	counts := map[string]int{}
	mux := http.NewServeMux()
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		counts[r.URL.Path]++
		mu.Unlock()
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	m.URL = srv.URL + "/app.tar.gz"
	mux.HandleFunc("/app.tar.gz", func(w http.ResponseWriter, r *http.Request) { w.Write(archive.Bytes()) })
	if deltaFrom != "" {
		old, _ := installedApp(t)
		var delta bytes.Buffer
		if err := update.WriteDelta(&delta, deltaFrom, version, old, filepath.Join(src, entries[0]), nil); runtime.GOOS != "darwin" {
			delta.Reset()
			err = update.WriteDelta(&delta, deltaFrom, version, old, src, entries)
		} else if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(delta.Bytes())
		m.Deltas = []update.Delta{{From: deltaFrom, URL: srv.URL + "/app.delta", Size: int64(delta.Len()), Signature: update.Sign(priv, sum[:])}}
		mux.HandleFunc("/app.delta", func(w http.ResponseWriter, r *http.Request) { w.Write(delta.Bytes()) })
	}
	mux.HandleFunc("/update.json", func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(m) })
	return srv, base64.StdEncoding.EncodeToString(pub), m, func(path string) int {
		mu.Lock()
		defer mu.Unlock()
		return counts[path]
	}
}

// installedApp creates what an update replaces, holding main with "v1".
func installedApp(t *testing.T) (target, main string) {
	t.Helper()
	dir := t.TempDir()
	target = dir
	if runtime.GOOS == "darwin" {
		target = filepath.Join(dir, "My App.app")
	}
	main = filepath.Join(target, "main")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(main, []byte("v1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "resource"), []byte("unchanged"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "code"), oldCode, 0o644); err != nil {
		t.Fatal(err)
	}
	return target, main
}

// oldCode is a file of installedApp, which newCode, of the update, changes
// a little: a delta patches it.
var oldCode, newCode = func() ([]byte, []byte) {
	r := rand.New(rand.NewPCG(1, 2))
	old := make([]byte, 64<<10)
	for i := range old {
		old[i] = byte(r.Uint32())
	}
	new := bytes.Clone(old)
	for range 20 {
		new[r.IntN(len(new))]++
	}
	return old, new
}()

func TestUpdater(t *testing.T) {
	ctx := context.Background()
	srv, key, m, _ := updateServer(t, "1.2.0", "v2", "")

	if _, err := Updater.Check(ctx); err != ErrUpdatesDisabled {
		t.Errorf("Check without updates: %v", err)
	}
	up, err := checkUpdate(ctx, feedSource(srv.URL+"/update.json"), key, "1.1.9", false)
	if err != nil || up == nil || up.Version != "1.2.0" || up.Notes != "- Faster" || up.Date.Year() != 2026 {
		t.Fatalf("checkUpdate = %+v, %v", up, err)
	}
	// An app on a prerelease, whose user turned prereleases off, takes no
	// older version, and the release of its version once there is one.
	for _, current := range []string{"1.2.0", "1.10.0", "1.3.0-beta.1"} {
		if up, err := checkUpdate(ctx, feedSource(srv.URL+"/update.json"), key, current, false); up != nil || err != nil {
			t.Errorf("checkUpdate from %s = %+v, %v", current, up, err)
		}
	}
	if up, err := checkUpdate(ctx, feedSource(srv.URL+"/update.json"), key, "1.2.0-rc.1", false); err != nil || up == nil || up.Version != "1.2.0" {
		t.Errorf("checkUpdate from 1.2.0-rc.1 = %+v, %v", up, err)
	}
	// Feeds of updates.url are the same with prereleases on.
	if up, err := checkUpdate(ctx, feedSource(srv.URL+"/update.json"), key, "1.1.9", true); err != nil || up == nil || up.Version != "1.2.0" {
		t.Errorf("checkUpdate with prereleases = %+v, %v", up, err)
	}
	if _, err := checkUpdate(ctx, feedSource("http://example.com/update.json"), key, "1.0.0", false); err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Errorf("plain HTTP: %v", err)
	}

	target, main := installedApp(t)
	quit := runningApp(t)
	var progress [2]int64
	err = installUpdate(ctx, httpGet, up.manifest, key, up.current, target, func(n, total int64) { progress = [2]int64{n, total} })
	if err != nil {
		t.Fatal(err)
	}
	quit()
	if b, _ := os.ReadFile(main); string(b) != "v2" {
		t.Errorf("after the update main = %q", b)
	}
	if progress != [2]int64{m.Size, m.Size} {
		t.Errorf("progress = %v, want %d", progress, m.Size)
	}
	if leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(target), ".*")); runtime.GOOS == "darwin" && len(leftovers) > 0 {
		t.Errorf("left behind: %q", leftovers)
	}

	// A tampered archive, or one signed with another key, is refused and
	// changes nothing.
	target, main = installedApp(t)
	bad := up.manifest
	bad.Signature = base64.StdEncoding.EncodeToString(make([]byte, 64))
	if err := installUpdate(ctx, httpGet, bad, key, up.current, target, nil); err == nil || !strings.Contains(err.Error(), "signed") {
		t.Errorf("bad signature: %v", err)
	}
	otherKey, _, _ := ed25519.GenerateKey(nil)
	if err := installUpdate(ctx, httpGet, up.manifest, base64.StdEncoding.EncodeToString(otherKey), up.current, target, nil); err == nil {
		t.Error("an update signed with another key was installed")
	}
	short := up.manifest
	short.Size++
	if err := installUpdate(ctx, httpGet, short, key, up.current, target, nil); err == nil || !strings.Contains(err.Error(), "bytes") {
		t.Errorf("wrong size: %v", err)
	}
	if b, _ := os.ReadFile(main); string(b) != "v1" {
		t.Errorf("a refused update changed main to %q", b)
	}
}

func TestUpdaterDelta(t *testing.T) {
	ctx := context.Background()
	srv, key, m, requests := updateServer(t, "1.2.0", "v2, a bigger version", "1.1.9")
	up, err := checkUpdate(ctx, feedSource(srv.URL+"/update.json"), key, "1.1.9", false)
	if err != nil || len(up.manifest.Deltas) != 1 {
		t.Fatalf("checkUpdate = %+v, %v", up, err)
	}
	install := func(m update.Manifest, current string, corrupt bool) (progress [][2]int64) {
		t.Helper()
		target, main := installedApp(t)
		quit := runningApp(t)
		if corrupt {
			os.WriteFile(filepath.Join(target, "code"), newCode[:1000], 0o644)
		}
		err := installUpdate(ctx, httpGet, m, key, current, target, func(n, total int64) { progress = append(progress, [2]int64{n, total}) })
		if err != nil {
			t.Fatal(err)
		}
		quit()
		if b, _ := os.ReadFile(main); string(b) != "v2, a bigger version" {
			t.Errorf("after the update main = %q", b)
		}
		if b, _ := os.ReadFile(filepath.Join(target, "resource")); string(b) != "unchanged" {
			t.Errorf("after the update resource = %q", b)
		}
		if b, _ := os.ReadFile(filepath.Join(target, "code")); !bytes.Equal(b, newCode) {
			t.Error("after the update code is not the new one")
		}
		if leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(target), ".*")); runtime.GOOS == "darwin" && len(leftovers) > 0 {
			t.Errorf("left behind: %q", leftovers)
		}
		return progress
	}
	delta := m.Deltas[0]

	// The delta of the running version is all it downloads.
	if delta.Size > m.Size/10 {
		t.Errorf("the delta takes %d bytes, the archive %d", delta.Size, m.Size)
	}
	if progress := install(up.manifest, up.current, false); progress[len(progress)-1] != [2]int64{delta.Size, delta.Size} {
		t.Errorf("progress = %v, want the delta's %d bytes", progress, delta.Size)
	}
	if requests("/app.delta") != 1 || requests("/app.tar.gz") != 0 {
		t.Errorf("downloaded the delta %d times and the archive %d", requests("/app.delta"), requests("/app.tar.gz"))
	}

	// An app that the delta was not made from gets the archive, and so do
	// other versions and deltas that are not signed.
	if progress := install(up.manifest, up.current, true); progress[len(progress)-1] != [2]int64{m.Size, m.Size} {
		t.Errorf("progress = %v, want the archive's %d bytes last", progress, m.Size)
	}
	if requests("/app.delta") != 2 || requests("/app.tar.gz") != 1 {
		t.Errorf("downloaded the delta %d times and the archive %d", requests("/app.delta"), requests("/app.tar.gz"))
	}
	install(up.manifest, "1.1.8", false)
	if requests("/app.delta") != 2 || requests("/app.tar.gz") != 2 {
		t.Errorf("downloaded the delta %d times and the archive %d", requests("/app.delta"), requests("/app.tar.gz"))
	}
	unsigned := up.manifest
	unsigned.Deltas = []update.Delta{delta}
	unsigned.Deltas[0].Signature = m.Signature
	install(unsigned, up.current, false)
	if requests("/app.delta") != 3 || requests("/app.tar.gz") != 3 {
		t.Errorf("downloaded the delta %d times and the archive %d", requests("/app.delta"), requests("/app.tar.gz"))
	}
}

// runningApp stands in for the app that installUpdate updates on macOS,
// where the update waits for the app to exit: quit starts the process
// that installs the staged update, as the app does as it finishes, ends
// the app and returns once the update is in place.
func runningApp(t *testing.T) (quit func()) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		return func() {}
	}
	app := fakeApp(t)
	fakeSwap(t, false)
	return func() {
		t.Helper()
		stageMu.Lock()
		p := pendingUpdate
		pendingUpdate = nil
		stageMu.Unlock()
		if p == nil {
			t.Fatal("no update staged")
		}
		swap, err := startSwap(p, false, nil)
		if err != nil {
			t.Fatal(err)
		}
		exitApp(app)
		if err := swap.Wait(); err != nil {
			t.Fatal(err)
		}
	}
}

// fakeApp starts a process that stands in for the app, whose exit the
// process that installs a staged update waits for (appPid), until
// exitApp ends it.
func fakeApp(t *testing.T) *exec.Cmd {
	t.Helper()
	app := exec.Command("/bin/sleep", "60")
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	old := appPid
	appPid = app.Process.Pid
	t.Cleanup(func() {
		appPid = old
		exitApp(app)
	})
	return app
}

// exitApp ends the fake app and reaps it: until then its pid stays, and
// the swap process takes it for running.
func exitApp(app *exec.Cmd) {
	if app.ProcessState == nil {
		app.Process.Kill()
		app.Wait()
	}
}

// fakeSwap makes the process that installs a staged update log to a file
// of the test, and open the app with a fake open, which records its
// arguments and fails, when failOpen is set, unless its first argument is
// -n: tests never open apps. opens returns the arguments of each call.
// The state of staged updates is restored after the test.
func fakeSwap(t *testing.T, failOpen bool) (logPath string, opens func() [][]string) {
	t.Helper()
	dir := t.TempDir()
	logPath = filepath.Join(dir, "Logs", "update.log")
	record := filepath.Join(dir, "opens")
	status := 0
	if failOpen {
		status = 1
	}
	open := filepath.Join(dir, "open")
	script := fmt.Sprintf("#!/bin/sh\n{ printf '%%s\\0' \"$@\"; printf '\\001'; } >> '%s'\n[ \"$1\" = -n ] || exit %d\n", record, status)
	if err := os.WriteFile(open, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	oldOpen, oldSteps, oldLog := swapOpen, swapWaitSteps, updateLogPath
	swapOpen = open
	updateLogPath = func() (string, error) { return logPath, nil }
	t.Cleanup(func() {
		swapOpen, swapWaitSteps, updateLogPath = oldOpen, oldSteps, oldLog
		stageMu.Lock()
		pendingUpdate, updateQuitting = nil, false
		stageMu.Unlock()
	})
	return logPath, func() [][]string {
		b, _ := os.ReadFile(record)
		var calls [][]string
		for _, call := range strings.Split(string(b), "\x01") {
			if call != "" {
				args := strings.Split(call, "\x00")
				calls = append(calls, args[:len(args)-1])
			}
		}
		return calls
	}
}

// stagedApp makes an installed app bundle, whose main is "v1", and the
// update that stageBundle stages for it, whose main is "v2", next to it.
func stagedApp(t *testing.T) *stagedUpdate {
	t.Helper()
	dir := t.TempDir()
	p := &stagedUpdate{target: filepath.Join(dir, "My App.app"), work: filepath.Join(dir, ".My App.app.update-1")}
	p.bundle = filepath.Join(p.work, "app", "My App.app")
	for content, bundle := range map[string]string{"v1": p.target, "v2": p.bundle} {
		if err := os.MkdirAll(bundle, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(bundle, "main"), []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

// mainOf returns the content of the main file of the app bundle target.
func mainOf(target string) string {
	b, _ := os.ReadFile(filepath.Join(target, "main"))
	return string(b)
}

// readLog returns what the update log at path holds.
func readLog(path string) string {
	b, _ := os.ReadFile(path)
	return string(b)
}

// On macOS the bundle of the running app stays in place until the app has
// exited: moving it away took the app's Dock tile with it, and the
// relaunched version got a second tile. Nothing runs before the app
// quits: the update is only staged.
func TestUpdaterWaitsForTheAppToExit(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("only macOS waits for the app to exit")
	}
	ctx := context.Background()
	srv, key, _, _ := updateServer(t, "1.2.0", "v2", "")
	up, err := checkUpdate(ctx, feedSource(srv.URL+"/update.json"), key, "1.1.9", false)
	if err != nil {
		t.Fatal(err)
	}
	target, main := installedApp(t)
	quit := runningApp(t)
	if err := installUpdate(ctx, httpGet, up.manifest, key, up.current, target, nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(main); string(b) != "v1" {
		t.Errorf("while the app runs main = %q", b)
	}
	if pendingUpdate == nil || pendingUpdate.target != target {
		t.Fatalf("staged %+v", pendingUpdate)
	}
	quit()
	if b, _ := os.ReadFile(main); string(b) != "v2" {
		t.Errorf("after the app exited main = %q", b)
	}
	if leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(target), ".*")); len(leftovers) > 0 {
		t.Errorf("left behind: %q", leftovers)
	}
}

// TestUpdateSwap runs the process that installs a staged update as the
// app starts it when it relaunches: it waits for the app to exit, swaps
// the bundles, removes its work, opens the new version with Relaunch's
// arguments, quoted as they are, and logs each step.
func TestUpdateSwap(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("only macOS installs updates with a script")
	}
	logPath, opens := fakeSwap(t, false)
	p := stagedApp(t)
	app := fakeApp(t)
	args := []string{"--reason", "it's an update", "a b", `$HOME "quoted" \ back; $(touch pwned)`, "line\nbreak", ""}
	swap, err := startSwap(p, true, args)
	if err != nil {
		t.Fatal(err)
	}
	wait.Until(t, "the swap to wait for the app", func() bool { return strings.Contains(readLog(logPath), "once process") })
	if got := mainOf(p.target); got != "v1" {
		t.Errorf("while the app runs main = %q", got)
	}
	exitApp(app)
	if err := swap.Wait(); err != nil {
		t.Fatal(err)
	}
	if got := mainOf(p.target); got != "v2" {
		t.Errorf("after the app exited main = %q", got)
	}
	if _, err := os.Stat(p.work); !os.IsNotExist(err) {
		t.Error("the swap left its work")
	}
	if got, want := opens(), [][]string{openArgs(p.target, args)}; !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("opened the app with %q, want %q", got, want)
	}
	if _, err := os.Stat("pwned"); err == nil {
		os.Remove("pwned")
		t.Error("an argument ran as a command")
	}
	log := readLog(logPath)
	for _, step := range []string{
		"installing " + p.bundle + " in " + p.target + " once process " + strconv.Itoa(appPid) + " exits",
		"process " + strconv.Itoa(appPid) + " exited",
		"installed the update in " + p.target,
		"opened " + p.target,
		"done",
	} {
		if !strings.Contains(log, step) {
			t.Errorf("the log lacks %q:\n%s", step, log)
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(log), "\n") {
		if _, err := time.Parse("2006-01-02 15:04:05", line[:min(len(line), 19)]); err != nil {
			t.Errorf("a line of the log has no time: %q", line)
		}
	}
}

// An app that quits without relaunching gets the update, and is not
// opened again.
func TestUpdateSwapWithoutRelaunch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("only macOS installs updates with a script")
	}
	_, opens := fakeSwap(t, false)
	p := stagedApp(t)
	app := fakeApp(t)
	swap, err := startSwap(p, false, []string{"--ignored"})
	if err != nil {
		t.Fatal(err)
	}
	exitApp(app)
	if err := swap.Wait(); err != nil {
		t.Fatal(err)
	}
	if got := mainOf(p.target); got != "v2" {
		t.Errorf("after the app exited main = %q", got)
	}
	if got := opens(); len(got) != 0 {
		t.Errorf("opened the app with %q", got)
	}
}

// When open cannot open the app, which has exited, the swap tries again
// with -n, and logs it.
func TestUpdateSwapOpensAgain(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("only macOS installs updates with a script")
	}
	logPath, opens := fakeSwap(t, true)
	p := stagedApp(t)
	app := fakeApp(t)
	swap, err := startSwap(p, true, []string{"--x"})
	if err != nil {
		t.Fatal(err)
	}
	exitApp(app)
	if err := swap.Wait(); err != nil {
		t.Fatal(err)
	}
	want := [][]string{openArgs(p.target, []string{"--x"}), append([]string{"-n"}, openArgs(p.target, []string{"--x"})...)}
	if got := opens(); !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("opened the app with %q, want %q", got, want)
	}
	if log := readLog(logPath); !strings.Contains(log, "open failed with status 1: trying again with -n") || !strings.Contains(log, "opened "+p.target) {
		t.Errorf("the log:\n%s", log)
	}
}

// A swap whose app does not exit gives up after its wait, logs it, and
// leaves the update and the app as they are, for the next Install to
// remove.
func TestUpdateSwapGivesUp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("only macOS installs updates with a script")
	}
	logPath, opens := fakeSwap(t, false)
	swapWaitSteps = 0
	p := stagedApp(t)
	fakeApp(t)
	swap, err := startSwap(p, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := swap.Wait(); err == nil {
		t.Error("the swap succeeded while the app runs")
	}
	if got := mainOf(p.target); got != "v1" {
		t.Errorf("while the app runs main = %q", got)
	}
	if got := mainOf(p.bundle); got != "v2" {
		t.Errorf("the update's main = %q", got)
	}
	if got := opens(); len(got) != 0 {
		t.Errorf("opened the app with %q", got)
	}
	if log := readLog(logPath); !strings.Contains(log, "giving up, the update stays in "+p.work) {
		t.Errorf("the log:\n%s", log)
	}
}

// Once the app finishes, the update it staged is the one installed: an
// Install that completes meanwhile is discarded, and does not remove the
// update that the swap process moves into place. The swap opens the app,
// and the app does not.
func TestUpdaterRestageWhileQuitting(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("only macOS stages updates")
	}
	ctx := context.Background()
	srv, key, _, _ := updateServer(t, "1.2.0", "v2", "")
	up, err := checkUpdate(ctx, feedSource(srv.URL+"/update.json"), key, "1.1.9", false)
	if err != nil {
		t.Fatal(err)
	}
	target, main := installedApp(t)
	app := fakeApp(t)
	logPath, opens := fakeSwap(t, false)
	if err := installUpdate(ctx, httpGet, up.manifest, key, up.current, target, nil); err != nil {
		t.Fatal(err)
	}
	staged := pendingUpdate
	if !installStagedUpdate(true, []string{"--updated"}) {
		t.Fatal("no process installs the update")
	}
	if err := installUpdate(ctx, httpGet, up.manifest, key, up.current, target, nil); err == nil || !strings.Contains(err.Error(), "quitting") {
		t.Errorf("an update staged while quitting: %v", err)
	}
	if pendingUpdate != staged {
		t.Errorf("staged %+v, want %+v", pendingUpdate, staged)
	}
	if leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(target), ".*")); !slices.Equal(leftovers, []string{staged.work}) {
		t.Errorf("next to the app: %q, want the staged update's work alone", leftovers)
	}
	exitApp(app)
	wait.Until(t, "the update to be installed", func() bool { return strings.Contains(readLog(logPath), "done") })
	if b, _ := os.ReadFile(main); string(b) != "v2" {
		t.Errorf("after the app exited main = %q", b)
	}
	if got, want := opens(), [][]string{openArgs(target, []string{"--updated"})}; !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("opened the app with %q, want %q", got, want)
	}
	if leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(target), ".*")); len(leftovers) > 0 {
		t.Errorf("left behind: %q", leftovers)
	}
}

// An Install on macOS first removes the work that earlier updates left
// next to the app, as a crash does, but not the staged update's, nor that
// of an Install under way, nor anything else.
func TestUpdaterRemovesStaleWork(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("only macOS leaves staged updates")
	}
	fakeSwap(t, false)
	target, _ := installedApp(t)
	dir := filepath.Dir(target)
	stale := []string{".My App.app.update-123", ".My App.app.update-4567"}
	kept := []string{".My App.app.update-", ".My App.app.update-12x", ".Other.app.update-123", ".mygo-update-123", "My App.app.update-123"}
	for _, name := range append(slices.Clone(stale), kept...) {
		if err := os.MkdirAll(filepath.Join(dir, name, "app"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	file := ".My App.app.update-999"
	if err := os.WriteFile(filepath.Join(dir, file), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	kept = append(kept, file)
	pending, err := os.MkdirTemp(dir, workPattern(target))
	if err != nil {
		t.Fatal(err)
	}
	pendingUpdate = &stagedUpdate{work: pending, target: target}
	under, doneUnder, err := makeWork(target)
	if err != nil {
		t.Fatal(err)
	}
	defer doneUnder()
	work, done, err := makeWork(target)
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	for _, name := range stale {
		if _, err := os.Lstat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s stays: %v", name, err)
		}
	}
	for _, p := range append(kept, filepath.Base(pending), filepath.Base(under), filepath.Base(work), filepath.Base(target)) {
		if _, err := os.Lstat(filepath.Join(dir, p)); err != nil {
			t.Errorf("%s was removed: %v", p, err)
		}
	}
}

func TestUpdaterWaitsForOpenFiles(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("only Windows keeps open files from being renamed")
	}
	ctx := context.Background()
	srv, key, _, _ := updateServer(t, "1.2.0", "v2", "")
	up, err := checkUpdate(ctx, feedSource(srv.URL+"/update.json"), key, "1.1.9", false)
	if err != nil {
		t.Fatal(err)
	}
	target, main := installedApp(t)
	// Open main as antivirus software does to scan it: Go opens files
	// without sharing their deletion, which renaming needs.
	f, err := os.Open(main)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	time.AfterFunc(500*time.Millisecond, func() { f.Close() })
	if err := installUpdate(ctx, httpGet, up.manifest, key, up.current, target, nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(main); string(b) != "v2" {
		t.Errorf("after the update main = %q", b)
	}
}

func TestUpdaterPrereleases(t *testing.T) {
	t.Cleanup(func() { Updater.SetPrereleases(false) })
	if Updater.Prereleases() {
		t.Error("prereleases are on at launch")
	}
	// Check may run on another goroutine.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 100 {
			Updater.Prereleases()
		}
	}()
	Updater.SetPrereleases(true)
	<-done
	if !Updater.Prereleases() {
		t.Error("SetPrereleases(true) did not turn them on")
	}
	Updater.SetPrereleases(false)
	if Updater.Prereleases() {
		t.Error("SetPrereleases(false) did not turn them off")
	}
}

// testSource serves the update of updateServer as an update service of the
// app's own could: its manifest names the files by ids that only Open
// knows, and Open reads them from srv with a client of its own.
type testSource struct {
	srv *httptest.Server
	m   update.Manifest
	// pre holds the prereleases argument of each call of Manifest, and
	// opened the URL of each call of Open.
	pre    []bool
	opened []string
}

func (s *testSource) Manifest(ctx context.Context, pre bool) (io.ReadCloser, error) {
	s.pre = append(s.pre, pre)
	b, err := json.Marshal(s.m)
	return io.NopCloser(bytes.NewReader(b)), err
}

func (s *testSource) Open(ctx context.Context, url string) (io.ReadCloser, error) {
	s.opened = append(s.opened, url)
	name, ok := strings.CutPrefix(url, "service:")
	if !ok {
		return nil, fmt.Errorf("%s is not a file of the service", url)
	}
	resp, err := s.srv.Client().Get(s.srv.URL + "/" + name)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func TestUpdaterSource(t *testing.T) {
	ctx := context.Background()
	srv, key, m, requests := updateServer(t, "1.2.0", "v2", "")
	// httpGet refuses this URL: only the source can download it.
	m.URL = "service:app.tar.gz"
	src := &testSource{srv: srv, m: m}
	up, err := checkUpdate(ctx, src, key, "1.1.9", true)
	if err != nil || up == nil || up.Version != "1.2.0" || up.Notes != "- Faster" {
		t.Fatalf("checkUpdate = %+v, %v", up, err)
	}
	if len(src.pre) != 1 || !src.pre[0] || requests("/update.json") != 0 {
		t.Errorf("Manifest got prereleases %v, and the feed was read %d times", src.pre, requests("/update.json"))
	}

	target, main := installedApp(t)
	quit := runningApp(t)
	if err := installUpdate(ctx, up.source.Open, up.manifest, key, up.current, target, nil); err != nil {
		t.Fatal(err)
	}
	quit()
	if b, _ := os.ReadFile(main); string(b) != "v2" {
		t.Errorf("after the update main = %q", b)
	}
	if len(src.opened) != 1 || src.opened[0] != "service:app.tar.gz" || requests("/app.tar.gz") != 1 {
		t.Errorf("opened %q, and the archive was downloaded %d times", src.opened, requests("/app.tar.gz"))
	}

	// A source's updates are checked as the feed's: Check refuses one that
	// is not signed, and Install one that is signed badly, which changes
	// nothing.
	src.m.Signature = ""
	if up, err := checkUpdate(ctx, src, key, "1.1.9", false); err == nil {
		t.Errorf("an unsigned update was offered: %+v", up)
	}
	src.m.Signature = base64.StdEncoding.EncodeToString(make([]byte, 64))
	up, err = checkUpdate(ctx, src, key, "1.1.9", false)
	if err != nil || up == nil {
		t.Fatalf("checkUpdate = %+v, %v", up, err)
	}
	target, main = installedApp(t)
	if err := installUpdate(ctx, up.source.Open, up.manifest, key, up.current, target, nil); err == nil || !strings.Contains(err.Error(), "signed") {
		t.Errorf("bad signature: %v", err)
	}
	if b, _ := os.ReadFile(main); string(b) != "v1" {
		t.Errorf("a refused update changed main to %q", b)
	}

	// SetSource sets the source that Check reads, and nil the feed again.
	t.Cleanup(func() { Updater.SetSource(nil) })
	Updater.SetSource(src)
	if s := updateSource.Load(); s == nil || *s != UpdateSource(src) {
		t.Error("SetSource did not set the source")
	}
	Updater.SetSource(nil)
	if updateSource.Load() != nil {
		t.Error("SetSource(nil) did not restore the feed")
	}
}

// A gateway failing for a moment is asked again, and an error that stays
// is returned.
func TestHTTPGetRetries(t *testing.T) {
	defer func(r []time.Duration) { httpRetries = r }(httpRetries)
	httpRetries = []time.Duration{time.Millisecond, time.Millisecond}
	var tries atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/missing":
			tries.Add(1)
			http.NotFound(w, r)
		case r.URL.Path == "/down":
			tries.Add(1)
			w.WriteHeader(http.StatusServiceUnavailable)
		case tries.Add(1) < 3:
			w.WriteHeader(http.StatusGatewayTimeout)
		default:
			io.WriteString(w, "ok")
		}
	}))
	defer srv.Close()
	body, err := httpGet(t.Context(), srv.URL+"/file")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(body)
	body.Close()
	if string(b) != "ok" || tries.Load() != 3 {
		t.Errorf("read %q in %d tries", b, tries.Load())
	}
	tries.Store(0)
	if _, err := httpGet(t.Context(), srv.URL+"/down"); err == nil || !strings.Contains(err.Error(), "503") || tries.Load() != 3 {
		t.Errorf("down: %v in %d tries", err, tries.Load())
	}
	tries.Store(0)
	if _, err := httpGet(t.Context(), srv.URL+"/missing"); err == nil || tries.Load() != 1 {
		t.Errorf("missing: %v in %d tries", err, tries.Load())
	}
	if got := retryAfter(" 5 "); got != 5*time.Second {
		t.Errorf("Retry-After 5: %v", got)
	}
}

// A server that accepts the connection and never answers is given up on,
// rather than holding a check open until the socket times out.
func TestHTTPGetHeaderTimeout(t *testing.T) {
	transport := httpClient().Transport.(*http.Transport)
	defer func(r []time.Duration, d time.Duration) {
		httpRetries, transport.ResponseHeaderTimeout = r, d
	}(httpRetries, transport.ResponseHeaderTimeout)
	httpRetries = []time.Duration{time.Millisecond}
	transport.ResponseHeaderTimeout = 50 * time.Millisecond

	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		<-block
	}))
	defer srv.Close()
	defer close(block)

	start := time.Now()
	if _, err := httpGet(t.Context(), srv.URL+"/file"); err == nil {
		t.Fatal("a server that never answers returned no error")
	}
	if waited := time.Since(start); waited > 5*time.Second {
		t.Fatalf("waited %v for the headers of a response", waited)
	}
}

// A body that stops arriving is given up on, rather than blocking the read
// of a download until the socket times out.
func TestHTTPGetStallGivesUp(t *testing.T) {
	defer func(d time.Duration) { httpStallTimeout = d }(httpStallTimeout)
	httpStallTimeout = 100 * time.Millisecond

	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		io.WriteString(w, "start")
		w.(http.Flusher).Flush()
		<-block
	}))
	defer srv.Close()
	defer close(block)

	body, err := httpGet(t.Context(), srv.URL+"/file")
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	if _, err := io.ReadAll(body); err == nil || !strings.Contains(err.Error(), "stalled") {
		t.Fatalf("reading a body that stopped ended with %v", err)
	}
}

// Even an immediately firing callback sees a fully initialized body.
func TestStalledBodyShortTimeout(t *testing.T) {
	defer func(d time.Duration) { httpStallTimeout = d }(httpStallTimeout)
	httpStallTimeout = time.Nanosecond
	for range 100 {
		ctx, cancel := context.WithCancel(t.Context())
		body := newStalledBody(io.NopCloser(strings.NewReader("")), cancel)
		wait.For(t, "the stalled body to cancel its request", ctx.Done())
		body.Close()
		if !body.stalled.Load() {
			t.Fatal("the body did not report a stall")
		}
	}
}

// A body that keeps arriving is not given up, however slowly it sends.
func TestHTTPGetKeepsASlowBody(t *testing.T) {
	defer func(d time.Duration) { httpStallTimeout = d }(httpStallTimeout)
	// Each byte comes well within the stall timeout, even on a loaded
	// runner whose sleeps overrun, and all of them take longer than it.
	httpStallTimeout = 400 * time.Millisecond

	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for i := 0; i < 10; i++ {
			io.WriteString(w, "x")
			w.(http.Flusher).Flush()
			time.Sleep(50 * time.Millisecond)
		}
		<-block
	}))
	defer srv.Close()
	defer close(block)

	body, err := httpGet(t.Context(), srv.URL+"/file")
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	b, err := io.ReadAll(io.LimitReader(body, 10))
	if err != nil || len(b) != 10 {
		t.Fatalf("read %d bytes of a slow body: %v", len(b), err)
	}
}

// TestWorkPattern checks that an app at the root of a drive or share, as
// D:\, unpacks its update next to it (#1529).
func TestWorkPattern(t *testing.T) {
	roots := []string{string(filepath.Separator), "."}
	if runtime.GOOS == "windows" {
		roots = append(roots, `D:\`, `\\server\share\`)
	}
	for _, target := range roots {
		pattern := workPattern(target)
		if strings.ContainsFunc(pattern, func(r rune) bool { return r < 0x80 && os.IsPathSeparator(uint8(r)) }) {
			t.Errorf("workPattern(%q) = %q, which holds a separator", target, pattern)
		}
	}
	if got, want := workPattern(filepath.Join(t.TempDir(), "MyApp")), ".MyApp.update-"; got != want {
		t.Errorf("workPattern of an app in a folder = %q, want %q", got, want)
	}
	// The pattern makes a directory next to a root.
	dir, err := os.MkdirTemp(t.TempDir(), workPattern(string(filepath.Separator)))
	if err != nil {
		t.Fatal(err)
	}
	os.Remove(dir)
}

// TestRelaunchArguments: Relaunch's arguments replace those the app
// started with, and reach the new instance however it is started: by
// itself, through Launch Services, after a staged update on macOS, and
// under mygo dev.
func TestRelaunchArguments(t *testing.T) {
	args := []string{"--reason", "it's an update", "a b", `$HOME "quoted" \ back`, "line\nbreak", ""}
	onMain(func() {
		if got := relaunchArguments(); !slices.Equal(got, os.Args[1:]) {
			t.Errorf("without arguments: %q, want the app's %q", got, os.Args[1:])
		}
		App.relaunchArgs = args
		defer func() { App.relaunchArgs = nil }()
		if got := relaunchArguments(); !slices.Equal(got, args) {
			t.Errorf("with arguments: %q", got)
		}
	})

	if got := openArgs("/A.app", nil); !slices.Equal(got, []string{"/A.app"}) {
		t.Errorf("open without arguments: %q", got)
	}
	if got := openArgs("/A.app", args); !slices.Equal(got, append([]string{"/A.app", "--args"}, args...)) {
		t.Errorf("open with arguments: %q", got)
	}

	dev := strings.Split(string(devRelaunchData(args)), "\x00")
	if !slices.Equal(dev[:len(dev)-1], args) {
		t.Errorf("for mygo dev: %q", dev)
	}

	if runtime.GOOS == "windows" {
		return
	}
	// The process that installs a staged update opens the app with the
	// arguments of open that Relaunch passed it, as arguments of the
	// script.
	_, opens := fakeSwap(t, false)
	p := stagedApp(t)
	exited := exec.Command("/bin/sh", "-c", "exit 0")
	if err := exited.Run(); err != nil {
		t.Fatal(err)
	}
	old := appPid
	appPid = exited.Process.Pid
	defer func() { appPid = old }()
	swap, err := startSwap(p, true, args)
	if err != nil {
		t.Fatal(err)
	}
	if err := swap.Wait(); err != nil {
		t.Fatal(err)
	}
	if got, want := opens(), [][]string{openArgs(p.target, args)}; !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("the update opened the app with %q, want %q", got, want)
	}
}
