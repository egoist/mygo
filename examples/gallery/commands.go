package main

import (
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/command"
)

// commandPalette navigates the gallery from anywhere, without rebuilding
// a separate search UI for its pages.
func (g *gallery) commandPalette(c *ui.Context) {
	if c.Shortcut(ui.Cmd, ui.KeyK) {
		g.palette.Open = true
	}
	if g.commands == nil {
		for _, page := range pages {
			g.commands = append(g.commands, command.Item{ID: pagePath(page), Title: page, Detail: "Go to the " + page + " page", Keywords: "navigate go page"})
		}
	}
	if path, ok := command.View(c, &g.palette, g.commands); ok {
		g.router.Push(path)
	}
}
