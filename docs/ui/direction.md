# Interface direction

Native UI lays out interfaces from the reading start edge. A window's root
uses `ui.AutoDirection`, derived from `mygo.App.Locale()`. Arabic, Hebrew,
Persian and other RTL language/script tags choose `ui.RTL`; other and unknown
tags choose `ui.LTR`. An explicit script wins: `ar-Latn` is LTR.

```go
func (app *settings) view(c *ui.Context) {
    c.SetLayoutLocale("ar-EG") // this frame's layout locale
    // c.SetDirection(ui.RTL) also works, independent of the locale.
    ui.Column(c).Fill().Padding(16).Gap(12).Children(func() {
        ui.Row(c).Gap(8).Children(func() {
            ui.Icon(c, folder)
            ui.Text(c, "المستندات").Grow(1)
            ui.Text(c, "12 items")
        })
        ui.Column(c).Direction(ui.LTR).Children(func() {
            ui.Text(c, "~/Documents/report.go").Font("monospace")
        })
    })
}
```

`Direction` inherits by default (`ui.InheritDirection`). Set it before
`Children`. Widgets process input as they are created: put an override on
a container around a widget. `LayoutLocale("he-IL").Direction(ui.AutoDirection)`
selects another locale for a subtree. An explicit LTR/RTL ancestor stays
explicit until a descendant sets its own direction or automatic mode.
`c.Direction()` and `element.LayoutDirection()` return the effective LTR/RTL.

`SetLayoutLocale` controls interface layout only: it does not format dates,
translate strings, or change text shaping. It is kept separate from the
locale-aware widget APIs. The window locale hook uses `App.Locale`, so
application/window locale configuration can supply the same tag. Use a
matching layout locale when providing an independent locale scope.
`Tester.SetLayoutLocale` changes the host locale without losing state.

## Layout and logical edges

Rows start on the right in RTL; columns keep their top-to-bottom order and
align across from the right. Grid column 1 is at inline start, including
explicit column placements and spans. Wrapping and automatic margins use
the same direction. `Start` and `End` are logical alignment in flex and grid;
use `TextAlignInline(Start/End)` for lines following interface direction.
Existing `TextAlign` and unspecified text alignment retain their natural
paragraph direction.

`Reverse` reverses the authored order relative to direction: an RTL row with
`Reverse()` goes left to right. `WrapReverse` reverses stacking of lines,
and composes with RTL column wrapping. Use `Direction(LTR)` for a deliberate
physical layout such as a timeline or coordinate plot.

| Logical method | LTR edge | RTL edge |
| --- | --- | --- |
| `PaddingStart`, `MarginStart`, `BorderStart`, `InsetStart` | left | right |
| `PaddingEnd`, `MarginEnd`, `BorderEnd`, `InsetEnd` | right | left |

Margins accept `ui.Auto`; insets also have `InsetStartPercent` and
`InsetEndPercent`. Logical edges override physical ones on the same edge.
The physical padding/margin/border shorthands reset their logical overrides.
`Left`, `Right`, `Top`, `Bottom`, physical padding, border sides, radii,
gradients and pointer coordinates keep their physical meaning.

MyGo moves boxes, without reflecting their contents. Glyphs retain their
bidirectional shaping; images, icons, custom drawings, gradients and text
editor caret/selection movement keep their orientation. Only built-in
navigation artwork, such as back/forward and disclosure arrows, is mirrored.

## Popovers

Popovers, selects and comboboxes align below the inline start of their
anchor, inherit its direction even in the overlay layer, and flip/clamp to
fit the window. Nested overrides inside the popup still work.

Use logical anchors for your own overlays:

```go
panel.AttachTo(button, ui.AnchorBottomStart, ui.AnchorTopStart)
panel.AttachTo(button, ui.AnchorEnd, ui.AnchorStart).MarginStart(6)
```

`AnchorTopEnd`, `AnchorBottomEnd` and their start equivalents work with
`Attach` too. Physical anchors such as `AnchorTopLeft` retain their meaning.
Native context menus continue to use the system toolkit's own menu layout.

## Keyboard and focus

Tab and Shift+Tab follow source order, including inside mixed-direction
interfaces; a focus group remains one Tab stop. Home/End choose the first
and last authored item. Horizontal arrows follow visual reading direction:

| Control | RTL behavior |
| --- | --- |
| Tabs, segmented/radio groups, toolbars | Left moves to the next authored item; Right to the previous. Explicit Reverse composes with direction. |
| Lists, sidebars | Up/Down keep their vertical behavior. |
| Grid views and calendars | Left moves forward one item/day; Right backward. Up/Down keep their row/day offsets. |
| Trees and outlines | Left expands/enters a branch; Right collapses/goes to the parent. |
| Sliders and range sliders | Minimum is on the right; Left increases and Right decreases. Up/Down still increase/decrease; Home/End choose minimum/maximum. |
| Time inputs | Numeric hour/minute sequences keep LTR order and visual Left/Right keys within the mirrored interface. |
| Split panes | First pane is on the right; horizontal dragging/arrows resize it from that side. |
| Router | Back/forward artwork and automatic page slides mirror. Alt+arrow follows direction; dedicated Back/Forward keys and macOS Cmd+[ / Cmd+] retain their semantic actions. |

Text editors use the existing visual bidirectional caret navigation. Numeric
increment/decrement accessibility actions keep their value meaning even when
a slider is mirrored. Vertical sliders keep minimum at the bottom.

## Scrolling and geometry

`ScrollState.X` is a nonnegative distance from inline start: zero shows the
right edge in RTL, `MaxX` the left edge. `Y` stays the distance from the top.
A physical wheel/arrow move toward the left increases X in RTL. Horizontal
Home/End choose logical start/end. Scroll thumbs, track clicks, focus reveal,
`ScrollIntoView`, attached overlays, hit tests and accessibility bounds use
the same physical window geometry. Vertical scrollbars show at inline end,
on the left in RTL. Insets still specify physical sides.

Run `go run ./examples/gallery` and open **RTL** to switch Arabic, Hebrew,
English and script overrides, try keyboard navigation, and inspect nested
LTR content, wrapping, grids, popovers and horizontal scrolling.
