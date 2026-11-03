// Liquid-glass shows MyGo's Liquid Glass, native UI and a web page side by
// side: merged panes and an interactive button over a painted gradient, and
// a toolbar of the system's glass over a web view, where macOS draws
// NSGlassEffectView over the page.
//
//	go run ./examples/liquid-glass
package main

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

func main() {
	var page *mygo.WebView
	mygo.App.WhenReady(func() {
		w := mygo.NewWindow(mygo.WindowOptions{
			Title:  "Liquid Glass",
			Width:  900,
			Height: 560,
			Hidden: true,
			Content: ui.View(func(c *ui.Context) {
				c.Root().Background(ui.Transparent)
				ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
					// The painted side: the shader, which merges panes.
					ui.Column(c).Width(420).Gradient(ui.Hex("#0f172a"), ui.Hex("#312e81"), 135).Padding(28).Gap(18).Children(func() {
						ui.Text(c, "Merged glass").FontSize(30).Bold().TextColor(ui.RGB(255, 255, 255))
						ui.Text(c, "Panes that nearly touch become one surface.").TextColor(ui.RGBA(255, 255, 255, 0.7))
						g := glass.NewGroup(c)
						g.Radius = 22
						g.Gap(6).Children(func() {
							for _, label := range []string{"Cut", "Copy", "Paste"} {
								g.Pane(ui.Box(c).Size(96, 44).Center().Children(func() {
									ui.Text(c, label).Bold()
								}))
							}
						})
						button := ui.Row(c).Padding(10, 18).AlignSelf(ui.Start).Radius(22).Material(glass.Glass{
							Tint:        ui.RGBA(139, 92, 246, 0.55),
							Interactive: true,
						}).Children(func() {
							ui.Text(c, "Interactive glass").Bold().TextColor(ui.RGB(255, 255, 255))
						})
						if button.Clicked() {
							c.Toast("The glass grew, as AppKit's does")
						}
						ui.Box(c).Grow(1)
						ui.Text(c, "Drag the window over other apps: the web page stays under the toolbar.").FontSize(12).TextColor(ui.RGBA(255, 255, 255, 0.6))
					})
					// The web side: the system's glass, over the page.
					ui.Box(c).Grow(1).Children(func() {
						if page != nil {
							ui.WebView(c, page).Fill()
						}
						ui.Row(c).Absolute().Top(16).Left(16).Right(16).Height(54).Radius(27).Padding(0, 18).AlignItems(ui.Center).Material(glass.Glass{Native: true}).Children(func() {
							ui.Text(c, "Native glass over the web page").Bold()
						})
					})
				})
			}),
		})
		w.OnReadyToShow(func() {
			v, err := w.NewWebView(mygo.WebViewOptions{})
			if err != nil {
				log.Print(err)
				return
			}
			w.Update(func() { page = v })
			v.Page().LoadHTML(`<html><body style="margin:0;background:linear-gradient(135deg,#ff2d55,#5856d6 48%,#34c759);font:44px -apple-system;color:white"><div style="padding:120px 40px;line-height:1.15">A web page under<br>Liquid Glass</div><p style="padding:0 40px;font-size:18px;opacity:.8">Scroll, select and click as usual: the glass is under the content.</p></body></html>`, "")
			w.Show()
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
