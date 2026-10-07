package main

import (
	"context"
	"github.com/egoist/mygo/ui"
	"testing"
)

type galleryServices struct{}

func (galleryServices) Info(language string) (ui.TextServiceInfo, error) {
	return ui.TextServiceInfo{Language: "en-US", Provider: "test", Languages: []string{"en-US", "fr-FR"}, Spelling: true, Suggestions: true}, nil
}
func (galleryServices) Check(_ context.Context, text string, _ ui.TextCheckOptions, done func(ui.TextCheckResult, error)) {
	done(ui.TextCheckResult{Text: text}, nil)
}
func (galleryServices) LearnWord(string, string) error { return nil }

func TestTextServicesGallery(t *testing.T) {
	g := &galleryTextServices{}
	tt := ui.NewTester(g.view, 800, 500)
	tt.SetTextServices(galleryServices{})
	if len(g.languages) != 2 {
		t.Fatal("language picker did not receive installed dictionaries")
	}
	if err := tt.Click("Spelling sample"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("A changed sample")
	if g.input != "A changed sample" {
		t.Fatal("sample lost its editing state")
	}
	if err := tt.Click("Check spelling"); err != nil {
		t.Fatal(err)
	}
	if g.input != "A changed sample" {
		t.Fatal("checking edited the sample")
	}
}
