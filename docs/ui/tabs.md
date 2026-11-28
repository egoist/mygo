# Tabs

<!-- preview tabs 160 -->

`ui.Tabs` creates a row of tabs showing labels, of which an `*int` is the
index of the one chosen. A click chooses a tab, as do the arrows, Home and
End while one has the keyboard focus, which follows the choice; `Changed`
reports a new choice. Build the page of the tab chosen below them:

```go
ui.Tabs(c, &app.tab, "General", "Appearance", "Advanced")
switch app.tab {
case 0:
	app.general(c)
case 1:
	app.appearance(c)
case 2:
	app.advanced(c)
}
```

The tabs are one stop of Tab, which goes to the tab chosen. For pages with a
history, which Back returns to, use a [router](navigation.md); for a choice
of view in a toolbar, a [segmented control](segmented.md).

## Without a look

`ui.TabsBase` is a tab list without a look: the `List` holding the tabs,
whose `Tab`s choose the index, with the arrows moving the choice and the
focus. A tab with `Disabled(true)` cannot be clicked, and the arrows, Home
and End pass over it:

```go
tabs := ui.TabsBase(c, &app.tab, len(names))
tabs.List.Gap(4).Children(func() {
	for i, name := range names {
		tab := tabs.Tab(i).Padding(6, 12).Radius(6).Disabled(!app.enabled[i])
		if i == app.tab {
			tab.Background(t.Surface)
		}
		tab.Children(func() { ui.Text(c, name) })
	}
})
```

### Closing and reordering

Tabs of documents close, and move with a drag. `Reorder` lets the user drag
a tab to another place in the list, which shows where it would go: move it
there. The tab dragged is chosen as it drops, as the systems' tab bars
choose a tab as it is pressed. `OnClose` closes tabs: Delete, or Backspace,
on the tab with the focus closes it, and so does a click on its
`CloseButton`, a button without a look that takes no stop of Tab and does
not choose the tab. When the tab chosen closes, the one after it is
chosen, or the one before it for the last. Either way the index follows
the tab chosen, and `Changed` reports another tab chosen. Call both before
building the tabs, and give the tabs keys, so that what each keeps moves
with it:

```go
tabs := ui.TabsBase(c, &app.tab, len(app.files))
tabs.Reorder(func(from, to int) {
	f := app.files[from]
	app.files = slices.Delete(app.files, from, from+1)
	if to > from {
		to--
	}
	app.files = slices.Insert(app.files, to, f)
})
tabs.OnClose(func(i int) { app.files = slices.Delete(app.files, i, i+1) })
tabs.List.Gap(4).Children(func() {
	for i, f := range app.files {
		c.Key(f.Path)
		tabs.Tab(i).Padding(6, 12).Gap(6).Children(func() {
			ui.Text(c, f.Name)
			tabs.CloseButton(i).Padding(0, 4).Radius(4).Children(func() { ui.Text(c, "×") })
		})
	}
})
```

Only the list's own tabs drop on it. A tab let go elsewhere, as past the
window's edge to open it in a window of its own, reports where with
`DragMissed` on its `Tab`.

See [custom widgets](custom-widgets.md).

## Accessibility

Assistive technology sees a tab list of tabs named by their labels, the one
chosen selected, and those disabled disabled. Close buttons are hidden from
it, since a tab's name is all the text inside it: Delete closes the tab
with the focus, as in WAI-ARIA's tabs.
