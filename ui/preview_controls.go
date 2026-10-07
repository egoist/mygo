//go:build !mygo_noinspector

package ui

import "reflect"

// Controls builds a playground's controls view. Show it in a separate
// window with ui.View(preview.Controls); the preview window keeps the full
// simulated viewport and its inspector. Configure extends these controls.
// A controls window follows its own desktop appearance and preferences.
func (p *Preview) Controls(c *Context) {
	p.watch(c.rt)
	config := p.Config()
	before := clonePreviewConfig(config)
	name := p.Preset()
	names := make([]string, len(p.presets))
	for i, preset := range p.presets {
		names[i] = preset.Name
	}
	errText := Local(c.Root(), p, func() string { return "" })
	Scroll(c).Fill().Padding(16).Gap(12).Children(func() {
		Text(c, "Preview playground").Bold().FontSize(18)
		Field(c, "Sample", func() { Select(c, &name, names).Label("Preview sample") })
		if name != p.Preset() {
			_ = p.SelectPreset(name)
			config, before = p.Config(), p.Config()
		}
		if Button(c, "Reset sample").Clicked() {
			p.Reset()
		}
		Divider(c)
		Form(c, func() {
			theme := string(config.Theme)
			Field(c, "Theme", func() {
				Select(c, &theme, []string{string(PreviewSystem), string(PreviewLight), string(PreviewDark)}).Label("Preview theme")
			})
			config.Theme = PreviewTheme(theme)
			width, height, scale := float64(config.Width), float64(config.Height), float64(config.Scale)
			Field(c, "Width (DIPs)", func() { NumberInput(c, &width, 1, 16384, 1).Label("Preview width") })
			Field(c, "Height (DIPs)", func() { NumberInput(c, &height, 1, 16384, 1).Label("Preview height") })
			Field(c, "Display scale", func() { NumberInput(c, &scale, 0.25, 8, 0.25).Label("Preview scale") })
			config.Width, config.Height, config.Scale = int(width), int(height), float32(scale)
			Field(c, "Locale", func() { TextInput(c, &config.Locale).Label("Preview locale").Placeholder("App default") })
		})
		Divider(c)
		follow := config.Preferences == nil
		Checkbox(c, &follow, "Follow system preferences")
		if follow {
			config.Preferences = nil
		} else {
			if config.Preferences == nil {
				prefs := c.Preferences()
				config.Preferences = &prefs
			}
			prefs := config.Preferences
			Checkbox(c, &prefs.ReduceMotion, "Reduced motion")
			Checkbox(c, &prefs.HighContrast, "High contrast")
			textScale := float64(prefs.TextScale)
			if textScale == 0 {
				textScale = 1
			}
			Field(c, "Text scale", func() { NumberInput(c, &textScale, 0.25, 8, 0.25).Label("Preview text scale") })
			prefs.TextScale = float32(textScale)
		}
		if p.configure != nil {
			p.configure(c, &config)
		}
		if !reflect.DeepEqual(before, config) {
			if err := p.SetConfig(config); err != nil {
				*errText = err.Error()
			} else {
				*errText = ""
			}
		}
		if *errText != "" {
			Text(c, *errText).TextColor(c.Theme().Danger)
		}
		Text(c, "F12 in the preview opens the inspector.\nViewport and scale also apply to captures and tests.").TextColor(c.Theme().TextMuted)
	})
}
