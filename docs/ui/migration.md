# Migrating to checked UI values

This redesign is a breaking change to native UI. The window, webview and
typed IPC APIs keep their existing signatures. Views now receive a
`ui.Frame` value and widget constructors return `ui.Element` values.
See the [performance measurements](performance.md) for the measured cost
of handle validation and deferred list construction.

## Apply the source migration

Run the command from the app's checkout, with its usual MyGo CLI:

```sh
go tool mygo migrate-ui .          # preview the affected files
go tool mygo migrate-ui -write .   # write the source migration
go test ./...
```

In a checkout of MyGo itself, use `go run ./cmd/mygo` instead of
`go tool mygo`. Review the diff before committing it.

The codemod reads Go syntax and the imported MyGo package name. It changes
context and element types, adds scoped frame parameters to inline child
builders, migrates element nil checks and assignments, and renames grid
`Rows(n)` to `GridRows(n)`. It leaves other libraries and ordinary pointer,
slice and error nil checks alone. Dot imports and stored handles need
manual review. Helper functions used as builders may also need a frame
parameter; the compiler identifies their mismatched signatures.

## Frame and element values

```go
// Earlier
func (a *app) view(c *ui.Context) {
    ui.Column(c).Children(func() {
        ui.Text(c, "Hello")
    })
}

// Now
func (a *app) view(f ui.Frame) {
    ui.Column(f).Children(func(child ui.Frame) {
        ui.Text(child, "Hello")
    })
}
```

Every builder receives the frame scoped to its parent. Pass that frame to
the elements created inside it. A helper should accept `ui.Frame` too:

```go
func details(f ui.Frame) { ui.Text(f, "Details") }
ui.Column(f).Children(details)
```

Replace `*ui.Element` declarations and callback parameters with
`ui.Element`. Its zero value is absent. Use `e.Valid()` instead of
`e != nil`; use `ui.Element{}` instead of assigning or returning `nil`.
Queries on expired elements return false or zero, and mutations do nothing.
An absent element's `Children` does not run its callback.

Frame and element copies retain their original window and generation.
They expire before the next build pass, including a rebuild in the same
frame. Keeping one in app state no longer risks accidentally controlling a
different element, but it does not create a persistent reference.

## Handle actions after building

Prefer callbacks for application actions:

```go
ui.Button(f, "Save").Key("save").Disabled(a.saving).OnClick(a.save)
ui.TextInput(f, &a.query).Key("query").OnChange(a.search)
ui.TextInput(f, &a.draft).Key("draft").OnSubmit(a.send)
f.OnShortcut(ui.Cmd, ui.KeyS, a.save)
```

Actions run on the UI thread after the view and its configuration are
built. Handled input is consumed before another pass, so rebuilding does
not repeat the action. Keep I/O in worker goroutines and publish results
with `Window.Update`.

Polling methods such as `Clicked`, `Changed` and `Submitted` remain for
low-level composition and migration. An interaction query realizes a
deferred widget immediately, so finish its configuration first. In
particular, set `Key` before children, interaction queries or local state.
Stateful controls now accept keys before initialization; `Disabled` and
text input `ReadOnly` are available before they handle input.

The codemod deliberately does not replace arbitrary polling conditions
with callbacks: moving `return`, `break`, `continue`, an `else` branch or
captured variables into a function can change program behavior.

## Keep persistent control references

Replace saved element pointers used for focus with a `ui.Ref`:

```go
type app struct {
    query string
    search ui.Ref
}

func (a *app) view(f ui.Frame) {
    ui.TextInput(f, &a.query).Key("search").Ref(&a.search)
}

// On the UI thread, including inside Window.Update:
a.search.RequestFocus()
```

A request waits while the control is hidden, and coalesces with another
request. Closing the window cancels it. Use `CancelFocus` to cancel it
earlier. `f.Resolve(a.search)` returns this build's element, and
`f.Focused(a.search)` and `f.FocusWithin(a.search)` query its current focus.
A Ref belongs to one window and can bind one control per build. Keep
separate references and widget state for separate windows.

## Lists and row scopes

```go
ui.List(f, &a.list, len(a.files)).Key("files").Ref(&a.filesView).Grow(1).
    ItemKey(func(i int) any { return a.files[i].ID }).
    Selection(&a.selection).
    OnShortcut(ui.Cmd, ui.KeyK, a.openSelected).
    Rows(func(row ui.ListRow) {
        text := ui.Text(row.Frame, a.files[row.Index].Name)
        if row.Selected() && row.ListFocused() {
            text.TextColor(row.Frame.Theme().Accent)
        }
    })
```

`ListRow` supplies its current frame, index, selection and list focus, so a
row builder does not need a saved list element or an extra focus wrapper.
`ListState` and `Selection[T]` remain persistent app state. The earlier
list callback form is available as `func(ui.Frame, int)` while migrating.
Provide keys for reorderable items; a widget pointer is not item identity.

## Persistent services for custom controls

Do not retain a Frame for input callbacks or background redraw requests.
Capture its window's `ui.Services` instead:

```go
services := f.Services()
element.HandleInput(func(event ui.InputEvent) bool {
    services.WriteClipboard("copied")
    return true
})

// Safe from another goroutine:
services.Invalidate()
```

Services has weak window ownership and no frame data. Clipboard and URL
methods run on the UI thread. Invalidate requests a frame without changing
model state; use `Window.Update` when publishing a model change.

## Verify the migration

Exercise extra frames while controls disappear and return. Test pending
focus, keyboard shortcuts, list reordering, text editing and disabled
states with `ui.NewTester`. Run your normal tests and inspect the app when
changing callbacks or layout. Custom material builders now receive
`ui.Element` values; update their method signatures too.
