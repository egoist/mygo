# SF Symbols

The sfsymbols plugin draws Apple's SF Symbols for native UI on macOS. The
symbols are read from the running system: AppKit draws each with its own
design and weight, and the plugin copies the pixels into an image. Nothing
of Apple's artwork is in your app or its build. It is all Go: no
JavaScript package, and nothing to `mygo.Use`.

Show one as SwiftUI's `Image(systemName:)` does:

```go
import "github.com/egoist/mygo/plugins/sfsymbols"

sfsymbols.Symbol(c, "gearshape")
sfsymbols.Symbol(c, "trash").Size(20).Color(t.Accent).Weight(sfsymbols.Semibold).Fill()
sfsymbols.Symbol(c, "sparkles").Fallback(sparklesSVG) // where the system has none
```

## Symbol

`Symbol(c, name)` makes an element that shows the symbol at the theme's
font size, in its text color, drawn at the display's scale so it stays
sharp, and named `name` for assistive technology. Its modifiers chain, as
SwiftUI's:

| sfsymbols | SwiftUI |
|---|---|
| `.Size(24)` | `.font(.system(size: 24))` |
| `.Color(col)` | `.foregroundStyle(col)` |
| `.Weight(sfsymbols.Bold)` | `.fontWeight(.bold)` |
| `.Fill()` | `.symbolVariant(.fill)`: `trash.fill` for `trash`, the symbol itself where it has no filled variant |
| `.Natural()` | the size a symbol has beside others of one point size, rather than filling its box |
| `.Fallback(svg)` | an SVG, in the symbol's color, where the system has no such symbol |

A `SymbolElement` is a `ui.Element` too: `Margin`, `Tooltip` and the rest
work on it. Where the system has no symbol of the name (on another OS, or
an older macOS) it draws its fallback, or nothing, at the same size.

What it doesn't do, unlike SwiftUI: follow the text color of the element
it is in (it takes the theme's, or `Color`), sit on the baseline of text
beside it, or draw multicolor and hierarchical symbols in more than one
color.

## Image

For drawing with a `ui.Painter`, `Image(name, px, col, opts)` returns the symbol named `name` (as
`"gearshape"`), drawn `px` pixels square in the color `col`, as a
`ui.ImageSource`. It returns false where the system has no such symbol: on
another OS, or on a macOS that lacks it. `Available` reports whether the
system draws SF Symbols at all.

- `opts` holds `Natural` and `Weight`, as Symbol's modifiers.
- `px` is limited to 8 through 256. Draw at two or three times the size in
  DIPs, so that the symbol stays sharp on any display.
- By default the symbol's ink fills its box, centered with a small inset.
  `Options{Natural: true}` draws every symbol at one scale instead, the
  size it has beside others of the same point size: a cross stays smaller
  than a wide rectangle, as SF Symbols balance them.
- Results are cached by name, size, color and options, and the names the
  system has no symbol for too, the least recently used dropped past a
  limit, so an animated color or size doesn't grow
  memory without end. `Image` is safe to call from any goroutine.

Every other OS has no SF Symbols: `Image` returns false there, so keep an
icon of your own to fall back on, and use `ui.Image` or `Painter.Image`
(with `ui.Contain`) for what it returns.

Apps that don't import the plugin don't pay for it.

See `examples/sf-symbols`: `go run ./examples/sf-symbols`.
