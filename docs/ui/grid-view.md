# Grid view

<!-- preview grid-view 310 -->

`ui.GridView` shows items in a grid, as Photos' library and Finder's icon
view: as many columns as fit items at least as wide as asked, sharing the
width left, in rows of a height, built only as they show, so grids of
millions of items are as fast as small ones. `Gap` spaces the items.

```go
app.photos.Selected = &app.photo
ui.GridView(c, &app.photos, len(photos), 140, 120, func(i int) {
	ui.Image(c, thumbs[i]).Fit(ui.Contain).Grow(1)
	ui.Text(c, photos[i].Name).SingleLine()
}).Grow(1)
```

## Grid state

A `GridState` keeps the grid's place and says how items are chosen, as a
`ListState` does rows, or `nil`:

- `Selected` lets a click choose an item, as the arrows do in both
  directions while the grid has the keyboard focus, with Home, End, and
  Enter (`Submitted`).
- `Selection` lets the user choose several, by `Key`: Cmd-click adds an
  item or takes it out, Shift-click and Shift with the arrows choose those
  from the item last chosen, and Cmd+A chooses all.
- `Label` returns an item's text: typing its first letters chooses it, and
  assistive technology reads it as the item's name.
- `Reorder` lets the user drag items to another place, as a list's rows:
  see [drag and drop](drag-and-drop.md).

## Masonry view

`ui.MasonryView` shows items of their own heights in columns, as a wall of
stickers or of photos of any shape: as many columns as fit items at least
as wide as asked, sharing the width left, each item at the top of the
column whose items end highest. The app says how high item `i` is for the
columns' width, as a picture's aspect ratio does, and the view builds only
the items in view, so walls of thousands of items scroll as fast as small
ones.

```go
app.stickers.Selected = &app.sticker
ui.MasonryView(c, &app.stickers, len(stickers), 140, func(i int, width float32) float32 {
	return width / stickers[i].Aspect
}, func(i int) {
	ui.Image(c, thumbs[i]).Fit(ui.Cover).Grow(1)
}).Grow(1)
```

An item's place follows from the heights of the items before it alone, so
items keep their places as the view scrolls up and down, and items added at
the end leave those before them where they are. When the window is resized,
the items take the columns that fit, and the item at the top stays in view.
The view asks again for the heights of the items in view as they show: one
that changed places that item and those after it anew.

It takes a `GridState`, as a grid view does, with the same `Selected`,
`Selection`, `Key`, `Label` and `Reorder`. Up and Down move the choice to
the item above or below in its column, and Left and Right to the nearest
item in the next column. `ScrollTo` and `ScrollIntoView` scroll to an item.
`Gap` spaces the items in a column further.

## Accessibility

Assistive technology reads the grid, or the masonry view, as a list of
items, each saying which of all it is, built or not.
