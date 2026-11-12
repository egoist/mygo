// Package apptest runs an app's own tests on a fake desktop, so that the
// code wiring the app to the system is tested as it is: the functions
// that show dialogs and notifications, set the badge, open URLs and make
// windows, with no window or dialog on the screen.
//
// TestMain runs the tests inside the app, as main runs the app:
//
//	func TestMain(m *testing.M) {
//		os.Exit(apptest.Run(m))
//	}
//
// A test then answers the dialogs the app will show, calls the app's own
// function, and reads what it asked of the desktop:
//
//	func TestArchive(t *testing.T) {
//		apptest.Reset()
//		apptest.AnswerOpen([]string{"a.pdf"})                // the picker returns this
//		apptest.AnswerMessage(mygo.MessageResult{Button: 1}) // Delete
//		app.archiveSelected()                                // the app's real function
//
//		if got := apptest.Dialogs(); len(got) != 2 {
//			t.Fatalf("dialogs = %+v", got) // the picker, then the question
//		}
//		if got := apptest.Badge(); got != "3" {
//			t.Errorf("badge = %q", got)
//		}
//	}
//
// Only tests import this package: an app built with go build links none
// of it.
package apptest

import (
	"fmt"
	"os"
	"slices"
	"sync"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/appbackend"
	"github.com/egoist/mygo/internal/fake"
	"github.com/egoist/mygo/internal/platform"
)

var (
	// fb is the fake backend Run installed.
	fb *fake.Backend
	// ended is closed once App.Run has returned.
	ended = make(chan struct{})

	mu sync.Mutex
	// opens, saves and messages are the answers of the dialogs to come.
	opens    [][]string
	saves    []string
	messages []mygo.MessageResult
	// dialogs are the dialogs shown since Reset.
	dialogs []Dialog
	// windows are the windows created since Run, in order, as the fake
	// backend made their native windows; windowsFrom is where those
	// created since Reset start.
	windows     []*mygo.Window
	windowsFrom int
	// openedFrom and trashedFrom are where the URLs opened and the items
	// trashed since Reset start.
	openedFrom, trashedFrom int
)

// Run runs the tests of m inside the app, on a fake desktop, and returns
// their exit code: call it from TestMain, which runs on the main goroutine
// as App.Run needs. It starts the app with App.Run, runs the tests once
// the app is ready, as App.WhenReady does, then quits.
//
// The app keeps running when its last window closes, as one with an
// App.OnWindowAllClosed listener does, so that one test closing its
// windows does not end the tests after it. A test that quits the app ends
// the event loop for the tests after it.
//
// Run panics if something made the app's desktop before it, such as a
// call of mygo.WebViewAvailable in TestMain.
func Run(m *testing.M) int {
	b := fake.New()
	installed := false
	appbackend.Once.Do(func() { appbackend.Backend, installed = b, true })
	if !installed {
		panic("apptest: Run must come first in TestMain: the app's desktop was made before it")
	}
	fb = b
	b.OnOpenDialog = openDialog
	b.OnSaveDialog = saveDialog
	b.OnMessageBox = messageBox
	mygo.App.OnWindowCreated(func(w *mygo.Window) {
		mu.Lock()
		windows = append(windows, w)
		mu.Unlock()
	})
	mygo.App.OnWindowAllClosed(func() {})

	code := make(chan int, 1)
	mygo.App.WhenReady(func() {
		go func() {
			code <- m.Run()
			// The tests are done: stop the event loop, whatever the app's
			// quit listeners would say.
			mygo.App.WhenReady(b.Quit)
		}()
	})
	err := mygo.App.Run()
	close(ended)
	if err != nil {
		fmt.Fprintln(os.Stderr, "apptest:", err)
		return 1
	}
	// A test may have quit the app before the tests ended.
	return <-code
}

// backend returns the fake backend, or panics without Run.
func backend() *fake.Backend {
	if fb == nil {
		panic("apptest: call apptest.Run from TestMain first")
	}
	return fb
}

// onMain runs fn on the app's main thread and waits for it, as the
// backend's methods and the app's handlers run there.
func onMain(fn func()) {
	b := backend()
	if b.IsMainThread() {
		fn()
		return
	}
	done := make(chan struct{})
	mygo.App.WhenReady(func() {
		fn()
		close(done)
	})
	select {
	case <-done:
	case <-ended:
		panic("apptest: the app has quit")
	}
}

// Reset forgets the dialogs shown, the answers no dialog used, the URLs
// opened, the items trashed and the windows created, so that a test reads
// only its own. Call it as a test starts. The notifications shown, the
// badge and the windows themselves are the app's state, which it keeps.
func Reset() {
	backend()
	mu.Lock()
	opens, saves, messages, dialogs = nil, nil, nil, nil
	windowsFrom = len(windows)
	mu.Unlock()
	onMain(func() {
		openedFrom, trashedFrom = len(fake.OpenedExternal), len(fake.Trashed)
	})
}

// AnswerOpen answers the next open dialog the app shows, which returns
// paths: one call per dialog, used in order. nil cancels it. A dialog
// without an answer is canceled.
func AnswerOpen(paths []string) {
	backend()
	mu.Lock()
	opens = append(opens, slices.Clone(paths))
	mu.Unlock()
}

// AnswerSave answers the next save dialog the app shows, which returns
// path: one call per dialog, used in order. "" cancels it. A dialog
// without an answer is canceled.
func AnswerSave(path string) {
	backend()
	mu.Lock()
	saves = append(saves, path)
	mu.Unlock()
}

// AnswerMessage answers the next message dialog the app shows with the
// button r.Button, by its index in MessageOptions.Buttons, and the
// checkbox r.CheckboxChecked: one call per dialog, used in order. A
// dialog without an answer is canceled, as Escape does, which chooses its
// CancelButton.
func AnswerMessage(r mygo.MessageResult) {
	backend()
	mu.Lock()
	messages = append(messages, r)
	mu.Unlock()
}

// Dialog is a dialog the app showed: the options of an open dialog in
// Open, of a save dialog in Save, or of a message dialog in Message, the
// others nil. The options are those the dialog showed, with MyGo's
// defaults: a message dialog without Buttons has an OK button, and its
// CancelButton is the button Escape chooses.
type Dialog struct {
	Open    *mygo.OpenDialogOptions
	Save    *mygo.SaveDialogOptions
	Message *mygo.MessageOptions
	// Answered reports whether an Answer call answered it; one that none
	// did was canceled.
	Answered bool
}

// Dialogs returns the dialogs the app showed since Reset, in order.
func Dialogs() []Dialog {
	backend()
	mu.Lock()
	defer mu.Unlock()
	return slices.Clone(dialogs)
}

// parentOf returns the window of a native one, or nil. The fake backend
// lists native windows in the order it made them, as OnWindowCreated
// reported them. Main thread only.
func parentOf(nw platform.Window) *mygo.Window {
	if nw == nil {
		return nil
	}
	i := slices.IndexFunc(fb.Windows(), func(w *fake.Window) bool { return platform.Window(w) == nw })
	mu.Lock()
	defer mu.Unlock()
	if i < 0 || i >= len(windows) {
		return nil
	}
	return windows[i]
}

func toFilters(fs []platform.FileFilter) []mygo.FileFilter {
	if fs == nil {
		return nil
	}
	out := make([]mygo.FileFilter, len(fs))
	for i, f := range fs {
		out[i] = mygo.FileFilter(f)
	}
	return out
}

// openDialog, saveDialog and messageBox answer the dialogs, on the main
// thread, as the fake backend shows them.
func openDialog(o *platform.OpenDialogOptions) []string {
	d := Dialog{Open: &mygo.OpenDialogOptions{
		Parent:                     parentOf(fb.DialogParent),
		Title:                      o.Title,
		DefaultPath:                o.DefaultPath,
		ButtonLabel:                o.ButtonLabel,
		Message:                    o.Message,
		Filters:                    toFilters(o.Filters),
		Directory:                  o.OpenDirectories,
		Multiple:                   o.Multiple,
		ShowHiddenFiles:            o.ShowHiddenFiles,
		CreateDirectories:          o.CreateDirectories,
		TreatPackagesAsDirectories: o.TreatPackagesAsDirectories,
	}}
	mu.Lock()
	defer mu.Unlock()
	var paths []string
	if len(opens) > 0 {
		paths, opens, d.Answered = opens[0], opens[1:], true
	}
	dialogs = append(dialogs, d)
	return slices.Clone(paths)
}

func saveDialog(o *platform.SaveDialogOptions) string {
	d := Dialog{Save: &mygo.SaveDialogOptions{
		Parent:                     parentOf(fb.DialogParent),
		Title:                      o.Title,
		DefaultPath:                o.DefaultPath,
		ButtonLabel:                o.ButtonLabel,
		Message:                    o.Message,
		NameFieldLabel:             o.NameFieldLabel,
		Filters:                    toFilters(o.Filters),
		ShowHiddenFiles:            o.ShowHiddenFiles,
		CreateDirectories:          o.CreateDirectories,
		TreatPackagesAsDirectories: o.TreatPackagesAsDirectories,
	}}
	mu.Lock()
	defer mu.Unlock()
	var path string
	if len(saves) > 0 {
		path, saves, d.Answered = saves[0], saves[1:], true
	}
	dialogs = append(dialogs, d)
	return path
}

func messageBox(o *platform.MessageBoxOptions) platform.MessageBoxResult {
	d := Dialog{Message: &mygo.MessageOptions{
		Parent:             parentOf(fb.DialogParent),
		Type:               mygo.MessageType(o.Type),
		Title:              o.Title,
		Message:            o.Message,
		Detail:             o.Detail,
		Buttons:            slices.Clone(o.Buttons),
		DefaultButton:      o.DefaultID,
		CancelButton:       o.CancelID,
		DestructiveButtons: slices.Clone(o.DestructiveIDs),
		CheckboxLabel:      o.CheckboxLabel,
		CheckboxChecked:    o.CheckboxChecked,
	}}
	mu.Lock()
	defer mu.Unlock()
	// Dismissed without a button, as Escape: MyGo chooses CancelButton.
	r := platform.MessageBoxResult{Response: -1, CheckboxChecked: o.CheckboxChecked}
	if len(messages) > 0 {
		r = platform.MessageBoxResult{Response: messages[0].Button, CheckboxChecked: messages[0].CheckboxChecked}
		messages, d.Answered = messages[1:], true
	}
	dialogs = append(dialogs, d)
	return r
}

// Notifications returns the notifications the app shows, in the order it
// showed them: those not closed, by Notification.Close or
// mygo.ClearNotifications, each with its ID.
func Notifications() []mygo.NotificationOptions {
	var out []mygo.NotificationOptions
	for _, n := range backend().Notifications() {
		o := mygo.NotificationOptions{
			ID:       n.ID,
			Title:    n.Title,
			Subtitle: n.Subtitle,
			Body:     n.Body,
			Silent:   n.Silent,
			Group:    n.Group,
			Image:    slices.Clone(n.Image),
		}
		if s := n.Sender; s != nil {
			o.Sender = &mygo.NotificationSender{ID: s.ID, Name: s.Name, Avatar: slices.Clone(s.Avatar)}
		}
		out = append(out, o)
	}
	return out
}

// ClickNotification clicks the notification with id, as the user does in
// the system's notifications, and returns once the app's listeners ran:
// the notification's OnClick and App.OnNotificationClick. An id the app
// did not show in this run is one an earlier run showed, which only
// App.OnNotificationClick receives.
func ClickNotification(id string) {
	b := backend()
	onMain(func() { b.ClickNotification(id) })
}

// FailNotifications makes the notifications the app shows from now on
// fail with err, as when the user did not allow them
// (mygo.ErrNotificationsDenied); nil lets them through again.
func FailNotifications(err error) {
	b := backend()
	onMain(func() { b.NotificationError = err })
}

// Badge returns the text on the app's icon, which App.SetBadgeCount and
// App.Dock.SetBadge set: "" for none.
func Badge() string { return backend().App().Badge() }

// OpenedURLs returns the URLs the app opened with Shell.OpenExternal since
// Reset, in order.
func OpenedURLs() []string {
	backend()
	var out []string
	onMain(func() { out = slices.Clone(fake.OpenedExternal[openedFrom:]) })
	return out
}

// Trashed returns the paths the app moved to the trash with
// Shell.TrashItem since Reset, in order. The files stay where they are.
func Trashed() []string {
	backend()
	var out []string
	onMain(func() { out = slices.Clone(fake.Trashed[trashedFrom:]) })
	return out
}

// Windows returns the windows the app created since Reset, in order,
// those it closed since too: mygo.Windows returns those still open.
func Windows() []*mygo.Window {
	backend()
	mu.Lock()
	defer mu.Unlock()
	return slices.Clone(windows[windowsFrom:])
}
