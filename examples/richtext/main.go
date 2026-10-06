// Richtext demonstrates MyGo's native rich-text editor and formatting toolbar.
// Select words to style them, type with a style at the caret, Cmd/Ctrl-click a
// link, and copy/paste styled text with another app. No frontend is needed.
//
//	go run ./examples/richtext
package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type app struct {
	editor *ui.RichEditorState
	url    string
}

func newApp() *app {
	text := "Rich text, drawn by MyGo.\nSelect words and use the toolbar or Cmd/Ctrl+B, I and U. Formatting at a caret applies to new text.\nTry é, 👩‍💻, العربية and עברית. Copy styled text into another desktop app and paste it back.\nRead the MyGo guide."
	d := ui.NewRichDocument(text)
	d = d.WithStyle(ui.TextRange{Start: 0, End: strings.IndexByte(text, '\n')}, ui.RichStyle{Size: 24, Weight: 700})
	d = d.WithParagraphStyle(ui.TextRange{}, ui.ParagraphStyle{Alignment: ui.Center, LineHeight: 1.4})
	start := len([]rune(text[:strings.Index(text, "the MyGo guide")]))
	d = d.WithStyle(ui.TextRange{Start: start, End: start + len("the MyGo guide")}, ui.RichStyle{Link: "https://mygo.egoist.dev/docs/ui/rich-text-editor"})
	return &app{editor: ui.NewRichEditorState(d), url: "https://example.com/"}
}

func (a *app) view(c *ui.Context) {
	t := c.Theme()
	refocus := false
	ui.Column(c).Fill().Padding(16).Gap(10).Children(func() {
		ui.Row(c).Gap(6).Children(func() {
			for _, action := range []struct {
				label  string
				format ui.RichFormat
			}{{"Bold", ui.FormatBold}, {"Italic", ui.FormatItalic}, {"Underline", ui.FormatUnderline}, {"Strike", ui.FormatStrikethrough}, {"Clear", ui.FormatClear}} {
				if ui.Button(c, action.label).Clicked() {
					a.editor.Format(action.format)
					refocus = true
				}
			}
			if ui.Button(c, "Undo").Disabled(!a.editor.CanUndo()).Clicked() {
				a.editor.Undo()
				refocus = true
			}
			if ui.Button(c, "Redo").Disabled(!a.editor.CanRedo()).Clicked() {
				a.editor.Redo()
				refocus = true
			}
		})
		ui.Row(c).Gap(6).Children(func() {
			ui.TextInput(c, &a.url).Placeholder("Link URL").Label("Link URL").Grow(1)
			if ui.Button(c, "Set link").Clicked() {
				a.editor.SetLink(a.url)
				refocus = true
			}
			if ui.Button(c, "Unlink").Clicked() {
				a.editor.SetLink("")
				refocus = true
			}
			for i, label := range []string{"Start", "Center", "End"} {
				if ui.Button(c, label).Clicked() {
					a.editor.SetParagraphStyle(ui.ParagraphStyle{Alignment: ui.Align(i), LineHeight: 1.4})
					refocus = true
				}
			}
		})
		e := ui.RichTextEditor(c, a.editor).Grow(1).MinHeight(200).Label("Rich document")
		if refocus {
			e.Focus()
		}
		sel := a.editor.Selection()
		ui.Text(c, fmt.Sprintf("%d runes · selection %d → %d · Cmd/Ctrl-click a link to open it", a.editor.Document().Len(), sel.Anchor, sel.Caret)).TextColor(t.TextMuted)
	})
}

func main() {
	a := newApp()
	mygo.App.WhenReady(func() {
		mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{{Role: mygo.RoleAppMenu}, {Role: mygo.RoleEditMenu}}))
		mygo.NewWindow(mygo.WindowOptions{Title: "MyGo Rich Text", Width: 820, Height: 580, Content: ui.View(a.view)})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
