package ui

import (
	"testing"
	"time"
)

// ticker returns tick, which moves the clock of a clockTester by d and
// builds a frame.
func ticker(tt *Tester, now *time.Time) (tick func(d time.Duration)) {
	return func(d time.Duration) {
		*now = now.Add(d)
		tt.Frame()
	}
}

func TestTooltipBase(t *testing.T) {
	var save, tip *node
	tt, now := clockTester(func(c *context) {
		coreColumn(c).Padding(80, 40).Gap(20).Children(func() {
			save = coreButton(c, "Save").Label("save")
			tip = coreTooltipBase(c, save, func(tip *node) {
				tip.Padding(4).Children(func() { coreText(c, "Saves the file") })
			})
			coreButton(c, "Open").Label("open").Tooltip("Opens a file")
		})
	}, 400, 300)
	tick := ticker(tt, now)
	s, _ := tt.Find("save")
	tt.Move(s.X+s.W/2, s.Y+s.H/2)
	tick(500 * time.Millisecond)
	if tt.HasText("Saves the file") || tip != nil {
		t.Fatal("the tip shows before the pointer rested 0.6 seconds")
	}
	tick(150 * time.Millisecond)
	if !tt.HasText("Saves the file") || tip == nil {
		t.Fatal("no tip once the pointer rested")
	}
	// Above the anchor, centered.
	tt.Frame()
	if r := tip.Bounds(); r.Y+r.H > s.Y+0.5 || abs32(r.X+r.W/2-(s.X+s.W/2)) > 0.5 {
		t.Errorf("the tip is at %v, not above the anchor %v", r, s)
	}
	// Moving to the next one shows its tip at once.
	o, _ := tt.Find("open")
	tt.Move(o.X+o.W/2, o.Y+o.H/2)
	tick(16 * time.Millisecond)
	if !tt.HasText("Opens a file") || tt.HasText("Saves the file") {
		t.Errorf("moving along: texts %q", tt.Texts())
	}
	// Not once the tips have been gone a while.
	tt.Move(390, 290)
	tick(time.Second)
	tt.Move(s.X+s.W/2, s.Y+s.H/2)
	tick(16 * time.Millisecond)
	if tt.HasText("Saves the file") {
		t.Error("the tip shows at once, long after the last one went")
	}
	// Escape hides it until the pointer leaves.
	tick(time.Second)
	tt.Key(0, KeyEscape)
	tick(time.Second)
	if tt.HasText("Saves the file") {
		t.Error("the tip shows after Escape")
	}
	tt.Move(390, 290)
	tt.Frame()
	tt.Move(s.X+s.W/2, s.Y+s.H/2)
	tick(time.Second)
	if !tt.HasText("Saves the file") {
		t.Error("no tip as the pointer comes back after Escape")
	}
}

func TestTooltipFocus(t *testing.T) {
	disabled := false
	tt, now := clockTester(func(c *context) {
		coreColumn(c).Padding(40).Gap(20).Children(func() {
			coreButton(c, "Save").Tooltip("Saves the file").Disabled(disabled)
			coreButton(c, "Open").Tooltip("Opens a file")
		})
	}, 400, 300)
	tick := ticker(tt, now)
	// The keyboard focus shows the tip at once, above the element.
	tt.Key(0, KeyTab)
	if !tt.HasText("Saves the file") {
		t.Fatalf("no tip for the focus; texts %q", tt.Texts())
	}
	b, _ := tt.Find("Save")
	if r, _ := tt.Find("Saves the file"); r.Y+r.H > b.Y {
		t.Errorf("the tip of the focus is at %v, not above %v", r, b)
	}
	tt.Key(0, KeyTab)
	if tt.HasText("Saves the file") || !tt.HasText("Opens a file") {
		t.Errorf("after Tab: texts %q", tt.Texts())
	}
	// The pointer coming to another element later takes the tip.
	tick(time.Second)
	tt.Move(b.X+b.W/2, b.Y+b.H/2)
	tick(time.Second)
	if !tt.HasText("Saves the file") || tt.HasText("Opens a file") {
		t.Errorf("the pointer after the focus: texts %q", tt.Texts())
	}
	// Escape hides the tip of the focus until the focus leaves.
	tt.Move(390, 290)
	tick(time.Second)
	if !tt.HasText("Opens a file") {
		t.Fatalf("the tip of the focus does not come back; texts %q", tt.Texts())
	}
	tt.Key(0, KeyEscape)
	if tt.HasText("Opens a file") {
		t.Error("the tip of the focus shows after Escape")
	}
	tt.Key(Shift, KeyTab)
	tt.Key(0, KeyTab)
	if !tt.HasText("Opens a file") {
		t.Error("no tip as the focus comes back after Escape")
	}
	// A disabled element under the pointer still shows its tip, the
	// pointer coming to it after the focus came to Open.
	disabled = true
	tick(time.Second)
	tt.Move(b.X+b.W/2, b.Y+b.H/2)
	tick(time.Second)
	if !tt.HasText("Saves the file") {
		t.Errorf("a disabled element under the pointer shows no tip; texts %q", tt.Texts())
	}
}

// TestTooltipDisabled checks that a disabled element shows its tip under
// the pointer, as AppKit's and GTK's do, to tell why it is disabled, while
// it still takes no click and no focus: Tab passes it, and an element
// disabled while it has the focus shows no tip for it.
func TestTooltipDisabled(t *testing.T) {
	disabled, clicks, off, reach := true, 0, false, false
	tt, now := clockTester(func(c *context) {
		coreColumn(c).Padding(40).Gap(20).Children(func() {
			coreButton(c, "Open").Tooltip("Opens a file").Disabled(off)
			if coreButton(c, "Commit").Tooltip("Stage changes to commit").Disabled(disabled && !reach).AccessDisabled(reach).Clicked() {
				clicks++
			}
			coreFieldset(c, "Remote", func() {
				coreButton(c, "Push").Tooltip("Nothing to push")
			}).Disabled(disabled)
			coreBox(c).Padding(8).Tooltip("Ask for access").Children(func() {
				coreButton(c, "Delete").Tooltip("Delete the branch").Disabled(true)
			})
		})
	}, 400, 400)
	tick := ticker(tt, now)
	b, _ := tt.Find("Commit")
	tt.Move(b.X+b.W/2, b.Y+b.H/2)
	tick(500 * time.Millisecond)
	if tt.HasText("Stage changes to commit") {
		t.Fatal("the tip shows before the pointer rested")
	}
	tick(150 * time.Millisecond)
	if !tt.HasText("Stage changes to commit") {
		t.Fatalf("no tip on the disabled button; texts %q", tt.Texts())
	}
	// A press takes no click and hides the tip, as on an enabled one.
	tt.ClickAt(b.X+b.W/2, b.Y+b.H/2)
	tick(time.Second)
	if clicks != 0 || tt.Focused("Commit") {
		t.Fatalf("the disabled button took the click (%d) or the focus", clicks)
	}
	if tt.HasText("Stage changes to commit") {
		t.Error("the tip shows after a press")
	}
	// Inside a disabled Fieldset too.
	p, _ := tt.Find("Push")
	tt.Move(p.X+p.W/2, p.Y+p.H/2)
	tick(time.Second)
	if !tt.HasText("Nothing to push") {
		t.Errorf("no tip in a disabled fieldset; texts %q", tt.Texts())
	}
	// Over elements inside one another, the innermost one's tip shows,
	// disabled or not: a reason belongs on the disabled element itself.
	d, _ := tt.Find("Delete")
	tt.Move(d.X+d.W/2, d.Y+d.H/2)
	tick(time.Second)
	if !tt.HasText("Delete the branch") || tt.HasText("Ask for access") {
		t.Errorf("a disabled element inside one with a tip; texts %q", tt.Texts())
	}
	// The keyboard passes it.
	tt.Move(390, 390)
	tick(time.Second)
	for i := range 3 {
		tt.Key(0, KeyTab)
		tick(time.Second)
		if tt.Focused("Commit") || tt.HasText("Stage changes to commit") {
			t.Fatalf("Tab %d reached the disabled button; texts %q", i+1, tt.Texts())
		}
	}
	if !tt.Focused("Open") {
		t.Error("Tab left the focus off Open, the one element it can reach")
	}
	// An element disabled with the focus shows no tip for it, from the
	// frame after the one that disabled it.
	off = true
	tick(time.Second)
	tick(time.Second)
	if tt.HasText("Opens a file") {
		t.Error("an element disabled with the focus shows its tip for it")
	}
	// AccessDisabled keeps it in the Tab order, its tip showing for the
	// focus.
	off = false
	reach = true
	tick(time.Second)
	tt.Key(0, KeyTab)
	tick(time.Second)
	if !tt.Focused("Commit") || !tt.HasText("Stage changes to commit") {
		t.Errorf("AccessDisabled: focus on Commit %v; texts %q", tt.Focused("Commit"), tt.Texts())
	}
	reach = false
	tick(time.Second)
	// Assistive technology reads the tip as its description, disabled.
	if info, ok := tt.Access("Commit"); !ok || info.Description != "Stage changes to commit" || !info.Disabled {
		t.Errorf("the disabled button for assistive technology: %+v", info)
	}
}

// TestTooltipDisabledMenuItemAndHistory checks the disabled elements
// MyGo draws itself with tips of their own: a drawn menu's disabled item,
// and the router's Back and Forward buttons at either end of the history.
func TestTooltipDisabledMenuItemAndHistory(t *testing.T) {
	t.Run("menu item", func(t *testing.T) {
		const tip = "Select a query to run it"
		tt, now := clockTester(func(c *context) {
			coreRow(c).Padding(20).Children(func() { coreText(c, "Report.pdf") }).PopoverMenu(func(m *Menu) {
				m.Item("Open")
				m.Item("Run").Disabled(true).Tooltip(tip)
			})
		}, 400, 300)
		tick := ticker(tt, now)
		if err := tt.RightClick("Report.pdf"); err != nil {
			t.Fatal(err)
		}
		r, ok := tt.Find("Run")
		if !ok {
			t.Fatal("no drawn item Run")
		}
		tt.Move(r.X+r.W/2, r.Y+r.H/2)
		tick(700 * time.Millisecond)
		if !tt.HasText(tip) {
			t.Fatalf("no tip on the disabled item; texts %q", tt.Texts())
		}
	})
	t.Run("history", func(t *testing.T) {
		r := NewRouter("/")
		r.Transition = TransitionNone
		tt, now := clockTester(func(c *context) {
			coreRow(c).Padding(40).Children(func() {
				coreBackButton(c, r)
				coreForwardButton(c, r)
			})
		}, 400, 300)
		tick := ticker(tt, now)
		// The buttons' labels are among the texts; the tip adds its own.
		shown := func(label string) bool {
			n := 0
			for _, s := range tt.Texts() {
				if s == label {
					n++
				}
			}
			return n > 1
		}
		for _, label := range []string{"Back", "Forward"} {
			b, ok := tt.Find(label)
			if !ok {
				t.Fatalf("no %s button", label)
			}
			tt.Move(390, 290)
			tick(time.Second)
			if shown(label) {
				t.Fatalf("the %s tip shows away from it; texts %q", label, tt.Texts())
			}
			tt.Move(b.X+b.W/2, b.Y+b.H/2)
			tick(time.Second)
			if !shown(label) {
				t.Errorf("no tip on the disabled %s button; texts %q", label, tt.Texts())
			}
		}
	})
}

// TestTooltipAnchored checks that the tip of Tooltip goes by its element,
// not by the pointer: above it, centered, wherever the pointer rests on it,
// and below it where there is no room above.
func TestTooltipAnchored(t *testing.T) {
	tt, now := clockTester(func(c *context) {
		coreColumn(c).Children(func() {
			coreButton(c, "Top").Label("top").Tooltip("At the top")
			coreBox(c).Height(100)
			coreButton(c, "A wide button, to rest on its ends").Label("wide").Tooltip("Wide")
		})
	}, 400, 300)
	tick := ticker(tt, now)
	w, _ := tt.Find("wide")
	var at Rect
	for i, x := range []float32{w.X + 4, w.X + w.W - 4} {
		tt.Move(390, 290)
		tick(time.Second)
		tt.Move(x, w.Y+w.H-2)
		tick(time.Second)
		tt.Frame()
		r, ok := tt.Find("Wide")
		if !ok {
			t.Fatalf("no tip with the pointer at %v; texts %q", x, tt.Texts())
		}
		if r.Y+r.H > w.Y || abs32(r.X+r.W/2-(w.X+w.W/2)) > 12 {
			t.Errorf("with the pointer at %v, the tip's text is at %v, not above the middle of %v", x, r, w)
		}
		if i > 0 && r != at {
			t.Errorf("the tip moved with the pointer: %v, then %v", at, r)
		}
		at = r
	}
	// No room above: below it.
	tt.Move(390, 290)
	tick(time.Second)
	b, _ := tt.Find("top")
	tt.Move(b.X+b.W/2, b.Y+b.H/2)
	tick(time.Second)
	tt.Frame()
	if r, ok := tt.Find("At the top"); !ok || r.Y < b.Y+b.H {
		t.Errorf("the tip of an element at the window's top is at %v, not below %v", r, b)
	}
}

// TestTooltipTakesNoWheel checks that the wheel over a tip scrolls what
// shows under it: the tip takes no pointer, its text included.
func TestTooltipTakesNoWheel(t *testing.T) {
	var s ScrollState
	tt := coreNewTester(func(c *context) {
		coreScroll(c).Fill().TrackScroll(&s).Children(func() {
			coreBox(c).Height(80).Shrink(0)
			coreButton(c, "Save").Label("save").Tooltip("Saves the file")
			coreBox(c).Height(1000).Shrink(0)
		})
	}, 300, 300)
	// The keyboard focus shows the tip, which stays as the pointer comes
	// to it.
	tt.Key(0, KeyTab)
	tip, ok := tt.Find("Saves the file")
	if !ok {
		t.Fatalf("no tip for the focus; texts %q", tt.Texts())
	}
	x, y := tip.X+tip.W/2, tip.Y+tip.H/2
	tt.Move(x, y)
	if !tt.HasText("Saves the file") {
		t.Fatalf("the tip went as the pointer came to it; texts %q", tt.Texts())
	}
	tt.Wheel(x, y, 0, 100)
	tt.Advance(time.Second)
	if s.Y == 0 {
		t.Error("the wheel over the tip did not scroll the content under it")
	}
}

func TestTooltipInnermost(t *testing.T) {
	tt, now := clockTester(func(c *context) {
		coreRow(c).Padding(20).Tooltip("The toolbar").Children(func() {
			coreButton(c, "Cut").Label("cut").Tooltip("Cut the selection")
		})
	}, 300, 200)
	tick := ticker(tt, now)
	b, _ := tt.Find("cut")
	tt.Move(b.X+b.W/2, b.Y+b.H/2)
	tick(time.Second)
	if !tt.HasText("Cut the selection") || tt.HasText("The toolbar") {
		t.Errorf("over the button: texts %q", tt.Texts())
	}
	tt.Move(b.X+b.W+10, b.Y+b.H/2)
	tick(time.Second)
	if tt.HasText("Cut the selection") || !tt.HasText("The toolbar") {
		t.Errorf("over the toolbar: texts %q", tt.Texts())
	}
}

// TestTooltipDelay checks that the theme's TooltipDelay sets how long the
// pointer rests on an element before its tip shows, and the time within
// which the next one shows at once, for the elements built with it.
func TestTooltipDelay(t *testing.T) {
	var delay time.Duration
	tt, now := clockTester(func(c *context) {
		th := *c.Theme()
		th.TooltipDelay = delay
		coreColumn(c).Padding(80, 40).Gap(20).Children(func() {
			coreButton(c, "Default").Label("default").Tooltip("The default delay")
			coreThemed(c, &th).Gap(20).Children(func() {
				coreButton(c, "Save").Label("save").Tooltip("Saves the file")
				coreButton(c, "Open").Label("open").Tooltip("Opens a file")
			})
		})
	}, 400, 400)
	tick := ticker(tt, now)
	rest := func(label string) {
		r, _ := tt.Find(label)
		tt.Move(r.X+r.W/2, r.Y+r.H/2)
		tt.Frame()
	}
	away := func() {
		tt.Move(390, 390)
		tick(10 * time.Second)
	}
	// A shorter delay.
	delay = 200 * time.Millisecond
	tt.Frame()
	rest("save")
	tick(150 * time.Millisecond)
	if tt.HasText("Saves the file") {
		t.Fatal("the tip shows before the pointer rested 0.2 seconds")
	}
	tick(60 * time.Millisecond)
	if !tt.HasText("Saves the file") {
		t.Fatalf("no tip once the pointer rested 0.2 seconds; texts %q", tt.Texts())
	}
	// Outside the themed column, the default 0.6 seconds.
	away()
	rest("default")
	tick(300 * time.Millisecond)
	if tt.HasText("The default delay") {
		t.Fatal("an element outside the theme shows its tip after 0.3 seconds")
	}
	tick(310 * time.Millisecond)
	if !tt.HasText("The default delay") {
		t.Fatalf("no tip after the default delay; texts %q", tt.Texts())
	}

	// A longer delay, and the warm time scaling with it: two thirds.
	away()
	delay = 1500 * time.Millisecond
	tt.Frame()
	rest("save")
	tick(time.Second)
	if tt.HasText("Saves the file") {
		t.Fatal("the tip shows before the pointer rested 1.5 seconds")
	}
	tick(510 * time.Millisecond)
	if !tt.HasText("Saves the file") {
		t.Fatalf("no tip once the pointer rested 1.5 seconds; texts %q", tt.Texts())
	}
	// Gone for 0.9 seconds, more than the default 0.4 but less than 1:
	// the next one shows at once.
	tt.Move(390, 390)
	tick(16 * time.Millisecond)
	tick(900 * time.Millisecond)
	rest("open")
	tick(16 * time.Millisecond)
	if !tt.HasText("Opens a file") {
		t.Errorf("the next tip waits 0.9 seconds after one went; texts %q", tt.Texts())
	}
	// Gone for longer than that, it waits again.
	tt.Move(390, 390)
	tick(16 * time.Millisecond)
	tick(1100 * time.Millisecond)
	rest("save")
	tick(16 * time.Millisecond)
	if tt.HasText("Saves the file") {
		t.Error("the tip shows at once 1.1 seconds after the last one went")
	}

	// A negative delay shows it at once.
	away()
	delay = -1
	tt.Frame()
	rest("save")
	if !tt.HasText("Saves the file") {
		t.Errorf("a negative delay does not show the tip at once; texts %q", tt.Texts())
	}
}

// TestTooltipHidesWithMenu checks that a button's tip goes as its menu or
// popover opens, however it opens, and stays hidden while it shows, even
// as the pointer comes back to the button, and as the menu closes, until
// the pointer and the focus leave the button.
func TestTooltipHidesWithMenu(t *testing.T) {
	open := false
	buttons := map[string]func(c *context){
		"MenuButton": func(c *context) {
			coreMenuButton(c, "More", func(m *Menu) { m.Item("Duplicate") }).Tooltip("More actions")
		},
		"PopoverMenuButton": func(c *context) {
			corePopoverMenuButton(c, "More", func(m *Menu) { m.Item("Duplicate") }).Tooltip("More actions")
		},
		"MenuBase": func(c *context) {
			m := coreMenuBase(c, &open)
			m.Trigger.Children(func() { coreText(c, "More") }).Tooltip("More actions")
			m.Popup(func(*node) { m.Item().Children(func() { coreText(c, "Duplicate") }) })
		},
		"Popover": func(c *context) {
			b := coreButton(c, "More").Tooltip("More actions")
			if b.Clicked() {
				open = !open
			}
			corePopover(c, b, &open, func() { coreText(c, "Duplicate") })
		},
		"ContextMenu": func(c *context) {
			coreButton(c, "More").Tooltip("More actions").ContextMenu(func(m *Menu) { m.Item("Duplicate") })
		},
	}
	for name, button := range buttons {
		t.Run(name, func(t *testing.T) {
			open = false
			tt, now := clockTester(func(c *context) {
				coreColumn(c).Padding(60).Gap(20).Children(func() {
					coreButton(c, "First")
					button(c)
				})
			}, 400, 400)
			tick := ticker(tt, now)
			shows := func() bool { return tt.Menu() != nil || tt.HasText("Duplicate") }
			b := at(t, tt, "More")
			x, y := b.X+b.W/2, b.Y+b.H/2
			tt.Move(x, y)
			tick(time.Second)
			tick(16 * time.Millisecond)
			if !tt.HasText("More actions") {
				t.Fatal("no tip over the button")
			}
			if name == "ContextMenu" {
				tt.RightClickAt(x, y)
			} else {
				tt.ClickAt(x, y)
			}
			tick(16 * time.Millisecond)
			if !shows() || tt.HasText("More actions") {
				t.Fatalf("menu shows %v; the tip shows with it %v", shows(), tt.HasText("More actions"))
			}
			// The pointer goes onto the menu, and back to the button.
			if d, ok := tt.Find("Duplicate"); ok {
				tt.Move(d.X+d.W/2, d.Y+d.H/2)
				tick(100 * time.Millisecond)
			}
			tt.Move(x+2, y)
			tick(time.Second)
			tick(16 * time.Millisecond)
			if tt.HasText("More actions") {
				t.Error("the tip shows while the menu does")
			}
			tt.CloseMenu()
			open = false
			tick(time.Second)
			tick(16 * time.Millisecond)
			if tt.HasText("More actions") {
				t.Error("the tip shows as the menu closes, the pointer on the button")
			}
			tt.Move(390, 390)
			tick(time.Second)
			tt.Move(x, y)
			tick(time.Second)
			tick(16 * time.Millisecond)
			if !tt.HasText("More actions") {
				t.Error("no tip as the pointer comes back once the menu closed")
			}
		})
	}
}

// TestTooltipHidesWithMenuByKeys checks that the tip the keyboard focus
// brought to a menu button goes as the keys open its menu, and stays
// hidden as the menu closes with the focus back on the button.
func TestTooltipHidesWithMenuByKeys(t *testing.T) {
	for _, drawn := range []bool{false, true} {
		tt, now := clockTester(func(c *context) {
			if drawn {
				c.SetMenuPresentation(PopoverMenus)
			}
			coreColumn(c).Padding(60).Gap(20).Children(func() {
				coreButton(c, "First")
				coreMenuButton(c, "More", func(m *Menu) { m.Item("Duplicate") }).Tooltip("More actions")
			})
		}, 400, 400)
		tick := ticker(tt, now)
		tt.Key(0, KeyTab)
		tt.Key(0, KeyTab)
		tick(16 * time.Millisecond)
		if !tt.HasText("More actions") {
			t.Fatalf("drawn %v: no tip as the focus comes to the button", drawn)
		}
		tt.Key(0, KeyDown)
		tick(16 * time.Millisecond)
		if tt.Menu() == nil || tt.HasText("More actions") {
			t.Errorf("drawn %v: menu %q; the tip shows with it %v", drawn, tt.Menu(), tt.HasText("More actions"))
		}
		if drawn {
			tt.Key(0, KeyEscape)
		} else {
			tt.CloseMenu()
		}
		tick(time.Second)
		if tt.HasText("More actions") {
			t.Errorf("drawn %v: the tip shows again as the menu closes", drawn)
		}
	}
}
