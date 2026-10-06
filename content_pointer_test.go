package mygo

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/ui"
)

func TestContentPointerContactsAndClose(t *testing.T) {
	var got []ui.InputEvent
	w, _, s := contentWindow(t, func(c *ui.Context) {
		ui.Column(c).Padding(20).Children(func() {
			ui.Box(c).Size(100, 100).TrackContacts().HandleInput(func(ev ui.InputEvent) bool { got = append(got, ev); return true })
		})
	})
	onMain(func() {
		s.Scale = 2
		for _, id := range []uint64{1, 2} {
			s.Send(platform.SurfaceEvent{Kind: platform.PointerDown, X: 35, Y: 45,
				Pointer: platform.PointerInfo{ID: id, Device: platform.PointerTouch, Contact: true, Primary: id == 1}})
		}
		s.Send(platform.SurfaceEvent{Kind: platform.PointerCaptureLost, Pointer: platform.PointerInfo{ID: 2, Device: platform.PointerTouch}})
	})
	w.Destroy()
	onMain(func() {
		var ids, cancel, lost []uint64
		for _, ev := range got {
			switch ev.Kind {
			case ui.InputPointerDown:
				ids = append(ids, ev.Pointer.ID)
				if ev.X != 15 || ev.Y != 25 {
					t.Errorf("DIPs changed at scale 2: %+v", ev)
				}
			case ui.InputPointerCancel:
				cancel = append(cancel, ev.Pointer.ID)
			case ui.InputPointerCaptureLost:
				lost = append(lost, ev.Pointer.ID)
			case ui.InputPointerUp:
				t.Error("handled cancellation produced release")
			}
		}
		if !slices.Equal(ids, []uint64{1, 2}) || !slices.Equal(cancel, []uint64{2, 1}) || !slices.Equal(lost, []uint64{2, 1}) {
			t.Errorf("ids/cancel/lost: %v/%v/%v", ids, cancel, lost)
		}
	})
}
