// The #1841 repro: 1000 nodes and 2000 translucent cubic StrokePath links
// under a zoom that changes every frame. "still" keeps the zoom at 0.25
// (and repaints every frame) so shots of two builds compare. It prints
// the frames drawn per second to stdout every 2s.
package main

import (
	"fmt"
	"log"
	"math"
	"os"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const nNodes = 1000

var (
	pos    [nNodes][2]float32
	links  [][2]int
	path   ui.Path
	start  = time.Now()
	still  = len(os.Args) > 1 && os.Args[1] == "still"
	frames int
	since  = time.Now()
)

func init() {
	for i := 0; i < nNodes; i++ {
		pos[i] = [2]float32{float32((i % 40) * 200), float32((i / 40) * 100)}
		links = append(links, [2]int{i, (i*7 + 13) % nNodes}, [2]int{i, (i*13 + 5) % nNodes})
	}
}

func draw(p *ui.Painter, r ui.Rect) {
	frames++
	if d := time.Since(since); d >= 2*time.Second {
		fmt.Printf("fps %.1f\n", float64(frames)/d.Seconds())
		frames, since = 0, time.Now()
	}
	p.Fill(r, ui.Hex("#e6e6e6"), 0)
	t := time.Since(start).Seconds()
	z := float32(0.25 + 0.1*math.Sin(t))
	if still {
		z = 0.25
	}
	ox, oy := r.X+20, r.Y+20
	for _, l := range links {
		a, b := pos[l[0]], pos[l[1]]
		x0, y0 := ox+(a[0]+120)*z, oy+(a[1]+24)*z
		x1, y1 := ox+b[0]*z, oy+(b[1]+24)*z
		c1x, c2x := x0+80*z, x1-80*z
		path.Reset()
		path.MoveTo(x0, y0)
		path.CubeTo(c1x, y0, c2x, y1, x1, y1)
		p.StrokePath(&path, 1.5*z, ui.RGBA(0x70, 0x90, 0xb0, 0.8))
	}
	for i := 0; i < nNodes; i++ {
		p.Fill(ui.Rect{X: ox + pos[i][0]*z, Y: oy + pos[i][1]*z, W: 120 * z, H: 48 * z}, ui.Hex("#404040"), 8*z)
	}
	p.AnimationFrame()
}

func main() {
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{Title: "strokepaths", Width: 1000, Height: 700, Content: ui.View(func(c *ui.Context) {
			ui.Box(c.Key("canvas")).Fill().Draw(draw)
		})})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
