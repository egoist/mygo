// SF Symbols shows Apple's SF Symbols, drawn by the running macOS through
// the sfsymbols plugin, in native UI, as SwiftUI's Image(systemName:) shows
// them, with their modifiers: size, color, weight, the filled variant, and
// whether each fills its box or keeps its natural size. A name the system
// has no symbol for, and every symbol on another OS, shows its fallback.
//
//	go run ./examples/sf-symbols
package main

import (
	"log"

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

// missing is the fallback of a symbol the system has not: a dashed square.
var missing = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-dasharray="3 3"><rect x="3" y="3" width="18" height="18" rx="3"/></svg>`))

// gallery is what the window shows: the options chosen.
type gallery struct {
	size, color, weight int
	fill, natural       bool
}

var weights = [...]sfsymbols.Weight{sfsymbols.Light, sfsymbols.Regular, sfsymbols.Semibold, sfsymbols.Black}

var sizes = [...]float32{16, 24, 32, 48}

func (g *gallery) view(c *ui.Context) {
	t := c.Theme()
	size := sizes[g.size]
	col := [...]ui.Color{t.Text, t.Accent, t.TextMuted}[g.color]
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Wrap().Gap(12).Padding(16).AlignItems(ui.Center).Children(func() {
			ui.Segmented(c, &g.size, "16", "24", "32", "48").Label("Size")
			ui.Segmented(c, &g.color, "Text", "Accent", "Muted").Label("Color")
			ui.Segmented(c, &g.weight, "Light", "Regular", "Semibold", "Black").Label("Weight")
			ui.Checkbox(c, &g.fill, "Fill")
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
							s := sfsymbols.Symbol(c, name).Size(size).Color(col).Weight(weights[g.weight]).Fallback(missing)
							if g.fill {
								s.Fill()
							}
							if g.natural {
								s.Natural()
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
	g := &gallery{size: 1, weight: 1}
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
