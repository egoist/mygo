//go:build darwin

package e2e

import (
	"slices"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/examples/native-host/control"
	"github.com/egoist/mygo/internal/darwin"
	"github.com/egoist/mygo/ui"
)

// TestNativeViewAppKit exercises a real NSTextField inside MyGo: native
// input, main-thread hooks, clipping, tab traversal, accessibility,
// hiding/restoration and disposal while the native handles are live.
func TestNativeViewAppKit(t *testing.T) {
	var field *mygo.NativeView
	var shown atomic.Bool
	var disposed atomic.Int32
	var after atomic.Bool
	show, crop, overlay := true, false, false
	var text string
	var disposedText string
	w := newWindow(t, mygo.WindowOptions{Title: "Native view", Width: 500, Height: 240, Content: ui.View(func(c *ui.Context) {
		ui.Button(c, "Before").Absolute().Left(20).Top(20).Size(120, 30).AutoFocus()
		clipW, clipH := float32(260), float32(80)
		if crop {
			clipW, clipH = 180, 40
		}
		ui.Box(c).Absolute().Left(50).Top(70).Size(clipW, clipH).Clip().Children(func() {
			if show {
				e := ui.HostView(c, field).Key("field").Absolute().Label("Platform field").Size(260, 36)
				if crop {
					e.Left(-20).Top(-10).Height(64)
				}
			}
		})
		after.Store(ui.Button(c, "After").Absolute().Left(20).Top(180).Size(120, 30).Focused())
		if overlay {
			ui.Button(c, "Above").Absolute().Left(60).Top(75).Size(100, 30)
		}
	})})
	opts := control.Options("native", func(value string) { text = value; w.Invalidate() })
	dispose := opts.Dispose
	opts.Dispose = func(c mygo.NativeViewContext) {
		disposedText = control.Text(c)
		dispose(c)
		disposed.Add(1)
	}
	opts.Update = func(c mygo.NativeViewContext) { shown.Store(c.Visible) }
	v, err := w.NewNativeView(opts)
	if err != nil {
		t.Fatal(err)
	}
	w.Update(func() { field = v })
	eventually(t, "native placement", shown.Load)
	var parent uintptr
	if err := v.Update(func(c mygo.NativeViewContext) { parent = c.Parent; control.SetText(c, "") }); err != nil {
		t.Fatal(err)
	}
	var aboveScene bool
	mygo.RunOnMain(func() { aboveScene = darwin.TestNativeViewAboveScene(parent) })
	if !aboveScene {
		t.Error("native control drew below MyGo's rendering layer")
	}
	if !clickAndType(w, 90, 86, "hello") {
		var committed bool
		mygo.RunOnMain(func() { committed = darwin.TestNativeViewText(parent, "hello") })
		if !committed {
			t.Fatal("native input-method commit failed")
		}
		t.Log("input method selected; committed text through the native NSTextInputClient")
	}
	eventually(t, "native text delegate", func() bool { var got string; mygo.RunOnMain(func() { got = text }); return got == "hello" })
	var tab bool
	mygo.RunOnMain(func() { tab = darwin.TestNativeViewTab(parent, false) })
	if !tab {
		t.Fatal("native text field did not own the first responder")
	}
	eventually(t, "focus leaving native control", after.Load)
	v.Focus()
	var focused bool
	mygo.RunOnMain(func() { _, _, _, focused = darwin.TestNativeViewFrame(parent) })
	if !focused {
		t.Fatal("Focus did not reach the NSTextField")
	}
	nodes, _ := accessibility(w)
	if !slices.ContainsFunc(nodes, func(n accessNode) bool {
		return n.role == roleTextField && n.value == "hello"
	}) {
		t.Fatalf("native accessibility subtree missing: %+v", nodes)
	}
	w.Update(func() { crop = true })
	eventually(t, "native clipping geometry", func() bool {
		var b, c [4]float64
		mygo.RunOnMain(func() { b, c, _, _ = darwin.TestNativeViewFrame(parent) })
		return b == [4]float64{30, 60, 260, 64} && c == [4]float64{50, 70, 180, 40}
	})
	w.Update(func() { overlay = true })
	eventually(t, "native hidden under overlay", func() bool { return !shown.Load() })
	nodes, _ = accessibility(w)
	if slices.ContainsFunc(nodes, func(n accessNode) bool { return n.role == roleTextField && n.value == "hello" }) {
		t.Error("hidden native subtree stayed accessible")
	}
	w.Update(func() { overlay, show = false, false })
	w.Update(func() { show = true })
	eventually(t, "native restored", shown.Load)
	var value string
	_ = v.Update(func(c mygo.NativeViewContext) { value = control.Text(c) })
	if value != "hello" {
		t.Errorf("native state after hide: %q", value)
	}
	w.Destroy()
	if !v.IsDestroyed() || disposed.Load() != 1 {
		t.Errorf("destroyed=%v disposed=%d", v.IsDestroyed(), disposed.Load())
	}
	if disposedText != "hello" {
		t.Errorf("Dispose read native text %q, want hello", disposedText)
	}
	if err := v.Update(nil); err == nil {
		t.Error("native update succeeded after close")
	}
}
