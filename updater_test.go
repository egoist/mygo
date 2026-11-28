//go:build !js

package mygo

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
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
	if leftovers := leftoversBeside(target); runtime.GOOS == "darwin" && len(leftovers) > 0 {
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
		if leftovers := leftoversBeside(target); runtime.GOOS == "darwin" && len(leftovers) > 0 {
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

// leftoversBeside returns the hidden entries beside target that an update
// left, but for the previous version it keeps.
func leftoversBeside(target string) []string {
	all, _ := filepath.Glob(filepath.Join(filepath.Dir(target), ".*"))
	return slices.DeleteFunc(all, func(p string) bool { return p == previousDir(target) })
}

// An update keeps the version it replaced until the new one has started
// well: a start after a first start that did not prove itself puts it
// back and starts it, and one that proved itself deletes it.
func TestUpdaterRollback(t *testing.T) {
	ctx := context.Background()
	srv, key, _, _ := updateServer(t, "1.2.0", "v2", "")
	up, err := checkUpdate(ctx, feedSource(srv.URL+"/update.json"), key, "1.1.9", false)
	if err != nil || up == nil {
		t.Fatalf("checkUpdate = %+v, %v", up, err)
	}
	var started []string
	old := startRestored
	startRestored = func(target string) bool { started = append(started, target); return true }
	t.Cleanup(func() { startRestored = old })
	rolledBack := filepath.Join(t.TempDir(), "mygo-rolled-back")
	oldFile := rolledBackFile
	rolledBackFile = func() (string, error) { return rolledBack, nil }
	t.Cleanup(func() { rolledBackFile = oldFile })
	install := func() (target, main string) {
		t.Helper()
		target, main = installedApp(t)
		quit := runningApp(t)
		if err := installUpdate(ctx, httpGet, up.manifest, key, up.current, target, nil); err != nil {
			t.Fatal(err)
		}
		quit()
		if b, _ := os.ReadFile(main); string(b) != "v2" {
			t.Fatalf("after the update main = %q", b)
		}
		return target, main
	}

	// A first start that dies: the next start puts 1.1.9 back.
	target, main := install()
	prev := previousDir(target)
	if v, from, started, err := readPreviousMark(prev); v != "1.2.0" || started || err != nil || runtime.GOOS != "darwin" && from != "1.1.9" {
		t.Fatalf("the mark is %q from %q, %v, %v", v, from, started, err)
	}
	if checkUpdatedStart(target, "1.1.9") {
		t.Error("another version judged the update")
	}
	if checkUpdatedStart(target, "1.2.0") {
		t.Fatal("the first start was rolled back")
	}
	if v, from, started, _ := readPreviousMark(prev); !started || v != "1.2.0" || runtime.GOOS != "darwin" && from != "1.1.9" {
		t.Errorf("the mark of the first start is %q from %q, started %v", v, from, started)
	}
	// While the first start runs, another start goes on.
	if checkUpdatedStart(target, "1.2.0") || len(started) != 0 {
		t.Fatal("a start while the first one ran was rolled back")
	}
	firstStart.Lock()
	firstStart.lock.Close() // the first start ends without proving itself
	firstStart.lock = nil
	firstStart.Unlock()
	if !checkUpdatedStart(target, "1.2.0") || len(started) != 1 || started[0] != target {
		t.Fatalf("a start after a first one that died started %q", started)
	}
	// 1.2.0 is offered no more, only a newer version, and the app is told
	// once.
	if !skipsUpdate("1.2.0") || !skipsUpdate("1.1.9") || skipsUpdate("1.2.1") {
		t.Error("Check skips other versions than those up to the one put back")
	}
	if v := Updater.RolledBack(); v != "1.2.0" {
		t.Errorf("RolledBack = %q", v)
	}
	if v := Updater.RolledBack(); v != "" {
		t.Errorf("RolledBack told again: %q", v)
	}
	if !skipsUpdate("1.2.0") {
		t.Error("telling the app forgot the version")
	}
	if b, _ := os.ReadFile(main); string(b) != "v1" {
		t.Errorf("after the rollback main = %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(target, "code")); !bytes.Equal(b, oldCode) {
		t.Error("after the rollback code is not the old one")
	}
	if _, err := os.Stat(prev); err == nil {
		t.Error("the previous version is still kept")
	}
	if left := leftoversBeside(target); len(left) > 0 {
		t.Errorf("left behind: %q", left)
	}
	if runtime.GOOS != "darwin" {
		if b, _ := os.ReadFile(filepath.Join(target, update.FileList)); len(b) > 0 {
			t.Errorf("the list of the files of 1.2.0 stayed: %q", b)
		}
	}

	// A first start that proved itself: the previous version goes.
	target, main = install()
	prev = previousDir(target)
	if checkUpdatedStart(target, "1.2.0") {
		t.Fatal("the first start was rolled back")
	}
	Updater.ConfirmStart() // as its first window shows
	if _, err := os.Stat(prev); err == nil {
		t.Error("the previous version is kept after the update proved itself")
	}
	Updater.ConfirmStart() // and as it quits: nothing left to do
	confirmUpdate(target, "1.2.0")
	if _, err := os.Stat(prev); err == nil {
		t.Error("the previous version is kept after the update proved itself")
	}
	deletingPrevious.Wait()
	if aside, _ := filepath.Glob(prev + ".old-*"); len(aside) > 0 {
		t.Errorf("the previous version moved aside stayed: %q", aside)
	}
	// What an app that quit before it finished deleting left goes at the
	// next start.
	aside := prev + ".old-1"
	if err := os.MkdirAll(filepath.Join(aside, "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	removeConfirmedLeftovers(target)
	if _, err := os.Stat(aside); err == nil {
		t.Error("removeConfirmedLeftovers left a previous version moved aside")
	}
	if checkUpdatedStart(target, "1.2.0") || len(started) != 1 {
		t.Error("a start after one that proved itself was rolled back")
	}
	if b, _ := os.ReadFile(main); string(b) != "v2" {
		t.Errorf("main = %q", b)
	}
}

// The first start of an update proves itself when a window of the app
// shows: not as it is made hidden.
func TestUpdaterProvesOnShow(t *testing.T) {
	ctx := context.Background()
	srv, key, _, _ := updateServer(t, "1.2.0", "v2", "")
	up, err := checkUpdate(ctx, feedSource(srv.URL+"/update.json"), key, "1.1.9", false)
	if err != nil || up == nil {
		t.Fatalf("checkUpdate = %+v, %v", up, err)
	}
	target, _ := installedApp(t)
	quit := runningApp(t)
	if err := installUpdate(ctx, httpGet, up.manifest, key, up.current, target, nil); err != nil {
		t.Fatal(err)
	}
	quit()
	prev := previousDir(target)
	if checkUpdatedStart(target, "1.2.0") {
		t.Fatal("the first start was rolled back")
	}
	t.Cleanup(func() { confirmUpdate(target, "1.2.0"); deletingPrevious.Wait() })
	t.Cleanup(watchFirstStart())

	w, _ := testWindow(t, WindowOptions{Hidden: true})
	onMain(func() {})
	if _, err := os.Stat(prev); err != nil {
		t.Fatalf("a hidden window proved the start: %v", err)
	}
	w.Show()
	onMain(func() {})
	deletingPrevious.Wait()
	if _, err := os.Stat(prev); err == nil {
		t.Error("the previous version is kept after the first window showed")
	}
	if aside, _ := filepath.Glob(prev + ".old-*"); len(aside) > 0 {
		t.Errorf("the previous version moved aside stayed: %q", aside)
	}
}

// runningApp stands in for the app that installUpdate updates on macOS,
// where the update waits for the app to exit: quit ends it and returns once
// the update is in place.
func runningApp(t *testing.T) (quit func()) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		return func() {}
	}
	app := exec.Command("/bin/sleep", "60")
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	old := appPid
	appPid = app.Process.Pid
	t.Cleanup(func() { appPid = old })
	return func() {
		t.Helper()
		app.Process.Kill()
		app.Wait()
		p := pendingUpdate.Swap(nil)
		if p == nil {
			t.Fatal("no update staged")
		}
		if err := p.helper.Wait(); err != nil {
			t.Fatal(err)
		}
	}
}

// On macOS the bundle of the running app stays in place until the app has
// exited: moving it away took the app's Dock tile with it, and the
// relaunched version got a second tile.
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
	time.Sleep(300 * time.Millisecond)
	if b, _ := os.ReadFile(main); string(b) != "v1" {
		t.Errorf("while the app runs main = %q", b)
	}
	quit()
	if b, _ := os.ReadFile(main); string(b) != "v2" {
		t.Errorf("after the app exited main = %q", b)
	}
	if leftovers := leftoversBeside(target); len(leftovers) > 0 {
		t.Errorf("left behind: %q", leftovers)
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
	// The script that installs a staged update opens the app with the
	// arguments of open that the relaunch file sets, as Relaunch wrote
	// them once the update was staged. It prints them here.
	dir := t.TempDir()
	target, bundle, work := filepath.Join(dir, "A.app"), filepath.Join(dir, "new", "A.app"), filepath.Join(dir, "work")
	for _, d := range []string{target, bundle, work} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(work, "relaunch"), []byte(setArgs(openArgs(target, args))), 0o644); err != nil {
		t.Fatal(err)
	}
	exited := exec.Command("/bin/sh", "-c", "exit 0")
	if err := exited.Run(); err != nil {
		t.Fatal(err)
	}
	script := strings.Replace(installAfterExit, "exec /usr/bin/open", `printf '%s\0'`, 1)
	out, err := exec.Command("/bin/sh", "-c", script, "mygo-update", strconv.Itoa(exited.Process.Pid), target, bundle, work).Output()
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(string(out), "\x00")
	if want := openArgs(target, args); !slices.Equal(got[:len(got)-1], want) {
		t.Errorf("the update opened the app with %q, want %q", got, want)
	}
	if _, err := os.Stat(work); !os.IsNotExist(err) {
		t.Error("the update left its work")
	}
}

// An update's request follows redirects only to HTTPS, or over the loopback,
// and not past ten.
func TestUpdateCheckRedirect(t *testing.T) {
	req := func(u string) *http.Request {
		r, err := http.NewRequest("GET", u, nil)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	via := []*http.Request{req("https://example.com/feed.json")}
	if err := checkRedirect(req("https://cdn.example.com/feed.json"), via); err != nil {
		t.Errorf("to HTTPS: %v", err)
	}
	if err := checkRedirect(req("http://cdn.example.com/feed.json"), via); err == nil {
		t.Error("to plain HTTP: followed")
	}
	if err := checkRedirect(req("http://127.0.0.1:8080/feed.json"), via); err != nil {
		t.Errorf("over the loopback: %v", err)
	}
	many := make([]*http.Request, 10)
	for i := range many {
		many[i] = via[0]
	}
	if err := checkRedirect(req("https://cdn.example.com/feed.json"), many); err == nil {
		t.Error("an eleventh redirect: followed")
	}

	// httpGet gives up on a refused redirect at once: asked again, the
	// server would only redirect again.
	var asked atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		http.Redirect(w, r, "http://updates.example.invalid/feed.json", http.StatusFound)
	}))
	defer srv.Close()
	if _, err := httpGet(context.Background(), srv.URL+"/feed.json"); !errors.Is(err, errRedirect) {
		t.Errorf("httpGet = %v", err)
	}
	if n := asked.Load(); n != 1 {
		t.Errorf("asked %d times", n)
	}
}

func saveUpdateBuild() (feed, key, exe string) {
	return packageUpdateFeed, packageUpdateKey, startExe
}

func restoreUpdateBuild(feed, key, exe string) {
	packageUpdateFeed, packageUpdateKey, startExe = feed, key, exe
}

func TestCleanUpdateLeftoversKeepsLookAlikesOfABuildWithoutUpdates(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("darwin's updater leaves no entries of this shape to clean")
	}
	dir := t.TempDir()
	foreign := filepath.Join(dir, ".notes.old-backup")
	if err := os.WriteFile(foreign, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(dir, ".folder.old-1234", "nested")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "sentinel.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	feed, key, exe := saveUpdateBuild()
	defer restoreUpdateBuild(feed, key, exe)
	packageUpdateFeed, packageUpdateKey, startExe = "", "", filepath.Join(dir, "app")
	cleanUpdateLeftovers()
	if _, err := os.Stat(foreign); err != nil {
		t.Fatalf("the unrelated file beside the executable was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".folder.old-1234", "nested", "sentinel.txt")); err != nil {
		t.Fatalf("the unrelated folder beside the executable was removed: %v", err)
	}
}

func TestCleanUpdateLeftoversRemovesTheLeftoversOfABuildWithUpdates(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("darwin's updater leaves no entries of this shape to clean")
	}
	dir := t.TempDir()
	write := func(name string) string {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	gone := []string{
		write(".notes.old-1234"),
		filepath.Dir(write(filepath.Join(".lib.old-99", "nested", "sentinel.txt"))),
	}
	kept := []string{
		write(".notes.old-backup"),
		write(".notes.old-" + strconv.Itoa(os.Getpid())), // this process's own, mid-update
		write("notes.old-1234"),
		write(".notes.old-"),
	}
	feed, key, exe := saveUpdateBuild()
	defer restoreUpdateBuild(feed, key, exe)
	packageUpdateFeed, packageUpdateKey, startExe = "https://updates.example/feed", "key", filepath.Join(dir, "app")
	cleanUpdateLeftovers()
	for _, p := range gone {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("the update leftover %s stayed: %v", filepath.Base(p), err)
		}
	}
	for _, p := range kept {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s was removed: %v", filepath.Base(p), err)
		}
	}
}

func TestIsUpdateLeftover(t *testing.T) {
	for name, want := range map[string]bool{
		".app.exe.old-1234":  true,
		".lib.so.old-7":      true,
		".app.exe.old-42":    false, // the running process's
		".notes.old-backup":  false,
		".notes.old-12a":     false,
		".notes.old-":        false,
		".notes.old":         false,
		"notes.old-1234":     false,
		"..old-1234":         false,
		".old-1234":          false,
		".x.old-12345678901": false,
	} {
		if got := isUpdateLeftover(name, 42); got != want {
			t.Errorf("isUpdateLeftover(%q) = %v, want %v", name, got, want)
		}
	}
}
