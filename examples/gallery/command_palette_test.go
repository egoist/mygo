package main

import (
	"github.com/egoist/mygo/ui"
	"testing"
)

func TestCommandPaletteNavigation(t *testing.T) {
	g := &gallery{router: ui.NewRouter("/overview")}
	tt := ui.NewTester(func(c *ui.Context) { g.toolbar(c); g.commandPalette(c) }, 800, 600)
	tt.Key(ui.Cmd, ui.KeyK)
	tt.Type("rcht")
	tt.Key(0, ui.KeyEnter)
	if g.router.Path() != "/rich-text" {
		t.Fatalf("command did not navigate: %s", g.router.Path())
	}
	if g.palette.Open {
		t.Fatal("palette stayed open")
	}
	if err := tt.Click("Go to page…"); err != nil {
		t.Fatal(err)
	}
	tt.Type("charts")
	tt.Key(0, ui.KeyEnter)
	if g.router.Path() != "/charts" {
		t.Fatal(g.router.Path())
	}
}
