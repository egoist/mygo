package ui

import (
	"testing"
	"time"
)

func TestApplicationMotionPreferencesOverrideAndReset(t *testing.T) {
	var got Preferences
	var override *MotionPreferences
	tt := coreNewTester(func(c *context) { c.SetMotionPreferences(override); got = c.Preferences() }, 200, 200)
	tt.SetPreferences(Preferences{ReduceMotion: true, TextScale: 1})
	if !got.ReduceMotion {
		t.Fatal("nil override lost desktop reduced motion")
	}
	override = &MotionPreferences{Motion: MotionFull, SmoothScrolling: true}
	tt.Frame()
	if got.ReduceMotion {
		t.Fatal("full motion did not override the desktop")
	}
	override.Motion = MotionSystem
	tt.Frame()
	if !got.ReduceMotion {
		t.Fatal("system motion did not restore desktop preference")
	}
	tt.SetPreferences(Preferences{TextScale: 1})
	override.Motion = MotionReduced
	tt.Frame()
	if !got.ReduceMotion {
		t.Fatal("application reduced motion did not apply")
	}
	override = nil
	tt.Frame()
	if got.ReduceMotion {
		t.Fatal("nil override did not restore desktop preference")
	}
}

func TestApplicationSmoothScrollingAndPendingGlide(t *testing.T) {
	var scroll ScrollState
	settings := MotionPreferences{Motion: MotionSystem, SmoothScrolling: false}
	tt := coreNewTester(func(c *context) {
		c.SetMotionPreferences(&settings)
		coreScroll(c).Fill().TrackScroll(&scroll).Children(func() { coreBox(c).Height(1000).Shrink(0) })
	}, 200, 200)
	tt.Move(100, 100)
	tt.Wheel(100, 100, 0, 100)
	if scroll.Y != 100 {
		t.Fatalf("disabled smooth scrolling moved to %g, want 100", scroll.Y)
	}
	settings.SmoothScrolling = true
	tt.Frame()
	tt.Wheel(100, 100, 0, 100)
	tt.Advance(50 * time.Millisecond)
	if scroll.Y <= 100 || scroll.Y >= 200 {
		t.Fatalf("enabled glide offset = %g", scroll.Y)
	}
	settings.SmoothScrolling = false
	tt.Frame()
	if scroll.Y != 200 {
		t.Fatalf("turning glide off did not finish its target: %g", scroll.Y)
	}
	for _, state := range tt.rt.states {
		if state.wheel != nil {
			t.Fatal("turning glide off left a wheel animation alive")
		}
	}
	tt.Advance(time.Second)
	if scroll.Y != 200 {
		t.Fatalf("a stopped glide continued: %g", scroll.Y)
	}
}

func TestApplicationReducedMotionWinsOverSmoothScrolling(t *testing.T) {
	var scroll ScrollState
	tt := coreNewTester(func(c *context) {
		c.SetMotionPreferences(&MotionPreferences{Motion: MotionReduced, SmoothScrolling: true})
		coreScroll(c).Fill().TrackScroll(&scroll).Children(func() { coreBox(c).Height(1000).Shrink(0) })
	}, 200, 200)
	tt.Move(100, 100)
	tt.Wheel(100, 100, 0, 100)
	if scroll.Y != 100 {
		t.Fatalf("reduced motion did not disable glide: %g", scroll.Y)
	}
}

func TestMotionPreferencesPublicGeneratedAPI(t *testing.T) {
	var got Preferences
	NewTester(func(c *Context) {
		c.SetMotionPreferences(&MotionPreferences{Motion: MotionReduced, SmoothScrolling: false})
		got = c.Preferences()
	}, 200, 200)
	if !got.ReduceMotion {
		t.Fatal("generated Context API did not apply motion preferences")
	}
}
