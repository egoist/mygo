package e2e

import (
	"slices"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// TestContentWindowPointer exercises native mouse translation, DIP
// coordinates, capture outside the target, and capture lifecycle on a real
// surface. Device-specific touch and pen injection needs supported hardware.
func TestContentWindowPointer(t *testing.T) {
	var frames atomic.Int32
	var events []ui.InputEvent
	w := newWindow(t, mygo.WindowOptions{Title: "Pointer lifecycle", Width: 280, Height: 260, Content: ui.View(func(c *ui.Context) {
		frames.Add(1)
		ui.Column(c).Padding(20).Children(func() {
			ui.Box(c).Size(100, 100).HandleInput(func(ev ui.InputEvent) bool { events = append(events, ev); return true })
		})
	})})
	eventually(t, "a pointer surface frame", func() bool { return frames.Load() > 0 })
	if !drag(w, [][2]float64{{40, 60}, {170, 190}, {180, 200}}) {
		t.Skip("native drag injection unavailable")
	}
	eventually(t, "native pointer release", func() (up bool) {
		mygo.RunOnMain(func() {
			for _, ev := range events {
				if ev.Kind == ui.InputPointerUp {
					up = true
				}
			}
		})
		return
	})
	var got []ui.InputEvent
	mygo.RunOnMain(func() { got = slices.Clone(events) })
	var down, moved, up, captured, lost bool
	for _, ev := range got {
		if ev.Kind < ui.InputPointerDown {
			continue
		}
		if ev.Pointer.ID != 0 || ev.Pointer.Device != ui.PointerMouse {
			t.Errorf("mouse identity = %+v", ev.Pointer)
		}
		switch ev.Kind {
		case ui.InputPointerDown:
			down = true
			if ev.X != 20 || ev.Y != 40 || !ev.Pointer.Contact || !ev.Pointer.Primary {
				t.Errorf("native down = %+v", ev)
			}
		case ui.InputPointerMove:
			if ev.X >= 150 && ev.Y >= 170 {
				moved = true
			}
		case ui.InputPointerUp:
			up = true
			if ev.Pointer.Contact {
				t.Error("up still in contact")
			}
		case ui.InputPointerCapture:
			captured = true
		case ui.InputPointerCaptureLost:
			lost = true
		}
	}
	if !down || !moved || !up || !captured || !lost {
		t.Fatalf("native lifecycle: down/move/up/capture/lost = %v/%v/%v/%v/%v: %+v", down, moved, up, captured, lost, got)
	}
}
