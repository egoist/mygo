# Inspector

The inspector shows the elements of a window of native UI in a panel
beside the content, as a browser's developer tools show a page: the tree
of elements, the box, layout and style of the one chosen, outlined over the
content, how long frames take, and warnings.

Windows whose developer tools are on open it with View → Toggle Developer
Tools, `F12`, or `Alt+Cmd+I` on macOS and `Ctrl+Shift+I` elsewhere. They
are on in development builds, as the web inspector is, and
`WindowOptions.Page.DevTools` turns them on or off for a window:

```go
mygo.NewWindow(mygo.WindowOptions{
	Content: ui.View(app.view),
	Page:    mygo.PageOptions{DevTools: mygo.DevToolsDisabled},
})
```

The content gives the panel the right of the window, up to half of it:
the view gets a narrower window, as `c.Size()` tells.

## The tree

Each element shows as what it is (the widget, as `Button` or `List`, else
`Row`, `Column`, `Text` and the like), its text or label, and its size.
Hovering an element in the tree outlines it over the content: its margins
in orange, its border and padding in green, its content in blue. Clicking
it chooses it, and its arrow hides and shows the elements inside it.

**Pick**, then a click on the content, chooses the element under the
pointer: the click goes to the inspector, not to the element. Escape stops
picking.

## The element chosen

The details of the element chosen are those of the last frame:

- where the app built it (the file and line of the call that created it,
  or gave it its `Key`), and its ID;
- its box in the window, its content box, padding, border and margins;
- its layout (a row, a column, a grid or a list, with its gap and
  alignment), its size as asked (in DIPs, a percentage or `auto`, with
  its minimum and maximum), and how it grows and shrinks;
- how it is positioned, when absolutely or attached;
- its font and colors, background and corners;
- its state: focused, under the pointer, pressed, moving with a
  [transition](transitions.md), its scroll offset.

## Frames and warnings

The line below the title tells how long the last frame took to build, lay
out and paint, and how many elements it had. Set `MYGO_FRAME_STATS` to log
slow frames as well (see [Rendering](rendering.md)).

Warnings list mistakes found while building, as two elements given the
same `Key` under one parent: they share one state, so that a click, the
focus or scrolling meant for one goes to the other. Apps log each one
once; in tests, they panic where the second key was given.
