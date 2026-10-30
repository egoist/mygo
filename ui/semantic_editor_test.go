package ui_test

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/automation"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/richtext"
)

func TestTesterSemanticEditorInput(t *testing.T) {
	code := &editor.State{Buffer: editor.NewBuffer("before")}
	doc := richtext.NewDocument(richtext.NewText("before"))
	for _, test := range []struct {
		name string
		view func(*ui.Context)
	}{
		{"code", func(c *ui.Context) { editor.View(c, code).Fill().Label("Editor") }},
		{"rich text", func(c *ui.Context) { richtext.Editor(c, doc, richtext.EditorOptions{Label: "Editor"}).Fill() }},
	} {
		t.Run(test.name, func(t *testing.T) {
			driver := ui.NewTester(test.view, 300, 200)
			snapshot := driver.Inspect()
			target, err := snapshot.Resolve(0, "Editor")
			if err != nil {
				t.Fatal(err)
			}
			if err := driver.Perform(target, automation.Request{Action: automation.Focus}); err != nil {
				t.Fatal(err)
			}
			for _, input := range []automation.Input{
				{Kind: automation.KeyInput, Key: "CmdOrCtrl+A"},
				{Kind: automation.TextInput, Text: "after 🌍"},
				{Kind: automation.KeyInput, Key: "Enter"},
				{Kind: automation.TextInput, Text: "next"},
			} {
				if err := driver.Input(input); err != nil {
					t.Fatal(err)
				}
			}
			snapshot = driver.Inspect()
			found := false
			for _, n := range snapshot.Nodes {
				if n.ID == target.ID {
					found = true
					if n.Value != "after 🌍\nnext" {
						t.Fatalf("value = %q", n.Value)
					}
				}
			}
			if !found {
				t.Fatal("editor missing after input")
			}
			for _, input := range []automation.Input{
				{}, {Kind: automation.KeyInput, Key: "invalid"},
				{Kind: automation.KeyInput, Key: "F24"},
				{Kind: automation.ScrollInput, X: math.NaN()},
				{Kind: automation.ScrollInput, DY: math.Inf(1)},
				{Kind: automation.ScrollInput, DX: math.MaxFloat64},
				{Kind: automation.ScrollInput, X: -1},
			} {
				if err := driver.Input(input); err == nil {
					t.Fatalf("accepted invalid input: %+v", input)
				}
			}
		})
	}
}
