// The repro of #1840, sized for the runner's screen. Left: a card laid
// out at 1 and scaled 1.6x (with .Scale, or "anim": an AnimatePose growing
// it from 1 to 1.6 two seconds in, over 1.5s; "cache" also calls .Cache()).
// Right: the same card laid out at 1.6x. Text color is black on white.
package main

import (
	"log"
	"os"
	"slices"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const k = 1.6

var (
	start = time.Now()
	anim  = slices.Contains(os.Args[1:], "anim")
	cache = slices.Contains(os.Args[1:], "cache")
)

func card(c *ui.Context, key string, s float32) ui.Element {
	el := ui.Box(c.Key(key)).Size(240*s, 90*s).Background(ui.Hex("#ffffff")).
		Border(1*s, ui.Hex("#000000")).Radius(12*s)
	el.Children(func() {
		ui.Text(c.Key(key+"t1"), "Scaled text 0123").Absolute().Left(12*s).Top(8*s).FontSize(20*s).FontWeight(700).TextColor(ui.Hex("#000000"))
		ui.Text(c.Key(key+"t2"), "The quick brown fox").Absolute().Left(12*s).Top(40*s).FontSize(14*s).TextColor(ui.Hex("#000000"))
	})
	return el
}

func view(c *ui.Context) {
	ui.Row(c).Fill().Center().Gap(130).Background(ui.Hex("#e6e6e6")).Children(func() {
		left := card(c, "scaled", 1)
		if anim {
			p := ui.Pose{}
			if d := time.Since(start); d >= 2*time.Second {
				p = ui.Pose{Scale: k}
			} else {
				c.After(2*time.Second - d)
			}
			left.AnimatePose(p, ui.Animation{Duration: 1500 * time.Millisecond})
		} else {
			left.Scale(k, k)
		}
		if cache {
			left.Cache()
		}
		card(c, "laidout", k)
	})
}

func main() {
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{Title: "textscale", Width: 960, Height: 520, Content: ui.View(view)})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
