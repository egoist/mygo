//go:build darwin && !ios

package gpusurface

import (
	"testing"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

type cgPoint struct{ X, Y float64 }

var hitTest func(obj uintptr, sel objc.SEL, p cgPoint) uintptr

func layerHidden(l uintptr) bool { return objc.Send[bool](objc.ID(l), selIsHidden) }

func viewHidden(v uintptr) bool { return objc.Send[bool](objc.ID(v), objc.RegisterName("isHidden")) }

// TestDirectToggleRestores turns direct mode on and off 100 times: on, the
// drawables have the screen's size and the other views' layers are hidden
// while the views themselves still take the mouse; off, every layer is as
// it was, one hidden before staying hidden, and the drawables have their
// size again.
func TestDirectToggleRestores(t *testing.T) {
	win, content, ui := testWindow(t, 400, 300)
	if hitTest == nil {
		purego.RegisterFunc(&hitTest, lib.msgSend)
	}
	var other uintptr
	var s *Surface
	var err error
	onMain(func() {
		other = lib.msgInitRect(send(class("NSView"), "alloc"), objc.RegisterName("initWithFrame:"), cgRect{W: 50, H: 50})
		send(other, "setWantsLayer:", true)
		send(content, "addSubview:", other)
		send(class("CATransaction"), "flush")
		objc.ID(send(other, "layer")).Send(selSetHidden, true)
		s, err = New(Options{Window: win, Composite: true})
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { onMain(func() { s.Close(); send(other, "release") }) })
	var sw, sh int
	onMain(func() { sw, sh = s.ScreenSize() })
	if sw <= 0 || sh <= 0 {
		t.Fatalf("ScreenSize %dx%d", sw, sh)
	}
	w0, h0 := s.Size()
	var hiddenBefore bool
	onMain(func() { hiddenBefore = layerHidden(send(other, "layer")) })
	if !hiddenBefore {
		t.Fatal("the other view's layer is not hidden to begin with")
	}
	for i := range 100 {
		onMain(func() {
			uiLayer, otherLayer := send(ui, "layer"), send(other, "layer")
			if err := s.SetDirect(true); err != nil {
				t.Errorf("SetDirect(true): %v", err)
				return
			}
			if w, h := s.Size(); !s.Direct() || w != sw || h != sh {
				t.Errorf("on: Direct %v, drawables %dx%d, screen %dx%d", s.Direct(), w, h, sw, sh)
			}
			if !layerHidden(uiLayer) || !layerHidden(otherLayer) || layerHidden(s.layer) {
				t.Errorf("on: UI layer hidden %v, other %v, surface %v", layerHidden(uiLayer), layerHidden(otherLayer), layerHidden(s.layer))
			}
			if viewHidden(ui) {
				t.Error("on: the UI view itself is hidden")
			}
			if v := hitTest(content, objc.RegisterName("hitTest:"), cgPoint{200, 150}); v != ui {
				t.Errorf("on: a click at the window's centre goes to %#x, not the UI view %#x", v, ui)
			}
			if err := s.SetDirect(false); err != nil {
				t.Errorf("SetDirect(false): %v", err)
				return
			}
			if w, h := s.Size(); s.Direct() || w != w0 || h != h0 {
				t.Errorf("off: Direct %v, drawables %dx%d, want %dx%d", s.Direct(), w, h, w0, h0)
			}
			if layerHidden(uiLayer) || !layerHidden(otherLayer) {
				t.Errorf("off: UI layer hidden %v, the other (hidden before) %v", layerHidden(uiLayer), layerHidden(otherLayer))
			}
		})
		if t.Failed() {
			t.Fatalf("round %d", i)
		}
	}
}

// TestDirectHidesViewAddedJustBefore: a view added in the same main
// thread turn as SetDirect has not had its first commit, which would show
// its layer again; it stays hidden.
func TestDirectHidesViewAddedJustBefore(t *testing.T) {
	win, content, _ := testWindow(t, 400, 300)
	var s *Surface
	var err error
	onMain(func() { s, err = New(Options{Window: win, Composite: true}) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { onMain(s.Close) })
	var late uintptr
	var hidden bool
	onMain(func() {
		late = lib.msgInitRect(send(class("NSView"), "alloc"), objc.RegisterName("initWithFrame:"), cgRect{W: 50, H: 50})
		send(late, "setWantsLayer:", true)
		send(content, "addSubview:", late)
		if err = s.SetDirect(true); err != nil {
			return
		}
		send(class("CATransaction"), "flush")
		hidden = layerHidden(send(late, "layer"))
		err = s.SetDirect(false)
		send(late, "removeFromSuperview")
		send(late, "release")
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hidden {
		t.Fatal("the layer of a view added just before SetDirect(true) shows")
	}
}

// TestDirectNeedsComposite: a surface owning the content view's layer has
// no view of its own to make the only one shown.
func TestDirectNeedsComposite(t *testing.T) {
	win, _, _ := testWindow(t, 200, 100)
	var s *Surface
	var err error
	onMain(func() { s, err = New(Options{Window: win}) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { onMain(s.Close) })
	onMain(func() { err = s.SetDirect(true) })
	if err == nil || s.Direct() {
		t.Fatalf("SetDirect(true) without Composite: %v, Direct %v", err, s.Direct())
	}
}

// TestPresentAllocsNothingWithOverlay: with an overlay of a pill's size
// set, in windowed and in direct mode, neither Present nor SetOverlay
// allocates.
func TestPresentAllocsNothingWithOverlay(t *testing.T) {
	frame := testIOSurface(t, 64, 48)
	win, _, _ := testWindow(t, 400, 300)
	var s *Surface
	var err error
	onMain(func() { s, err = New(Options{Window: win, Composite: true}) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { onMain(s.Close) })
	atlas := make([]byte, 64*16)
	for i := range atlas {
		atlas[i] = byte(i)
	}
	if err := s.SetOverlayAtlas(atlas, 64, 16); err != nil {
		t.Fatal(err)
	}
	rects := make([]OverlayRect, 40)
	for i := range rects {
		rects[i] = OverlayRect{X: float32(10 + 8*i), Y: 12, W: 7, H: 14, Color: [4]float32{0.95, 0.94, 0.92, 1}, UV: [4]float32{0, 0, 0.1, 1}}
	}
	rects[0] = OverlayRect{X: 4, Y: 4, W: 380, H: 32, Radius: 16, Color: [4]float32{0.05, 0.06, 0.07, 0.85}}
	present := func() {
		if err := s.PresentTagged(frame, 1); err != nil && err != errNoDrawable {
			t.Error(err)
		}
	}
	s.SetOverlay(rects)
	for range 4 {
		present()
	}
	if n := testing.AllocsPerRun(100, func() { s.SetOverlay(rects) }); n != 0 {
		t.Errorf("SetOverlay allocates %.1f times", n)
	}
	if n := testing.AllocsPerRun(100, present); n != 0 {
		t.Errorf("PresentTagged with an overlay allocates %.1f times", n)
	}
	onMain(func() { err = s.SetDirect(true) })
	if err != nil {
		t.Fatal(err)
	}
	present()
	if n := testing.AllocsPerRun(100, present); n != 0 {
		t.Errorf("PresentTagged in direct mode, pillarboxed, with an overlay allocates %.1f times", n)
	}
	onMain(func() { err = s.SetDirect(false) })
	if err != nil {
		t.Fatal(err)
	}
}

func TestOverlayAtlasRejectsShortPixels(t *testing.T) {
	win, _, _ := testWindow(t, 200, 100)
	var s *Surface
	var err error
	onMain(func() { s, err = New(Options{Window: win, Composite: true}) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { onMain(s.Close) })
	if err := s.SetOverlayAtlas(make([]byte, 10), 4, 4); err == nil {
		t.Fatal("a 4x4 atlas from 10 bytes was taken")
	}
}
