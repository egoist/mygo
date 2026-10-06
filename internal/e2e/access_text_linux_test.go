//go:build linux && (amd64 || arm64)

package e2e

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/linux"
	"github.com/egoist/mygo/ui"
	"sync/atomic"
	"testing"
)

func TestContentWindowTextAccessibilitySignals(t *testing.T) {
	value := "A😀e\u0301\nsecond"
	secret := "secret"
	var frames atomic.Int32
	w := newWindow(t, mygo.WindowOptions{Width: 340, Height: 230, Content: ui.View(func(c *ui.Context) {
		frames.Add(1)
		ui.Column(c).Fill().Children(func() {
			ui.TextArea(c, &value).Label("Editor").Height(120)
			ui.TextInput(c, &secret).Label("Password").Password()
		})
	})})
	eventually(t, "text frame", func() bool { return frames.Load() > 0 })
	var read, password func() [3]int
	var stop, stopPassword func()
	var ok bool
	mygo.RunOnMain(func() {
		read, stop, ok = linux.TestAccessibilityTextEvents(w.NativeHandle(), "Editor")
		password, stopPassword, _ = linux.TestAccessibilityTextEvents(w.NativeHandle(), "Password")
	})
	if !ok {
		t.Skip("ATK text signals unavailable")
	}
	defer mygo.RunOnMain(stop)
	if stopPassword != nil {
		defer mygo.RunOnMain(stopPassword)
	}
	counts := func(fn func() [3]int) (out [3]int) { mygo.RunOnMain(func() { out = fn() }); return }
	accessSelectText(w, "Editor", 0, 2)
	eventually(t, "selection and caret signals", func() bool { n := counts(read); return n[1] > 0 && n[2] > 0 })
	if n := counts(read); n[0] != 0 {
		t.Fatalf("selection emitted text changes: %v", n)
	}
	w.Update(func() { value = "A😀changed\nsecond"; secret = "another secret" })
	eventually(t, "text change signal", func() bool { return counts(read)[0] > 0 })
	if password != nil {
		if n := counts(password); n != [3]int{} {
			t.Fatalf("password signaled private text: %v", n)
		}
	}
}
