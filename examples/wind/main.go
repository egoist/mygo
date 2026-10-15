// Wind is the texture behind the composer of the Claude desktop app's
// Code tab, redrawn in native UI: a field of monospaced characters that a
// wind lights in passing gusts, curling, streaked and thinning around the
// content, while each character steps through a set of its own. Moving
// the pointer warms the air along its path; a click blows a ripple out
// from it, tinted, and every third click blows a heart.
//
//	go run ./examples/wind
//
// It draws as the app's WebGL does, on the CPU: the wind is worked out
// at two samples a cell, the glyphs come from an atlas the system's text
// engine rasterizes (ui.Render), and each pixel of their ink is lit by
// the wind's samples around it, more where its edge faces the wind,
// into a bitmap shown as video is (Bitmap.Update). Every visit has a
// wind of its own, picked by a random seed; ⌘R (Ctrl+R) starts another.
// After three minutes without input the wind settles and the texture
// fades out, to come back at the next move.
package main

import (
	"log"
	"math/rand/v2"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const windowW, windowH = 1100, 720

func main() {
	s := newApp(rand.Uint32())
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:   "Wind",
			Width:   windowW,
			Height:  windowH,
			Content: ui.View(s.view),
		})
		// The atlases are drawn between the window's frames.
		s.tex.later = win.Update
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
