// Command app is MyGo's side of the comparison with GPUI: the stress scenes of
// ui/stress_bench_test.go in a real window, and the scenes only the comparison
// has. See ../README.md.
//
// A frame's view changes what the scene shows and asks for the next frame. It
// counts warmup frames, prints "ready", and once the driver answers on stdin
// counts the frames it measures, then prints what it measured as JSON.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// meter measures frames, the same on both sides.
type meter struct {
	warmup, want, seen int
	first              time.Time
	started            atomic.Bool
	ready, measuring   bool
	done               bool
	stamps             []time.Time
	thread0, process0  uint64
}

func (m *meter) tick() {
	m.seen++
	if m.done {
		return
	}
	if m.seen == 1 {
		m.first = time.Now()
	}
	// Scenes too heavy for many frames warm up for 5 seconds, and measure 12.
	if !m.ready && (m.seen >= m.warmup || time.Since(m.first) > 5*time.Second) {
		m.ready = true
		fmt.Println("ready")
		go func() {
			bufio.NewReader(os.Stdin).ReadString('\n')
			m.started.Store(true)
			// The driver ends this process when it has what it needs.
		}()
	}
	if !m.measuring {
		if m.started.Load() {
			m.measuring = true
			m.stamps = append(make([]time.Time, 0, m.want+1), time.Now())
			m.thread0, m.process0 = threadCycles(), processCycles()
		}
		return
	}
	m.stamps = append(m.stamps, time.Now())
	if n := len(m.stamps) - 1; n >= m.want || (n >= 30 && time.Since(m.stamps[0]) > 12*time.Second) {
		thread, process := threadCycles()-m.thread0, processCycles()-m.process0
		intervals := make([]float64, n)
		for i := range intervals {
			ms := float64(m.stamps[i+1].Sub(m.stamps[i])) / 1e6
			intervals[i] = math.Round(ms*1000) / 1000
		}
		out, _ := json.Marshal(map[string]any{
			"frames": n, "thread_cycles": thread, "process_cycles": process,
			"tid": threadID(), "intervals_ms": intervals,
		})
		fmt.Println(string(out))
		m.done = true
	}
}

// bench holds a scene's state.
type bench struct {
	scene        string
	count, frame int
	meter        *meter
	list         ui.ListState
	scroll       ui.ScrollState
	right        bool
	half         float32
}

var light = ui.LightTheme()

const words = "The quick brown fox jumps over the lazy dog while a sphinx of black quartz judges my vow, and five boxing wizards jump quickly past the pack of liquor jugs that sits beside the old stone wall at the end of a long and winding road through the hills."

func row(c *ui.Context, label string) {
	ui.Row(c).Height(28).PaddingX(12).Gap(8).Children(func() {
		ui.Text(c, label).Grow(1)
		ui.Text(c, "detail").TextColor(ui.RGB(110, 110, 120))
	})
}

func (b *bench) boxes(c *ui.Context, left float32) {
	frame := b.frame
	for i := range b.count {
		x := 8 + float32(i%30)*29 + float32(frame%40)*0.25
		y := 8 + float32(i/30%20)*31 + float32((frame+i)%20)*0.5
		t := frame + i
		ui.Box(c).Absolute().Left(left+x).Top(y).Size(24, 24).Radius(6).
			Shadow(0, 2, 8, 0, ui.RGBA(0, 0, 0, 0.27)).
			Background(ui.RGB(uint8(40+t*7%180), uint8(80+i*13%150), uint8(120+frame*3%120)))
	}
}

func (b *bench) view(c *ui.Context) {
	b.meter.tick()
	b.frame++
	// The light theme whatever the system's, as GPUI has none.
	ui.Themed(c, light).Fill().Children(func() { b.draw(c) })
}

func (b *bench) draw(c *ui.Context) {
	frame := b.frame
	switch b.scene {
	case "idle":
		ui.Column(c).Fill().Center().Gap(16).Background(ui.RGB(0x7b, 0x2f, 0xf7)).Children(func() {
			ui.Text(c, "0").FontSize(56)
			ui.Box(c).PaddingX(16).PaddingY(8).Radius(6).Background(ui.RGB(229, 231, 235)).Children(func() { ui.Text(c, "Add") })
		})
		return
	case "input", "input-busy":
		busy := b.scene == "input-busy"
		if busy {
			c.AnimationFrame()
		}
		bg := ui.RGB(0, 0, 0)
		if b.right {
			bg = ui.RGB(255, 255, 255)
		}
		root := ui.Box(c).Fill().Background(bg)
		root.HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind == ui.InputPointerMove {
				if right := ev.X >= b.half; right != b.right {
					b.right = right
					c.Invalidate()
				}
			}
			return false
		})
		if busy {
			root.Children(func() { b.boxes(c, 40) })
		}
		return
	}
	c.AnimationFrame()
	switch b.scene {
	case "boxes":
		ui.Box(c).Fill().Background(ui.RGB(246, 247, 249)).Children(func() { b.boxes(c, 0) })
	case "labels":
		ui.Column(c).Fill().Padding(8).Gap(4).Background(ui.RGB(255, 255, 255)).Children(func() {
			for r := range 20 {
				ui.Row(c).Gap(4).Children(func() {
					for col := range 15 {
						ui.Textf(c, "%d", (frame*31+r*15+col)*7919%100000).Width(54).FontSize(13)
					}
				})
			}
		})
	case "translated_column":
		ui.Box(c).Fill().Clip().Background(ui.RGB(255, 255, 255)).Children(func() {
			ui.Column(c).FillWidth().Translate(0, -float32(frame%2000)*3).Children(func() {
				for i := range 1000 {
					row(c, fmt.Sprintf("Row %d", i))
				}
			})
		})
	case "scrolled_column":
		b.scroll.Y = float32(frame%2000) * 3
		ui.Scroll(c).Fill().Background(ui.RGB(255, 255, 255)).TrackScroll(&b.scroll).Children(func() {
			for i := range 1000 {
				row(c, fmt.Sprintf("Row %d", i))
			}
		})
	case "list":
		b.list.ScrollTo(frame*3%99_000, ui.Start)
		ui.List(c, &b.list, 100_000).Fill().Background(ui.RGB(255, 255, 255)).Rows(func(r ui.ListRow) {
			row(r.Context, fmt.Sprintf("Row %d", r.Index))
		})
	case "dashboard":
		ui.Column(c).Fill().Padding(16).Gap(16).Background(ui.RGB(242, 243, 246)).Children(func() {
			for r := range 3 {
				ui.Row(c).Gap(16).Grow(1).Children(func() {
					for col := range 4 {
						i := r*4 + col
						v := (frame*(i+3) + i*17) % 1000
						ui.Column(c).Grow(1).Basis(0).Padding(14).Gap(8).Radius(10).Background(ui.RGB(255, 255, 255)).
							Shadow(0, 4, 16, 0, ui.RGBA(0, 0, 0, 0.12)).Children(func() {
							ui.Textf(c, "Metric %d", i+1).FontSize(12).TextColor(ui.RGB(110, 110, 120))
							ui.Textf(c, "%d.%d%%", v/10, v%10).FontSize(28).Bold()
							ui.Progress(c, float64(v)/1000).FillWidth()
							ui.Textf(c, "%d requests, %d errors", v*13, v%7).FontSize(12)
						})
					}
				})
			}
		})
	case "paragraphs":
		ui.Column(c).Fill().Padding(16).Gap(8).Background(ui.RGB(255, 255, 255)).Children(func() {
			for i := range b.count {
				ui.Textf(c, "%d %d: %s", frame*(i+1), i, words).FontSize(13).FillWidth()
			}
		})
	default:
		log.Fatalf("no scene %s", b.scene)
	}
}

func main() {
	scene := flag.String("scene", "boxes", "the scene to draw")
	count := flag.Int("count", 600, "how many things the scene draws, where it can say")
	warmup := flag.Int("warmup", 120, "frames before measuring")
	frames := flag.Int("frames", 600, "frames to measure")
	x := flag.Int("x", 40, "where the window opens")
	y := flag.Int("y", 40, "where the window opens")
	w := flag.Int("w", 900, "width of the window's content")
	h := flag.Int("h", 640, "height of the window's content")
	flag.Parse()
	b := &bench{scene: *scene, count: *count, half: float32(*w) / 2, meter: &meter{warmup: *warmup, want: *frames}}
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{Title: "gpuibench", X: *x, Y: *y, Width: *w, Height: *h, UseContentSize: true, Content: ui.View(b.view)})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
