//go:build linux && (amd64 || arm64)

package e2e

import (
	"slices"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/examples/native-host/control"
	"github.com/egoist/mygo/internal/linux"
	"github.com/egoist/mygo/ui"
)

func TestNativeViewGTK(t *testing.T) {
	var field *mygo.NativeView
	var placed atomic.Bool
	var after atomic.Bool
	w := newWindow(t, mygo.WindowOptions{Title: "Native GTK view", Width: 500, Height: 240, Content: ui.View(func(c *ui.Context) {
		ui.Button(c, "Before").Absolute().Left(20).Top(20).Size(100, 30).AutoFocus()
		ui.Box(c).Absolute().Left(50).Top(70).Size(180, 40).Clip().Children(func() {
			ui.HostView(c, field).Key("field").Absolute().Left(-20).Top(-10).Size(260, 64).Label("Platform field")
		})
		after.Store(ui.Button(c, "After").Absolute().Left(20).Top(180).Size(100, 30).Focused())
	})})
	opts := control.Options("native GTK", nil)
	opts.Update = func(c mygo.NativeViewContext) { placed.Store(c.Visible) }
	v, err := w.NewNativeView(opts)
	if err != nil {
		t.Fatal(err)
	}
	w.Update(func() { field = v })
	eventually(t, "GtkEntry placed", placed.Load)
	var parent uintptr
	_ = v.Update(func(c mygo.NativeViewContext) { parent = c.Parent })
	var actualBounds, actualClip [4]float64
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("actual GTK bounds=%v clip=%v", actualBounds, actualClip)
		}
	})
	eventually(t, "GTK clipping allocation", func() bool {
		var b, c [4]float64
		mygo.RunOnMain(func() { b, c, _, _ = linux.TestNativeViewFrame(parent) })
		actualBounds, actualClip = b, c
		return b == [4]float64{30, 60, 260, 64} && c == [4]float64{50, 70, 180, 40}
	})
	v.Focus()
	var focused, tab bool
	mygo.RunOnMain(func() {
		_, _, _, focused = linux.TestNativeViewFrame(parent)
		tab = linux.TestNativeViewTab(parent, false)
	})
	if !focused || !tab {
		t.Fatalf("native GTK focus=%v tab=%v", focused, tab)
	}
	eventually(t, "Tab left GtkEntry for MyGo", after.Load)
	nodes, _ := accessibility(w)
	if !slices.ContainsFunc(nodes, func(n accessNode) bool {
		return n.role == "text" && n.label == "Native editor" && n.value == "native GTK"
	}) {
		t.Fatalf("native ATK subtree missing: %+v", nodes)
	}
	var logical bool
	mygo.RunOnMain(func() { logical = linux.TestNativeViewAccessParent(parent) })
	if !logical {
		t.Error("native ATK object did not join the logical MyGo tree")
	}
}
