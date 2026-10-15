package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// The grid's cells are 6.5 DIPs wide, in whole pixels, twice as high,
// centered on the pane and overhanging it; a pane too small has none.
func TestGrid(t *testing.T) {
	g, ok := gridOf(2200, 1440, 2)
	if !ok || g.cellW != 13 || g.cellH != 26 || g.cols != 170 || g.rows != 56 {
		t.Fatalf("grid of 2200×1440 at 2: %+v, want 170×56 cells of 13×26", g)
	}
	if g.left != -5 || g.top != -8 {
		t.Errorf("the grid starts at %d, %d, want it centered at -5, -8", g.left, g.top)
	}
	if g, _ := gridOf(1100, 720, 1); g.cellW != 7 {
		t.Errorf("cells %d pixels wide at 1, want 7 (6.5 rounded)", g.cellW)
	}
	if _, ok := gridOf(100, 100, 1); ok {
		t.Error("a grid in a pane of 100×100 DIPs")
	}
}

// A seed picks the same wind every time, heading down the pane, and a
// look that the last visits did not show.
func TestWind(t *testing.T) {
	a, b := newWind(42, "mixed"), newWind(42, "mixed")
	if a != b {
		t.Fatal("one seed, two winds")
	}
	for seed := range uint32(200) {
		w := newWind(seed, "mixed")
		if w.heading <= 0 || w.heading >= 3.1416 || w.speed < 42 || w.speed > 64 || w.gustWidth < 120 || w.gustWidth > 190 {
			t.Fatalf("wind of %d out of range: %+v", seed, w)
		}
		if l := pickLook(seed, []string{"mixed", "figured"}); l != "lettered" {
			t.Fatalf("look %q after mixed and figured, want lettered", l)
		}
		if l := pickLook(seed, []string{"mixed", "figured", "lettered"}); l == "mixed" {
			t.Fatal("the latest look again")
		}
	}
}

// Clicks on the pane blow ripples from the pointer, every third a heart;
// a click on the composer blows none.
func TestClicks(t *testing.T) {
	s := newApp(7)
	s.tex.atlases[atlasKey{7, 1}] = newAtlas(7, 1)
	tt := ui.NewTester(s.view, windowW, windowH)

	for i := range 3 {
		tt.ClickAt(100+float32(i)*50, 120)
	}
	taps := s.tex.hand.taps
	if len(taps) != 3 || taps[0].x != 100 || taps[0].y != 120 {
		t.Fatalf("taps %+v, want 3 from (100, 120) on", taps)
	}
	if taps[0].force <= 0 || taps[1].force <= 0 || taps[2].force >= 0 {
		t.Errorf("forces %v, %v, %v, want the third a heart's (negative)", taps[0].force, taps[1].force, taps[2].force)
	}
	if err := tt.Click("Task"); err != nil {
		t.Fatal(err)
	}
	if len(s.tex.hand.taps) != 3 {
		t.Error("a click on the composer blew a ripple")
	}
}

// The wind blows for its budget, settles and fades out; whatever the user
// does next brings it back in.
func TestSettles(t *testing.T) {
	tex := newTexture(7, nil)
	now := time.Unix(1000, 0)
	for range int((budget + settleTime + 1) / 0.04) {
		now = now.Add(40 * time.Millisecond)
		tex.step(now)
	}
	if tex.energy != 0 {
		t.Fatalf("energy %v after the budget, want 0", tex.energy)
	}
	frozen := tex.windTime
	tex.step(now.Add(time.Second))
	if tex.windTime != frozen {
		t.Error("the wind moved once settled")
	}
	tex.calm = true
	tex.move(10, 10, now)
	if tex.calm || tex.revealFrom != tex.active || tex.until != tex.active+budget {
		t.Errorf("woken: calm %v, comes in from %v, blows until %v, at %v", tex.calm, tex.revealFrom, tex.until, tex.active)
	}
}

// A frame shows glyphs in the plain ink where the wind blows, and a
// ripple tints those its front reaches.
func TestRender(t *testing.T) {
	tex := newTexture(0x3fa1c2d9, nil)
	g, _ := gridOf(800, 600, 1)
	a := newAtlas(g.cellW, 1)
	r := ui.Rect{W: 800, H: 600}
	start := time.Unix(1000, 0)
	tex.shown = start.Add(-fadeTime)
	frame := func(at time.Duration) (plain, tinted int) {
		now := start.Add(at)
		tex.step(now)
		tex.render(g, a, r, false, now)
		pix := tex.images[tex.front].Pix
		for i := 0; i < len(pix); i += 4 {
			if pix[i+3] < 40 {
				continue
			}
			if int(pix[i+2]) > int(pix[i])+20 {
				tinted++
			} else {
				plain++
			}
		}
		return plain, tinted
	}
	for at := time.Duration(0); at < 5*time.Second; at += 40 * time.Millisecond {
		frame(at)
	}
	plain, tinted := frame(5 * time.Second)
	if plain == 0 || tinted != 0 {
		t.Fatalf("%d plain and %d tinted pixels once revealed, want some plain and none tinted", plain, tinted)
	}
	tex.tap(400, 300, start.Add(5*time.Second))
	for at := 5 * time.Second; at < 6*time.Second; at += 40 * time.Millisecond {
		frame(at)
	}
	if _, tinted := frame(6 * time.Second); tinted == 0 {
		t.Error("no pixel tinted a second after a click")
	}
}
