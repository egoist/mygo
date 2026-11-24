//go:build !js

package mygo

import (
	"errors"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/egoist/mygo/internal/update"
)

// An update keeps the version it replaced until the new one has started
// well (keepPrevious): a new version that dies before it does, as one that
// crashes at start, is put back by the previous one at its next start
// (checkUpdatedStart). The previous version waits in previousDir with the
// file update, which names the new version, and once the new version has
// started, the word started; that start holds the lock of the file
// started.lock while it runs. A start of that version that finds the word
// and can take the lock follows one that ended without proving itself, and
// restores the previous version; while the first start runs, as when the
// app is opened twice, the lock is held and the start goes on.
// A start proves itself once it shows its first window, quits normally
// before, or calls Updater.ConfirmStart, as an app without windows does
// (confirmUpdate); the previous version is deleted then. Only the new
// version judges: a mark of another version is left alone. A version put
// back so is remembered (rememberRolledBack): Check offers it no more, only
// a newer one, and Updater.RolledBack tells the app once.
//
// The judging is reached through updatedStart, which updater_start.go sets
// in the builds of apps with updates alone (the build tag mygo_updates of
// `mygo build`), so that the start of other apps does not carry it.

// previousDir is where an update keeps the version of the app at target it
// replaced: a hidden directory in the app's on Linux and Windows, which no
// list of the app's files names, and beside the bundle on macOS.
func previousDir(target string) string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(filepath.Dir(target), "."+filepath.Base(target)+".previous")
	}
	return filepath.Join(target, ".mygo-previous")
}

// markPrevious marks the version kept in prev as the one that version
// replaced.
func markPrevious(prev, version string) {
	if err := os.WriteFile(filepath.Join(prev, "update"), []byte(version+"\n"), 0o644); err != nil {
		log.Printf("mygo: not keeping the previous version: %v", err)
		_ = os.RemoveAll(prev)
	}
}

// readPreviousMark reads the mark of prev: the version that replaced the
// one kept there, and whether it has started.
func readPreviousMark(prev string) (version string, started bool, err error) {
	b, err := os.ReadFile(filepath.Join(prev, "update"))
	if err != nil {
		return "", false, err
	}
	lines := strings.Fields(string(b))
	if len(lines) == 0 {
		return "", false, errors.New("an empty mark")
	}
	return lines[0], len(lines) > 1 && lines[1] == "started", nil
}

// checkUpdatedStart judges the start of the app of version at target after
// an update: at its first start it marks it started and has it prove
// itself (confirmUpdate); at a start after one that did not, it restores
// the previous version and reports true, having started it.
func checkUpdatedStart(target, version string) bool {
	prev := previousDir(target)
	v, started, err := readPreviousMark(prev)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			log.Printf("mygo: forgetting the previous version: %v", err)
			_ = os.RemoveAll(prev)
		}
		return false
	}
	if v != version {
		return false
	}
	lock, err := openSingleInstanceLock(filepath.Join(prev, "started.lock"))
	if err != nil {
		log.Printf("mygo: cannot judge the start of the update: %v", err)
		return false
	}
	if lock == nil {
		return false // its first start still runs
	}
	if !started {
		if err := os.WriteFile(filepath.Join(prev, "update"), []byte(version+"\nstarted\n"), 0o644); err != nil {
			lock.Close()
			log.Printf("mygo: cannot mark the first start of the update: %v", err)
			return false
		}
		firstStart.Lock()
		firstStart.lock, firstStart.target, firstStart.version = lock, target, version
		firstStart.Unlock()
		return false
	}
	lock.Close()
	log.Printf("mygo: version %s did not start well after its update: putting the previous version back", version)
	if err := restorePrevious(prev, target); err != nil {
		log.Printf("mygo: cannot put the previous version back: %v", err)
		_ = os.RemoveAll(prev)
		return false
	}
	rememberRolledBack(version)
	return startRestored(target)
}

// firstStart holds the lock of started.lock while the first start of an
// update runs, with the app's target and version, which proveStart
// confirms.
var firstStart struct {
	sync.Mutex
	lock            *os.File
	target, version string
}

// proveStart confirms the first start of an update, when this start is
// one.
func proveStart() {
	firstStart.Lock()
	target, version := firstStart.target, firstStart.version
	firstStart.Unlock()
	if target != "" {
		confirmUpdate(target, version)
	}
}

// confirmUpdate deletes the version that the update of version replaced at
// target: version has proved itself.
func confirmUpdate(target, version string) {
	prev := previousDir(target)
	firstStart.Lock()
	defer firstStart.Unlock()
	if v, _, err := readPreviousMark(prev); err == nil && v == version {
		if firstStart.lock != nil {
			// Windows deletes no file that is open.
			firstStart.lock.Close()
			firstStart.lock = nil
		}
		_ = os.RemoveAll(prev)
	}
	if firstStart.target == target {
		firstStart.target, firstStart.version = "", ""
	}
}

// rolledBackFile is the file in the app's user data that names the version
// put back last (rememberRolledBack), and, once Updater.RolledBack told
// it, the word told.
var rolledBackFile = func() (string, error) {
	dir, err := App.Path(PathUserData)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "mygo-rolled-back"), nil
}

// rememberRolledBack remembers that version was put back, so that Check
// skips it.
func rememberRolledBack(version string) {
	path, err := rolledBackFile()
	if err == nil {
		if err = os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
			err = os.WriteFile(path, []byte(version+"\n"), 0o644)
		}
	}
	if err != nil {
		log.Printf("mygo: cannot remember that %s was put back, so it may be offered again: %v", version, err)
	}
}

// readRolledBack reads rolledBackFile: the version put back last, and
// whether the app was told.
func readRolledBack() (version string, told bool) {
	path, err := rolledBackFile()
	if err != nil {
		return "", false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	f := strings.Fields(string(b))
	if len(f) == 0 {
		return "", false
	}
	return f[0], len(f) > 1 && f[1] == "told"
}

// rolledBackOnce returns the version put back last, the first time it is
// asked, and "" after.
func rolledBackOnce() string {
	v, told := readRolledBack()
	if v == "" || told {
		return ""
	}
	if path, err := rolledBackFile(); err == nil {
		_ = os.WriteFile(path, []byte(v+"\ntold\n"), 0o644)
	}
	return v
}

// skipsUpdate reports whether Check skips version: it is not newer than a
// version that was put back.
func skipsUpdate(version string) bool {
	v, _ := readRolledBack()
	return v != "" && update.Compare(version, v) <= 0
}

// restorePrevious puts the version kept in prev back at target: on macOS
// its bundle, elsewhere its files, after the files of the version that
// replaced it, which its update.FileList names, have moved aside, to be
// removed now, or at the next launch where Windows keeps them
// (cleanUpdateLeftovers).
func restorePrevious(prev, target string) error {
	if runtime.GOOS == "darwin" {
		failed := prev + ".failed"
		_ = os.RemoveAll(failed)
		if err := os.Rename(target, failed); err != nil {
			return err
		}
		if err := os.Rename(filepath.Join(prev, filepath.Base(target)), target); err != nil {
			_ = os.Rename(failed, target)
			return err
		}
		_ = os.RemoveAll(prev)
		_ = os.RemoveAll(failed)
		return nil
	}
	entries, err := os.ReadDir(prev)
	if err != nil {
		return err
	}
	entries = slices.DeleteFunc(entries, func(e fs.DirEntry) bool { return e.Name() == "update" || e.Name() == "started.lock" })
	suffix := ".old-" + strconv.Itoa(os.Getpid())
	var aside, back [][2]string
	undo := func() {
		for i := len(back) - 1; i >= 0; i-- {
			_ = rename(back[i][1], back[i][0])
		}
		for i := len(aside) - 1; i >= 0; i-- {
			_ = rename(aside[i][1], aside[i][0])
		}
	}
	for _, name := range append(installedEntries(target), update.FileList) {
		p := filepath.Join(target, name)
		if _, err := os.Lstat(p); err != nil {
			continue
		}
		a := filepath.Join(target, "."+name+suffix)
		if err := rename(p, a); err != nil {
			undo()
			return err
		}
		aside = append(aside, [2]string{p, a})
	}
	for _, e := range entries {
		src, dst := filepath.Join(prev, e.Name()), filepath.Join(target, e.Name())
		if _, err := os.Lstat(dst); err == nil {
			a := filepath.Join(target, "."+e.Name()+suffix)
			if err := rename(dst, a); err != nil {
				undo()
				return err
			}
			aside = append(aside, [2]string{dst, a})
		}
		if err := rename(src, dst); err != nil {
			undo()
			return err
		}
		back = append(back, [2]string{src, dst})
	}
	_ = os.RemoveAll(prev)
	for _, a := range aside {
		_ = os.RemoveAll(a[1])
	}
	return nil
}

// startRestored starts the version restored at target with the arguments
// of this start, reporting whether it did: through Launch Services on
// macOS, else the executable this start ran from, which the restored
// version has as the update kept it.
var startRestored = func(target string) bool {
	args := os.Args[1:]
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("/usr/bin/open", append([]string{"-n"}, openArgs(target, args)...)...)
	} else {
		cmd = exec.Command(startExe, args...)
		cmd.Dir = startDir
	}
	if err := cmd.Start(); err != nil {
		log.Printf("mygo: cannot start the previous version: %v", err)
		return false
	}
	_ = cmd.Process.Release()
	return true
}

// judgeUpdatedStart runs checkUpdatedStart for an app that updates itself,
// as early in its start as it can, exiting when it started the previous
// version instead. A first start proves itself when its first window
// shows, or it quits normally.
func judgeUpdatedStart() {
	if packageUpdateFeed == "" || IsDev() {
		return
	}
	target, err := installTarget()
	if err != nil {
		return
	}
	if checkUpdatedStart(target, App.Version()) {
		os.Exit(0)
	}
	firstStart.Lock()
	first := firstStart.target != ""
	firstStart.Unlock()
	if first {
		App.OnWindowCreated(func(w *Window) { w.OnShow(proveStart) })
		App.OnQuit(proveStart)
	}
}
