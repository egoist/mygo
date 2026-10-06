package ui

import (
	"math"
	"slices"
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

func touch(id uint64) PointerInfo {
	return PointerInfo{ID: id, Device: PointerTouch, Contact: true, Primary: id == 1}
}
func pointerNear(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-4 }

func TestPointerContactsCaptureAndCoordinates(t *testing.T) {
	var events []InputEvent
	tt := NewTester(func(c *Context) {
		Column(c).Padding(20).Children(func() {
			Box(c).Key("pad").Size(100, 100).HandleInput(func(ev InputEvent) bool { events = append(events, ev); return true })
		})
	}, 240, 220)
	defer tt.rt.close()
	tt.SetScale(2)
	tt.Pointer(InputPointerDown, touch(1), 35, 45)
	tt.Pointer(InputPointerDown, touch(2), 50, 55)
	tt.Pointer(InputPointerMove, touch(1), 210, 200)
	tt.Pointer(InputPointerCancel, touch(1), 210, 200)
	tt.Pointer(InputPointerUp, touch(2), 200, 180)
	var down, move, up, cancel, capture, lost []uint64
	for _, ev := range events {
		switch ev.Kind {
		case InputPointerDown:
			down = append(down, ev.Pointer.ID)
			if ev.Pointer.ID == 1 && (ev.X != 15 || ev.Y != 25) {
				t.Errorf("DIP coordinates = %v", ev)
			}
		case InputPointerMove:
			move = append(move, ev.Pointer.ID)
			if ev.Pointer.ID == 1 && ev.X == 190 && ev.Y != 180 {
				t.Errorf("captured move = %v", ev)
			}
		case InputPointerUp:
			up = append(up, ev.Pointer.ID)
		case InputPointerCancel:
			cancel = append(cancel, ev.Pointer.ID)
		case InputPointerCapture:
			capture = append(capture, ev.Pointer.ID)
		case InputPointerCaptureLost:
			lost = append(lost, ev.Pointer.ID)
		}
	}
	if !slices.Equal(down, []uint64{1, 2}) || !slices.Equal(up, []uint64{2}) || !slices.Equal(cancel, []uint64{1}) || !slices.Equal(capture, []uint64{1, 2}) || !slices.Equal(lost, []uint64{1, 2}) || !slices.Contains(move, uint64(1)) {
		t.Fatalf("lifecycle: down%v move%v up%v cancel%v capture%v lost%v", down, move, up, cancel, capture, lost)
	}
}

func TestPenAxesStationaryAndCancellation(t *testing.T) {
	var events []InputEvent
	tt := NewTester(func(c *Context) {
		Box(c).Fill().HandleInput(func(ev InputEvent) bool { events = append(events, ev); return true })
	}, 100, 100)
	defer tt.rt.close()
	p := PointerInfo{ID: 99, Device: PointerPen, Contact: true, HasPressure: true, HasTilt: true, Pressure: .2, TiltX: -20, TiltY: 30, Eraser: true}
	tt.Pointer(InputPointerDown, p, 10, 10)
	p.Pressure = .7
	tt.Pointer(InputPointerMove, p, 10, 10)
	tt.SetFocused(false)
	var moved, cancelled, lost bool
	for _, ev := range events {
		if ev.Kind == InputPointerMove && ev.Pointer.Pressure == .7 {
			moved = true
			if ev.Pointer.TiltX != -20 || ev.Pointer.TiltY != 30 || !ev.Pointer.Eraser {
				t.Errorf("pen data = %+v", ev.Pointer)
			}
		}
		if ev.Kind == InputPointerCancel {
			cancelled = true
		}
		if ev.Kind == InputPointerCaptureLost {
			lost = true
		}
		if ev.Kind == InputPointerUp {
			t.Error("handled cancellation became release")
		}
	}
	if !moved || !cancelled || !lost {
		t.Fatalf("move/cancel/lost = %v/%v/%v: %+v", moved, cancelled, lost, events)
	}
}

func TestCaptureLossAndTargetRemovalDoNotClick(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(map[bool]string{false: "native capture loss", true: "target removed"}[remove], func(t *testing.T) {
			show, clicks := true, 0
			var kinds []InputKind
			tt := NewTester(func(c *Context) {
				if show {
					e := Box(c).Key("target").Size(100, 100).HandleInput(func(ev InputEvent) bool { kinds = append(kinds, ev.Kind); return true })
					if e.Clicked() {
						clicks++
					}
				}
			}, 150, 150)
			defer tt.rt.close()
			tt.Press(20, 20)
			// A handled raw press captures, then the target disappears.
			if remove {
				show = false
				tt.Frame()
			} else {
				tt.Pointer(InputPointerCaptureLost, PointerInfo{}, 20, 20)
			}
			tt.Release(20, 20)
			if clicks != 0 || !slices.Contains(kinds, InputPointerCancel) || !slices.Contains(kinds, InputPointerCaptureLost) {
				t.Fatalf("clicks=%v kinds=%v", clicks, kinds)
			}
		})
	}
}

func TestExplicitCaptureProtectsTouchFromScroll(t *testing.T) {
	var scroll ScrollState
	var captured bool
	var kinds []InputKind
	tt := NewTester(func(c *Context) {
		Scroll(c).Size(180, 100).TrackScroll(&scroll).Children(func() {
			e := Box(c).Size(140, 400)
			e.HandleInput(func(ev InputEvent) bool {
				kinds = append(kinds, ev.Kind)
				if ev.Kind == InputPointerDown {
					captured = e.CapturePointer(ev.Pointer.ID)
				}
				return true
			})
		})
	}, 200, 120)
	defer tt.rt.close()
	tt.Pointer(InputPointerDown, touch(1), 30, 70)
	tt.Pointer(InputPointerMove, touch(1), 30, 20)
	tt.Pointer(InputPointerUp, touch(1), 30, 20)
	if !captured || scroll.Y != 0 || slices.Contains(kinds, InputPointerCancel) {
		t.Fatalf("capture=%v scroll=%v kinds=%v", captured, scroll.Y, kinds)
	}
}

func TestCompatibilityReleaseOnBlur(t *testing.T) {
	var release *InputEvent
	tt := NewTester(func(c *Context) {
		Box(c).Fill().HandleInput(func(ev InputEvent) bool {
			if ev.Kind == InputPointerUp {
				copy := ev
				release = &copy
			}
			return ev.Kind == InputPointerDown
		})
	}, 100, 100)
	defer tt.rt.close()
	tt.Press(20, 20)
	tt.SetFocused(false)
	if release == nil || !release.Cancelled {
		t.Fatalf("compatibility release = %+v", release)
	}
}

func TestTouchPanScrollCancelsTap(t *testing.T) {
	var scroll ScrollState
	clicks := 0
	tt := NewTester(func(c *Context) {
		Scroll(c).Size(160, 100).TrackScroll(&scroll).Children(func() {
			e := Box(c).Size(140, 400)
			if e.Clicked() {
				clicks++
			}
		})
	}, 200, 160)
	defer tt.rt.close()
	tt.Pointer(InputPointerDown, touch(1), 30, 80)
	tt.Pointer(InputPointerMove, touch(1), 30, 50)
	tt.Pointer(InputPointerMove, touch(1), 30, 30)
	tt.Pointer(InputPointerUp, touch(1), 30, 30)
	if scroll.Y != 50 || clicks != 0 {
		t.Fatalf("pan scroll=%v clicks=%v", scroll.Y, clicks)
	}
	tt.Pointer(InputPointerDown, touch(2), 30, 50)
	tt.Pointer(InputPointerUp, touch(2), 30, 50)
	if clicks != 1 {
		t.Fatalf("ordinary tap clicked %d times", clicks)
	}
}

func TestTouchGestureDeltasAndPhases(t *testing.T) {
	var got []GestureEvent
	tt := NewTester(func(c *Context) {
		Box(c).Fill().Gestures(GesturePan|GesturePinch|GestureRotation, func(ev GestureEvent) bool { got = append(got, ev); return true })
	}, 400, 400)
	defer tt.rt.close()
	tt.Pointer(InputPointerDown, touch(1), 100, 100)
	tt.Pointer(InputPointerDown, touch(2), 200, 100)
	tt.Pointer(InputPointerMove, touch(2), 200, 200)
	tt.Pointer(InputPointerMove, touch(1), 110, 110)
	tt.Pointer(InputPointerUp, touch(2), 200, 200)
	if len(got) != 4 || got[0].Phase != GestureBegin || got[1].Phase != GestureUpdate || got[2].Phase != GestureUpdate || got[3].Phase != GestureEnd {
		t.Fatalf("phases: %+v", got)
	}
	if !pointerNear(got[1].Scale, float32(math.Sqrt(2))) || !pointerNear(got[1].Rotation, math.Pi/4) || got[1].DY != 50 || got[1].DX != 0 {
		t.Fatalf("first delta: %+v", got[1])
	}
	if !pointerNear(got[2].Scale, .9) || !pointerNear(got[2].TotalScale, float32(math.Sqrt(2))*.9) || !pointerNear(got[2].TotalRotation, math.Pi/4) || got[2].TotalX != 5 || got[2].TotalY != 55 {
		t.Fatalf("accumulation: %+v", got[2])
	}
	if got[3].Scale != 1 || got[3].DX != 0 || got[3].Rotation != 0 {
		t.Fatal("terminal event has deltas")
	}
}

func TestGestureCoordinationNestedAndScroll(t *testing.T) {
	for _, claim := range []bool{false, true} {
		t.Run(map[bool]string{false: "scroll fallback", true: "ancestor handler"}[claim], func(t *testing.T) {
			var scroll ScrollState
			var child, parent []GestureEvent
			tt := NewTester(func(c *Context) {
				Scroll(c).Size(150, 100).TrackScroll(&scroll).Gestures(GesturePan, func(ev GestureEvent) bool { parent = append(parent, ev); return claim }).Children(func() {
					Box(c).Size(130, 400).Gestures(GesturePan, func(ev GestureEvent) bool { child = append(child, ev); return false })
				})
			}, 200, 150)
			defer tt.rt.close()
			tt.Pointer(InputPointerDown, touch(1), 25, 75)
			tt.Pointer(InputPointerMove, touch(1), 25, 45)
			tt.Pointer(InputPointerUp, touch(1), 25, 45)
			if len(child) != 1 || child[0].Phase != GestureBegin {
				t.Fatalf("declining child = %+v", child)
			}
			if claim && (len(parent) != 3 || scroll.Y != 0) || !claim && (len(parent) != 1 || scroll.Y != 30) {
				t.Fatalf("parent=%+v scroll=%v", parent, scroll.Y)
			}
		})
	}
}

func TestNativeGestureStableOwnerAndCancel(t *testing.T) {
	var got []GestureEvent
	show := true
	tt := NewTester(func(c *Context) {
		Column(c).Padding(20).Children(func() {
			if show {
				Box(c).Key("pad").Size(100, 100).Gestures(GesturePinch|GestureRotation, func(ev GestureEvent) bool { got = append(got, ev); return true })
			}
		})
	}, 250, 250)
	defer tt.rt.close()
	tt.Gesture(GestureEvent{Kind: GesturePinch, Phase: GestureBegin, Device: PointerTouchpad, X: 30, Y: 40})
	tt.Gesture(GestureEvent{Kind: GesturePinch, Phase: GestureUpdate, Device: PointerTouchpad, X: 210, Y: 200, Scale: 1.2})
	show = false
	tt.Frame()
	if len(got) != 3 || got[0].X != 10 || got[0].Y != 20 || got[1].X != 190 || got[1].TotalScale != 1.2 || got[2].Phase != GestureCancel {
		t.Fatalf("native lifecycle=%+v", got)
	}
}

func TestRotationWrapAndDegenerateTouches(t *testing.T) {
	if math.Abs(angleDelta(-math.Pi+.1, math.Pi-.1)-.2) > 1e-9 {
		t.Fatal("rotation jumps at branch cut")
	}
	var got []GestureEvent
	tt := NewTester(func(c *Context) {
		Box(c).Fill().Gestures(GesturePinch|GestureRotation, func(ev GestureEvent) bool { got = append(got, ev); return true })
	}, 200, 200)
	defer tt.rt.close()
	tt.Pointer(InputPointerDown, touch(1), 20, 20)
	tt.Pointer(InputPointerDown, touch(2), 20, 20)
	tt.Pointer(InputPointerMove, touch(2), 40, 20)
	for _, ev := range got {
		if math.IsNaN(float64(ev.Scale)) || math.IsInf(float64(ev.Scale), 0) {
			t.Fatal("invalid scale")
		}
	}
	tt.rt.event(platform.SurfaceEvent{Kind: platform.SurfaceBlur})
}

func TestCaptureTransferAndRelease(t *testing.T) {
	var parent, child *Element
	var parentEvents, childEvents []InputKind
	tt := NewTester(func(c *Context) {
		parent = Column(c).Fill().HandleInput(func(ev InputEvent) bool { parentEvents = append(parentEvents, ev.Kind); return true })
		parent.Children(func() {
			child = Box(c).Size(80, 80).HandleInput(func(ev InputEvent) bool { childEvents = append(childEvents, ev.Kind); return true })
		})
	}, 200, 200)
	defer tt.rt.close()
	if child.CapturePointer(1) {
		t.Fatal("captured an inactive ID")
	}
	tt.Pointer(InputPointerDown, touch(1), 20, 20)
	if !parent.CapturePointer(1) {
		t.Fatal("transfer refused active contact")
	}
	tt.Pointer(InputPointerMove, touch(1), 160, 160)
	parent.ReleasePointer(1)
	tt.Pointer(InputPointerUp, touch(1), 160, 160)
	if countPointerKind(childEvents, InputPointerCaptureLost) != 1 || countPointerKind(parentEvents, InputPointerCaptureLost) != 1 || !slices.Contains(parentEvents, InputPointerMove) {
		t.Fatalf("transfer lifecycle: child%v parent%v", childEvents, parentEvents)
	}
}

func TestNativePanScrollHandlerAndTerminalDelta(t *testing.T) {
	var scroll ScrollState
	var deltas []float32
	tt := NewTester(func(c *Context) {
		Scroll(c).Size(140, 80).TrackScroll(&scroll).Children(func() {
			Box(c).Size(120, 300).HandleInput(func(ev InputEvent) bool {
				if ev.Kind == InputScroll {
					deltas = append(deltas, ev.DY)
					return true
				}
				return false
			})
		})
	}, 180, 120)
	defer tt.rt.close()
	tt.Gesture(GestureEvent{Kind: GesturePan, Phase: GestureBegin, Device: PointerTouchpad, X: 30, Y: 40, DY: -12})
	tt.Gesture(GestureEvent{Kind: GesturePan, Phase: GestureEnd, Device: PointerTouchpad, X: 30, Y: 40, DY: -3})
	if !slices.Equal(deltas, []float32{12, 3}) || scroll.Y != 0 || len(tt.rt.nativeGestures) != 0 {
		t.Fatalf("native scroll deltas=%v scroll=%v sessions=%v", deltas, scroll.Y, len(tt.rt.nativeGestures))
	}
}

func TestCancelledNativeGestureCannotRetarget(t *testing.T) {
	var got []GestureEvent
	show := true
	tt := NewTester(func(c *Context) {
		if show {
			Box(c).Fill().Gestures(GesturePinch, func(ev GestureEvent) bool { got = append(got, ev); return true })
		}
	}, 100, 100)
	defer tt.rt.close()
	tt.Gesture(GestureEvent{Kind: GesturePinch, Phase: GestureBegin, X: 20, Y: 20})
	show = false
	tt.Frame()
	show = true
	tt.Frame()
	tt.Gesture(GestureEvent{Kind: GesturePinch, Phase: GestureUpdate, X: 20, Y: 20, Scale: 2})
	tt.Gesture(GestureEvent{Kind: GesturePinch, Phase: GestureEnd, X: 20, Y: 20})
	if len(got) != 2 || got[1].Phase != GestureCancel {
		t.Fatalf("cancelled gesture restarted: %+v", got)
	}
}

func countPointerKind(kinds []InputKind, kind InputKind) int {
	n := 0
	for _, k := range kinds {
		if k == kind {
			n++
		}
	}
	return n
}

func TestRemovingInputHandlerCancelsCapture(t *testing.T) {
	handles := true
	var kinds []InputKind
	tt := NewTester(func(c *Context) {
		e := Box(c).Key("pad").Fill()
		if handles {
			e.HandleInput(func(ev InputEvent) bool { kinds = append(kinds, ev.Kind); return true })
		}
	}, 100, 100)
	defer tt.rt.close()
	tt.Press(20, 20)
	handles = false
	tt.Frame()
	if !slices.Contains(kinds, InputPointerCancel) || !slices.Contains(kinds, InputPointerCaptureLost) || tt.rt.contacts[0].fn != nil {
		t.Fatalf("removed callback retained capture: %v", kinds)
	}
}

func TestGestureUsesCurrentCallback(t *testing.T) {
	version := 0
	var versions []int
	tt := NewTester(func(c *Context) {
		v := version
		Box(c).Fill().Gestures(GesturePinch, func(ev GestureEvent) bool { versions = append(versions, v); return true })
	}, 100, 100)
	defer tt.rt.close()
	tt.Gesture(GestureEvent{Kind: GesturePinch, Phase: GestureBegin, X: 20, Y: 20})
	version = 1
	tt.Frame()
	tt.Gesture(GestureEvent{Kind: GesturePinch, Phase: GestureUpdate, X: 20, Y: 20, Scale: 1.2})
	tt.Gesture(GestureEvent{Kind: GesturePinch, Phase: GestureEnd, X: 20, Y: 20})
	if !slices.Equal(versions, []int{0, 1, 1}) {
		t.Fatalf("stale gesture callback: %v", versions)
	}
}

func TestCaptureTransferDuringDown(t *testing.T) {
	var parent *Element
	var parentMoves int
	tt := NewTester(func(c *Context) {
		parent = Column(c).Fill().HandleInput(func(ev InputEvent) bool {
			if ev.Kind == InputPointerMove {
				parentMoves++
			}
			return true
		})
		parent.Children(func() {
			Box(c).Size(70, 70).HandleInput(func(ev InputEvent) bool {
				if ev.Kind == InputPointerDown {
					parent.CapturePointer(ev.Pointer.ID)
				}
				return true
			})
		})
	}, 200, 200)
	defer tt.rt.close()
	tt.Pointer(InputPointerDown, touch(1), 20, 20)
	tt.Pointer(InputPointerMove, touch(1), 160, 150)
	tt.Pointer(InputPointerUp, touch(1), 160, 150)
	if parentMoves != 1 || tt.rt.pressed != nil || tt.rt.pointerX != 160 || tt.rt.pointerY != 150 {
		t.Fatalf("transferred press retained state: moves=%v pressed=%v position=%v/%v", parentMoves, tt.rt.pressed, tt.rt.pointerX, tt.rt.pointerY)
	}
}

func TestMouseCaptureWithMultipleButtons(t *testing.T) {
	var first, second []InputEvent
	tt := NewTester(func(c *Context) {
		Column(c).Children(func() {
			Box(c).Size(100, 80).HandleInput(func(ev InputEvent) bool { first = append(first, ev); return true })
			Box(c).Size(100, 80).HandleInput(func(ev InputEvent) bool { second = append(second, ev); return true })
		})
	}, 180, 180)
	defer tt.rt.close()
	for _, ev := range []platform.SurfaceEvent{
		{Kind: platform.PointerDown, X: 20, Y: 20, Button: 0},
		{Kind: platform.PointerDown, X: 20, Y: 120, Button: 1},
		{Kind: platform.PointerUp, X: 20, Y: 120, Button: 1, Pointer: PointerInfo{Contact: true}},
		{Kind: platform.PointerUp, X: 20, Y: 120, Button: 0},
	} {
		tt.send(ev)
	}
	var downs, ups, lost int
	for _, ev := range first {
		switch ev.Kind {
		case InputPointerDown:
			downs++
		case InputPointerUp:
			ups++
		case InputPointerCaptureLost:
			lost++
		}
	}
	for _, ev := range second {
		if ev.Kind == InputPointerDown || ev.Kind == InputPointerUp {
			t.Errorf("capture leaked to second handler: %+v", ev)
		}
	}
	if downs != 2 || ups != 2 || lost != 1 || tt.rt.pressed != nil {
		t.Fatalf("multi-button lifecycle: down/up/lost=%v/%v/%v", downs, ups, lost)
	}
}

func TestCompatibilityCancellationReleasesHeldButton(t *testing.T) {
	var released []int
	tt := NewTester(func(c *Context) {
		Box(c).Fill().HandleInput(func(ev InputEvent) bool {
			if ev.Kind == InputPointerUp {
				released = append(released, ev.Button)
			}
			return ev.Kind == InputPointerDown
		})
	}, 100, 100)
	defer tt.rt.close()
	tt.send(platform.SurfaceEvent{Kind: platform.PointerDown, Button: 1, X: 20, Y: 20})
	tt.send(platform.SurfaceEvent{Kind: platform.PointerMove, X: 30, Y: 30}) // GDK motion has no changed button
	tt.SetFocused(false)
	if !slices.Equal(released, []int{1}) {
		t.Fatalf("compatibility released %v", released)
	}
}
