package ui

import (
	"math"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/internal/scene"
)

// matchTile is the identity of the tiles of these tests.
type matchTile struct{ id int }

// linear flies over 100 ms, along a line, to read how far things went.
var linear = ElementTransition{Duration: 100 * time.Millisecond, Ease: Linear, Match: matchTile{1}, Fly: true}

// lifted returns the elements flying, which paint over the rest of their
// layer, in their order.
func lifted(tt *Tester) []*node {
	m, _ := tt.rt.matches.(*matches)
	if m == nil {
		return nil
	}
	var ls []*node
	for _, l := range m.lifted {
		ls = append(ls, l.e)
	}
	return ls
}

func within(a, b, eps float32) bool { return a-b <= eps && b-a <= eps }

func TestMatchMovesTheLook(t *testing.T) {
	detail := false
	tt, now := clockTester(func(c *context) {
		coreBox(c).Size(400, 300).Children(func() {
			if !detail {
				keep("thumb", coreBox(c).Key("thumb")).Absolute().Left(20).Top(20).Size(40, 40).Radius(4).Opacity(0.5).
					TextColor(RGB(255, 0, 0)).Rotate(90).Transition(linear)
				return
			}
			keep("hero", coreBox(c).Key("hero")).Absolute().Left(100).Top(100).Size(200, 100).Radius(20).
				TextColor(RGB(0, 0, 255)).Transition(linear)
		})
	}, 400, 300)
	detail = true
	tt.Frame()
	*now = now.Add(50 * time.Millisecond)
	tt.Frame()
	hero := keyed(tt, "hero")
	if hero.radius[0] != 12 {
		t.Errorf("halfway, the hero's corners are %v", hero.radius)
	}
	// Opaque by half the way, crossfading.
	if !hero.opacitySet || !within(hero.opacity, 0.75, 0.01) {
		t.Errorf("halfway, the hero's opacity is %v", hero.opacity)
	}
	if c := hero.resolvedText().color; c.R < 100 || c.B < 100 || c.R > 160 || c.B > 160 {
		t.Errorf("halfway, the hero's text is %v", c)
	}
	if d := decompose(hero.transform.m); !within(float32(d[2]*180/math.Pi), 45, 0.5) {
		t.Errorf("halfway, the hero is turned by %v degrees", d[2]*180/math.Pi)
	}
	*now = now.Add(time.Second)
	tt.Frame()
	hero = keyed(tt, "hero")
	if hero.radius[0] != 20 || hero.opacitySet && hero.opacity != 1 || hero.transform.set() || hero.resolvedText().color != RGB(0, 0, 255) {
		t.Errorf("once over, the hero looks as %v, %v, %+v, %v", hero.radius, hero.opacity, hero.transform, hero.resolvedText().color)
	}
}

func TestMatchCrossfades(t *testing.T) {
	detail := false
	tr := linear
	tr.Exit = &Motion{Y: 50}
	tt, now := clockTester(func(c *context) {
		coreBox(c).Size(400, 300).Children(func() {
			keep("grid", coreBox(c).Key("grid")).Absolute().Size(200, 200).Children(func() {
				if !detail {
					keep("thumb", coreBox(c).Key("thumb")).Size(40, 40).Background(RGB(255, 0, 0)).Transition(tr).Children(func() {
						coreBox(c).Size(20, 20).Background(RGB(0, 255, 0))
					})
				}
			})
			if detail {
				keep("hero", coreBox(c).Key("hero")).Absolute().Left(200).Top(100).Size(200, 100).Background(RGB(0, 0, 255)).Transition(tr)
			}
		})
	}, 400, 300)
	thumbID := keyed(tt, "thumb").id
	detail = true
	tt.Frame()
	// The thumbnail goes with the hero, not as its Exit says.
	for g := keyed(tt, "grid").first; g != nil; g = g.next {
		t.Errorf("the thumbnail goes as its Exit says, as %+v", g)
	}
	ls := lifted(tt)
	if len(ls) != 2 || ls[0].id != thumbID || ls[0].leaving == 0 || ls[1] != keyed(tt, "hero") {
		t.Fatalf("on the way, %d elements paint over the window", len(ls))
	}
	// Under the hero, transparent at first, the thumbnail as it was.
	if p := tt.Image().RGBAAt(10, 10); p.G < 250 || p.R > 10 {
		t.Errorf("at first, the thumbnail's content is %v", p)
	}
	*now = now.Add(75 * time.Millisecond)
	tt.Frame()
	ls = lifted(tt)
	if len(ls) != 2 || !within(ls[0].opacity, 1-0.25/0.45, 0.01) || keyed(tt, "hero").opacity != 1 {
		t.Fatalf("three quarters of the way, the thumbnail's opacity is %v, the hero's %v", ls[0].opacity, keyed(tt, "hero").opacity)
	}
	if b := ls[0].Bounds(); !nearRect(b, keyed(tt, "hero").Bounds()) {
		t.Errorf("the thumbnail is at %v, not with the hero at %v", b, keyed(tt, "hero").Bounds())
	}
	*now = now.Add(time.Second)
	tt.Frame()
	if len(lifted(tt)) != 0 {
		t.Errorf("once over, %d elements still paint over the window", len(lifted(tt)))
	}
	for g := tt.rt.c.root.first; g != nil; g = g.next {
		if g.leaving != 0 {
			t.Errorf("once over, a copy of %d stays", g.id)
		}
	}
}

func TestMatchPaintsOverTheWindow(t *testing.T) {
	detail := false
	tr := linear
	tt, now := clockTester(func(c *context) {
		coreBox(c).Size(300, 100).Children(func() {
			coreBox(c).Key("a").Absolute().Size(100, 100).Clip().Children(func() {
				if !detail {
					coreBox(c).Key("thumb").Absolute().Left(10).Top(10).Size(20, 20).Background(RGB(0, 0, 255)).Transition(tr)
				}
			})
			coreBox(c).Key("b").Absolute().Left(150).Size(150, 100).Clip().Children(func() {
				if detail {
					coreBox(c).Key("hero").Absolute().Left(25).Top(10).Size(100, 80).Background(RGB(0, 0, 255)).Transition(tr)
				}
			})
			// Over both, as later.
			coreBox(c).Key("cover").Absolute().Left(100).Size(50, 100).Background(RGB(0, 255, 0))
		})
	}, 300, 100)
	detail = true
	tt.Frame()
	*now = now.Add(50 * time.Millisecond)
	tt.Frame()
	// Halfway, at (92.5, 10) 60×50: over the cover, out of b's clip.
	if p := tt.Image().RGBAAt(120, 30); p.B < 250 || p.G > 5 {
		t.Errorf("halfway, the hero shows under the cover or clipped: %v", p)
	}
	*now = now.Add(time.Second)
	tt.Frame()
	if p := tt.Image().RGBAAt(120, 30); p.G < 250 {
		t.Errorf("once over, the cover shows %v", p)
	}
}

func TestMatchTakesThePointerWhereItShows(t *testing.T) {
	detail := false
	clicked := ""
	tt, now := clockTester(func(c *context) {
		coreBox(c).Size(300, 100).Children(func() {
			coreBox(c).Key("a").Absolute().Size(100, 100).Clip().Children(func() {
				if !detail {
					coreBox(c).Key("thumb").Absolute().Left(10).Top(10).Size(20, 20).Background(RGB(0, 0, 255)).Transition(linear)
				}
			})
			coreBox(c).Key("b").Absolute().Left(150).Size(150, 100).Clip().Children(func() {
				if detail && coreBox(c).Key("hero").Absolute().Left(25).Top(10).Size(100, 80).Background(RGB(0, 0, 255)).Transition(linear).Clicked() {
					clicked = "hero"
				}
			})
			if coreBox(c).Key("cover").Absolute().Left(100).Size(50, 100).Background(RGB(0, 255, 0)).Clicked() {
				clicked = "cover"
			}
		})
	}, 300, 100)
	detail = true
	tt.Frame()
	*now = now.Add(50 * time.Millisecond)
	tt.Frame()
	// Halfway, at (92.5, 10) 60×50: over the cover, out of b's clip.
	for _, c := range []struct {
		x, y float32
		want string
	}{{120, 30, "hero"}, {97, 30, "hero"}, {120, 80, "cover"}} {
		clicked = ""
		tt.ClickAt(c.x, c.y)
		if clicked != c.want {
			t.Errorf("halfway, a click at %v, %v clicks %q, not %q", c.x, c.y, clicked, c.want)
		}
	}
}

func TestMatchMovesItsTextAsOne(t *testing.T) {
	detail := false
	tr := ElementTransition{Animation: &Snappy, Match: matchTile{1}, Fly: true}
	tt, now := clockTester(func(c *context) {
		coreBox(c).Size(400, 300).Children(func() {
			x, y := float32(10.3), float32(10.6)
			if detail {
				x, y = 100.2, 100.7
			}
			coreBox(c).Key(detail).Absolute().Left(x).Top(y).Size(120, 40).Padding(8).Transition(tr).Children(func() {
				coreText(c, "Hello").FontSize(14)
			})
		})
	}, 400, 300)
	tt.SetScale(2)
	tt.Frame()
	detail = true
	var frames [][]scene.Glyph
	for range 120 {
		*now = now.Add(time.Second / 60)
		tt.Frame()
		if len(lifted(tt)) == 0 {
			break
		}
		frames = append(frames, slices.Clone(tt.rt.scene.Glyphs))
	}
	rest := tt.rt.scene.Glyphs
	if len(frames) < 10 || len(rest) != 5 {
		t.Fatalf("on its way for %d frames, %d glyphs at rest", len(frames), len(rest))
	}
	// On its way, the text of the card and of the copy it came from show
	// each glyph as at rest, whole pixels from where it rests, and move
	// it with the card.
	whole := func(v float32) bool { return within(v, float32(math.Round(float64(v))), 1e-3) }
	for f, gs := range frames {
		for i, g := range gs {
			r := rest[i%5]
			if g.U != r.U || g.V != r.V || g.W != r.W || g.H != r.H || !whole(g.X-r.X) || !whole(g.Y-r.Y) {
				t.Fatalf("frame %d, glyph %d draws %v, %v at %v, %v, not as at rest, %v, %v at %v, %v", f, i, g.U, g.V, g.X, g.Y, r.U, r.V, r.X, r.Y)
			}
		}
	}
}

func TestMatchHidesItsCopyGoingAway(t *testing.T) {
	detail := false
	page := ElementTransition{Duration: 100 * time.Millisecond, Ease: Linear, Enter: &Motion{}, Exit: &Motion{}}
	tt, now := clockTester(func(c *context) {
		keep("stage", coreBox(c).Key("stage")).Size(400, 300).Children(func() {
			if !detail {
				coreColumn(c).Key("grid").Absolute().Fill().Transition(page).Children(func() {
					keep("thumb", coreBox(c).Key("thumb")).Size(40, 40).Background(RGB(255, 0, 0)).Transition(linear)
				})
				return
			}
			coreColumn(c).Key("detail").Absolute().Fill().Transition(page).Children(func() {
				keep("hero", coreBox(c).Key("hero")).Size(200, 100).Background(RGB(0, 0, 255)).Transition(linear)
			})
		})
	}, 400, 300)
	thumbID := keyed(tt, "thumb").id
	detail = true
	tt.Frame()
	*now = now.Add(25 * time.Millisecond)
	tt.Frame()
	var grid *node
	for g := keyed(tt, "stage").first; g != nil; g = g.next {
		if g.leaving != 0 {
			grid = g
		}
	}
	if grid == nil || grid.first == nil || grid.first.id != thumbID {
		t.Fatalf("the grid's copy going away is %+v", grid)
	}
	if grid.first.flags&flagInvisible == 0 {
		t.Error("the grid going away shows the thumbnail, which goes with the hero")
	}
	// The hero shows as its own opacity says, not as its page, coming in,
	// says: about opaque, in the thumbnail's red going to blue, a quarter
	// of the way, rather than faded to the window's background.
	if p := tt.Image().RGBAAt(10, 10); p.R < 150 || p.B < 30 || p.G > 30 {
		t.Errorf("a quarter of the way, the hero shows %v", p)
	}
}

func TestMatchScalesContent(t *testing.T) {
	detail := false
	tr := linear
	tr.ScaleContent = true
	tt, now := clockTester(func(c *context) {
		coreBox(c).Size(400, 300).Children(func() {
			if !detail {
				coreBox(c).Key("thumb").Absolute().Size(40, 20).Transition(tr)
				return
			}
			keep("hero", coreBox(c).Key("hero")).Absolute().Left(100).Top(100).Size(200, 100).Radius(20).Transition(tr).Children(func() {
				keep("inside", coreBox(c).Key("inside")).Size(200, 50)
			})
		})
	}, 400, 300)
	detail = true
	tt.Frame()
	*now = now.Add(50 * time.Millisecond)
	tt.Frame()
	hero := keyed(tt, "hero")
	if b := hero.Bounds(); !nearRect(b, Rect{50, 50, 120, 60}) {
		t.Errorf("halfway, the hero shows at %v", b)
	}
	if hero.w != 200 || keyed(tt, "inside").w != 200 {
		t.Errorf("halfway, the hero is laid out %v wide, its content %v", hero.w, keyed(tt, "inside").w)
	}
	// Its corners halfway from none to 20 as they show, scaled by 0.6.
	if !within(hero.radius[0]*0.6, 10, 0.01) {
		t.Errorf("halfway, the hero's corners are %v", hero.radius)
	}
	*now = now.Add(time.Second)
	tt.Frame()
	if keyed(tt, "hero").st.world.Set {
		t.Error("once over, the hero is still scaled")
	}

	// Resizing, as Size animates.
	w := float32(100)
	tt, now = clockTester(func(c *context) {
		keep("box", coreBox(c).Key("box")).Size(w, 50).Transition(ElementTransition{Duration: 100 * time.Millisecond, Ease: Linear, ScaleContent: true})
	}, 400, 300)
	w = 200
	tt.Frame()
	*now = now.Add(50 * time.Millisecond)
	tt.Frame()
	if box := keyed(tt, "box"); box.w != 200 || !nearRect(box.Bounds(), Rect{0, 0, 150, 50}) {
		t.Errorf("halfway, the box is laid out %v wide, shows at %v", box.w, box.Bounds())
	}
}

func TestRouterTransitionMatch(t *testing.T) {
	r := NewRouter("/")
	r.Transition = TransitionMatch
	tt, now := clockTester(func(c *context) {
		r.coreView(c, func(route *Route) {
			switch {
			case route.Match("/"):
				keep("thumb", coreBox(c).Key("thumb")).Size(40, 40).Background(RGB(255, 0, 0)).Transition(linear)
				keep("other", coreBox(c).Key("other")).Size(40, 40).Background(RGB(0, 255, 0))
			case route.Match("/photo"):
				keep("hero", coreBox(c).Key("hero")).Size(200, 100).Background(RGB(0, 0, 255)).Transition(linear)
			}
		})
	}, 400, 300)
	r.Push("/photo")
	tt.Frame()
	ls := lifted(tt)
	if len(ls) != 2 || ls[0] != keyed(tt, "thumb") || ls[1] != keyed(tt, "hero") {
		t.Fatalf("on the way, %d elements paint over the window", len(ls))
	}
	// From the thumbnail's color, the thumbnail still built.
	if c := keyed(tt, "hero").bg; c != RGB(255, 0, 0) {
		t.Errorf("appearing, the hero is %v, not the thumbnail's red", c)
	}
	// The match over before the pages faded into each other, the
	// thumbnail of the page going away shows no more.
	*now = now.Add(150 * time.Millisecond)
	tt.Frame()
	if len(lifted(tt)) != 0 {
		t.Fatalf("once over, %d elements paint over the window", len(lifted(tt)))
	}
	if keyed(tt, "thumb").flags&flagInvisible == 0 {
		t.Error("once over, the thumbnail shows in the page going away")
	}
	if keyed(tt, "other").flags&flagInvisible != 0 {
		t.Error("the page going away hides more than the thumbnail")
	}
	if p := tt.Image().RGBAAt(20, 20); p.B < 250 {
		t.Errorf("once over, the hero shows %v", p)
	}
	// And back.
	*now = now.Add(time.Second)
	tt.Frame()
	r.Back()
	tt.Frame()
	if ls := lifted(tt); len(ls) != 2 || ls[0] != keyed(tt, "hero") || ls[1] != keyed(tt, "thumb") {
		t.Fatalf("back, %d elements paint over the window", len(ls))
	}
}

func TestMixTransform(t *testing.T) {
	for _, c := range []struct {
		a, b  Transform
		k     float32
		want  Transform
		label string
	}{
		{Rotation(0), Rotation(90), 0.5, Rotation(45), "a turn"},
		{Rotation(170), Rotation(-170), 0.5, Rotation(180), "the short way round"},
		{Scaling(1, 1), Scaling(3, 2), 0.5, Scaling(2, 1.5), "a scale"},
		{Translation(0, 0), Translation(10, 20), 0.25, Translation(2.5, 5), "a move"},
		{Scaling(-1, 1), Scaling(-1, 1), 0.5, Scaling(-1, 1), "a mirror"},
	} {
		got := mixTransform(c.a, c.b, c.k).m
		want := c.want.m
		for i, v := range [6][2]float32{{got.A, want.A}, {got.B, want.B}, {got.C, want.C}, {got.D, want.D}, {got.X, want.X}, {got.Y, want.Y}} {
			if !within(v[0], v[1], 0.001) {
				t.Errorf("%s: component %d is %v, not %v (%+v)", c.label, i, v[0], v[1], got)
			}
		}
	}
}

func TestMatchInsideMatch(t *testing.T) {
	detail := false
	type title struct{}
	tile := linear
	name := ElementTransition{Duration: 100 * time.Millisecond, Ease: Linear, Match: title{}, Fly: true}
	tt, _ := clockTester(func(c *context) {
		coreBox(c).Size(400, 300).Children(func() {
			if !detail {
				keep("thumb", coreColumn(c).Key("thumb")).Size(80, 80).Background(RGB(255, 0, 0)).Transition(tile).Children(func() {
					keep("small", coreText(c, "Ocean")).Key("small").Transition(name)
				})
				return
			}
			coreColumn(c).Key("detail").Absolute().Left(100).Top(50).Children(func() {
				keep("hero", coreBox(c).Key("hero")).Size(200, 100).Background(RGB(0, 0, 255)).Transition(tile)
				keep("big", coreText(c, "Ocean")).Key("big").FontSize(24).Transition(name)
			})
		})
	}, 400, 300)
	small := keyed(tt, "small").id
	detail = true
	tt.Frame()
	// The tile's copy, the hero over it, the title's copy, which the
	// tile's copy shows no more, and the title over it.
	ls := lifted(tt)
	if len(ls) != 4 || ls[0].leaving == 0 || ls[1] != keyed(tt, "hero") || ls[2].id != small || ls[2].leaving == 0 || ls[3] != keyed(tt, "big") {
		t.Fatalf("on the way, %d elements paint over the window", len(ls))
	}
	if in := ls[0].first; in == nil || in.id != small || in.flags&flagInvisible == 0 {
		t.Errorf("the tile's copy shows its title, %+v", in)
	}
}

// A Match without Fly moves the element appearing as it did before Fly:
// its box and colors, in its parent, the other going as its Exit says.
func TestMatchWithoutFly(t *testing.T) {
	detail := false
	tr := ElementTransition{Duration: 100 * time.Millisecond, Ease: Linear, Match: matchTile{1}, Exit: &Motion{Y: 50}}
	tt, now := clockTester(func(c *context) {
		coreBox(c).Size(400, 300).Children(func() {
			keep("grid", coreBox(c).Key("grid")).Absolute().Size(200, 200).Children(func() {
				if !detail {
					coreBox(c).Key("thumb").Size(40, 40).Radius(4).Opacity(0.5).Background(RGB(255, 0, 0)).Transition(tr)
				}
			})
			coreBox(c).Key("pane").Absolute().Left(200).Top(100).Size(200, 100).Clip().Children(func() {
				if detail {
					keep("hero", coreBox(c).Key("hero")).Size(200, 100).Radius(20).Background(RGB(0, 0, 255)).Transition(tr)
				}
			})
		})
	}, 400, 300)
	detail = true
	tt.Frame()
	if g := keyed(tt, "grid").first; g == nil || g.leaving == 0 {
		t.Error("the thumbnail does not go as its Exit says")
	}
	if c := keyed(tt, "hero").bg; c != RGB(255, 0, 0) {
		t.Errorf("appearing, the hero's background is %v, not the thumbnail's red", c)
	}
	*now = now.Add(50 * time.Millisecond)
	tt.Frame()
	hero := keyed(tt, "hero")
	if b := hero.Bounds(); !nearRect(b, Rect{100, 50, 120, 70}) {
		t.Errorf("halfway, the hero shows at %v", b)
	}
	if len(lifted(tt)) != 0 || hero.radius[0] != 20 || hero.opacitySet || hero.st.world.Set {
		t.Errorf("halfway, the hero flies: %d elements lifted, corners %v, opacity %v", len(lifted(tt)), hero.radius, hero.opacity)
	}
	// In its parent's clip.
	if p := tt.Image().RGBAAt(150, 80); p.B > 200 && p.R < 100 {
		t.Errorf("halfway, the hero shows out of its parent's clip: %v", p)
	}
	if p := tt.Image().RGBAAt(210, 110); p.R < 50 || p.B < 50 {
		t.Errorf("halfway, the hero shows %v, not between red and blue", p)
	}
	// Without an Exit, the frame keeps nothing for the next.
	tt = coreNewTester(func(c *context) {
		coreBox(c).Size(10, 10).Transition(ElementTransition{Match: matchTile{2}})
	}, 100, 100)
	if tt.rt.exitsBuilt {
		t.Error("a Match without Fly keeps the frame's elements for the next")
	}
}

// An element flying in an overlay goes over it, and takes the pointer
// there; one in the window's content goes under the overlays.
func TestFlyInItsLayer(t *testing.T) {
	open := false
	clicked := ""
	tt, now := clockTester(func(c *context) {
		coreBox(c).Size(300, 200).Children(func() {
			if !open {
				coreBox(c).Key("thumb").Absolute().Left(10).Top(10).Size(20, 20).Background(RGB(0, 0, 255)).Transition(linear)
			}
		})
		if !open {
			return
		}
		coreOverlay(c, func() {
			// A backdrop over the window, and the hero on it.
			coreBox(c).Key("back").Absolute().Left(0).Top(0).Size(300, 200).Background(RGB(0, 255, 0)).Children(func() {
				if coreBox(c).Key("hero").Absolute().Left(150).Top(100).Size(100, 80).Background(RGB(0, 0, 255)).Transition(linear).Clicked() {
					clicked = "hero"
				}
			})
		})
	}, 300, 200)
	open = true
	tt.Frame()
	*now = now.Add(50 * time.Millisecond)
	tt.Frame()
	// Halfway, at (80, 55) 60×50, over the backdrop.
	if p := tt.Image().RGBAAt(110, 80); p.B < 250 || p.G > 5 {
		t.Errorf("halfway, the hero shows under its overlay: %v", p)
	}
	tt.ClickAt(110, 80)
	if clicked != "hero" {
		t.Errorf("halfway, a click on the hero clicks %q", clicked)
	}

	clicked, open = "", false
	tt, now = clockTester(func(c *context) {
		coreBox(c).Size(300, 200).Children(func() {
			if !open {
				coreBox(c).Key("thumb").Absolute().Left(10).Top(10).Size(20, 20).Background(RGB(0, 0, 255)).Transition(linear)
			} else if coreBox(c).Key("hero").Absolute().Left(150).Top(100).Size(100, 80).Background(RGB(0, 0, 255)).Transition(linear).Clicked() {
				clicked = "hero"
			}
		})
		coreOverlay(c, func() {
			if coreBox(c).Key("cover").Absolute().Left(100).Top(0).Size(20, 200).Background(RGB(0, 255, 0)).Clicked() {
				clicked = "cover"
			}
		})
	}, 300, 200)
	open = true
	tt.Frame()
	*now = now.Add(50 * time.Millisecond)
	tt.Frame()
	if p := tt.Image().RGBAAt(110, 80); p.G < 250 || p.B > 5 {
		t.Errorf("halfway, the hero shows over an overlay: %v", p)
	}
	tt.ClickAt(110, 80)
	if clicked != "cover" {
		t.Errorf("halfway, a click on the overlay over the hero clicks %q", clicked)
	}
}

// An element removed on its way with the element around it, which goes
// as its Exit says, flies on in that one's copy: from where it showed to
// where that copy has it, fading as it fades.
func TestFlyRemovedOnItsWay(t *testing.T) {
	stage := 0
	page := ElementTransition{Duration: 200 * time.Millisecond, Ease: Linear, Exit: &Motion{}}
	tt, now := clockTester(func(c *context) {
		keep("stage", coreBox(c).Key("stage")).Size(400, 300).Children(func() {
			switch stage {
			case 0:
				coreBox(c).Key("thumb").Absolute().Left(10).Top(10).Size(20, 20).Background(RGB(255, 0, 0)).Transition(linear)
			case 1:
				coreBox(c).Key("detail").Absolute().Left(100).Top(100).Size(200, 100).Transition(page).Children(func() {
					keep("hero", coreBox(c).Key("hero")).Size(200, 100).Background(RGB(0, 0, 255)).Transition(linear)
				})
			}
		})
	}, 400, 300)
	stage = 1
	tt.Frame()
	*now = now.Add(50 * time.Millisecond)
	tt.Frame()
	halfway := keyed(tt, "hero").Bounds()
	if !nearRect(halfway, Rect{55, 55, 110, 60}) {
		t.Fatalf("halfway, the hero shows at %v", halfway)
	}
	stage = 2
	tt.Frame()
	var detail *node
	for g := keyed(tt, "stage").first; g != nil; g = g.next {
		if g.leaving != 0 {
			detail = g
		}
	}
	if detail == nil || detail.first == nil {
		t.Fatalf("the detail view's copy going away is %+v", detail)
	}
	hero := detail.first
	ls := lifted(tt)
	if len(ls) != 2 || ls[1] != hero {
		t.Fatalf("gone, the hero's copy does not fly on: %d elements fly", len(ls))
	}
	if b := hero.Bounds(); !nearRect(b, halfway) {
		t.Errorf("gone, the hero's copy shows at %v, not where the hero was, %v", b, halfway)
	}
	*now = now.Add(25 * time.Millisecond)
	tt.Frame()
	// Three quarters of the way, as the detail view's copy has faded by
	// an eighth.
	if b := hero.Bounds(); !nearRect(b, Rect{77.5, 77.5, 155, 80}) {
		t.Errorf("three quarters of the way, the hero's copy shows at %v", b)
	}
	if !within(hero.opacity, 0.875, 0.01) {
		t.Errorf("the hero's copy shows at opacity %v, not as the detail view's copy fades", hero.opacity)
	}
	if p := tt.Image().RGBAAt(155, 117); p.B < 150 || p.G > 60 {
		t.Errorf("three quarters of the way, the hero's copy shows %v", p)
	}
	*now = now.Add(50 * time.Millisecond)
	tt.Frame()
	// There, it goes on as the detail view's copy goes.
	if hero.lift || len(lifted(tt)) != 0 {
		t.Error("there, the hero's copy still flies")
	}
	if b := hero.Bounds(); !nearRect(b, Rect{100, 100, 200, 100}) || hero.opacity != 1 || hero.bg != RGB(0, 0, 255) {
		t.Errorf("there, the hero's copy shows at %v, at opacity %v, in %v", b, hero.opacity, hero.bg)
	}
	if p := tt.Image().RGBAAt(200, 150); p.B < 150 || p.R > 120 || p.G > 120 {
		t.Errorf("there, the detail view's copy shows %v, not the hero fading with it", p)
	}
}

// Elements flying go from the transform of the elements around the one
// they came from to their own's.
func TestFlyFromTransformedElements(t *testing.T) {
	detail := false
	tt, now := clockTester(func(c *context) {
		coreBox(c).Size(400, 300).Children(func() {
			if !detail {
				// Shown at half its size, from the top left corner.
				coreBox(c).Key("zoom").Absolute().Size(300, 300).TransformOrigin(0, 0).Scale(0.5, 0.5).Children(func() {
					keep("thumb", coreBox(c).Key("thumb")).Absolute().Left(100).Top(100).Size(40, 40).Background(RGB(255, 0, 0)).Transition(linear)
				})
				return
			}
			keep("hero", coreBox(c).Key("hero")).Absolute().Left(200).Top(100).Size(100, 100).Background(RGB(0, 0, 255)).Transition(linear)
		})
	}, 400, 300)
	if b := keyed(tt, "thumb").Bounds(); !nearRect(b, Rect{50, 50, 20, 20}) {
		t.Fatalf("the thumbnail shows at %v", b)
	}
	detail = true
	tt.Frame()
	ls := lifted(tt)
	if len(ls) != 2 || ls[1] != keyed(tt, "hero") {
		t.Fatalf("on the way, %d elements paint over the window", len(ls))
	}
	for i, e := range ls {
		if b := e.Bounds(); !nearRect(b, Rect{50, 50, 20, 20}) {
			t.Errorf("appearing, element %d shows at %v, not where the thumbnail was", i, b)
		}
	}
	*now = now.Add(time.Second)
	tt.Frame()
	if hero := keyed(tt, "hero"); !nearRect(hero.Bounds(), Rect{200, 100, 100, 100}) || hero.st.world.Set {
		t.Errorf("once over, the hero shows at %v", hero.Bounds())
	}
}

// Elements flying paint in the theme of the Themed around them, and the
// copy of the one they came from in that one's.
func TestFlyKeepsItsTheme(t *testing.T) {
	theme := *LightTheme()
	theme.Accent, theme.Text = RGB(255, 0, 255), RGB(0, 128, 0)
	accent := func(p *Painter, r Rect) { p.Fill(r, p.Theme().Accent, 0) }
	detail := false
	tt, now := clockTester(func(c *context) {
		c.SetTheme(DarkTheme())
		coreThemed(c, &theme).Size(400, 300).Background(Transparent).Children(func() {
			if !detail {
				coreBox(c).Key("thumb").Absolute().Left(10).Top(10).Size(40, 40).Transition(linear).Children(func() {
					coreBox(c).Size(40, 40).Draw(accent)
				})
				return
			}
			keep("hero", coreBox(c).Key("hero")).Absolute().Left(200).Top(100).Size(100, 100).Transition(linear).Children(func() {
				coreBox(c).Size(100, 100).Draw(accent)
			})
		})
	}, 400, 300)
	detail = true
	tt.Frame()
	// The thumbnail's copy, under the hero, transparent at first.
	ls := lifted(tt)
	if len(ls) != 2 || ls[0].leaving == 0 {
		t.Fatalf("on the way, %d elements paint over the window", len(ls))
	}
	if c := ls[0].resolvedText().color; c != theme.Text {
		t.Errorf("the thumbnail's copy has the text color %v, not its Themed's", c)
	}
	if p := tt.Image().RGBAAt(20, 20); p.R < 250 || p.G > 5 || p.B < 250 {
		t.Errorf("appearing, the thumbnail's copy paints %v, not in its Themed's theme", p)
	}
	*now = now.Add(50 * time.Millisecond)
	tt.Frame()
	if p := tt.Image().RGBAAt(120, 70); p.R < 250 || p.G > 5 || p.B < 250 {
		t.Errorf("halfway, the hero paints %v, not in its Themed's theme", p)
	}
}
