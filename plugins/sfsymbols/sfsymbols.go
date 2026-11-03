// Package sfsymbols draws Apple's SF Symbols, as the running macOS has
// them, for native UI.
//
//	if src, ok := sfsymbols.Image("gearshape", 48, t.Text, sfsymbols.Options{}); ok {
//		ui.Image(c, src).Size(16, 16)
//	}
//
// The symbols are read from the system: AppKit draws each with its own
// design and weight, and the plugin copies the pixels. Nothing of Apple's
// artwork is in the app or its build. Where the system has no symbol of a
// name, and on every other OS, Image says so, and the caller draws its own
// icon instead.
package sfsymbols

// Options of a symbol's drawing.
type Options struct {
	// Natural draws the symbol at the size it has beside others of the
	// same point size, rather than filling its box: a cross stays smaller
	// than a wide rectangle, as SF Symbols balance them.
	Natural bool
}
