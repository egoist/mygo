//go:build linux && (amd64 || arm64)

package e2e

import (
	"math"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/linux"
	"github.com/egoist/mygo/ui"
)

func TestContentWindowGDKContactsAndGestures(t *testing.T) {
	var frames atomic.Int32
	var input []ui.InputEvent
	var gestures []ui.GestureEvent
	w := newWindow(t, mygo.WindowOptions{Title: "GDK contacts", Width: 280, Height: 260, Content: ui.View(func(c *ui.Context) {
		frames.Add(1)
		ui.Column(c).Padding(20).Children(func() {
			ui.Box(c).Size(200, 200).TrackContacts().HandleInput(func(ev ui.InputEvent) bool { input = append(input, ev); return true }).Gestures(ui.GesturePinch|ui.GestureRotation, func(ev ui.GestureEvent) bool { gestures = append(gestures, ev); return true })
		})
	})})
	eventually(t, "GDK frame", func() bool { return frames.Load() > 0 })
	mygo.RunOnMain(func() {
		h := w.NativeHandle()
		linux.TestTouchSurface(h, 100, 0, 50, 60)
		linux.TestTouchSurface(h, 200, 0, 80, 60)
		linux.TestTouchSurface(h, 100, 1, 51, 61)
		linux.TestTouchSurface(h, 100, 3, 51, 61)
		linux.TestTouchSurface(h, 200, 2, 80, 60)
		linux.TestPinchSurface(h, 0, 70, 80, 0, 0, 1, 0)
		linux.TestPinchSurface(h, 1, 70, 80, 0, 0, 1.2, .1)
		linux.TestPinchSurface(h, 1, 70, 80, 0, 0, 1.5, .2)
		linux.TestPinchSurface(h, 2, 70, 80, 0, 0, 1.5, 0)
	})
	var got []ui.InputEvent
	var gg []ui.GestureEvent
	mygo.RunOnMain(func() { got = slices.Clone(input); gg = slices.Clone(gestures) })
	var down, up, cancel []uint64
	for _, ev := range got {
		switch ev.Kind {
		case ui.InputPointerDown:
			down = append(down, ev.Pointer.ID)
			if ev.Pointer.Device != ui.PointerTouch {
				t.Errorf("device=%v", ev.Pointer.Device)
			}
			if len(down) == 1 && (ev.X != 30 || ev.Y != 40) {
				t.Errorf("GDK DIPs=%v/%v", ev.X, ev.Y)
			}
		case ui.InputPointerUp:
			up = append(up, ev.Pointer.ID)
		case ui.InputPointerCancel:
			cancel = append(cancel, ev.Pointer.ID)
		}
	}
	if len(down) != 2 || down[0] == down[1] || !slices.Equal(cancel, down[:1]) || !slices.Equal(up, down[1:]) {
		t.Fatalf("GDK down/up/cancel=%v/%v/%v", down, up, cancel)
	}
	// The cancelled direct sequence may have recognized a small rotation;
	// keep only the native trackpad events for the cumulative-scale check.
	gg = slices.DeleteFunc(gg, func(ev ui.GestureEvent) bool { return ev.Device != ui.PointerTouchpad })
	if len(gg) != 4 || gg[0].Phase != ui.GestureBegin || gg[3].Phase != ui.GestureEnd || math.Abs(float64(gg[1].Scale)-1.2) > 1e-5 || math.Abs(float64(gg[2].Scale)-1.25) > 1e-5 || math.Abs(float64(gg[2].TotalRotation)-.3) > 1e-5 {
		t.Fatalf("GDK gesture phases/deltas=%+v", gg)
	}
}
