// Liquid-glass is a gallery of MyGo's Liquid Glass, driven by its sidebar:
// the window's own material, the system's glass over a web page, merged
// groups, interactive buttons, scroll edges, clarity and blur.
//
//	go run ./examples/liquid-glass
package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

const (
	pageOverview = iota
	pageWindow
	pageNative
	pageMerged
	pageInteractive
	pageEdges
	pageClarity
	pageBlur
)

var pages = []string{"Overview", "Window glass", "Over a page", "Merged panes", "Interactive", "Scroll edges", "Clarity", "Blur"}

type gallery struct {
	win *mygo.Window
	web *mygo.WebView

	page            int
	windowStyle     int // regular, clear, none
	tint            string
	material        mygo.Vibrancy
	appliedMaterial mygo.Vibrancy
	appliedTint     string

	merge   float64
	edge    int
	clarity float64
}

var swatches = []struct {
	name  string
	css   string
	color ui.Color
}{
	{"None", "", ui.Transparent},
	{"Blue", "rgba(0, 122, 255, 0.28)", ui.RGBA(0, 122, 255, 0.28)},
	{"Purple", "rgba(139, 92, 246, 0.32)", ui.RGBA(139, 92, 246, 0.32)},
	{"Pink", "rgba(236, 72, 153, 0.28)", ui.RGBA(236, 72, 153, 0.28)},
	{"White", "rgba(255, 255, 255, 0.18)", ui.RGBA(255, 255, 255, 0.18)},
}

var cards = [][2]ui.Color{
	{ui.Hex("#ef4444"), ui.Hex("#f97316")},
	{ui.Hex("#0ea5e9"), ui.Hex("#6366f1")},
	{ui.Hex("#10b981"), ui.Hex("#14b8a6")},
	{ui.Hex("#a855f7"), ui.Hex("#ec4899")},
}

var sections = [][2]string{
	{"#ff2d55", "#ff9500"},
	{"#5856d6", "#007aff"},
	{"#34c759", "#30d158"},
	{"#bf5af2", "#ff2d55"},
}

func main() {
	g := &gallery{windowStyle: 1, tint: swatches[4].css, merge: 24, clarity: 0.5}
	mygo.App.WhenReady(func() {
		g.win = mygo.NewWindow(mygo.WindowOptions{
			Title:        "Liquid Glass",
			Width:        1100,
			Height:       720,
			Frameless:    true,
			Transparent:  true,
			CornerRadius: 20,
			Hidden:       true,
			Content:      ui.View(g.view),
		})
		g.win.OnReadyToShow(func() {
			if g.web != nil {
				return
			}
			v, err := g.win.NewWebView(mygo.WebViewOptions{})
			if err != nil {
				log.Print(err)
				return
			}
			g.win.Update(func() { g.web = v })
			v.Page().LoadHTML(pageHTML(), "")
			g.win.Show()
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

func (g *gallery) view(c *ui.Context) {
	g.syncWindow()
	t := c.Theme()
	if g.page == pageWindow {
		c.Root().Background(ui.Transparent)
	} else {
		c.Root().Background(t.Background)
	}
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		g.sidebar(c)
		ui.Box(c).Grow(1).Children(func() {
			switch g.page {
			case pageWindow:
				g.windowPage(c)
			case pageNative:
				g.nativePage(c)
			case pageMerged:
				g.mergedPage(c)
			case pageInteractive:
				g.interactivePage(c)
			case pageEdges:
				g.edgesPage(c)
			case pageClarity:
				g.clarityPage(c)
			case pageBlur:
				g.blurPage(c)
			default:
				g.overview(c)
			}
		})
	})
}

// syncWindow gives the window the material and tint of the page shown.
func (g *gallery) syncWindow() {
	if g.win == nil {
		return
	}
	want := mygo.VibrancyNone
	tint := ""
	if g.page == pageWindow {
		switch g.windowStyle {
		case 0:
			want = mygo.VibrancyGlass
		case 1:
			want = mygo.VibrancyGlassClear
		}
		tint = g.tint
	}
	if want != g.appliedMaterial {
		g.win.SetVibrancy(want)
		g.appliedMaterial = want
	}
	if tint != g.appliedTint {
		g.win.SetVibrancyTint(tint)
		g.appliedTint = tint
	}
}

func (g *gallery) sidebar(c *ui.Context) {
	t := c.Theme()
	bg := t.Surface
	if g.page == pageWindow {
		bg = t.Surface.Alpha(0.55)
	}
	ui.Column(c).Width(232).Background(bg).Padding(12).Gap(2).Children(func() {
		top := ui.Row(c).Height(42).AlignItems(ui.Center).Gap(8).DragWindow()
		top.Children(func() {
			ui.Text(c, "Liquid Glass").Bold().FontSize(15)
			ui.Box(c).Grow(1)
			if ui.Button(c, "×").Width(26).Height(24).Clicked() {
				g.win.Close()
			}
		})
		ui.Text(c, "MYGO · GLASS PLUGIN").FontSize(10).TextColor(t.TextMuted)
		ui.Box(c).Height(8)
		for i, name := range pages {
			row := ui.ButtonBase(c).Height(34).Padding(0, 10).Radius(8)
			if g.page == i {
				row = row.Background(t.Selection)
			}
			row.Children(func() {
				ui.Text(c, name).FontSize(13)
			})
			if row.Clicked() {
				g.page = i
			}
		}
		ui.Box(c).Grow(1)
		ui.Text(c, "macOS 26+ · shader elsewhere").FontSize(10).TextColor(t.TextMuted)
	})
}

func pageHead(c *ui.Context, title, body string) {
	t := c.Theme()
	ui.Text(c, title).FontSize(30).Bold()
	ui.Text(c, body).TextColor(t.TextMuted)
}

func (g *gallery) overview(c *ui.Context) {
	items := []struct {
		page  int
		title string
		body  string
	}{
		{pageWindow, "Window glass", "VibrancyGlass and VibrancyGlassClear, tinted, with what is behind the window showing through."},
		{pageNative, "Over a page", "The system's NSGlassEffectView under the content and above a web page."},
		{pageMerged, "Merged panes", "Panes that nearly touch bridge into one surface, with a live merge ratio."},
		{pageInteractive, "Interactive", "Glass that grows while pressed and lights its rim on hover."},
		{pageEdges, "Scroll edges", "Content scrolling under a bar, fading or frost with macOS's edge."},
		{pageClarity, "Clarity", "Large panes that let more of what is under them read through."},
	}
	ui.Scroll(c).Fill().Padding(28).Gap(16).Children(func() {
		pageHead(c, "Liquid Glass", "What macOS 26 draws, from the window itself to a toolbar over a web page. Choose a page in the sidebar.")
		ui.Row(c).Wrap().Gap(12).Children(func() {
			for _, it := range items {
				box := ui.Column(c).Width(226).Padding(16).Gap(6).Radius(16).Material(glass.Glass{})
				box.Children(func() {
					ui.Text(c, it.title).Bold()
					ui.Text(c, it.body).FontSize(12).TextColor(c.Theme().TextMuted)
					if ui.Button(c, "Open").Clicked() {
						g.page = it.page
					}
				})
			}
		})
	})
}

func (g *gallery) windowPage(c *ui.Context) {
	ui.Scroll(c).Fill().Padding(28).Gap(16).Children(func() {
		pageHead(c, "Window glass", "The material behind the window: switch it, then tint it. The sidebar and this page are transparent so the window's glass shows.")
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Segmented(c, &g.windowStyle, "Regular", "Clear", "None").Label("Material")
		})
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Tint").FontSize(12).TextColor(c.Theme().TextMuted)
			for _, s := range swatches {
				b := ui.Box(c).Size(28, 28).Radius(14).Center().Background(s.color).Border(1, c.Theme().Border)
				if b.Clicked() {
					g.tint = s.css
				}
				ui.Text(c, s.name).FontSize(11).TextColor(c.Theme().TextMuted)
			}
		})
		ui.Box(c).Padding(24).Radius(20).Background(ui.RGBA(255, 255, 255, 0.08)).Children(func() {
			ui.Text(c, "This is the window's Liquid Glass.").FontSize(20).Bold()
			ui.Text(c, "Dragging the window over another app shows that app through the glass, as a launcher or HUD does.").TextColor(c.Theme().TextMuted)
		})
	})
}

func (g *gallery) nativePage(c *ui.Context) {
	ui.Box(c).Fill().Children(func() {
		if g.web != nil {
			ui.WebView(c, g.web).Fill()
		} else {
			ui.Text(c, "Opening the page…").Padding(28)
		}
		ui.Row(c).Absolute().Top(16).Left(16).Right(16).Height(52).Radius(26).Padding(0, 16).AlignItems(ui.Center).Material(glass.Glass{Native: true}).Children(func() {
			ui.Text(c, "Native glass over the page").Bold()
			ui.Box(c).Grow(1)
			ui.Text(c, "Scroll it").FontSize(12)
		})
	})
}

func (g *gallery) mergedPage(c *ui.Context) {
	ui.Scroll(c).Fill().Padding(28).Gap(16).Children(func() {
		pageHead(c, "Merged panes", "Panes that nearly touch blend into one surface, as NSGlassEffectContainerView's do. Drag the slider.")
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Merge").FontSize(12).TextColor(c.Theme().TextMuted)
			ui.Slider(c, &g.merge, 0, 48).Width(360)
			ui.Textf(c, "%d", int(g.merge)).FontSize(12)
		})
		ui.Box(c).Height(340).Radius(16).Gradient(ui.Hex("#111827"), ui.Hex("#4c1d95"), 135).Children(func() {
			ui.Scroll(c).Fill().Padding(80, 18, 18).Gap(10).Children(func() {
				for i := range 10 {
					a, b := cards[i%len(cards)][0], cards[i%len(cards)][1]
					ui.Row(c).AlignSelf(ui.Stretch).Gap(12).Children(func() {
						ui.Box(c).Size(64, 52).Radius(10).Gradient(a, b, 135)
						ui.Column(c).Grow(1).Gap(2).Children(func() {
							ui.Textf(c, "Card %d", i+1).Bold().TextColor(ui.RGB(255, 255, 255))
							ui.Text(c, "This content moves under the panes.").FontSize(12).TextColor(ui.RGBA(255, 255, 255, 0.6))
						})
					})
				}
			})
			g.group(c, 18, 18)
		})
	})
}

// group draws the merged panes at the bottom left of its parent.
func (g *gallery) group(c *ui.Context, left, bottom float32) {
	grp := glass.NewGroup(c)
	grp.Radius = 20
	grp.Merge = float32(g.merge)
	grp.Element.Absolute().Left(left).Bottom(bottom)
	grp.Gap(6).Children(func() {
		for _, label := range []string{"Cut", "Copy", "Paste"} {
			grp.Pane(ui.Box(c).Size(88, 42).Center().Children(func() {
				ui.Text(c, label).Bold().TextColor(ui.RGB(255, 255, 255))
			}))
		}
	})
}

func (g *gallery) interactivePage(c *ui.Context) {
	ui.Scroll(c).Fill().Padding(28).Gap(18).Children(func() {
		pageHead(c, "Interactive glass", "Hover lights the rim; pressing grows the glass.")
		ui.Row(c).Wrap().Gap(12).Children(func() {
			for _, item := range []struct {
				label string
				tint  ui.Color
			}{
				{"Default", ui.Color{}},
				{"Blue", ui.RGBA(0, 122, 255, 0.45)},
				{"Purple", ui.RGBA(139, 92, 246, 0.55)},
				{"Pink", ui.RGBA(236, 72, 153, 0.45)},
			} {
				b := ui.Row(c).Padding(10, 18).Radius(22).Material(glass.Glass{Tint: item.tint, Interactive: true}).Children(func() {
					ui.Text(c, item.label).Bold()
				})
				if b.Clicked() {
					c.Toast(item.label + " pressed")
				}
			}
		})
		ui.Box(c).Height(260).Radius(18).Gradient(ui.Hex("#0ea5e9"), ui.Hex("#a855f7"), 135).Children(func() {
			b := ui.Box(c).Absolute().Left(28).Top(28).Size(220, 120).Radius(28).Material(glass.Glass{Interactive: true}).Children(func() {
				ui.Text(c, "A pane of interactive glass").Bold().TextColor(ui.RGB(255, 255, 255))
			})
			if b.Clicked() {
				c.Toast("The pane grew")
			}
		})
	})
}

func (g *gallery) edgesPage(c *ui.Context) {
	ui.Scroll(c).Fill().Padding(28).Gap(16).Children(func() {
		pageHead(c, "Scroll edges", "Content scrolling under a bar, faded or frosted by macOS's scroll edge effect.")
		ui.Segmented(c, &g.edge, "Soft", "Hard", "Progressive blur").Label("Edge")
		ui.Box(c).Height(400).Radius(16).Background(ui.Hex("#0b1020")).Clip().Children(func() {
			ui.Scroll(c).Fill().Padding(84, 16, 16).Gap(10).Children(func() {
				for i := range 14 {
					a, b := cards[i%len(cards)][0], cards[i%len(cards)][1]
					ui.Row(c).AlignSelf(ui.Stretch).Gap(12).Children(func() {
						ui.Box(c).Size(64, 52).Radius(10).Gradient(a, b, 135)
						ui.Column(c).Grow(1).Gap(2).Children(func() {
							ui.Textf(c, "Content %d", i+1).Bold().TextColor(ui.RGB(255, 255, 255))
							ui.Text(c, "Scroll it under the bar.").FontSize(12).TextColor(ui.RGBA(255, 255, 255, 0.6))
						})
					})
				}
			})
			edge := ui.Box(c).Absolute().Top(0).Left(0).Right(0).PassThrough()
			switch g.edge {
			case 0:
				edge.Height(74).Material(glass.ScrollEdge{})
			case 1:
				edge.Height(64).Material(glass.ScrollEdge{Hard: true})
			default:
				edge.Height(88).Material(glass.Blur{Radius: 6, Mask: &ui.LinearGradient{From: ui.RGB(0, 0, 0), To: ui.Transparent, Angle: 180, Start: 0.3, End: 1}})
			}
			ui.Row(c).Absolute().Top(10).Left(12).Right(12).Height(46).Radius(23).Padding(0, 14).AlignItems(ui.Center).Material(glass.Glass{}).Children(func() {
				ui.Text(c, "Scroll edge").Bold().TextColor(ui.RGB(255, 255, 255))
			})
		})
	})
}

func (g *gallery) clarityPage(c *ui.Context) {
	ui.Scroll(c).Fill().Padding(28).Gap(16).Children(func() {
		pageHead(c, "Clarity", "Large panes let more of what is under them read through; small ones keep their material.")
		ui.Box(c).Height(320).Radius(16).Gradient(ui.Hex("#0f766e"), ui.Hex("#7c3aed"), 135).Children(func() {
			ui.Row(c).Fill().Padding(24).Gap(12).Children(func() {
				for _, k := range []float32{0, 0.5, 1} {
					ui.Box(c).Grow(1).Radius(22).Material(glass.Glass{Clarity: k}).Children(func() {
						ui.Textf(c, "Clarity %.1f", k).Bold().TextColor(ui.RGB(255, 255, 255))
					})
				}
			})
		})
	})
}

func (g *gallery) blurPage(c *ui.Context) {
	ui.Scroll(c).Fill().Padding(28).Gap(16).Children(func() {
		pageHead(c, "Blur", "A backdrop blur, even or fading along a gradient, without the glass's rim.")
		ui.Box(c).Height(360).Radius(16).Background(ui.Hex("#0b1020")).Clip().Children(func() {
			ui.Scroll(c).Fill().Padding(84, 16, 16).Gap(10).Children(func() {
				for i := range 14 {
					a, b := cards[i%len(cards)][0], cards[i%len(cards)][1]
					ui.Row(c).AlignSelf(ui.Stretch).Gap(12).Children(func() {
						ui.Box(c).Size(64, 52).Radius(10).Gradient(a, b, 135)
						ui.Textf(c, "Blur %d", i+1).Bold().TextColor(ui.RGB(255, 255, 255))
					})
				}
			})
			ui.Box(c).Absolute().Top(0).Left(0).Right(0).Height(90).PassThrough().Material(glass.Blur{
				Radius: 8,
				Mask:   &ui.LinearGradient{From: ui.RGB(0, 0, 0), To: ui.Transparent, Angle: 180, Start: 0.2, End: 1},
			})
			ui.Row(c).Absolute().Top(10).Left(12).Right(12).Height(46).Radius(23).Padding(0, 14).AlignItems(ui.Center).Material(glass.Glass{}).Children(func() {
				ui.Text(c, "Progressive blur").Bold().TextColor(ui.RGB(255, 255, 255))
			})
		})
	})
}

// pageHTML is tall enough to scroll under the native toolbar.
func pageHTML() string {
	var b strings.Builder
	b.WriteString(`<html><body style="margin:0;background:#101014;font-family:-apple-system,BlinkMacSystemFont,sans-serif;color:white">`)
	b.WriteString(`<main style="padding:104px 36px 80px">`)
	b.WriteString(`<h1 style="margin:0 0 8px;font-size:38px;line-height:1.1">A web page under Liquid Glass</h1>`)
	b.WriteString(`<p style="margin:0 0 32px;font-size:17px;opacity:.7">Scroll: the page moves under the toolbar, which samples it.</p>`)
	for i := range 12 {
		from, to := sections[i%len(sections)][0], sections[i%len(sections)][1]
		fmt.Fprintf(&b, `<section style="margin:0 0 20px;padding:28px;border-radius:22px;background:linear-gradient(135deg,%s,%s)"><h2 style="margin:0 0 8px;font-size:26px">Section %d</h2><p style="margin:0;font-size:16px;opacity:.85">The glass bends the light along its rim and frosts what is under its middle. Use the wheel, the trackpad, or the arrow keys.</p></section>`, from, to, i+1)
	}
	b.WriteString(`</main></body></html>`)
	return b.String()
}
