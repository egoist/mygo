// SF Symbols shows Apple's SF Symbols, drawn by the running macOS through
// the sfsymbols plugin, in native UI, with their options: size, color, and
// whether each fills its box or keeps its natural size. A name the system
// has no symbol for, and every symbol on another OS, shows a dash instead.
//
//	go run ./examples/sf-symbols
package main

import (
	"log"
	"math"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/sfsymbols"
	"github.com/egoist/mygo/ui"
)

// names are the symbols the window shows, the last of them one that no
// macOS has.
var names = []string{
	"gearshape", "magnifyingglass", "xmark", "checkmark", "plus", "minus",
	"trash", "pencil", "terminal", "globe", "cpu", "keyboard",
	"sidebar.leading", "sidebar.trailing", "rectangle.split.2x1", "rectangle.on.rectangle",
	"arrow.triangle.branch", "doc.text", "note.text", "sparkles",
	"server.rack", "display", "laptopcomputer", "no.such.symbol",
}

// gallery is what the window shows: the options chosen.
type gallery struct {
	size, color int
	natural     bool
}

var sizes = [...]float32{16, 24, 32, 48}

func (g *gallery) view(c *ui.Context) {
	t := c.Theme()
	size := sizes[g.size]
	col := [...]ui.Color{t.Text, t.Accent, t.TextMuted}[g.color]
	// Three pixels to a DIP: the symbols stay sharp on any display.
	px := int(math.Round(float64(size) * 3))
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Wrap().Gap(12).Padding(16).AlignItems(ui.Center).Children(func() {
			ui.Segmented(c, &g.size, "16", "24", "32", "48").Label("Size")
			ui.Segmented(c, &g.color, "Text", "Accent", "Muted").Label("Color")
			ui.Checkbox(c, &g.natural, "Natural size")
			if !sfsymbols.Available() {
				ui.Text(c, "This system has no SF Symbols").TextColor(t.TextMuted)
			}
		})
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Row(c).Wrap().Gap(12).Padding(0, 16, 16).Children(func() {
				for _, name := range names {
					ui.Column(c).Width(148).Height(108).Center().Gap(12).Radius(t.Radius).
						Background(t.Surface).Border(1, t.Border).Children(func() {
						ui.Column(c).Height(48).Center().Children(func() {
							if src, ok := sfsymbols.Image(name, px, col, sfsymbols.Options{Natural: g.natural}); ok {
								ui.Image(c, src).Size(size, size)
							} else {
								ui.Text(c, "—").FontSize(size).TextColor(t.TextMuted)
							}
						})
						ui.Text(c, name).FontSize(12).TextColor(t.TextMuted)
					})
				}
			})
		})
	})
}

func main() {
	g := &gallery{size: 1}
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title:   "SF Symbols",
			Width:   840,
			Height:  640,
			Content: ui.View(g.view),
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
