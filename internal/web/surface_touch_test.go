//go:build js

package web

import (
	"github.com/egoist/mygo/internal/platform"
	"reflect"
	"testing"
)

type pointerEventRecorder struct {
	platform.WindowHandler
	event platform.SurfaceEvent
}

func (h *pointerEventRecorder) SurfaceEvent(event platform.SurfaceEvent) bool {
	h.event = event
	return true
}

func TestSurfaceForwardsTouchPointerEvents(t *testing.T) {
	for _, pointer := range []platform.PointerKind{platform.PointerMouse, platform.PointerPen, platform.PointerTouch} {
		for _, kind := range []platform.SurfaceEventKind{platform.PointerDown, platform.PointerMove, platform.PointerUp, platform.PointerCancel} {
			handler := &pointerEventRecorder{}
			surface := &surface{w: &window{h: handler}}
			event := platform.SurfaceEvent{Kind: kind, Pointer: pointer, X: 21, Y: 34, Button: 1}
			if !surface.send(event) {
				t.Fatal("event was not forwarded")
			}
			want := event
			want.Touch = pointer == platform.PointerTouch
			if !reflect.DeepEqual(handler.event, want) {
				t.Errorf("forwarded %v/%v = %+v, want %+v", pointer, kind, handler.event, want)
			}
		}
	}
}
