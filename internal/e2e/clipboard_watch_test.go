//go:build !js

package e2e

import (
	"testing"
	"time"

	"github.com/egoist/mygo"
)

func TestClipboardChangeNotifications(t *testing.T) {
	preserveClipboard(t)
	changes := make(chan string, 32)
	off, err := mygo.Clipboard.OnChanged(func() {
		select {
		case changes <- mygo.Clipboard.ReadText():
		default:
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(off)
	wait := func(want string) {
		t.Helper()
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		for {
			select {
			case got := <-changes:
				if got == want {
					return
				}
			case <-timer.C:
				t.Fatalf("no clipboard notification with %q", want)
			}
		}
	}
	mygo.Clipboard.WriteText("clipboard observation 日本語")
	wait("clipboard observation 日本語")
	mygo.Clipboard.Clear()
	wait("")
	off()
	off()
	for len(changes) != 0 {
		<-changes
	}
	mygo.Clipboard.WriteText("after unsubscribe")
	select {
	case got := <-changes:
		t.Fatalf("notification after unsubscribe: %q", got)
	case <-time.After(600 * time.Millisecond):
	}
}
