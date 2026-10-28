package ui

import (
	"testing"
	"time"
)

// benchTile and benchTitle are the identities the tiles of the Magic Move
// benchmarks share with their detail view.
type (
	benchTile  struct{ i int }
	benchTitle struct{ i int }
)

// magicMoveScene is a grid of n tiles with titles, in a scroll view, that
// opens one into a detail view every period frames and closes it as
// many after, as the gallery's Magic Move card does: with match, the
// tile and its title move into the view's hero and heading, the title's
// characters morphing, while the rest fades; without, the views only
// fade.
func magicMoveScene(n, period int, match bool) func() (func(c *Context), func()) {
	return func() (func(c *Context), func()) {
		frame, open := 0, -1
		motion := ElementTransition{Duration: 300 * time.Millisecond}
		fade := ElementTransition{Duration: 300 * time.Millisecond, Enter: &Motion{}, Exit: &Motion{}}
		morph := ContentTransition{Morph: true}
		shared := func(i int) (tile, title ElementTransition) {
			tile, title = motion, motion
			if match {
				tile.Match, title.Match = benchTile{i}, benchTitle{i}
			}
			return tile, title
		}
		color := func(i int) Color { return RGB(uint8(40+i*37%180), uint8(80+i*13%150), uint8(120+i*29%120)) }
		view := func(c *Context) {
			Box(c).Fill().Background(RGB(246, 247, 249)).Children(func() {
				if open < 0 {
					Column(c.Key("grid")).Absolute().Fill().Transition(fade).Children(func() {
						Scroll(c).Fill().Children(func() {
							Grid(c).Columns(6).Gap(12).Padding(12).Children(func() {
								for i := range n {
									tile, title := shared(i)
									Column(c.Key(i)).Height(90).Padding(8).Justify(End).Radius(12).Background(color(i)).Transition(tile).Children(func() {
										Textf(c, "Tile %d", i).FontSize(12).Bold().TextColor(RGB(255, 255, 255)).
											Transition(title).ContentTransition(morph)
									})
								}
							})
						})
					})
					return
				}
				tile, title := shared(open)
				Column(c.Key("detail")).Absolute().Fill().Padding(24).Gap(16).Transition(fade).Children(func() {
					Box(c.Key("hero")).Height(320).Radius(16).Background(color(open)).Transition(tile)
					Textf(c, "Tile %d at dusk", open).FontSize(28).Bold().Transition(title).ContentTransition(morph)
					Text(c, "The light goes from the sea to the sky as the sun sets behind the hills, and the boats come in.").Wrap()
				})
			})
		}
		step := func() {
			frame++
			if period > 0 && frame%period == 0 {
				if open < 0 {
					open = frame / period * 7 % n
				} else {
					open = -1
				}
			}
		}
		return view, step
	}
}

// BenchmarkMagicMove builds, lays out and paints the frames of a grid of
// 60 tiles opening one into a detail view and closing it: steady, with
// nothing moving, and toggling every 18 frames, the 300 ms the
// transitions take, with the views' shared elements moving (match) or
// the views only fading; and steady with 600 tiles, whose 1200 elements
// of a Match each frame notes.
func BenchmarkMagicMove(b *testing.B) {
	for _, sc := range []struct {
		name   string
		n      int
		period int
		match  bool
	}{
		{"steady", 60, 0, false},
		{"steady_match", 60, 0, true},
		{"toggle", 60, 18, false},
		{"toggle_match", 60, 18, true},
		{"steady_600", 600, 0, false},
		{"steady_match_600", 600, 0, true},
	} {
		b.Run(sc.name, func(b *testing.B) {
			f := newStressFrames(magicMoveScene(sc.n, sc.period, sc.match), nil)
			b.ReportAllocs()
			for b.Loop() {
				f.next()
			}
		})
	}
}

// TestMagicMoveScene checks the benchmark's scene flies its elements.
func TestMagicMoveScene(t *testing.T) {
	f := newStressFrames(magicMoveScene(60, 18, true), nil)
	for range 15 {
		f.next() // to the first toggle
	}
	flew := 0
	for range 36 {
		f.next()
		if len(f.rt.lifted) > 0 {
			flew++
		}
	}
	if flew < 30 {
		t.Errorf("elements flew in %d frames of 36", flew)
	}
}
