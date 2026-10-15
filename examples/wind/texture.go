package main

import (
	"image"
	"math"
	"runtime"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
)

// The grid: cells cellSize DIPs wide and twice as high, at least minCols
// by minRows of them, and no more than maxPixels pixels in all.
const (
	cellSize         = 6.5
	minCols, minRows = 24, 8
	maxPixels        = 21e6
)

// The clock: a frame every frameTime, or rippleTime while a click's blow
// runs out; the wind blows for budget seconds after the last thing the
// user did and settles over settleTime, gathers speed again over
// resumeTime, and the texture comes in over revealTime seconds of wind
// and fades in over fadeTime.
const (
	frameTime, rippleTime          = 40 * time.Millisecond, 10 * time.Millisecond
	budget, settleTime, resumeTime = 180, 8, 3
	revealTime                     = 4
	fadeTime                       = 3 * time.Second
	// strength is how strong the canvas shows: its light is drawn
	// headroom times weaker, leaving room for the ripples' color above it.
	strength = 0.26
)

var (
	// inkColor is the plain glyphs' color, glowLight and glowDark the
	// ripples' on a light and a dark theme.
	inkColor  = ui.Hex("#898781")
	glowLight = [3]float32{0, 0.25, 1}
	glowDark  = [3]float32{0.1, 0.68, 1}
	// fadeEase is CSS's ease-out.
	fadeEase = ui.CubicBezier(0, 0, 0.58, 1)
)

// grid is the cells of a pane: their size and count, and where the first
// lies, in pixels; the grid is centered on the pane and overhangs it.
type grid struct {
	cellW, cellH, cols, rows int
	left, top                int
	scale                    float32
}

// gridOf returns the grid of a pane of w×h pixels at scale pixels a DIP,
// or false for a pane too small for one.
func gridOf(w, h int, scale float32) (grid, bool) {
	if w <= 0 || h <= 0 || scale <= 0 {
		return grid{}, false
	}
	cw := max(5, int(math.Round(cellSize*float64(scale))))
	g := grid{cellW: cw, cellH: 2 * cw, scale: scale}
	g.cols, g.rows = (w+cw-1)/cw, (h+2*cw-1)/(2*cw)
	if g.cols < minCols || g.rows < minRows {
		return grid{}, false
	}
	if k := float64(g.cols*g.cellW*g.rows*g.cellH) / maxPixels; k > 1 {
		g.cols, g.rows = int(float64(g.cols)/math.Sqrt(k)), int(float64(g.rows)/math.Sqrt(k))
	}
	g.left = int(math.Round(float64(w-g.cols*g.cellW) / 2))
	g.top = int(math.Round(float64(h-g.rows*g.cellH) / 2))
	return g, true
}

// cellX and cellY return the middle of a cell's column and row in the
// pane, in DIPs.
func (g grid) cellX(cx int) float32 {
	return (float32(g.left) + (float32(cx)+0.5)*float32(g.cellW)) / g.scale
}
func (g grid) cellY(cy int) float32 {
	return (float32(g.top) + (float32(cy)+0.5)*float32(g.cellH)) / g.scale
}

// atlasKey is what an atlas is drawn for.
type atlasKey struct {
	cellW int
	scale float32
}

// texture is the glyphs a pane shows, blown by a wind.
type texture struct {
	w      wind
	recent []string // the looks of the last visits, latest first
	// later runs a function outside the window's frames, where the
	// atlases are drawn (ui.Render draws a frame of its own).
	later   func(fn func())
	atlases map[atlasKey]*atlas
	asked   map[atlasKey]bool
	hand    hand
	field   field
	// The clock: when the texture started, which the hand's times count
	// from, its last frame, and when it first showed.
	start, last, shown time.Time
	// windTime is the wind's time; active the seconds the texture moved;
	// revealFrom when it last began to come in, resumedAt when the wind
	// last resumed, and until when it blows; calm that it has settled.
	windTime, active             float64
	revealFrom, resumedAt, until float64
	energy                       float64
	calm                         bool
	// still draws a frame that does not move, while the desktop asks for
	// less motion.
	still bool
	// The frame shown: two images, one shown while the other is drawn,
	// and the bitmap that shows them.
	images [2]*image.RGBA
	front  int
	bitmap *ui.Bitmap
	drawn  grid
	// When the frame shown was drawn, and on which theme.
	rendered time.Time
	dark     bool
}

// newTexture returns a texture blown by the wind of seed.
func newTexture(seed uint32, later func(fn func())) *texture {
	t := &texture{later: later, atlases: map[atlasKey]*atlas{}, asked: map[atlasKey]bool{}}
	t.reseed(seed)
	return t
}

// reseed starts a new visit, with the wind of seed, in a look the last
// visits did not show.
func (t *texture) reseed(seed uint32) {
	look := pickLook(seed, t.recent)
	t.recent = append([]string{look}, t.recent...)
	if len(t.recent) > 3 {
		t.recent = t.recent[:3]
	}
	t.w = newWind(seed, look)
	t.windTime = float64(seed) / 4294967296 * 64
	t.active, t.revealFrom, t.resumedAt, t.until = 0, 0, -resumeTime, budget
	t.energy, t.calm = 1, false
	t.last, t.shown = time.Time{}, time.Time{}
	t.hand = hand{}
}

// pickLook returns the look of seed among those not in recent, or not the
// latest of them.
func pickLook(seed uint32, recent []string) string {
	var fresh, notLast, all []string
	for _, l := range looks {
		all = append(all, l.name)
		if !contains(recent, l.name) {
			fresh = append(fresh, l.name)
		}
		if len(recent) == 0 || recent[0] != l.name {
			notLast = append(notLast, l.name)
		}
	}
	pick := all
	if len(fresh) > 0 {
		pick = fresh
	} else if len(notLast) > 0 {
		pick = notLast
	}
	return pick[int(random(seed, "look")()*float64(len(pick)))]
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// clock returns the seconds since the texture started, as the hand counts
// them.
func (t *texture) clock(now time.Time) float32 {
	if t.start.IsZero() {
		t.start = now
	}
	return float32(now.Sub(t.start).Seconds())
}

// wake has the wind blow again for its whole budget, after the user did
// something: settled, it comes in anew.
func (t *texture) wake() {
	if t.calm {
		t.calm = false
		t.revealFrom, t.resumedAt, t.until = t.active, t.active, t.active+budget
		t.last = time.Time{}
	} else if t.active < t.until {
		t.until = t.active + budget
	}
}

// move and tap note the pointer's moves and clicks at (x, y) in the pane,
// in DIPs.
func (t *texture) move(x, y float32, now time.Time) {
	if t.still {
		return
	}
	t.hand.move(x, y, t.clock(now))
	t.wake()
}

func (t *texture) tap(x, y float32, now time.Time) {
	if t.still {
		return
	}
	t.hand.tap(x, y, t.clock(now))
	t.wake()
}

// step moves the clock to now: the wind blows as long as its budget
// lasts, then settles, the texture fading with it.
func (t *texture) step(now time.Time) {
	dt := 0.0
	if !t.last.IsZero() {
		dt = min(max(now.Sub(t.last).Seconds(), 0), 0.25)
	}
	t.last = now
	t.active += dt
	t.energy = 1 - smooth((t.active-t.until)/settleTime)
	speed := smooth((t.active - t.resumedAt) / resumeTime)
	t.windTime += dt * t.energy * speed
}

func smooth(v float64) float64 { return float64(smoothstep(0, 1, float32(v))) }

// draw paints the texture in r, the pane, with p, moving it on when its
// next frame is due.
func (t *texture) draw(p *ui.Painter, r ui.Rect, dark bool) {
	scale := p.Scale()
	g, ok := gridOf(int(math.Round(float64(r.W*scale))), int(math.Round(float64(r.H*scale))), scale)
	if !ok {
		return
	}
	key := atlasKey{g.cellW, scale}
	a := t.atlases[key]
	if a == nil {
		if !t.asked[key] && t.later != nil {
			t.asked[key] = true
			t.later(func() { t.atlases[key] = newAtlas(key.cellW, key.scale) })
		}
		// Until the window can run it, ask again.
		if t.later == nil {
			p.After(frameTime)
		}
		return
	}
	now := p.Now()
	moving := !t.still && !t.calm
	// A frame may paint this more than once, and other repaints do too, as
	// the caret blinks: the texture moves on at its own pace.
	due := t.bitmap == nil || g != t.drawn || dark != t.dark || t.shown.IsZero() ||
		!now.Equal(t.rendered) && (moving && now.Sub(t.last) >= t.interval(now)/2 ||
			now.Sub(t.shown) < fadeTime+frameTime)
	if due {
		if t.shown.IsZero() {
			t.shown = now
		}
		if moving {
			t.step(now)
		}
		t.render(g, a, r, dark, now)
		t.rendered, t.dark = now, dark
		if moving && t.energy == 0 && !t.hand.busy(t.clock(now)) {
			t.calm = true
		}
	}
	if t.bitmap != nil {
		k := 1 / scale
		dst := ui.Rect{X: r.X + float32(g.left)*k, Y: r.Y + float32(g.top)*k, W: float32(g.cols*g.cellW) * k, H: float32(g.rows*g.cellH) * k}
		p.Clip(r, 0, func() { p.ImageRegion(t.bitmap, ui.Rect{}, dst, ui.ImageOptions{}) })
	}
	switch {
	case !t.still && !t.calm:
		p.After(t.interval(now))
	case now.Sub(t.shown) < fadeTime:
		p.After(frameTime)
	}
}

// interval returns the time between frames at now.
func (t *texture) interval(now time.Time) time.Duration {
	if t.hand.rippling(t.clock(now)) {
		return rippleTime
	}
	return frameTime
}

// render draws a frame of the texture, of grid g in pane r, into the
// image not shown, and shows it.
func (t *texture) render(g grid, a *atlas, r ui.Rect, dark bool, now time.Time) {
	w, h := g.cols*g.cellW, g.rows*g.cellH
	back := 1 - t.front
	img := t.images[back]
	if img == nil || img.Rect.Dx() != w || img.Rect.Dy() != h {
		img = image.NewRGBA(image.Rect(0, 0, w, h))
		t.images[back] = img
	} else {
		clear(img.Pix)
	}

	f := &t.field
	f.noises = noises{seed: t.w.seed}
	f.w, f.hand = &t.w, &t.hand
	f.time = float32(t.windTime)
	reveal := smooth((t.active-t.revealFrom)/revealTime) * t.energy
	if t.still {
		reveal = 1
		t.hand = hand{}
	}
	f.reveal = float32(reveal)
	t.hand.frame(t.clock(now))
	k := 1 / g.scale
	f.paneW, f.paneH = r.W, r.H
	f.cols, f.rows = g.cols, 2*g.rows
	f.originX, f.originY = float32(g.left)*k, float32(g.top)*k
	f.stepX, f.stepY = float32(g.cellW)*k, float32(g.cellH)*k/2
	f.prepare(float32(w)*k, float32(h)*k)
	parallel(f.rows, f.compute)

	glow := glowLight
	if dark {
		glow = glowDark
	}
	pt := &paint{
		ink:      [3]float32{float32(inkColor.R) / 255, float32(inkColor.G) / 255, float32(inkColor.B) / 255},
		glow:     glow,
		inkAlpha: float32(inkColor.A) / 255,
		pix:      img.Pix,
		stride:   img.Stride,
	}
	// The canvas shows headroom times as strong as its plain light, and
	// fades in with the pane holding it, both over fadeTime.
	fade := float32(1)
	if since := now.Sub(t.shown); since < fadeTime {
		e := fadeEase(float32(since) / float32(fadeTime))
		fade = e * e
	}
	pt.opacity = min(1, strength*headroom) * fade
	seq := a.sequence(t.w.look)
	parallel(g.rows, func(from, to int) { glyphRows(g, a, seq, f, pt, from, to) })

	if t.bitmap == nil {
		t.bitmap = ui.NewBitmap(img)
	} else {
		t.bitmap.Update(img)
	}
	t.front, t.drawn = back, g
}

// parallel calls fn on runs of n rows, from as many goroutines as there
// are processors, and returns when they are done.
func parallel(n int, fn func(from, to int)) {
	workers := min(runtime.GOMAXPROCS(0), n)
	if workers <= 1 {
		fn(0, n)
		return
	}
	var wg sync.WaitGroup
	chunk := (n + workers - 1) / workers
	for from := 0; from < n; from += chunk {
		wg.Add(1)
		go func(from, to int) {
			defer wg.Done()
			fn(from, to)
		}(from, min(from+chunk, n))
	}
	wg.Wait()
}
