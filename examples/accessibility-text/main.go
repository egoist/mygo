// Accessibility-text demonstrates native text providers for screen readers.
//
//	go run ./examples/accessibility-text
package main

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"log"
)

type model struct{ text, password string }

func (m *model) view(c *ui.Context) {
	ui.Column(c).Fill().Padding(20).Gap(12).Children(func() {
		ui.Text(c, "Text accessibility").Bold().FontSize(22).Role(ui.RoleHeading).Level(1)
		ui.Text(c, "Use your screen reader to move by characters, words and wrapped lines, select text, and inspect the caret.")
		ui.TextArea(c, &m.text).Label("Editable text").Width(460).Height(130)
		ui.TextInput(c, &m.text).Label("Read-only text").ReadOnly(true)
		ui.RichText(c, ui.Span{Text: "Selectable rich text: ", Weight: 600}, ui.Span{Text: "e\u0301, 😀, שלום and العربية", Italic: true}).Label("Selectable rich text").Selectable()
		ui.RichText(c).Label("Linked paragraph").Children(func() {
			ui.Text(c, "Read ")
			ui.Link(c, "the text accessibility guide", "https://mygo.egoist.dev/docs/ui/accessibility")
			ui.Text(c, " for details.")
		})
		ui.TextInput(c, &m.password).Label("Password").Password().Placeholder("Private text")
	})
}

func main() {
	m := &model{text: "A😀e\u0301 שלום\nA longer paragraph wraps into visual lines. The caret and selection remain available while paragraphs outside the viewport stay virtualized.", password: "private"}
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{Title: "Text accessibility", Width: 540, Height: 490, Content: ui.View(m.view)})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
