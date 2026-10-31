package ui

import (
	"testing"
	"time"
)

// A finger pans what scrolls under it once it moves, and the press ends
// without a click; lifted as it moves, the content flings on and slows
// down to a stop.
func TestTouchPans(t *testing.T) {
	var s ScrollState
	clicks := 0
	tt := coreNewTester(func(c *context) {
		coreScroll(c).Fill().TrackScroll(&s).Children(func() {
			b := coreBox(c).Height(1000).Shrink(0)
			if b.Clicked() {
				clicks++
			}
		})
	}, 200, 200)
	// A tap clicks.
	tt.TouchDown(100, 150)
	tt.TouchUp(100, 150)
	tt.Frame()
	if clicks != 1 || s.Y != 0 {
		t.Fatalf("a tap: %d clicks, scrolled to %v", clicks, s.Y)
	}
	// Moving less than the slop is still a press.
	tt.Advance(time.Second)
	tt.TouchDown(100, 150)
	tt.TouchMove(100, 146)
	if s.Y != 0 {
		t.Errorf("within the slop: scrolled to %v", s.Y)
	}
	// Past it, the finger drags the content, which follows it from there.
	tt.TouchMove(100, 140)
	tt.Advance(16 * time.Millisecond)
	tt.TouchMove(100, 100)
	if s.Y != 40 {
		t.Errorf("dragged 40 up: scrolled to %v", s.Y)
	}
	tt.Advance(16 * time.Millisecond)
	tt.TouchMove(100, 60)
	tt.Advance(16 * time.Millisecond)
	tt.TouchUp(100, 20)
	tt.Frame()
	if clicks != 1 {
		t.Errorf("a pan clicked: %d clicks", clicks)
	}
	at := s.Y
	if at != 80 {
		t.Errorf("after the pan: at %v", at)
	}
	// It flings on, slower and slower, until it stops.
	tt.Advance(50 * time.Millisecond)
	if s.Y <= at {
		t.Errorf("no fling: at %v, was %v", s.Y, at)
	}
	tt.Advance(10 * time.Second)
	if tt.FramePending() || s.Y <= at || s.Y > 800 {
		t.Errorf("after the fling: at %v, frames pending %v", s.Y, tt.FramePending())
	}
	// A finger stops a fling and taps nothing.
	s.Y = 0
	tt.Frame()
	tt.Advance(time.Second)
	tt.TouchDown(100, 150)
	tt.Advance(16 * time.Millisecond)
	tt.TouchMove(100, 100)
	tt.Advance(16 * time.Millisecond)
	tt.TouchUp(100, 40)
	tt.Advance(16 * time.Millisecond)
	tt.TouchDown(100, 100)
	tt.TouchUp(100, 100)
	stopped := s.Y
	tt.Advance(time.Second)
	if s.Y != stopped || clicks != 1 {
		t.Errorf("a tap on a fling: moved from %v to %v, %d clicks", stopped, s.Y, clicks)
	}
}

// A scroll container reaching the bottom of the safe area goes on under
// the bottom inset, as UIKit's under a floating tab bar, and scrolls as
// far again; what does not scroll stays clear of the insets.
func TestScrollUnderBottomInset(t *testing.T) {
	var s ScrollState
	var box *node
	tt := coreNewTester(func(c *context) {
		coreColumn(c).Fill().Children(func() {
			box = coreBox(c).Height(20).Shrink(0)
			coreScroll(c).Grow(1).TrackScroll(&s).Children(func() {
				coreBox(c).Height(1000).Shrink(0)
			})
		})
	}, 200, 400)
	tt.SetInsets(30, 0, 50, 0)
	if box.y != 30 {
		t.Errorf("the box is at %v, under the top inset", box.y)
	}
	s.Y = 10000
	tt.Frame()
	tt.Frame()
	// 400 high: 30 above, 20 of box, the scroll from 50 to the bottom
	// edge, 350 high, whose end comes out 50 above it.
	if want := float32(1000 - (400 - 50 - 50)); s.Y != want {
		t.Errorf("scrolled to the end at %v, want %v", s.Y, want)
	}
}
