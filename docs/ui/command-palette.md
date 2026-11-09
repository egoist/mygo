# Command palette

Package `ui/command` shows a searchable list of app commands over the
window. It uses native UI, including in browsers, and needs no registry,
background worker or extra dependency.

```go
import "github.com/egoist/mygo/ui/command"

// Keep the state in the app, one per palette and window.
app.palette = command.State{Label: "Commands"}
app.commands = []command.Item{
    {ID: "open", Title: "Open File", Keywords: "read browse", Shortcut: "Ctrl+O"},
    {ID: "settings", Title: "Preferences", Detail: "Appearance and editor", Keywords: "settings"},
    {ID: "save", Title: "Save", Disabled: !app.modified},
}

// Build the palette every frame, including while closed.
if c.Shortcut(ui.Cmd, ui.KeyK) {
    app.palette.Open = true
}
if id, chosen := command.View(c, &app.palette, app.commands); chosen {
    switch id {
    case "open": app.openFile()
    case "settings": app.showSettings()
    case "save": app.save()
    }
}
```

`View` reports a choice once and closes before returning its ID. The app
owns the actions and their key bindings: `Shortcut` is a display hint.
IDs must be nonempty and unique, even when titles repeat; invalid IDs
panic when the command set changes.

Search matches case-insensitive subsequences of the title and keywords;
spaces separate terms, all of which must match. Exact and prefix matches
rank ahead of loose matches; contiguous letters and word starts, including
camel case, rank higher. Equal scores keep the input order. Unicode case
equivalents and CJK text work without transliteration or accent removal.

Up and Down move through enabled results, Page Up and Page Down eight at
a time, and Enter chooses. A click chooses its row. Disabled commands stay
visible but cannot be chosen. Escape and a click outside dismiss, and the
dialog restores the focus it took. Opening clears the query. Changing
commands while open keeps the keyboard choice by ID, unless it disappears
or becomes disabled; changing the query chooses the first enabled result.

The palette follows the window's theme, direction and language. `Label`,
`Placeholder` and `EmptyText` override its accessible name, search prompt
and empty message; their defaults are MyGo's localized Search and No
results. `Width` (560 DIPs by default) and the results' `Height` (320) are
capped by the window. Rows have a uniform height that grows with the theme's
font size; only the visible rows and a few beyond are built. Search results
are cached until the query or commands change.

Assistive technology sees a modal dialog, a combo box controlling a list
box, and one option per command, with its selection, disabled state and
position in the full result set. The search field keeps the text input
focus while its active descendant follows the keyboard choice. An empty
message is a status.

The [gallery](../../examples/gallery) opens its page launcher with
Cmd+K (Ctrl+K elsewhere), or Go to page… in the toolbar.
