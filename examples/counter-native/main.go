package main

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// view builds the interface from the state, on the main thread, whenever
// the window needs a frame: the shortcuts in Go, the screen in
// counter.view.
func (s *counter) view(c *ui.Context) {
	if c.Shortcut(0, ui.KeyUp) {
		s.n++
	}
	if c.Shortcut(0, ui.KeyDown) {
		s.n--
	}
	s.View(c)
}

func main() {
	s := &counter{}
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title:   "Counter",
			Width:   360,
			Height:  260,
			Content: ui.View(s.view),
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
