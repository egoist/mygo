# Migrating to checked elements

This is a breaking change to native UI element types. Views keep their
`*ui.Context` parameter and child builders keep `func()` callbacks. Widget
constructors return `ui.Element` values. Window, webview and typed IPC
signatures keep their existing shape.

## Apply the source migration

From the app's checkout:

```sh
go tool mygo migrate-ui .          # preview affected files
go tool mygo migrate-ui -write .   # write the source migration
go tool mygo vet .
go test ./...
```

In MyGo's checkout, use `go run ./cmd/mygo` instead of `go tool mygo`.
Review the diff before committing it. The codemod uses Go syntax and the
actual UI import alias. It converts element and custom-parts pointer types,
element nil checks and zero assignments, moves fluent constructor keys into
`Context.Key`, and renames grid `Rows(n)` to `GridRows(n)`. Context pointers,
child callback signatures, ordinary model pointers and unrelated nil checks
are preserved. Dot imports and keys on separately stored elements need
manual review. It does not move arbitrary polling control flow into callbacks.

## Element values and keys

```go
// Earlier
var element *ui.Element
if element != nil { element.Focus() }

// Now
var element ui.Element
if element.Valid() { element.Focus() }
```

Use `ui.Element{}` instead of assigning or returning `nil`. Custom parts
such as `ui.SelectParts[T]` are values containing checked elements too.

An element expires before the next build pass, including a rebuild within
the same frame. Generation validation rejects it even when its arena slot
has been reused. `Tester` and development builds panic with a message naming
the passes and recommending `ui.Handle`; production builds with
`mygo_noinspector` return empty query results or ignore stale mutations.
The absent zero value remains safe in either mode. `Valid` does not panic.

Keys now enter before state initialization:

```go
ui.TextInput(c.Key("search"), &a.query).ReadOnly(a.readOnly)
ui.Checkbox(c.Key(todo.ID), &todo.Done, todo.Title)
parts := ui.SelectBase(c.Key("choice"), &a.choice)
```

`Element.Key` remains available for containers before their children or
local state are built. Use `Context.Key` for stateful widgets, including
custom base controls. A key names the next outer control, and is consumed
before it initializes its state.

## Input and actions

Construction is eager. Bound-value input applies after all controls and
fluent configuration have been built. A second pass reads the updated model
and its `Changed`/`Submitted` notices. Changing `Disabled`, `ReadOnly`, or
slider settings before construction finishes affects that input; interaction
queries do not trigger constructor realization.

```go
ui.Button(c, "Save").Disabled(a.saving).OnClick(a.save)
ui.TextInput(c.Key("query"), &a.query).OnChange(a.search)
ui.TextInput(c.Key("draft"), &a.draft).OnSubmit(a.send)
c.OnShortcut(ui.Cmd, ui.KeyS, a.save)
```

Click and shortcut actions run after configuration and bound input. Change
and submit actions run when the rebuilt view observes their notices. Handled
input is consumed before another pass so actions do not repeat. Polling
methods remain available; prefer callbacks when an action changes the
collection being built. Keep I/O in workers and publish model results with
`Window.Update`.

## Persistent identity

Store `ui.Handle` instead of an element:

```go
type app struct {
    query string
    search ui.Handle
}

func (a *app) view(c *ui.Context) {
    ui.TextInput(c.Key("search"), &a.query).Bind(&a.search)
}

// On the UI thread, including inside Window.Update:
a.search.Focus()
```

Focus requests coalesce and wait while the control is hidden. Closing its
window cancels that window's request. `CancelFocus` cancels earlier.

`handle.Focused(c)` and `handle.FocusWithin(c)` read persistent identity,
including before the control is constructed. `c.Resolve(handle)` returns
only this pass's element. These are different queries: previous focus can
still be observed in the build that removes a control; committing that build
removes its actual focus.

A handle supports independent bindings in several windows. Queries take the
window's Context. Use `handle.Focus(c)` and `CancelFocus(c)` to select a
window; the no-argument Focus form requires at most one open binding. Before
the first binding, it waits for that first control. Give each window its own
widget state and focus field. `Ref`/`RequestFocus` remain aliases for
`Handle`/`Focus`.

`ListState`, `ScrollState`, `GridState` and `Router` carry a Handle; other
controls can bind any app-owned handle. Older ListState polling helpers
remain available; use `state.Handle` for persistent focus and commands.

## Focus bound to app data

```go
type pane int
const (none pane = iota; files; diff)

ui.List(c, &a.list, len(a.files)).FocusBind(&a.pane, files).
    Rows(func(row ui.ListRow) { ui.Text(row.Context, a.files[row.Index].Name) })

// An action requests the diff pane, waiting while hidden:
a.pane = diff

// Actual focus can differ while that request waits:
focused := ui.FocusedValue(c, &a.pane)
```

`FocusBind` requires a pointer to a comparable field and a matching value.
Values are unique within that field in one window; reserve its zero value
for no focus. The field holds desired focus. User focus changes update it
when no request is waiting. Setting zero clears focus. `FocusedValue` reads
actual focus using committed identities, regardless of construction order.
A hidden binding keeps its desired request, while actual focus can be on
another control or absent. Use separate fields for separate windows.

## Lists and keyboard commands

```go
a.filesView.OnShortcut(c, ui.Cmd, ui.KeyK, a.openSelected)
ui.List(c.Key("files"), &a.list, len(a.files)).Bind(&a.filesView).Grow(1).
    ItemKey(func(i int) any { return a.files[i].ID }).
    Selection(&a.selection).
    Rows(func(row ui.ListRow) {
        text := ui.Text(row.Context, a.files[row.Index].Name)
        if row.Selected() && row.ListFocused() {
            text.TextColor(c.Theme().Accent)
        }
    })
```

A Handle's shortcut may be declared before Bind. It runs after construction
only if that window built an enabled control for the handle, so a hidden
control takes no command. `ListRow` supplies the shared Context, index,
selection and list focus. The original `List(c, state, n, func(i int))`
constructor remains supported. Configure fluent lists before calling Rows.

## Persistent services and verification

Capture `c.Services()` for clipboard/URL callbacks and background redraws.
Services has weak window ownership and no build data. Clipboard and URL
methods run on the UI thread; Invalidate is safe from another goroutine.
Use `Window.Update` when publishing model changes.

`mygo vet` runs ordinary Go vet and type-aware checks for elements, parts,
and Context pointers stored in struct fields/package variables or captured
by/passed to goroutines. It follows imports, type aliases and inferred
variables; Handle, Services and view callback signatures are allowed.
These checks help enforce build lifetimes; they do not prove all lifetimes
or goroutine safety. Runtime generation checks remain necessary.

Exercise controls disappearing and returning, pending focus, list reordering,
text editing/IME, disabled and read-only settings, and command routing. Run
normal tests and inspect the app. See [Performance](performance.md) for
measured costs and reproduction commands.
