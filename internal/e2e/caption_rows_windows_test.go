//go:build windows && (amd64 || arm64)

package e2e

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo"
	win "github.com/egoist/mygo/internal/windows"
	"github.com/egoist/mygo/ui"
)

// A window with a hidden title bar and a material shows one row of window
// controls, its own, as the screen shows it. DWM draws the caption buttons
// of the window's styles wherever its frame extends over the window's top,
// and they show through the all but transparent backplates of the
// window's own: under a title bar taller than DWM's caption, a second row
// above them.
//
// The material shows where the view draws no background, as Downbit's main
// window draws none at its top; elsewhere DWM's buttons hide under the
// view. Windows Server, which the x64 runners have, shows no material.
func TestContentWindowMaterialCaptionButtons(t *testing.T) {
	const height = 52
	var drawn, material atomic.Bool
	w := newWindow(t, mygo.WindowOptions{X: 80, Y: 80, Width: 480, Height: 320, AlwaysOnTop: true,
		TitleBarStyle: mygo.TitleBarHiddenInset, TitleBarHeight: height, Vibrancy: mygo.VibrancyUnderWindow,
		Content: ui.View(func(c *ui.Context) {
			material.Store(c.Vibrancy())
			drawn.Store(true)
			if c.Vibrancy() {
				c.Root().Background(ui.Transparent)
			}
			ui.Text(c, "native")
		})})
	w.Focus()
	eventually(t, "the view drawn", drawn.Load)
	if !material.Load() {
		t.Skip("no material on this version of Windows: the view covers the frame")
	}
	names := [...]string{"minimize", "maximize", "close"}
	for _, state := range []string{"restored", "maximized"} {
		if state == "maximized" {
			w.Maximize()
			eventually(t, "the window maximized", w.IsMaximized)
		}
		var glyphs [3][][2]float64
		var scale float64
		shown := func() bool {
			glyphs, scale = captionGlyphs(w, height)
			for _, g := range glyphs {
				if len(g) == 0 {
					return false
				}
			}
			return true
		}
		readable := false
		for deadline := time.Now().Add(5 * time.Second); !readable && time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
			readable = shown()
		}
		if !readable {
			t.Errorf("DIAGNOSTIC %s unreadable: glyphs %v scale %v\n%s", state, glyphs, scale, inkMap(w, height))

			t.Skipf("%s: the screen does not show the window's controls (glyphs at %v): not comparing", state, glyphs)
		}
		// Whatever else DWM draws, it has drawn by a few frames later.
		time.Sleep(200 * time.Millisecond)
		shown()
		t.Errorf("DIAGNOSTIC %s: readable %v glyphs %v scale %v\n%s", state, readable, glyphs, scale, inkMap(w, height))
		for i, g := range glyphs {
			if len(g) != 1 || (g[0][0]+g[0][1])/2 < height/2-4 || (g[0][0]+g[0][1])/2 > height/2+4 {
				t.Errorf("%s: the %s button shows glyphs in rows %v DIPs (%.2fx), want one, centered in the %d DIPs of the title bar",
					state, names[i], g, scale, height)
			}
		}
	}
}

// captionGlyphs returns, for each of the three buttons 46 DIPs wide at the
// top-right of a window, the spans of rows, in DIPs from the top, where
// the screen shows a glyph over the button's background, and the window's
// scale.
func captionGlyphs(w *mygo.Window, height int) (glyphs [3][][2]float64, scale float64) {
	const buttonWidth = 46
	var px []uint32
	var pw, ph int32
	var ok bool
	mygo.RunOnMain(func() { px, pw, ph, ok = win.TestScreenTopRight(w.NativeHandle(), 3*buttonWidth, height) })
	if !ok || pw <= 0 || ph <= 0 {
		return glyphs, 0
	}
	scale = float64(ph) / float64(height)
	bw := int(pw) / 3
	for i := range glyphs {
		x0, x1 := i*bw+2, (i+1)*bw-2 // clear of the buttons' edges
		// The background is the color the button shows most.
		counts := map[uint32]int{}
		for y := range int(ph) {
			for x := x0; x < x1; x++ {
				counts[px[y*int(pw)+x]]++
			}
		}
		var back uint32
		for c, n := range counts {
			if n > counts[back] {
				back = c
			}
		}
		start, last := -1, -1
		span := func() {
			if start >= 0 {
				glyphs[i] = append(glyphs[i], [2]float64{float64(start) / scale, float64(last+1) / scale})
			}
		}
		for y := range int(ph) {
			ink := false
			for x := x0; x < x1 && !ink; x++ {
				ink = colorDistance(px[y*int(pw)+x], back) > 48
			}
			if !ink {
				continue
			}
			// Rows a pixel or two apart are one glyph's, antialiased.
			if start < 0 || y-last > 3 {
				span()
				start = y
			}
			last = y
		}
		span()
	}
	return glyphs, scale
}

// colorDistance is the largest difference between the channels of two
// 0xRRGGBB colors.
func colorDistance(a, b uint32) int {
	d := 0
	for shift := 0; shift < 24; shift += 8 {
		ca, cb := int(a>>shift&0xFF), int(b>>shift&0xFF)
		d = max(d, ca-cb, cb-ca)
	}
	return d
}

// inkMap draws the top-right corner as text, a character per pixel, for
// diagnostics: '#' far from the first pixel's color, '+' a little, '.' not.
func inkMap(w *mygo.Window, height int) string {
	var px []uint32
	var pw, ph int32
	var ok bool
	mygo.RunOnMain(func() { px, pw, ph, ok = win.TestScreenTopRight(w.NativeHandle(), 3*46, height) })
	if !ok {
		return "no pixels"
	}
	var b []byte
	b = fmt.Appendf(b, "%dx%d first %06x\n", pw, ph, px[0])
	for y := range int(ph) {
		for x := range int(pw) {
			d := colorDistance(px[y*int(pw)+x], px[y*int(pw)])
			switch {
			case d > 48:
				b = append(b, '#')
			case d > 12:
				b = append(b, '+')
			default:
				b = append(b, '.')
			}
		}
		b = fmt.Appendf(b, " %06x\n", px[y*int(pw)+int(pw)/2])
	}
	return string(b)
}
