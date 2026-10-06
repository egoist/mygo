package main

import (
	"github.com/egoist/mygo/ui"
)

type rtlDemo struct {
	locale    string
	direction int
	tab       int
	volume    float64
	text      string
	open      bool
	enabled   bool
	scroll    ui.ScrollState
}

func (g *gallery) rtlPage(c *ui.Context) {
	s := &g.rtl
	if s.locale == "" {
		s.locale = "ar-EG"
		s.text = "مرحبا Go 123 — שלום"
		s.volume = 35
	}
	ui.Text(c, "Choose a locale and direction; arrow keys follow the visual layout. Tab keeps source order.").TextColor(c.Theme().TextMuted)
	ui.Row(c).Wrap().Gap(12).Children(func() {
		ui.Select(c, &s.locale, []string{"ar-EG", "he-IL", "en-US", "ar-Latn"}).Label("Layout locale")
		ui.Segmented(c, &s.direction, "Automatic", "LTR", "RTL").Label("Layout direction")
	})
	direction := []ui.LayoutDirection{ui.AutoDirection, ui.LTR, ui.RTL}[s.direction]
	ui.Column(c).LayoutLocale(s.locale).Direction(direction).Gap(18).Children(func() {
		card(c, "مرحبا — שלום — Hello", func() {
			ui.Tabs(c, &s.tab, "الرئيسية", "הגדרות", "English 123")
			ui.Toolbar(c, func() {
				ui.Button(c, "حفظ Save")
				ui.Button(c, "مشاركة Share")
				ui.Spacer(c)
				anchor := ui.Button(c, "تفاصيل Details")
				if anchor.Clicked() {
					s.open = !s.open
				}
				ui.Popover(c, anchor, &s.open, func() {
					ui.Text(c, "A popup follows its anchor's direction.")
					ui.Column(c).Direction(ui.LTR).Children(func() { ui.Text(c, "LTR island: Go 123 →") })
				})
			})
			ui.Row(c).Gap(16).Children(func() {
				ui.Checkbox(c, &s.enabled, "الإشعارات Notifications")
				ui.Switch(c, &s.enabled).Label("Notifications")
			})
			ui.TextInput(c, &s.text).Label("Mixed direction text").FillWidth()
			ui.Slider(c, &s.volume, 0, 100).Label("Volume")
			ui.Progress(c, s.volume/100)
		})
		card(c, "Logical edges and wrapping", func() {
			ui.Row(c).Wrap().Gap(8).Children(func() {
				for _, label := range []string{"الأول First", "שני Second", "الثالث Third", "الرابع Fourth"} {
					ui.Box(c).Width(145).Padding(12).PaddingStart(20).BorderStart(3).BorderColor(c.Theme().Accent).Background(c.Theme().Surface).Radius(5).Children(func() { ui.Text(c, label).TextAlignInline(ui.Start) })
				}
			})
			ui.Grid(c).Columns(3).Gap(8).Children(func() {
				for _, label := range []string{"1 — واحد", "2 — שני", "3 — Three", "4 — أربعة"} {
					ui.Text(c, label).Padding(12).Background(c.Theme().Surface).TextAlignInline(ui.Start)
				}
			})
			ui.Column(c).Direction(ui.LTR).Padding(12).Background(c.Theme().Surface).Gap(6).Children(func() {
				ui.Text(c, "Explicit LTR: code and media keep their orientation.")
				ui.Row(c).Gap(12).Children(func() { ui.Text(c, "func main() { }").Font("monospace"); ui.Image(c, badge).Size(30, 30) })
			})
		})
		card(c, "Horizontal scroll starts at inline start", func() {
			ui.ScrollHorizontal(c).Height(64).Gap(8).TrackScroll(&s.scroll).Children(func() {
				for _, label := range []string{"1 — أول", "2 — שני", "3 — Third", "4 — رابع", "5 — חמישי", "6 — Sixth"} {
					ui.Button(c, label).Width(140).Height(40)
				}
			})
			ui.Textf(c, "Offset %.0f / %.0f DIPs", s.scroll.X, s.scroll.MaxX).FontSize(12).TextColor(c.Theme().TextMuted)
		})
	})
}
