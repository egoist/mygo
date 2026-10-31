package ui

import (
	"testing"
	"time"

	"github.com/egoist/mygo/internal/platform"
)

func TestCollapsibleAnimationKeepsDefaultDurationAndAllowsCustomCurve(t *testing.T) {
	var defaultOpen, customOpen bool
	var standard, custom float32
	tt, now := motionTester(func(c *context) {
		coreColumn(c).Children(func() {
			a := coreCollapsibleBase(c, &defaultOpen)
			a.Trigger.Label("Default").Children(func() { coreText(c, "Default") })
			standard = a.Progress()
			b := coreCollapsibleBaseWithAnimation(c, &customOpen, Animation{Duration: 140 * time.Millisecond, Ease: Linear})
			b.Trigger.Label("Custom").Children(func() { coreText(c, "Custom") })
			custom = b.Progress()
		})
	})
	tt.Click("Default")
	tt.Click("Custom")
	*now = now.Add(70 * time.Millisecond)
	tt.Frame()
	closeFloat(t, standard, EaseOut(.35))
	closeFloat(t, custom, .5)
	*now = now.Add(70 * time.Millisecond)
	tt.Frame()
	closeFloat(t, standard, EaseOut(.7))
	closeFloat(t, custom, 1)
	if standard >= 1 || !tt.rt.animating {
		t.Fatal("custom duration changed the default disclosure's 200 ms motion")
	}
	*now = now.Add(60 * time.Millisecond)
	tt.Frame()
	closeFloat(t, standard, 1)
	if tt.rt.animating || tt.FramePending() {
		t.Fatal("completed disclosures kept a frame loop running")
	}
}

func TestCollapsibleCustomAnimationReversesContinuouslyAndParks(t *testing.T) {
	open := false
	var progress float32
	tt, now := motionTester(func(c *context) {
		p := coreCollapsibleBaseWithAnimation(c, &open, Animation{Duration: 140 * time.Millisecond, Ease: EaseOut})
		p.Trigger.Label("Custom").Children(func() { coreText(c, "Custom") })
		progress = p.Progress()
		p.Panel(func() { coreText(c, "Inside") })
	})
	tt.Click("Custom")
	*now = now.Add(50 * time.Millisecond)
	tt.Frame()
	opening := progress
	if opening <= 0 || opening >= 1 {
		t.Fatalf("opening progress=%v", opening)
	}
	tt.Click("Custom")
	closeFloat(t, progress, opening)
	*now = now.Add(40 * time.Millisecond)
	tt.Frame()
	closing := progress
	if closing <= 0 || closing >= opening || !tt.HasText("Inside") {
		t.Fatalf("closing lost content or failed to move: %v", closing)
	}
	tt.Click("Custom")
	closeFloat(t, progress, closing)
	*now = now.Add(140 * time.Millisecond)
	tt.Frame()
	if progress != 1 || tt.rt.animating || tt.FramePending() {
		t.Fatalf("140 ms animation did not park: progress=%v moving=%v requested=%v", progress, tt.rt.animating, tt.FramePending())
	}
	tt.Click("Custom")
	*now = now.Add(140 * time.Millisecond)
	tt.Frame()
	if progress != 0 || tt.HasText("Inside") || tt.rt.animating || tt.FramePending() {
		t.Fatalf("closed disclosure did not park: progress=%v content=%v moving=%v", progress, tt.HasText("Inside"), tt.rt.animating)
	}
	// No unused default 200 ms animation may schedule a later rebuild.
	*now = now.Add(time.Second)
	tt.Frame()
	if tt.rt.animating || tt.FramePending() {
		t.Fatal("unused default animation scheduled after custom animation completed")
	}
}

func TestCollapsibleCustomAnimationKeepsNativeInputAndReducedMotion(t *testing.T) {
	open, changes := false, 0
	var progress float32
	tt := coreNewTester(func(c *context) {
		coreColumn(c).Children(func() {
			p := coreCollapsibleBaseWithAnimation(c, &open, Animation{Duration: 140 * time.Millisecond, Ease: EaseOut})
			p.Trigger.Label("Custom").Children(func() { coreText(c, "Custom") })
			if p.Trigger.Changed() {
				changes++
			}
			progress = p.Progress()
			p.Panel(func() { coreButton(c, "Inside") })
		})
	}, 300, 200)
	tt.send(platform.SurfaceEvent{Kind: platform.AccessibilityOn})
	if n := accessNode(t, tt.h.access, platform.RoleDisclosure, "Custom"); n.States&platform.AccessExpanded != 0 || n.Actions&platform.ActionPress == 0 {
		t.Fatalf("closed disclosure accessibility=%+v", n)
	}
	tt.Click("Custom")
	tt.Advance(40 * time.Millisecond)
	if progress <= 0 || progress >= 1 {
		t.Fatalf("custom motion failed to begin: %v", progress)
	}
	tt.SetPreferences(Preferences{ReduceMotion: true})
	tt.Frame()
	if progress != 1 || tt.rt.animating || tt.FramePending() {
		t.Fatalf("reduced motion failed to finish/park: progress=%v moving=%v", progress, tt.rt.animating)
	}
	tt.Key(0, KeyTab)
	if !tt.Focused("Inside") {
		t.Fatal("custom disclosure lost native content keyboard traversal")
	}
	tt.Key(Shift, KeyTab)
	tt.Key(0, KeySpace)
	if open || progress != 0 || tt.HasText("Inside") {
		t.Fatal("Space did not close custom disclosure with reduced motion")
	}
	tt.Key(0, KeyEnter)
	if !open || progress != 1 || changes != 3 {
		t.Fatalf("native activation changed=%d open=%v progress=%v", changes, open, progress)
	}
	if n := accessNode(t, tt.h.access, platform.RoleDisclosure, "Custom"); n.States&platform.AccessExpanded == 0 {
		t.Fatalf("open disclosure accessibility=%+v", n)
	}
}

func TestPublicCollapsibleCustomAnimationFacade(t *testing.T) {
	open := false
	var progress float32
	tt := NewTester(func(c *Context) {
		c.DisableAnimations()
		parts := CollapsibleBaseWithAnimation(c, &open, Animation{Duration: 140 * time.Millisecond, Ease: EaseOut})
		parts.Trigger.Label("Public custom").Children(func() { Text(c, "Public custom") })
		progress = parts.Progress()
		parts.Panel(func() { Text(c, "Public content") })
	}, 300, 160)
	if err := tt.Click("Public custom"); err != nil {
		t.Fatal(err)
	}
	if !open || progress != 1 || !tt.HasText("Public content") || tt.FramePending() {
		t.Fatalf("public custom disclosure failed: open=%v progress=%v content=%v", open, progress, tt.HasText("Public content"))
	}
}
