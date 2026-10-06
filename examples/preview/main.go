// Preview is an interactive native UI playground with fresh sample state.
//
//	go run ./examples/preview
package main

import (
	"log"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type contact struct {
	name, email, notes string
	updates            bool
	saved              bool
}

func (s *contact) view(c *ui.Context) {
	config, _ := c.PreviewConfig()
	title, name, email, notes, updates, save, saved := "Contact", "Name", "Email", "Notes", "Send updates", "Save contact", "Saved"
	if strings.HasPrefix(config.Locale, "ja") {
		title, name, email, notes, updates, save, saved = "連絡先", "名前", "メール", "メモ", "通知を受け取る", "保存", "保存しました"
	}
	ui.Scroll(c).Fill().Padding(24).Gap(16).Children(func() {
		ui.Text(c, title).FontSize(24).Bold()
		ui.Form(c, func() {
			ui.Field(c, name, func() { ui.TextInput(c, &s.name) })
			ui.Field(c, email, func() { ui.TextInput(c, &s.email) })
			ui.Field(c, notes, func() { ui.TextArea(c, &s.notes).Height(100) })
		})
		ui.Checkbox(c, &s.updates, updates)
		if ui.PrimaryButton(c, save).Disabled(s.name == "").Clicked() {
			s.saved = true
		}
		if s.saved {
			ui.Text(c, saved).TextColor(c.Theme().Success)
		}
		ui.Divider(c)
		p := c.Preferences()
		ui.Textf(c, "%d × %d DIPs · %.2g× · %s", config.Width, config.Height, config.Scale, config.Locale).TextColor(c.Theme().TextMuted)
		ui.Textf(c, "Reduced motion: %t · High contrast: %t", p.ReduceMotion, p.HighContrast).TextColor(c.Theme().TextMuted)
	})
}

func sample(name, email, notes string) func() ui.PreviewSample {
	return func() ui.PreviewSample {
		s := &contact{name: name, email: email, notes: notes}
		return ui.PreviewSample{View: s.view}
	}
}

func playground() *ui.Preview {
	return ui.NewPreview(ui.PreviewOptions{
		Config: ui.PreviewConfig{Width: 520, Height: 440, Locale: "en-US"},
		Presets: []ui.PreviewPreset{
			{Name: "Empty", New: sample("", "", "")},
			{Name: "Populated", New: sample("Ada Lovelace", "ada@example.com", "Discuss the next desktop release.")},
			{Name: "Japanese / large text", New: sample("山田 太郎", "taro@example.com", "プレビューで表示を確認します。"),
				Config: ui.PreviewConfig{Locale: "ja-JP", Theme: ui.PreviewDark, Scale: 2, Preferences: &ui.Preferences{TextScale: 1.5}}},
			{Name: "Long content", New: sample("A contact with a long display name", "long-address@example.com", strings.Repeat("Notes with enough content to exercise scrolling. ", 20)),
				Config: ui.PreviewConfig{Width: 360, Height: 320}},
		},
		// Extend the controls by editing the same Preferences value. Future
		// OS preference fields can be exposed here without a second model.
		Configure: func(c *ui.Context, config *ui.PreviewConfig) {
			if ui.Button(c, "Purple accent").Clicked() {
				prefs := c.Preferences()
				if config.Preferences != nil {
					prefs = *config.Preferences
				}
				prefs.Accent = ui.Hex("#9333ea")
				config.Preferences = &prefs
			}
		},
	})
}

func main() {
	p := playground()
	mygo.App.WhenReady(func() {
		view := mygo.NewWindow(mygo.WindowOptions{Title: "Contact preview", Width: 1000, Height: 720,
			Content: p.Content(), Page: mygo.PageOptions{DevTools: mygo.DevToolsEnabled}})
		controls := mygo.NewWindow(mygo.WindowOptions{Title: "Preview controls", Width: 460, Height: 760,
			Content: ui.View(p.Controls)})
		view.OnClosed(controls.Destroy)
		controls.OnClosed(view.Destroy)
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
