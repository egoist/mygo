//go:build linux && (amd64 || arm64)

package linux

import (
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

// libayatana draws a status icon through the desktop shell: it registers the
// icon with the shell's StatusNotifierWatcher and the shell renders it. The
// library also keeps a GtkStatusIcon fallback for a session without one, and
// that fallback is what makes a shell restart dangerous: while the watcher has
// no owner, app_indicator_set_tooltip_full reaches the fallback icon, whose
// GTK_IS_STATUS_ICON assertion fails, and the dereference behind it killed the
// process with a SIGSEGV inside cgo. Go cannot catch that, and the app's own
// handle stays valid throughout, so a guard on it cannot help either.
//
// So the handle is not the thing to check: the backend stops calling into
// libayatana while the watcher is away, keeps what the app set, and applies it
// when the watcher returns. Every app gets that for free, and an app that
// knows nothing about the watcher no longer dies.

const statusNotifierWatcher = "org.kde.StatusNotifierWatcher"

var watcher struct {
	sync.Mutex
	// present is whether the watcher had an owner at the last look; the
	// signal handler keeps it current.
	present bool
	// known is whether it was ever established, so the first lookup asks
	// the bus and every later one trusts the signal.
	known bool
	// subscribed is whether the NameOwnerChanged handler is installed.
	subscribed bool
	// handler is the callback the subscription was made with, kept so it
	// outlives the call.
	handler ptr
}

// deferred reports whether a tray call has to wait for the shell. The state
// the app set is stored by the caller either way, so a resumed tray redraws
// from what it kept rather than from what the app repeated.
//
// It waits only once a watcher was seen and then lost. A session that never
// had one, or a test without a bus, keeps calling libayatana exactly as
// before: this change must not alter behavior it cannot improve.
func (t *tray) deferred() bool {
	if !watcherGone() {
		return false
	}
	t.pending = true
	return true
}

// watcherGone reports whether a shell owned the watcher name and then left.
// Until the first owner is seen the answer is no, so a session without a
// watcher, or a test without a bus, behaves as it did before this file.
func watcherGone() bool {
	watcher.Lock()
	present, known, subscribed := watcher.present, watcher.known, watcher.subscribed
	watcher.Unlock()
	if subscribed && known {
		return !present
	}
	present, ok := watcherHasOwner()
	watcher.Lock()
	if ok {
		watcher.present = present
		watcher.known = true
	}
	gone := watcher.known && !watcher.present
	watcher.Unlock()
	if !subscribed {
		watchWatcher()
	}
	return gone
}

// watcherHasOwner asks the bus whether the watcher name has an owner. A bus
// that cannot answer keeps the tray working as it did before this file
// existed, rather than deferring every call forever.
func watcherHasOwner() (present, ok bool) {
	// Without GLib and libayatana there is nothing to defer for, and the
	// bindings are nil: a unit test reaches this with neither.
	if appIndicatorNew == nil || gVariantNewString == nil {
		return false, false
	}
	reply, err := dbusCall("org.freedesktop.DBus", "/org/freedesktop/DBus",
		"org.freedesktop.DBus", "NameHasOwner", tuple(gVariantNewString(cs(statusNotifierWatcher))), "(b)")
	if err != nil || reply == 0 {
		return false, false
	}
	defer gVariantUnref(reply)
	child := gVariantGetChildValue(reply, 0)
	if child == 0 {
		return false, false
	}
	defer gVariantUnref(child)
	return gVariantGetBoolean(child), true
}

// watchWatcher installs the NameOwnerChanged handler, which keeps the cached
// answer current and resubscribes the icons when a shell takes the name back.
func watchWatcher() {
	watcher.Lock()
	if watcher.subscribed {
		watcher.Unlock()
		return
	}
	watcher.subscribed = true
	handler := watcher.handler
	watcher.Unlock()
	if handler == 0 {
		// The callback is made at startup with the others; nothing to do
		// here when GTK was never reached.
		return
	}
	conn, err := bus()
	if err != nil {
		return
	}
	gDBusConnectionSignalSubscribe(conn, cs("org.freedesktop.DBus"), cs("org.freedesktop.DBus"),
		cs("NameOwnerChanged"), cs("/org/freedesktop/DBus"), nil, 0, handler, 0, 0)
}

// watcherSignal handles NameOwnerChanged. The parameters carry the name, the
// old owner and the new one; an empty new owner means the name was released.
func watcherSignal(params ptr) {
	if gVariantNChildren(params) < 3 {
		return
	}
	name := variantString(params, 0)
	if name != statusNotifierWatcher {
		return
	}
	came := variantString(params, 2) != ""
	watcher.Lock()
	was, known := watcher.present, watcher.known
	watcher.present, watcher.known = came, true
	watcher.Unlock()
	if came && known && !was {
		resumeTrays()
	}
}

// variantString reads the string at index i of a parameters tuple.
func variantString(params ptr, i int) string {
	child := gVariantGetChildValue(params, uintptr(i))
	if child == 0 {
		return ""
	}
	defer gVariantUnref(child)
	var length uintptr
	return goStr(gVariantGetString(child, &length))
}

// resumeTrays applies everything the trays kept while the shell was away.
func resumeTrays() {
	b := theBackend
	if b == nil {
		return
	}
	for _, t := range b.trays {
		t.resume()
	}
}

// resume draws the tray from the state it cached. The icon starts over on a
// fresh shell, so all of it is applied, not only what changed while away.
func (t *tray) resume() {
	if !t.pending {
		return
	}
	t.pending = false
	if t.ind == 0 {
		return
	}
	if len(t.image) > 0 {
		if file, err := t.writeIcon(); err == nil {
			appIndicatorSetIconFull(t.ind, cs(file), cs(""))
		}
	}
	if t.title != "" {
		appIndicatorSetLabel(t.ind, cs(t.title), cs(t.title))
	}
	if t.tooltip != "" {
		t.applyToolTip(t.tooltip)
	}
	t.applyMenu(t.wanted)
}

// writeIcon writes the cached image under a new name, which makes the shell
// reload the icon, and returns its path.
func (t *tray) writeIcon() (string, error) {
	img := t.image
	if t.template && t.dark {
		whitened, err := whiten(img)
		if err != nil {
			return "", err
		}
		img = whitened
	}
	if t.iconDir == "" {
		dir, err := os.MkdirTemp("", "mygo-tray-")
		if err != nil {
			return "", err
		}
		t.iconDir = dir
	}
	t.icons++
	file := filepath.Join(t.iconDir, iconFileName(t.icons))
	if err := os.WriteFile(file, img, 0o644); err != nil {
		return "", err
	}
	return file, nil
}

// iconFileName is the name of the nth icon in a tray's icon directory.
func iconFileName(n int) string { return "icon-" + strconv.Itoa(n) + ".png" }
