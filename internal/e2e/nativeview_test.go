package e2e

import (
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/examples/native-host/control"
	"github.com/egoist/mygo/ui"
)

// TestNativeViewControl uses an actual NSTextField/GtkEntry/EDIT through
// the public API, including an update from a goroutine and live disposal.
func TestNativeViewControl(t *testing.T) {
	var field *mygo.NativeView
	var visible atomic.Bool
	var disposed atomic.Bool
	show := true
	w := newWindow(t, mygo.WindowOptions{Title: "Hosted platform control", Width: 400, Height: 220, Content: ui.View(func(c *ui.Context) {
		ui.Column(c).Fill().Padding(20).Gap(8).Children(func() {
			ui.Text(c, "A MyGo label")
			if show {
				ui.HostView(c, field).Key("control").Height(36).Label("Platform control")
			}
			ui.Button(c, "A MyGo button")
		})
	})})
	opts := control.Options("native", nil)
	dispose := opts.Dispose
	opts.Dispose = func(c mygo.NativeViewContext) {
		if value := control.Text(c); value != "updated" {
			t.Errorf("Dispose read %q", value)
		}
		if dispose != nil {
			dispose(c)
		}
		disposed.Store(true)
	}
	opts.Update = func(c mygo.NativeViewContext) { visible.Store(c.Visible) }
	v, err := w.NewNativeView(opts)
	if err != nil {
		t.Fatal(err)
	}
	w.Update(func() { field = v })
	eventually(t, "native control placed", visible.Load)
	var text string
	if err := v.Update(func(c mygo.NativeViewContext) { control.SetText(c, "updated"); text = control.Text(c) }); err != nil {
		t.Fatal(err)
	}
	if text != "updated" {
		t.Fatalf("native update read %q", text)
	}
	w.Update(func() { show = false })
	eventually(t, "native control hidden", func() bool { return !visible.Load() })
	if v.IsDestroyed() {
		t.Fatal("hiding disposed the control")
	}
	w.Update(func() { show = true })
	eventually(t, "native control restored", visible.Load)
	w.Destroy()
	if !v.IsDestroyed() || !disposed.Load() {
		t.Error("window did not dispose the native control")
	}
}

func TestNativeViewDisposalClosesWindow(t *testing.T) {
	w := newWindow(t, mygo.WindowOptions{Title: "Native disposal", Content: ui.View(func(*ui.Context) {})})
	var disposals atomic.Int32
	var views []*mygo.NativeView
	for range 2 {
		opts := control.Options("native", nil)
		clean := opts.Dispose
		opts.Dispose = func(c mygo.NativeViewContext) {
			if clean != nil {
				clean(c)
			}
			disposals.Add(1)
			w.Destroy()
		}
		v, err := w.NewNativeView(opts)
		if err != nil {
			t.Fatal(err)
		}
		views = append(views, v)
	}
	views[0].Destroy()
	if !w.IsDestroyed() || !views[0].IsDestroyed() || !views[1].IsDestroyed() || disposals.Load() != 2 {
		t.Errorf("window=%v disposals=%d", w.IsDestroyed(), disposals.Load())
	}
}
