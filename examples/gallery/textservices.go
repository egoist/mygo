package main

import (
	"fmt"
	"github.com/egoist/mygo/ui"
)

type galleryTextServices struct {
	initialized            bool
	input, notes, language string
	options                ui.TextServicesOptions
	languages              []string
}

func (g *galleryTextServices) view(c *ui.Context) {
	if !g.initialized {
		g.initialized = true
		g.input = "Try mispeling, then right-click the word."
		g.notes = "Spelling works in text areas too.\nType \"quotes\" -- or a text replacement with substitutions enabled."
		g.language = "System locale"
		g.options.SpellChecking = true
	}
	card(c, "System text services", func() {
		choices := append([]string{"System locale"}, g.languages...)
		ui.Select(c, &g.language, choices).Label("Checking language")
		g.options.Language = g.language
		if g.language == "System locale" {
			g.options.Language = ""
		}
		input := ui.TextInput(c, &g.input).Label("Spelling sample").TextServices(g.options)
		area := ui.TextArea(c, &g.notes).Label("Text services notes").Height(110).TextServices(g.options)
		status := input.TextServiceStatus()
		g.languages = status.Availability.Languages
		info := status.Availability
		ui.Row(c).Gap(12).Wrap().Children(func() {
			ui.Checkbox(c, &g.options.SpellChecking, "Spell checking").Disabled(!info.Spelling)
			ui.Checkbox(c, &g.options.AutomaticCorrection, "Automatic correction").Disabled(!info.Correction)
			ui.Checkbox(c, &g.options.SmartQuotes, "Smart quotes").Disabled(!info.SmartQuotes)
			ui.Checkbox(c, &g.options.SmartDashes, "Smart dashes").Disabled(!info.SmartDashes)
			ui.Checkbox(c, &g.options.TextReplacement, "Text replacement").Disabled(!info.TextReplacement)
		})
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			if ui.Button(c, "Check spelling").Disabled(!info.Spelling).Clicked() {
				input.CheckSpelling()
				area.CheckSpelling()
			}
			label := fmt.Sprintf("%s · %s · %d suggestions", info.Provider, info.Language, len(status.Issues))
			if status.Checking {
				label = "Checking…"
			}
			if status.Error != nil {
				label = "Text services unavailable: " + status.Error.Error()
			}
			ui.Text(c, label).FontSize(12).TextColor(c.Theme().TextMuted)
		})
		ui.Text(c, "Right-click a marked word for suggestions, Ignore Spelling or Learn Spelling. Automatic changes have their own Undo step; Learn Spelling updates your system dictionary.").FontSize(12).TextColor(c.Theme().TextMuted)
	})
}
