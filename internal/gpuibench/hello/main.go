// Hello is the smallest app worth measuring, as GPUI's in
// internal/gpuibench/gpui/src/bin/hello.rs: a number and a box that counts
// when clicked. Its size is what an app pays for MyGo's native UI.
package main

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type hello struct{ n int }

func (h *hello) view(c *ui.Context) {
	// The light theme whatever the system's, as GPUI has none.
	ui.Themed(c, ui.LightTheme()).Fill().Center().Gap(16).Background(ui.RGB(255, 255, 255)).Children(func() {
		ui.Textf(c, "%d", h.n).FontSize(56)
		add := ui.Box(c.Key("add")).PaddingX(16).PaddingY(8).Radius(6).Background(ui.RGB(229, 231, 235))
		add.Children(func() { ui.Text(c, "Add") })
		if add.Clicked() {
			h.n++
		}
	})
}

func main() {
	h := &hello{}
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{Title: "Hello", Width: 360, Height: 260, UseContentSize: true, Content: ui.View(h.view)})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
