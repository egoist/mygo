package main

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestPointerDemo(t *testing.T) {
	g := &gallery{}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Gap(12).Padding(20).Children(func() { g.inputPage(c) }) }, 760, 650)
	r, ok := tt.Find("Zoom 100% · rotation 0° · pan 0 / 0")
	if !ok {
		t.Fatal("demo telemetry not rendered")
	}
	// The pad is immediately above the telemetry label.
	x, y := r.X+100, r.Y-80
	tt.Press(x, y)
	tt.Move(x+20, y+15)
	tt.Release(x+20, y+15)
	if g.pointer.x != 20 || g.pointer.y != 15 {
		t.Fatalf("demo mouse pan %v/%v", g.pointer.x, g.pointer.y)
	}
	tt.Gesture(ui.GestureEvent{Kind: ui.GesturePinch | ui.GestureRotation, Phase: ui.GestureBegin, Device: ui.PointerTouchpad, X: x, Y: y})
	tt.Gesture(ui.GestureEvent{Kind: ui.GesturePinch | ui.GestureRotation, Phase: ui.GestureUpdate, Device: ui.PointerTouchpad, X: x, Y: y, Scale: 1.5, Rotation: math.Pi / 4})
	tt.Gesture(ui.GestureEvent{Kind: ui.GesturePinch | ui.GestureRotation, Phase: ui.GestureEnd, Device: ui.PointerTouchpad, X: x, Y: y})
	if g.pointer.scale != 1.5 || math.Abs(float64(g.pointer.rotation)-math.Pi/4) > .001 {
		t.Fatalf("demo transform %+v", g.pointer)
	}
	if err := tt.Click("Reset pad"); err != nil {
		t.Fatal(err)
	}
	if g.pointer.x != 0 || g.pointer.y != 0 || g.pointer.scale != 1 {
		t.Fatal("demo reset failed")
	}
}
