package main

import (
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

// preferencesPage follows the real desktop settings while the gallery is open.
// Tester can supply the same Preferences snapshots to exercise this view.
func (g *gallery) preferencesPage(c *ui.Context) {
	t, prefs := c.Theme(), c.Preferences()
	ui.Text(c, "Change your desktop's accessibility and scrollbar settings while this page is open. Use Tab to inspect focus, and select text in the fields.").TextColor(t.TextMuted)
	card(c, "Live desktop settings", func() {
		bars := "Automatic"
		if prefs.ScrollbarVisibility == ui.ScrollbarAlways {
			bars = "Always visible"
		} else if prefs.ScrollbarVisibility == ui.ScrollbarOnScroll {
			bars = "When scrolling"
		}
		ui.Textf(c, "Scrollbars: %s", bars)
		ui.Textf(c, "Reduced transparency: %t", prefs.ReduceTransparency)
		ui.Textf(c, "High contrast: %t · Reduced motion: %t · Text scale: %.2f", prefs.HighContrast, prefs.ReduceMotion, prefs.TextScale)
		ui.Textf(c, "Windows contrast palette available: %t", prefs.ContrastColors.Window.A != 0)
	})
	card(c, "Focus and selection", func() {
		body := ui.Column(c).Gap(12)
		value := ui.Local(body, "preference-text", func() string { return "Select these words to inspect their contrast." })
		body.Children(func() {
			ui.TextInput(c, value).Label("Preference selection").FillWidth()
			ui.Text(c, "Selectable text follows the same foreground and background pair.").Selectable()
			ui.Row(c).Gap(12).Children(func() {
				ui.Button(c, "Button")
				ui.PrimaryButton(c, "Primary")
				ui.Button(c, "Disabled").Disabled(true)
				ui.Link(c, "Documentation", "https://mygo.egoist.dev/docs/ui/styling")
			})
		})
	})
	card(c, "Scrollbars", func() {
		ui.Row(c).Gap(12).AlignItems(ui.Stretch).Children(func() {
			for _, sample := range []struct {
				title  string
				mode   ui.ScrollbarVisibility
				system bool
			}{
				{"Desktop preference", ui.ScrollbarAuto, true},
				{"Always (app override)", ui.ScrollbarAlways, false},
				{"Hidden (app override)", ui.ScrollbarNever, false},
			} {
				ui.Column(c).Grow(1).Gap(8).Children(func() {
					ui.Text(c, sample.title).Bold()
					s := ui.Scroll(c).Height(150).Border(1, t.Border).Radius(t.Radius)
					if !sample.system {
						s.Scrollbars(sample.mode)
					}
					s.Children(func() {
						for i := range 20 {
							ui.Textf(c, "Scrollable row %d", i+1).Padding(4, 8).Shrink(0)
						}
					})
				})
			}
		})
	})
	card(c, "Materials", func() {
		ui.Text(c, "Reduced transparency and high contrast replace these materials with opaque fills.")
		ui.Row(c).Height(100).Gap(16).Padding(16).Background(t.Surface).Stripes(t.Accent, 12, 16, 45).Children(func() {
			ui.Box(c).Grow(1).FillHeight().Radius(12).Center().Material(glass.Glass{}).Children(func() { ui.Text(c, "Glass") })
			ui.Box(c).Grow(1).FillHeight().Radius(12).Center().Material(glass.Blur{Radius: 8}).Children(func() { ui.Text(c, "Blur") })
			ui.Box(c).Grow(1).FillHeight().Radius(12).Center().Material(glass.ScrollEdge{Hard: true}).Children(func() { ui.Text(c, "Scroll edge") })
		})
	})
}
