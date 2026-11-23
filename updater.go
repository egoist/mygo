//go:build !js

package mygo

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/internal/update"
	"github.com/egoist/mygo/netproxy"
)

// Set by `mygo build` with -ldflags -X when mygo.config.ts configures updates:
// the manifest of this build's target, or with a tag prefix on GitHub its
// feed in the newest tagged release (update.TaggedFeed), and the public key
// updates must be signed with.
var (
	packageUpdateFeed string
	packageUpdateKey  string
)

// startExe is the executable the app started from. On Linux, os.Executable
// reports the old file once an update replaced it.
var startExe, _ = os.Executable()

// UpdaterModule installs new versions of the app, which `mygo build`
// publishes when mygo.config.ts configures updates: signed archives and a
// manifest per platform. Use the Updater singleton:
//
//	update, err := mygo.Updater.Check(ctx)
//	if err == nil && update != nil {
//		if err := update.Install(ctx, nil); err == nil {
//			mygo.App.Relaunch()
//		}
//	}
type UpdaterModule struct{}

// Updater installs new versions of the app.
var Updater = &UpdaterModule{}

// prereleases is set while Check offers prereleases (SetPrereleases).
var prereleases atomic.Bool

// updateSource is the source of SetSource, nil for the feed of
// mygo.config.ts.
var updateSource atomic.Pointer[UpdateSource]

// UpdateSource is where Updater gets updates from instead of the feed that
// mygo.config.ts configures (SetSource): an update service the app already
// has, an endpoint that needs the user's credentials, or a channel the app
// chooses while it runs. It serves what `mygo build` publishes, from
// wherever the app keeps it, and Updater checks it as it checks the feed's:
// the archive and deltas must be signed with the key of updates.publicKey
// that the app was built with, or they are not installed.
type UpdateSource interface {
	// Manifest returns the manifest of the running platform: the JSON that
	// `mygo build` writes as update-<target>.json, such as
	// update-darwin-arm64.json, of at most 1 MiB.
	// prereleases reports whether the app offers them (SetPrereleases).
	Manifest(ctx context.Context, prereleases bool) (io.ReadCloser, error)
	// Open returns the file at url, which the manifest names: the archive
	// of the update or one of its deltas. The URL is the manifest's, so it
	// may be anything the source understands, such as an id of its own.
	Open(ctx context.Context, url string) (io.ReadCloser, error)
}

// SetSource makes Check read the manifest from s, and Install download the
// files it names through s, instead of from the feed of mygo.config.ts; nil
// restores the feed. Updates are still enabled only in builds with updates
// in mygo.config.ts (Enabled), which give the key that updates are checked
// with. An Update that Check returned keeps downloading from the source it
// came from. It may be called from any goroutine.
func (u *UpdaterModule) SetSource(s UpdateSource) {
	if s == nil {
		updateSource.Store(nil)
		return
	}
	updateSource.Store(&s)
}

// ErrUpdatesDisabled is returned by Check for builds without updates:
// development builds, and builds of apps without updates in mygo.config.ts.
var ErrUpdatesDisabled = errors.New("mygo: updates are not enabled for this build")

// Update is a newer version of the app, found by Updater.Check.
type Update struct {
	// Version of the update, and its release notes in Markdown.
	Version string
	Notes   string
	// Date is when the update was built.
	Date time.Time

	manifest update.Manifest
	key      string
	// source is where the manifest came from, which serves its files.
	source UpdateSource
	// current is the version that Check compared with, which delta updates
	// update.
	current string
}

// Enabled reports whether the app can update itself: it was built with
// updates and can write where it is installed. Apps installed by a package
// manager, such as a .deb in /opt, are updated by it instead.
func (u *UpdaterModule) Enabled() bool {
	if packageUpdateFeed == "" || packageUpdateKey == "" || IsDev() {
		return false
	}
	target, err := installTarget()
	if err != nil {
		return false
	}
	probe, err := os.MkdirTemp(filepath.Dir(target), ".mygo-update-")
	if err != nil {
		return false
	}
	os.Remove(probe)
	return true
}

// Check asks the update feed, or the source of SetSource, for a version
// newer than the running one and returns it, or nil when the app is up to
// date.
func (u *UpdaterModule) Check(ctx context.Context) (*Update, error) {
	if !u.Enabled() {
		return nil, ErrUpdatesDisabled
	}
	var src UpdateSource = feedSource(packageUpdateFeed)
	if s := updateSource.Load(); s != nil {
		src = *s
	}
	return checkUpdate(ctx, src, packageUpdateKey, App.Version(), prereleases.Load())
}

// SetPrereleases makes Check offer prereleases too, such as 1.3.0-beta.1,
// for the users of an app who chose its beta: with updates on GitHub,
// Check then reads the release of the highest version that is not a
// draft, through the GitHub API (usually one request a check), so that a
// release newer than the prereleases is offered too. Check never offers a
// version older than the running one, so turning it off waits for the
// release after the prerelease the app runs. Updates of updates.url are
// the manifest the server has, whichever is set. It is off at launch; the
// app keeps the user's choice and sets it again. It may be called from
// any goroutine.
func (u *UpdaterModule) SetPrereleases(on bool) { prereleases.Store(on) }

// Prereleases reports whether Check offers prereleases (SetPrereleases).
func (u *UpdaterModule) Prereleases() bool { return prereleases.Load() }

// feedSource is the update feed that mygo.config.ts configures, the URL of
// a manifest or a feed that update.ResolveFeed resolves.
type feedSource string

func (f feedSource) Manifest(ctx context.Context, pre bool) (io.ReadCloser, error) {
	resolve := update.ResolveFeed
	if pre {
		resolve = update.ResolvePrereleaseFeed
	}
	feed, err := resolve(ctx, string(f), httpGet)
	if err != nil {
		return nil, err
	}
	return httpGet(ctx, feed)
}

func (f feedSource) Open(ctx context.Context, url string) (io.ReadCloser, error) {
	return httpGet(ctx, url)
}

func checkUpdate(ctx context.Context, src UpdateSource, key, current string, pre bool) (*Update, error) {
	body, err := src.Manifest(ctx, pre)
	if err != nil {
		return nil, fmt.Errorf("mygo: checking for updates: %w", err)
	}
	defer body.Close()
	var m update.Manifest
	if err := json.UnmarshalRead(io.LimitReader(body, update.MaxManifestSize), &m); err != nil {
		return nil, fmt.Errorf("mygo: checking for updates: %w", err)
	}
	if m.Version == "" || m.URL == "" || m.Signature == "" {
		return nil, errors.New("mygo: checking for updates: incomplete manifest")
	}
	if update.Compare(m.Version, current) <= 0 {
		return nil, nil
	}
	up := &Update{Version: m.Version, Notes: m.Notes, manifest: m, key: key, current: current, source: src}
	up.Date, _ = time.Parse(time.RFC3339, m.Date)
	return up, nil
}

// Install downloads the update, checks that it is signed with the app's
// key and replaces the app with it; progress, when not nil, is called with
// the bytes downloaded so far. When the update has a delta for the running
// version, only the delta is downloaded, unless it fails to make the new
// version: the whole update is downloaded then, and the progress starts
// over. The running app is not affected: the new version runs after
// App.Relaunch, or at the next launch. On macOS the update is installed
// as the app quits, through App.Quit, App.Relaunch, SIGINT or SIGTERM, and
// each step is logged to update.log in the app's logs directory
// (App.Path(PathLogs)). A crash or App.Exit leaves it next to the app,
// for the next Install to remove. A program that calls Install before
// App.Run, or never calls Run, gets the update installed before Install
// returns.
//
// The app must be able to write where it is installed: a bundle in
// /Applications of an administrator, or an app directory the user owns on
// Linux and Windows. Apps installed by a package manager are updated by it.
func (up *Update) Install(ctx context.Context, progress func(downloaded, total int64)) error {
	target, err := installTarget()
	if err != nil {
		return err
	}
	get := httpGet
	if up.source != nil {
		get = up.source.Open
	}
	if err := installUpdate(ctx, get, up.manifest, up.key, up.current, target, progress); err != nil {
		return fmt.Errorf("mygo: installing the update: %w", err)
	}
	if runtime.GOOS == "linux" {
		// install.sh registered the desktop entry of the version it installed.
		if err := update.RefreshDesktopEntry(target, filepath.Base(startExe)); err != nil {
			log.Printf("mygo: registering the desktop entry of the update: %v", err)
		}
	}
	return nil
}

// installTarget returns what an update replaces: the app bundle on macOS,
// the directory of the executable elsewhere.
func installTarget() (string, error) {
	exe := startExe
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	if runtime.GOOS == "darwin" {
		bundle := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
		if !strings.HasSuffix(bundle, ".app") {
			return "", errors.New("mygo: only app bundles can update themselves")
		}
		return bundle, nil
	}
	if os.Getenv("APPIMAGE") != "" {
		return "", errors.New("mygo: AppImages cannot update themselves")
	}
	return filepath.Dir(exe), nil
}

// installUpdate installs the version of m in place of target, the app of
// version current: from its delta for current when there is one, else, or
// when the delta fails, from its archive, both downloaded with get.
func installUpdate(ctx context.Context, get update.Getter, m update.Manifest, key, current, target string, progress func(downloaded, total int64)) error {
	pub, err := update.ParsePublicKey(key)
	if err != nil {
		return err
	}
	work, done, err := makeWork(target)
	if err != nil {
		return fmt.Errorf("cannot write next to the app: %w", err)
	}
	keep := false
	defer func() {
		if !keep {
			os.RemoveAll(work)
		}
		done()
	}()
	unpacked := filepath.Join(work, "app")
	swap := swapFiles
	if runtime.GOOS == "darwin" {
		swap = func(dir, target string) error {
			err := stageBundle(work, dir, target)
			// Staged, or a swap that failed left the old version there.
			_, oldErr := os.Lstat(filepath.Join(work, "old"))
			keep = err == nil || oldErr == nil
			return err
		}
	}

	if d := m.Delta(current); d != nil {
		err := applyDelta(ctx, get, *d, pub, m.Version, target, work, unpacked, progress)
		if err == nil {
			return swap(unpacked, target)
		}
		if ctx.Err() != nil {
			return err
		}
		log.Printf("mygo: downloading the whole update, as its delta update failed: %v", err)
		if err := os.RemoveAll(unpacked); err != nil {
			return err
		}
	}

	if m.Size <= 0 || m.Size > update.MaxArchiveSize {
		return fmt.Errorf("invalid size %d", m.Size)
	}
	archive := filepath.Join(work, "update.tar.gz")
	if err := download(ctx, get, m.URL, m.Size, m.Signature, pub, archive, progress); err != nil {
		return err
	}
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	err = update.ExtractArchive(f, unpacked)
	f.Close()
	if err != nil {
		return err
	}
	return swap(unpacked, target)
}

// workPattern returns the pattern of the directory an update of the app
// at target is unpacked in, next to it: named after the app, or after no
// name where target is the root of a drive or share, as D:\ (#1529),
// whose base name is a separator, which os.MkdirTemp rejects.
func workPattern(target string) string {
	name := filepath.Base(target)
	if name == "." || strings.ContainsFunc(name, func(r rune) bool { return r < 0x80 && os.IsPathSeparator(uint8(r)) }) {
		name = "app"
	}
	return "." + name + ".update-"
}

// applyDelta downloads the delta d into work and makes the new version of
// the app at target from it, in dir: its bundle on macOS, in dir too, as
// the bundle of an archive is.
func applyDelta(ctx context.Context, get update.Getter, d update.Delta, pub ed25519.PublicKey, version, target, work, dir string, progress func(downloaded, total int64)) error {
	if d.Size <= 0 || d.Size > update.MaxArchiveSize {
		return fmt.Errorf("invalid size %d", d.Size)
	}
	path := filepath.Join(work, "update.delta")
	if err := download(ctx, get, d.URL, d.Size, d.Signature, pub, path, progress); err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if runtime.GOOS == "darwin" {
		if err := os.Mkdir(dir, 0o755); err != nil {
			return err
		}
		dir = filepath.Join(dir, filepath.Base(target))
	}
	return update.ApplyDelta(f, d.Size, d.From, version, target, dir)
}

// download fetches url with get into the file path and checks its size and
// signature.
func download(ctx context.Context, get update.Getter, url string, size int64, signature string, pub ed25519.PublicKey, path string, progress func(downloaded, total int64)) error {
	body, err := get(ctx, url)
	if err != nil {
		return err
	}
	defer body.Close()
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	sum := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, sum), &progressReader{r: io.LimitReader(body, size+1), total: size, fn: progress})
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("downloading: %w", err)
	}
	if n != size {
		return fmt.Errorf("downloaded %d bytes, want %d", n, size)
	}
	return update.Verify(pub, sum.Sum(nil), signature)
}

// stagedUpdate is a new version of the app bundle target, at bundle in
// work, a directory next to target that the update was made in.
type stagedUpdate struct {
	work, bundle, target string
}

// The state of the updates that Install stages on macOS, which stageMu
// guards: the staged update, the work directories of the Installs under
// way, whether the app began installing the staged update as it
// finishes, and whether it started the process that installs it.
var (
	stageMu sync.Mutex
	// pendingUpdate is the update that Install staged on macOS. It replaces
	// the app's bundle once the app has exited, however it quits
	// (installStagedUpdate): replacing it while the app runs moved the
	// bundle of the running app away, the Dock's tile of the app followed
	// it, and the new version, opened where the app was, got a second tile.
	pendingUpdate *stagedUpdate
	// updateWork holds the work directories of the Installs under way,
	// which removeStaleWork leaves alone.
	updateWork = map[string]bool{}
	// updateQuitting is set once the app finishes and installStagedUpdate
	// took pendingUpdate: an update staged later would never be installed,
	// and replacing the pending one would remove the bundle that the swap
	// process is about to move into place.
	updateQuitting bool
	// swapStarted is closed once the call of installStagedUpdate that took
	// pendingUpdate started the swap process, which swapRunning reports,
	// or failed to.
	swapStarted = make(chan struct{})
	swapRunning bool
)

// swapNowMu keeps the swaps that Install runs itself, without App.Run,
// one at a time (swapNow).
var swapNowMu sync.Mutex

// appPid is the process whose exit a staged update waits for: the app's,
// which tests replace.
var appPid = os.Getpid()

// makeWork makes the directory that an update of the app at target is
// made in, next to it so that it can be renamed into place, and records
// it as under way until done is called. On macOS it first removes the
// work directories that earlier updates left (removeStaleWork).
func makeWork(target string) (work string, done func(), err error) {
	if runtime.GOOS == "darwin" {
		removeStaleWork(target)
	}
	stageMu.Lock()
	defer stageMu.Unlock()
	work, err = os.MkdirTemp(filepath.Dir(target), workPattern(target))
	if err != nil {
		return "", nil, err
	}
	updateWork[work] = true
	return work, func() {
		stageMu.Lock()
		delete(updateWork, work)
		stageMu.Unlock()
	}, nil
}

// staleWorkAge is how long the work directory of an update next to the
// app must have stayed unchanged before an Install takes it for stale.
// Another instance of the app, which this process knows nothing of, may
// be downloading into it, or its swap process may be waiting for it to
// exit: that one gives up after five minutes (swapWaitSteps), and
// startSwap touches the work as it starts it.
const staleWorkAge = 10 * time.Minute

// removeStaleWork removes the work directories of updates next to the app
// at target that are neither the staged update's nor an Install's under
// way, and that nothing changed for staleWorkAge (workChanged): an app
// that crashed, or quit without finishing, leaves the update it staged,
// and so does a swap process that gave up waiting for the app to exit.
// Only directories named as os.MkdirTemp names those of workPattern are
// removed.
func removeStaleWork(target string) {
	dir, prefix := filepath.Dir(target), workPattern(target)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var stale []string
	stageMu.Lock()
	for _, e := range entries {
		rest, ok := strings.CutPrefix(e.Name(), prefix)
		if !ok || rest == "" || strings.Trim(rest, "0123456789") != "" || !e.IsDir() {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if updateWork[p] || pendingUpdate != nil && pendingUpdate.work == p {
			continue
		}
		stale = append(stale, p)
	}
	stageMu.Unlock()
	stale = slices.DeleteFunc(stale, func(p string) bool {
		return time.Since(workChanged(p)) < staleWorkAge
	})
	// Outside the lock, which the app takes as it quits: a directory that
	// is stale stays stale, as only os.MkdirTemp names new ones.
	for _, p := range stale {
		if err := os.RemoveAll(p); err != nil {
			log.Printf("mygo: removing what an earlier update left: %v", err)
		}
	}
}

// workChanged returns when the work directory p or one of its entries
// last changed: a download writes into a file of p, and unpacking adds
// app to it.
func workChanged(p string) time.Time {
	var last time.Time
	if fi, err := os.Lstat(p); err == nil {
		last = fi.ModTime()
	}
	entries, _ := os.ReadDir(p)
	for _, e := range entries {
		if fi, err := e.Info(); err == nil && fi.ModTime().After(last) {
			last = fi.ModTime()
		}
	}
	return last
}

// stageBundle leaves the bundle that dir holds to replace target once the
// app has exited, in place of an update staged before, whose work it
// removes. Nothing runs until then: the app starts the process that
// installs it as it finishes (installStagedUpdate, which it reaches
// through stagedInstaller, set here). Once it looked for the update
// (stagingClosed), the update is refused, and Install removes it: the
// app may be about to take the update staged before, to open it after
// Relaunch, or have taken none and open the old version.
//
// Before App.Run started, no finish would start that process, and the
// program has no Dock tile to keep: stageBundle swaps the bundles itself,
// at once (swapNow).
func stageBundle(work, dir, target string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) != 1 || !strings.HasSuffix(entries[0].Name(), ".app") {
		return errors.New("the update holds no app bundle")
	}
	p := &stagedUpdate{work: work, bundle: filepath.Join(dir, entries[0].Name()), target: target}
	App.mu.Lock()
	running := App.running
	App.mu.Unlock()
	if !running {
		return swapNow(p)
	}
	// Before checking stagingClosed, which the app sets before it reads
	// stagedInstaller: either the app finds the installer, or the update
	// is refused.
	install := installStagedUpdate
	stagedInstaller.CompareAndSwap(nil, &install)
	stageMu.Lock()
	if updateQuitting || stagingClosed.Load() {
		stageMu.Unlock()
		return errors.New("the app is quitting")
	}
	old := pendingUpdate
	pendingUpdate = p
	stageMu.Unlock()
	if old != nil {
		_ = os.RemoveAll(old.work)
	}
	return nil
}

// swapNow installs p at once, with the script that installs a staged
// update as the app quits, in its mode that waits for nothing and opens
// nothing, and returns once it is done, with its steps in the update log.
// The script exits before Install returns, so the program's files it
// inherits stay with the program alone.
func swapNow(p *stagedUpdate) error {
	swapNowMu.Lock()
	defer swapNowMu.Unlock()
	cmd, closeLog := swapCommand(p, "now", nil)
	defer closeLog()
	if err := cmd.Run(); err != nil {
		path, _ := updateLogPath()
		return fmt.Errorf("the update was not installed (%v): see %s", err, path)
	}
	return nil
}

// takeStagedUpdate returns the update that Install staged, or nil, and
// keeps Install from staging another (updateQuitting). Only its first
// call returns it: a second signal can arrive as the app finishes, and
// both would start a process that installs it. Later calls report
// whether an earlier one took an update instead, with started, which
// that call closes once it started the process or failed to.
func takeStagedUpdate() (p *stagedUpdate, taken bool, started chan struct{}) {
	stageMu.Lock()
	defer stageMu.Unlock()
	if updateQuitting {
		return nil, pendingUpdate != nil, swapStarted
	}
	updateQuitting = true
	return pendingUpdate, false, swapStarted
}

// installStagedUpdate starts the process that installs the update that
// Install staged on macOS, once the app has exited, and that then opens
// the new version when relaunch is set, with the arguments args. It
// reports whether a process installs the update. Only its first call
// starts one: a later call waits for that one to start it, at most
// swapStartWait (2 s), and reports what it did; once that wait is over it
// takes the process as started and reports true, whether or not it ever
// starts. So a second signal does not exit before the process started,
// unless starting it takes longer than the wait, and Relaunch does not
// open the old version, which the process is about to move away. The app
// calls it as it finishes, however it quits, and on a second signal
// (stagedInstaller).
func installStagedUpdate(relaunch bool, args []string) bool {
	p, taken, started := takeStagedUpdate()
	if taken {
		if swapWaitHook != nil {
			swapWaitHook()
		}
		select {
		case <-started:
		case <-time.After(swapStartWait):
			return true
		}
		stageMu.Lock()
		defer stageMu.Unlock()
		return swapRunning
	}
	if p == nil {
		return false
	}
	ok := false
	if runtime.GOOS == "darwin" {
		if cmd, err := startSwap(p, relaunch, args); err != nil {
			log.Printf("mygo: cannot start installing the update in %s: %v", p.work, err)
		} else {
			_ = cmd.Process.Release()
			ok = true
		}
	}
	stageMu.Lock()
	swapRunning = ok
	close(started)
	stageMu.Unlock()
	return ok
}

// The swap process's settings, which tests replace: the shell that runs
// it, the open command it opens the app with, how many tenths of a second
// it waits for the app to exit (five minutes) before it gives up, the
// lsappinfo command it asks Launch Services with, how many tenths of a
// second it waits for Launch Services to forget the app, and then for the
// new version to run, the path of its log, which is called once the app
// finishes, and how long a second call of installStagedUpdate waits for
// the first to start it, after which it takes it as started.
var (
	swapShell     = "/bin/sh"
	swapOpen      = "/usr/bin/open"
	swapWaitSteps = 3000
	swapApps      = "/usr/bin/lsappinfo"
	swapAppSteps  = 100
	updateLogPath = func() (string, error) {
		dir, err := App.Path(PathLogs)
		return filepath.Join(dir, "update.log"), err
	}
	swapStartWait = 2 * time.Second
)

// swapWaitHook, when not nil, is called by a later call of
// installStagedUpdate as it starts waiting for the first to start the
// process: tests set it, to know that the call waits.
var swapWaitHook func()

// maxUpdateLog is the size of the update log above which it starts over.
const maxUpdateLog = 256 << 10

// startSwap starts the process that installs the staged update p once the
// app has exited (installAfterExit), with its output appended to the
// update log. It runs in a session of its own and ignores the signals
// that end the app's: whatever ends the app, the process must outlive it.
// It inherits no file of the app but its log (inheritNoFiles).
func startSwap(p *stagedUpdate, relaunch bool, args []string) (*exec.Cmd, error) {
	mode := "quit"
	if relaunch {
		mode = "relaunch"
	}
	// The work is in use again, until the process gives up: the Installs
	// of other instances leave it (removeStaleWork).
	now := time.Now()
	_ = os.Chtimes(p.work, now, now)
	inheritNoFiles()
	cmd, closeLog := swapCommand(p, mode, args)
	defer closeLog()
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

// swapCommand returns the command of the script that installs p in mode
// (installAfterExit), opening the app with the arguments args when mode
// is relaunch, with its output going to the update log, which closeLog
// closes once the command started.
func swapCommand(p *stagedUpdate, mode string, args []string) (cmd *exec.Cmd, closeLog func()) {
	cmd = exec.Command(swapShell, append([]string{"-c", installAfterExit, "mygo-update",
		strconv.Itoa(appPid), p.target, p.bundle, p.work, swapOpen, mode, strconv.Itoa(swapWaitSteps),
		swapApps, strconv.Itoa(swapAppSteps)},
		openArgs(p.target, args)...)...)
	cmd.SysProcAttr = detachedProcess()
	f, err := openUpdateLog()
	if err != nil {
		log.Printf("mygo: the update logs nothing: %v", err)
		return cmd, func() {}
	}
	cmd.Stdout, cmd.Stderr = f, f
	return cmd, func() { f.Close() }
}

// openUpdateLog opens the update log for appending, emptied first once
// it grew larger than maxUpdateLog.
func openUpdateLog() (*os.File, error) {
	path, err := updateLogPath()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if fi, err := os.Stat(path); err == nil && fi.Size() > maxUpdateLog {
		_ = os.Truncate(path, 0)
	}
	return os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
}

// installAfterExit is the script of /bin/sh that installs a staged update
// once the app has exited. Its arguments are the pid of the app; the
// target, bundle and work of the update; the open command; the mode:
// quit, relaunch to open the app afterwards, or now to install at once,
// without waiting for the program, which never ran App.Run; how many
// tenths of a second to wait for the app; the lsappinfo command; how many tenths of a second to wait for
// Launch Services; and the arguments of open, which are never part of the
// script. It runs the system's commands, whatever the app's PATH.
//
// It ignores the signals that end the app's processes, waits for the app
// to exit (a zombie has), but in mode now, and gives up after that wait, leaving the update
// for the next Install to remove. It then swaps the bundles at once: the
// end of the session, whose SIGKILL nothing ignores, must not find it
// between the two moves. It leaves the update when an instance of the app
// runs from the target, as one opened while the app quit, which waited for
// it to exit (RequestSingleInstanceLock): the update would remove its
// bundle under it. The app that exited, which Launch Services may still
// list, is no such instance. It leaves it too when the target is gone, or
// is there again once moved aside, as the new bundle would be moved into
// it then (checked again once moved, as a directory created in between
// takes the new bundle in, which it then takes back out); and when the new
// bundle cannot be moved in, after putting the old one back. It removes
// the work once the update is installed. When the app relaunches, it then
// waits for Launch Services to forget the app that exited, as open would
// otherwise only bring that forward, and opens the app through Launch
// Services, as openBundle does, but without -n, as the app has exited and
// the Dock gives the new version the app's tile; only if that fails it
// tries again with -n, which it uses at once when Launch Services still
// lists the app that exited. An open that succeeds is not repeated,
// however slowly the app starts: that would start two. An instance opened
// between the check and the swap still loses its bundle: that window is
// milliseconds. Every step goes to its output, the update log, with the
// time.
const installAfterExit = `PATH=/usr/bin:/bin:/usr/sbin:/sbin
export PATH
pid=$1 target=$2 new=$3 work=$4 open=$5 mode=$6 steps=$7 apps=$8 appsteps=$9
shift 9
trap '' HUP TERM INT
say() { printf '%s [%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$$" "$*"; }
alive() {
	kill -0 "$pid" 2>/dev/null || return 1
	case $(ps -o stat= -p "$pid" 2>/dev/null) in Z*) return 1 ;; esac
}
listed() { [ -n "$("$apps" find "pid=$pid" 2>/dev/null)" ]; }
# others: whether an app other than the one that exited runs from target.
others() { "$apps" find "bundlepath=$target" 2>/dev/null | grep -vxF "$gone" | grep -q .; }
launch() {
	"$open" "$@" || {
		say "open failed with status $?"
		return 1
	}
	i=0
	until others; do
		if [ "$i" -ge "$appsteps" ]; then
			say "open succeeded, but no app runs from $target yet"
			return 0
		fi
		i=$((i + 1))
		sleep 0.1
	done
	say "opened $target"
}
if [ "$mode" = now ]; then
	say "installing $new in $target now, as process $pid runs without App.Run"
else
	say "installing $new in $target once process $pid exits"
	i=0
	while alive; do
		if [ "$i" -ge "$steps" ]; then
			say "process $pid still runs after $((steps / 10)) s: giving up, the update stays in $work"
			exit 1
		fi
		i=$((i + 1))
		sleep 0.1
	done
	say "process $pid exited"
fi
# The entry of the app that exited, or of the program that installs now,
# while Launch Services lists it.
gone=$("$apps" find "pid=$pid" 2>/dev/null)
if others; then
	say "another instance runs from $target: the update stays in $work"
	exit 1
fi
if [ ! -e "$target" ]; then
	say "$target is gone: the update stays in $work"
	exit 1
fi
status=0
if ! mv "$target" "$work/old"; then
	say "cannot move $target aside: the update stays in $work"
	status=1
elif [ -e "$target" ]; then
	say "$target is there again: the update stays in $work, the old version in $work/old"
	exit 1
elif ! mv "$new" "$target"; then
	if mv "$work/old" "$target"; then
		say "cannot move the update into place: put the old version back, the update stays in $work"
		status=1
	else
		say "cannot put the old version back: it is in $work/old"
		exit 1
	fi
elif [ -e "$target/${new##*/}" ]; then
	# There again after the check: mv moved the update into it.
	if mv "$target/${new##*/}" "$new"; then
		say "$target is there again: the update stays in $work, the old version in $work/old"
	else
		say "$target is there again, and the update moved into it: the old version is in $work/old"
	fi
	exit 1
else
	say "installed the update in $target"
	rm -rf "$work"
fi
if [ "$mode" = relaunch ]; then
	i=0 again=
	while listed; do
		if [ "$i" -ge "$appsteps" ]; then
			say "Launch Services still lists process $pid"
			again=1
			break
		fi
		i=$((i + 1))
		sleep 0.1
	done
	[ -n "$again" ] || [ "$i" -eq 0 ] || say "Launch Services forgot process $pid"
	if [ -n "$again" ] || ! launch "$@"; then
		say "opening with -n"
		launch -n "$@" || say "cannot open $target"
	fi
fi
[ "$status" -eq 0 ] || exit 1
say done
`

// openArgs returns the arguments of /usr/bin/open that open the app bundle
// with the arguments args.
func openArgs(bundle string, args []string) []string {
	if len(args) == 0 {
		return []string{bundle}
	}
	return append([]string{bundle, "--args"}, args...)
}

// swapFiles replaces the entries of the app directory target with those in
// dir, and removes those that the version it replaces installed and dir
// lacks, which target's update.FileList names. Entries that no list names
// stay, as the data of a portable copy, and so do all the entries of an
// app installed before versions had a list: they cannot be told from the
// user's. Replaced and removed entries are renamed aside, and deleted once
// every entry of dir is in place; if one cannot be, dir's go back and the
// others are put back. The running executable is renamed away, which
// Windows allows, and removed at the next launch (cleanUpdateLeftovers).
// dir gets the list of its entries, which moves into target with them.
func swapFiles(dir, target string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var names []string
	ours := map[string]bool{}
	for _, e := range entries {
		if e.Name() != update.FileList {
			names = append(names, e.Name())
			ours[e.Name()] = true
		}
	}
	if len(names) == 0 {
		return errors.New("the update is empty")
	}
	suffix := ".old-" + strconv.Itoa(os.Getpid())
	var moved, placed [][2]string // renamed: old name, new name
	undo := func() {
		for i := len(placed) - 1; i >= 0; i-- {
			if rename(placed[i][1], placed[i][0]) != nil {
				_ = os.RemoveAll(placed[i][1])
			}
		}
		for i := len(moved) - 1; i >= 0; i-- {
			_ = rename(moved[i][1], moved[i][0])
		}
	}
	moveAside := func(dst string) error {
		aside := filepath.Join(target, "."+filepath.Base(dst)+suffix)
		if err := rename(dst, aside); err != nil {
			return err
		}
		moved = append(moved, [2]string{dst, aside})
		return nil
	}

	// The entries that the replaced version installed and dir lacks go
	// first, so that one that dir has under another case, which is the same
	// entry on Windows and macOS, is moved aside before dir's takes its
	// place. The executable the app runs from stays, whatever the new
	// version calls its own: shortcuts, the desktop entry and Relaunch start
	// it. One that cannot be moved stays listed, for the next update to
	// remove.
	running := runningEntry(target)
	var left []string
	for _, name := range installedEntries(target) {
		dst := filepath.Join(target, name)
		if ours[name] || name == running {
			continue
		}
		if _, err := os.Lstat(dst); err != nil {
			continue
		}
		if err := moveAside(dst); err != nil {
			log.Printf("mygo: leaving %s, which the update no longer has: %v", dst, err)
			left = append(left, name)
		}
	}
	if err := update.WriteFileList(dir, append(names, left...)); err != nil {
		undo()
		return err
	}

	for _, name := range append(names, update.FileList) {
		src, dst := filepath.Join(dir, name), filepath.Join(target, name)
		if _, err := os.Lstat(dst); err == nil {
			if err := moveAside(dst); err != nil {
				undo()
				return err
			}
		}
		if err := rename(src, dst); err != nil {
			undo()
			return err
		}
		placed = append(placed, [2]string{src, dst})
	}
	for _, m := range moved {
		_ = os.RemoveAll(m[1])
	}
	return nil
}

// installedEntries returns the entries of the app directory target that
// its version installed, which its update.FileList names: none without
// one.
func installedEntries(target string) []string {
	b, err := readAppFile(filepath.Join(target, update.FileList))
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			log.Printf("mygo: removing none of the files that the update no longer has: %v", err)
		}
		return nil
	}
	return update.ParseFileList(b)
}

// runningEntry returns the entry of the app directory target that the app
// runs from, or "" when it runs from elsewhere.
func runningEntry(target string) string {
	if startExe == "" {
		return ""
	}
	exe, dir := startExe, target
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	if filepath.Dir(exe) != filepath.Clean(dir) {
		return ""
	}
	return filepath.Base(exe)
}

// relaunchNow starts the app again; it runs after the quit sequence.
func relaunchNow() {
	args := relaunchArguments()
	if launchedByDev() {
		devRelaunch(App.relaunchArgs)
	}
	// With an update staged, the process that installs it opens the new
	// version: opening one here too would start two instances. finish
	// started it (installStaged reports what that call did), or a second
	// signal did, which exits without opening anything: opening the old
	// version then would run it from the bundle that the process moves
	// away and removes.
	if runtime.GOOS == "darwin" && (installStaged(true, args) || openBundle(args)) {
		return
	}
	// The executable the app started from, which an update may have
	// replaced.
	cmd := exec.Command(startExe, args...)
	cmd.Dir = startDir
	if runtime.GOOS != "windows" {
		// GUI executables on Windows have no standard streams to pass on.
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	}
	if cmd.Start() == nil {
		_ = cmd.Process.Release()
	}
}

// devRelaunch exits for `mygo dev` to start the build again, with args
// unless they are nil, which it reads from the file that
// MYGO_DEV_RELAUNCH names.
func devRelaunch(args []string) {
	if args != nil {
		if devRelaunchFile == "" {
			log.Print("mygo: this mygo dev cannot pass Relaunch's arguments to the build; update it")
		} else if err := os.WriteFile(devRelaunchFile, devRelaunchData(args), 0o600); err != nil {
			log.Printf("mygo: cannot pass Relaunch's arguments to mygo dev: %v", err)
		}
	}
	os.Exit(devRelaunchCode)
}

// devRelaunchData is what devRelaunch writes: each argument, ended with
// a NUL byte, which no argument holds.
func devRelaunchData(args []string) []byte {
	var b []byte
	for _, a := range args {
		b = append(append(b, a...), 0)
	}
	return b
}

// openBundle starts the app bundle again through Launch Services, as the
// Dock does, with args, reporting whether it did. A child of this process would stay
// in this instance's coalition, which has no app once this instance exits:
// when an app that the new instance started quits, as one a terminal's
// shell ran, loginwindow takes the coalition's other processes for that
// app's and ends them with SIGTERM.
// Tests replace it.
var openBundle = func(args []string) bool {
	bundle, err := installTarget()
	if err != nil {
		return false
	}
	// -n: this instance still runs, and would only be brought forward.
	return exec.Command("/usr/bin/open", append([]string{"-n"}, openArgs(bundle, args)...)...).Run() == nil
}

// cleanUpdateLeftovers removes what an update could not remove while the
// previous version ran (Windows keeps running executables).
func cleanUpdateLeftovers() {
	if runtime.GOOS == "darwin" || startExe == "" {
		return
	}
	olds, _ := filepath.Glob(filepath.Join(filepath.Dir(startExe), ".*.old-*"))
	for _, p := range olds {
		_ = os.RemoveAll(p)
	}
}

// httpGet fetches a URL over HTTPS (or plain HTTP from the loopback
// interface, for tests), through the system's proxy (package netproxy).
// A failure that a moment may mend, as a gateway timing out in front of
// the store of a release's assets, is tried again (httpRetries).
//
// The deadlines of the request are these: the headers must arrive within
// httpHeaderTimeout, and a body that sends nothing for httpStallTimeout is
// given up. Without them a connection that the network dropped without a
// reset holds the caller until the socket times out, which keepalive may
// never do, and a check in the background has no window to cancel it from
// and keeps the updater from scheduling the next one.
func httpGet(ctx context.Context, rawURL string) (io.ReadCloser, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && isLoopback(u.Hostname())) {
		return nil, fmt.Errorf("refusing to download %s: updates need HTTPS", rawURL)
	}
	client := httpClient()
	for try := 0; ; try++ {
		// Canceled when the body closes, or when nothing arrives: the one
		// cancels the request of a body that stopped, the other ends a
		// request whose response was refused.
		reqCtx, cancel := context.WithCancel(ctx)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawURL, nil)
		if err != nil {
			cancel()
			return nil, err
		}
		// Not the app's name: GitHub's release assets come from a store that
		// answers 400 to a User-Agent with bytes outside ASCII, as 云音乐, for
		// every file its CDN has not cached yet.
		req.Header.Set("User-Agent", "mygo-updater")
		resp, err := client.Do(req)
		var wait time.Duration
		switch {
		case err != nil:
			cancel()
			if ctx.Err() != nil || try >= len(httpRetries) {
				return nil, err
			}
		case resp.StatusCode == http.StatusOK:
			return newStalledBody(resp.Body, cancel), nil
		default:
			resp.Body.Close()
			cancel()
			if !transientStatus(resp.StatusCode) || try >= len(httpRetries) {
				return nil, fmt.Errorf("GET %s: %s", rawURL, resp.Status)
			}
			wait = retryAfter(resp.Header.Get("Retry-After"))
		}
		wait = max(wait, httpRetries[try])
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
}

// httpHeaderTimeout is how long the headers of a response may take to
// arrive: the client has no deadline above the socket. Five minutes of a
// check in the background would be five minutes without the next check.
var httpHeaderTimeout = 30 * time.Second

// httpStallTimeout is how long a body may send nothing before the download
// is given up. A slow download is fine, however long it takes; one that
// stopped is not, and its reads never return.
var httpStallTimeout = time.Minute

// httpClient is the client of the updater's requests: the transport of
// package netproxy, so that the system's proxy settings apply, with the
// deadline for the headers that it leaves to its callers. A test that needs
// another deadline changes the transport for its run.
var updateHTTPClient struct {
	once   sync.Once
	client *http.Client
}

func httpClient() *http.Client {
	updateHTTPClient.once.Do(func() { updateHTTPClient.client = newHTTPClient() })
	return updateHTTPClient.client
}

func newHTTPClient() *http.Client {
	transport := netproxy.Transport().Clone()
	transport.ResponseHeaderTimeout = httpHeaderTimeout
	return &http.Client{Transport: transport}
}

// stalledBody cancels the request of a body that stops arriving, and says
// so instead of the canceled request it would otherwise be read as.
type stalledBody struct {
	body   io.ReadCloser
	cancel context.CancelFunc
	// d is the timeout it was made with, which the watch reads in its own
	// goroutine: reading the setting there would race with a test's change.
	d time.Duration
	// last is when the last byte arrived, in Unix nanoseconds; stalled is
	// set once cancel was called, so that the error can say why; done ends
	// the watch when the body closes.
	last    atomic.Int64
	stalled atomic.Bool
	done    atomic.Bool
	timer   *time.Timer
}

func newStalledBody(body io.ReadCloser, cancel context.CancelFunc) *stalledBody {
	b := &stalledBody{body: body, cancel: cancel, d: httpStallTimeout}
	b.last.Store(time.Now().UnixNano())
	// Publish the timer before its callback can read it to rearm it.
	b.timer = time.AfterFunc(1<<63-1, b.check)
	b.timer.Stop()
	b.timer.Reset(b.d / 4)
	return b
}

// check cancels the request when nothing arrived for its timeout, and looks
// again a quarter of that later otherwise: often enough to stop a download
// soon after it stops, rarely enough to cost nothing while it runs.
func (b *stalledBody) check() {
	if b.done.Load() {
		return
	}
	if time.Since(time.Unix(0, b.last.Load())) >= b.d {
		b.stalled.Store(true)
		b.cancel()
		return
	}
	b.timer.Reset(b.d / 4)
}

func (b *stalledBody) Read(p []byte) (int, error) {
	n, err := b.body.Read(p)
	if n > 0 {
		b.last.Store(time.Now().UnixNano())
	}
	if err != nil && b.stalled.Load() {
		return n, errors.New("the download stalled: the connection sent nothing")
	}
	return n, err
}

func (b *stalledBody) Close() error {
	b.done.Store(true)
	b.timer.Stop()
	b.cancel()
	return b.body.Close()
}

// httpRetries are how long httpGet waits before each try after the first
// of a request that failed for a while: three tries in all.
var httpRetries = []time.Duration{time.Second, 3 * time.Second}

// transientStatus reports whether a response of status code may succeed
// when asked again: too many requests, or a server or gateway failing for
// now.
func transientStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// retryAfter is the wait of a Retry-After header in seconds, up to 30
// seconds; 0 for none, or for a date.
func retryAfter(h string) time.Duration {
	n, err := strconv.Atoi(strings.TrimSpace(h))
	if err != nil || n <= 0 {
		return 0
	}
	return min(time.Duration(n)*time.Second, 30*time.Second)
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// progressReader reports how much of a download was read.
type progressReader struct {
	r     io.Reader
	n     int64
	total int64
	fn    func(downloaded, total int64)
	last  time.Time
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.n += int64(n)
	if p.fn != nil && (err != nil || p.n == p.total || time.Since(p.last) > 100*time.Millisecond) {
		p.last = time.Now()
		p.fn(p.n, p.total)
	}
	return n, err
}
