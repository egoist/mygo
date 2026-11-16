package ui

import "testing"

// TestPopoverIsKeyedByItsOpener checks that what an opener opens over the
// window is keyed under it (toast.go): it keeps its identity and its state
// while it shows, and what another opener opens in its place takes
// neither, so that the hover and the focus of one never follow the other.
// A modal popover, as a select's popup, holds two overlays: the backdrop
// taking the presses outside it, then the panel.
func TestPopoverIsKeyedByItsOpener(t *testing.T) {
	first, second := true, false
	var over []*node // what the pass's overlays hold, in the order they built it
	tt := coreNewTester(func(c *context) {
		over = over[:0]
		one := coreButton(c, "one")
		two := coreButton(c, "two")
		if first {
			popover(c, one, &first, true, func(panel *node) { panel.Label("first").Size(50, 50) })
		}
		if second {
			popover(c, two, &second, true, func(panel *node) { panel.Label("second").Size(50, 50) })
		}
		for e := c.overlayRoot().first; e != nil; e = e.next {
			over = append(over, e)
		}
	}, 400, 300)
	if len(over) != 2 {
		t.Fatalf("the open popover holds %d overlays, not its backdrop and its panel", len(over))
	}
	backID, backST := over[0].id, over[0].st
	id, st := over[1].id, over[1].st
	// It keeps them while it shows.
	tt.Frame()
	if len(over) != 2 || over[0].id != backID || over[0].st != backST || over[1].id != id || over[1].st != st {
		t.Fatal("the popover did not keep its identity and its state while it shows")
	}
	// What the other opener opens takes none of it.
	first, second = false, true
	before := tt.rt.frame
	tt.Frame()
	if len(over) != 2 {
		t.Fatalf("the second popover holds %d overlays, not its backdrop and its panel", len(over))
	}
	if over[0].id == backID {
		t.Error("the second opener's backdrop took the first's identity")
	}
	if over[0].st.born <= before {
		t.Error("the second opener's backdrop took a state older than its build")
	}
	if over[1].id == id {
		t.Error("the second opener's panel took the first's identity")
	}
	if over[1].st.born <= before {
		t.Error("the second opener's panel took a state older than its build")
	}
}

// TestTipIsKeyedByItsOpener checks that a tip is keyed under the element
// it opens for: as the focus moves on and the next element's tip shows in
// its place, that tip takes neither its identity nor its state.
func TestTipIsKeyedByItsOpener(t *testing.T) {
	var tip *node
	tt := coreNewTester(func(c *context) {
		tip = nil
		coreColumn(c).Children(func() {
			one := coreButton(c, "one")
			if shown := coreTooltipBase(c, one, func(t *node) { t.Label("tip of one") }); shown != nil {
				tip = shown
			}
			two := coreButton(c, "two")
			if shown := coreTooltipBase(c, two, func(t *node) { t.Label("tip of two") }); shown != nil {
				tip = shown
			}
		})
	}, 400, 300)
	// The keyboard focus shows the tip at once.
	tt.Key(0, KeyTab)
	if tip == nil {
		t.Fatal("the focus shows no tip")
	}
	id, st := tip.id, tip.st
	tt.Frame()
	if tip == nil || tip.id != id || tip.st != st {
		t.Fatal("the tip did not keep its identity and its state while it shows")
	}
	// The next element's tip takes neither.
	before := tt.rt.frame
	tt.Key(0, KeyTab)
	if tip == nil {
		t.Fatal("the focus shows no tip for the second element")
	}
	if tip.id == id {
		t.Error("the second element's tip took the first's identity")
	}
	if tip.st.born <= before {
		t.Error("the second element's tip took a state older than its build")
	}
}

// TestOptionsAreKeyedByTheirInput checks that the options of a combo box
// are keyed under the input they open for: as they close and another
// input's options show in their place, those take neither their identity
// nor their state.
func TestOptionsAreKeyedByTheirInput(t *testing.T) {
	textOne, textTwo := "", ""
	var panelOne, panelTwo *node
	tt := coreNewTester(func(c *context) {
		panelOne, panelTwo = nil, nil
		one := coreComboboxBase(c, &textOne)
		one.Input.Label("one")
		panelOne = one.Popup(func(panel *node) {
			one.Item("a").Label("a").Children(func() { coreText(c, "a") })
		})
		two := coreComboboxBase(c, &textTwo)
		two.Input.Label("two")
		panelTwo = two.Popup(func(panel *node) {
			two.Item("b").Label("b").Children(func() { coreText(c, "b") })
		})
	}, 400, 300)
	tt.Click("one")
	if panelOne == nil {
		t.Fatal("the options do not show")
	}
	id, st := panelOne.id, panelOne.st
	tt.Frame()
	if panelOne == nil || panelOne.id != id || panelOne.st != st {
		t.Fatal("the options did not keep their identity and their state while they show")
	}
	tt.Key(0, KeyEscape)
	before := tt.rt.frame
	tt.Click("two")
	if panelTwo == nil {
		t.Fatal("the second input's options do not show")
	}
	if panelTwo.id == id {
		t.Error("the second input's options took the first's identity")
	}
	if panelTwo.st.born <= before {
		t.Error("the second input's options took a state older than their build")
	}
}
