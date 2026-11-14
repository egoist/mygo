package apptest_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/apptest"
	"github.com/egoist/mygo/ui"
)

// TestMain runs the tests inside the app, on the fake desktop.
func TestMain(m *testing.M) {
	os.Exit(apptest.Run(m))
}

// mail is a tiny mail app: its methods are the wiring the tests call, as
// the app's menus, buttons and listeners would.
type mail struct {
	inbox    *mygo.Window
	selected []string // the attachments imported, which Delete trashes
	unread   int
}

func newMail() *mail {
	a := &mail{}
	a.inbox = mygo.NewWindow(mygo.WindowOptions{Title: "Inbox", Content: ui.View(func(c *ui.Context) {
		ui.Text(c, "Inbox")
	})})
	return a
}

// importFiles asks for attachments.
func (a *mail) importFiles() error {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:   a.inbox,
		Title:    "Import",
		Filters:  []mygo.FileFilter{{Name: "Documents", Extensions: []string{"pdf"}}},
		Multiple: true,
	})
	a.selected = append(a.selected, paths...)
	return err
}

// deleteSelected asks before trashing the attachments.
func (a *mail) deleteSelected() error {
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Parent:             a.inbox,
		Type:               mygo.MessageWarning,
		Message:            fmt.Sprintf("Delete %d attachments?", len(a.selected)),
		Buttons:            []string{"Cancel", "Delete"},
		DestructiveButtons: []int{1},
	})
	if err != nil || res.Button != 1 {
		return err
	}
	for _, p := range a.selected {
		if err := mygo.Shell.TrashItem(p); err != nil {
			return err
		}
	}
	a.selected = nil
	return nil
}

// receive notifies of a new message and counts it on the app's icon; a
// click on the notification opens the message in a window of its own.
func (a *mail) receive(id, from, subject string) error {
	a.unread++
	mygo.App.SetBadgeCount(a.unread)
	n := mygo.NewNotification(mygo.NotificationOptions{ID: "msg/" + id, Title: from, Body: subject})
	n.OnClick(func() {
		a.unread--
		mygo.App.SetBadgeCount(a.unread)
		mygo.NewWindow(mygo.WindowOptions{Title: subject, Parent: a.inbox})
		n.Close()
	})
	return n.Show()
}

// export saves the inbox where the user says.
func (a *mail) export() error {
	path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Title: "Export", DefaultPath: "inbox.mbox"})
	if err != nil || path == "" {
		return err
	}
	return os.WriteFile(path, []byte("From ada\n"), 0o644)
}

// help opens the manual in the browser.
func (a *mail) help() error { return mygo.Shell.OpenExternal("https://example.com/help") }

// The tests call the app's own functions, answering its dialogs, and read
// what it asked of the desktop.
func Example() {
	apptest.Reset()
	a := newMail()
	defer a.inbox.Destroy() // and the message's window, its child

	apptest.AnswerOpen([]string{"a.pdf", "b.pdf"})       // the picker
	apptest.AnswerMessage(mygo.MessageResult{Button: 1}) // Delete
	a.importFiles()
	a.deleteSelected()
	for _, d := range apptest.Dialogs() {
		switch {
		case d.Open != nil:
			fmt.Println("open:", d.Open.Title)
		case d.Message != nil:
			fmt.Println("message:", d.Message.Message, strings.Join(d.Message.Buttons, "/"))
		}
	}
	fmt.Println("trashed:", len(apptest.Trashed()))

	a.receive("1", "Ada", "Notes")
	fmt.Printf("badge: %q\n", apptest.Badge())
	apptest.ClickNotification("msg/1")
	fmt.Printf("badge: %q\n", apptest.Badge())
	for _, w := range apptest.Windows() {
		fmt.Println("window:", w.Title())
	}

	// Output:
	// open: Import
	// message: Delete 2 attachments? Cancel/Delete
	// trashed: 2
	// badge: "1"
	// badge: ""
	// window: Inbox
	// window: Notes
}
