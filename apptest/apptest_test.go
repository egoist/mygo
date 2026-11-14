package apptest_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/apptest"
)

// start resets what the fake desktop recorded and opens the app's inbox,
// which the test leaves behind closed.
func start(t *testing.T) *mail {
	t.Helper()
	apptest.Reset()
	mygo.ClearNotifications()
	a := newMail()
	t.Cleanup(func() {
		for _, w := range mygo.Windows() {
			w.Destroy()
		}
	})
	return a
}

// TestAnswerDialogs answers a picker and a question, which the app's
// own functions show over its window.
func TestAnswerDialogs(t *testing.T) {
	a := start(t)
	dir := t.TempDir()
	pdfs := []string{filepath.Join(dir, "a.pdf"), filepath.Join(dir, "b.pdf")}
	apptest.AnswerOpen(pdfs)
	apptest.AnswerMessage(mygo.MessageResult{Button: 1})
	if err := a.importFiles(); err != nil {
		t.Fatal(err)
	}
	if err := a.deleteSelected(); err != nil {
		t.Fatal(err)
	}

	got := apptest.Dialogs()
	if len(got) != 2 || got[0].Open == nil || got[1].Message == nil {
		t.Fatalf("dialogs = %+v; want the picker, then the question", got)
	}
	open, msg := got[0].Open, got[1].Message
	if !got[0].Answered || !open.Multiple || open.Directory || open.Parent != a.inbox ||
		len(open.Filters) != 1 || !slices.Equal(open.Filters[0].Extensions, []string{"pdf"}) {
		t.Errorf("picker %+v, answered %v", open, got[0].Answered)
	}
	if !got[1].Answered || msg.Parent != a.inbox || msg.Type != mygo.MessageWarning ||
		msg.Message != "Delete 2 attachments?" || !slices.Equal(msg.Buttons, []string{"Cancel", "Delete"}) ||
		!slices.Equal(msg.DestructiveButtons, []int{1}) || msg.CancelButton != 0 {
		t.Errorf("question %+v, answered %v", msg, got[1].Answered)
	}
	if got := apptest.Trashed(); !slices.Equal(got, pdfs) {
		t.Errorf("trashed %q", got)
	}
}

// TestUnansweredDialogs: a dialog no Answer call answered is canceled,
// the question by Escape, which chooses Cancel; answers are used in order
// and Reset drops those left.
func TestUnansweredDialogs(t *testing.T) {
	a := start(t)
	a.selected = []string{"a.pdf"}
	if err := a.deleteSelected(); err != nil {
		t.Fatal(err)
	}
	if got := apptest.Trashed(); len(got) != 0 {
		t.Errorf("trashed %q after Escape", got)
	}
	if err := a.importFiles(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(a.selected, []string{"a.pdf"}) {
		t.Errorf("a canceled picker added %q", a.selected)
	}
	got := apptest.Dialogs()
	if len(got) != 2 || got[0].Answered || got[1].Answered {
		t.Errorf("dialogs %+v; want two canceled", got)
	}

	apptest.AnswerOpen([]string{"first.pdf"})
	apptest.AnswerOpen([]string{"second.pdf"})
	a.importFiles()
	apptest.Reset()
	a.importFiles()
	if !slices.Equal(a.selected, []string{"a.pdf", "first.pdf"}) {
		t.Errorf("selected %q; want the first answer, and none after Reset", a.selected)
	}
	if got := apptest.Dialogs(); len(got) != 1 || got[0].Answered {
		t.Errorf("dialogs since Reset %+v", got)
	}
}

// TestAnswerSave saves where the answer says.
func TestAnswerSave(t *testing.T) {
	a := start(t)
	path := filepath.Join(t.TempDir(), "inbox.mbox")
	apptest.AnswerSave(path)
	if err := a.export(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("not exported: %v", err)
	}
	if got := apptest.Dialogs(); len(got) != 1 || got[0].Save == nil || got[0].Save.DefaultPath != "inbox.mbox" || got[0].Save.Parent != nil {
		t.Errorf("dialogs %+v", got)
	}
}

// TestNotifications: the app's notifications in the order shown, the
// badge counting them, and a click reaching the app's listener, which
// opens a window and closes the notification.
func TestNotifications(t *testing.T) {
	a := start(t)
	for _, m := range [][3]string{{"1", "Ada", "Notes"}, {"2", "Grace", "Compilers"}} {
		if err := a.receive(m[0], m[1], m[2]); err != nil {
			t.Fatal(err)
		}
	}
	if got := apptest.Badge(); got != "2" {
		t.Errorf("badge %q", got)
	}
	shown := apptest.Notifications()
	if len(shown) != 2 || shown[0].ID != "msg/1" || shown[0].Title != "Ada" || shown[1].Body != "Compilers" {
		t.Fatalf("notifications %+v", shown)
	}

	apptest.ClickNotification("msg/2")
	if got := apptest.Badge(); got != "1" {
		t.Errorf("badge %q after the click", got)
	}
	wins := apptest.Windows()
	if len(wins) != 2 || wins[0] != a.inbox || wins[1].Title() != "Compilers" || wins[1].Parent() != a.inbox {
		t.Errorf("windows %v", wins)
	}
	if got := apptest.Notifications(); len(got) != 1 || got[0].ID != "msg/1" {
		t.Errorf("notifications after the click %+v", got)
	}

	var clicked []string
	off := mygo.App.OnNotificationClick(func(id string) { clicked = append(clicked, id) })
	defer off()
	apptest.ClickNotification("msg/earlier-run")
	if !slices.Equal(clicked, []string{"msg/earlier-run"}) {
		t.Errorf("App.OnNotificationClick got %q", clicked)
	}
}

// TestFailNotifications: the app sees notifications refused.
func TestFailNotifications(t *testing.T) {
	a := start(t)
	apptest.FailNotifications(mygo.ErrNotificationsDenied)
	t.Cleanup(func() { apptest.FailNotifications(nil) })
	if err := a.receive("1", "Ada", "Notes"); !errors.Is(err, mygo.ErrNotificationsDenied) {
		t.Errorf("Show: %v", err)
	}
	if got := apptest.Notifications(); len(got) != 0 {
		t.Errorf("notifications %+v", got)
	}
}

// TestOpenedURLs reads the URLs opened since Reset.
func TestOpenedURLs(t *testing.T) {
	a := start(t)
	if err := a.help(); err != nil {
		t.Fatal(err)
	}
	if got := apptest.OpenedURLs(); !slices.Equal(got, []string{"https://example.com/help"}) {
		t.Errorf("opened %q", got)
	}
	apptest.Reset()
	if got := apptest.OpenedURLs(); len(got) != 0 {
		t.Errorf("opened %q after Reset", got)
	}
}

// TestWindowsClosed: a window closed is still one the app created, and
// the app keeps running without windows.
func TestWindowsClosed(t *testing.T) {
	a := start(t)
	a.inbox.Close()
	if got := apptest.Windows(); len(got) != 1 || !got[0].IsDestroyed() {
		t.Errorf("windows %v", got)
	}
	b := newMail()
	if got := apptest.Windows(); len(got) != 2 || got[1] != b.inbox {
		t.Errorf("windows %v after the last closed", got)
	}
}
