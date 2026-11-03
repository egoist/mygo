# SF Symbols

The sfsymbols plugin draws Apple's SF Symbols for native UI on macOS. The
symbols are read from the running system: AppKit draws each with its own
design and weight, and the plugin copies the pixels into an image. Nothing
of Apple's artwork is in your app or its build. It is all Go: no
JavaScript package, and nothing to `mygo.Use`.

```go
import "github.com/egoist/mygo/plugins/sfsymbols"

if src, ok := sfsymbols.Image("gearshape", 48, t.Text, sfsymbols.Options{}); ok {
	ui.Image(c, src).Size(16, 16)
} else {
	// No symbol here: draw your own icon.
}
```

## API

`Image(name, px, col, opts)` returns the symbol named `name` (as
`"gearshape"`), drawn `px` pixels square in the color `col`, as a
`ui.ImageSource`. It returns false where the system has no such symbol: on
another OS, or on a macOS that lacks it. `Available` reports whether the
system draws SF Symbols at all.

- `px` is limited to 8 through 256. Draw at two or three times the size in
  DIPs, so that the symbol stays sharp on any display.
- By default the symbol's ink fills its box, centered with a small inset.
  `Options{Natural: true}` draws every symbol at one scale instead, the
  size it has beside others of the same point size: a cross stays smaller
  than a wide rectangle, as SF Symbols balance them.
- Results are cached by name, size, color and options, the least recently
  used dropped past a limit, so an animated color or size doesn't grow
  memory without end. `Image` is safe to call from any goroutine.

Every other OS has no SF Symbols: `Image` returns false there, so keep an
icon of your own to fall back on, and use `ui.Image` or `Painter.Image`
(with `ui.Contain`) for what it returns.

Apps that don't import the plugin don't pay for it.

See `examples/sf-symbols`: `go run ./examples/sf-symbols`.
