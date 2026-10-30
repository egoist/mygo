# Testing

`ui.NewTester` runs a view without a window, as fast as a unit test: it
renders frames in memory, finds elements by their text, and clicks, types,
scrolls and presses keys.

```go
func TestCounter(t *testing.T) {
	app := &counter{}
	tt := ui.NewTester(app.view, 320, 240)
	if err := tt.Click("Increment"); err != nil {
		t.Fatal(err)
	}
	if app.n != 1 || !tt.HasText("1") {
		t.Errorf("count %d, texts %q", app.n, tt.Texts())
	}
}
```

Elements are found by their text, or by their `Label` for those without,
as an icon button: `tt.Click("Close")`, `tt.Find("Volume")`,
`tt.Focused("Name")`.

Two elements given the same `Key` under one parent share one state, so
that a click or the focus meant for one goes to the other: a tester
panics where the second key was given, as apps only log it.

## Semantic snapshots and control actions

`Inspect` and `Perform` expose the existing accessibility hierarchy to
agents and test tools. Import `github.com/egoist/mygo/ui/automation` for
the detached data types, action constants and sentinel errors:

```go
snapshot := tt.Inspect()
target, err := snapshot.Resolve(0, "Agree") // exact accessible name or leaf text
if err != nil {
	t.Fatal(err)
}
if err := tt.Perform(target, automation.Request{Action: automation.Press}); err != nil {
	t.Fatal(err)
}
fresh := tt.Inspect() // updated checked state, focus, values and geometry
data, err := json.Marshal(fresh) // encoding/json; suitable for an MCP response
if err != nil {
	t.Fatal(err)
}
_ = data
```

The runnable `ExampleTester_Inspect` in package `ui` shows the complete
sequence. Run it with `go test ./ui -run ExampleTester_Inspect` from the
repository root.

Nodes have an ID, parent index (`-1` at the top), role, accessible name,
available leaf text, value, bounds, states and supported actions. Chosen list
items, table rows, tabs and tree items have `Selected: true`; `Checked` also
retains their accessibility checked state. Checked checkboxes and radios do
not imply `Selected`. An explicit `Label` changes `Name`; available leaf
`Text` remains separately searchable.
`Resolve(id, "")` selects by ID. Duplicate name/text matches return
`automation.ErrAmbiguous`; inspect the nodes and select the desired ID.
Missing targets return `ErrMissing`. JSON encodes IDs, sessions, generations
and revisions as decimal strings to preserve all 64 bits in JavaScript clients.

Targets belong to one tester and one element lifetime. They survive
rebuilds and animation repaints while the element keeps its identity.
Removal returns `ErrMissing`; recreation with the same ID returns `ErrStale`,
as does using a target in another tester. Each action checks the
current semantic tree, enabled state, supported actions and, for clicks,
current geometry. Take fresh snapshots to observe the results of actions
and re-resolve removed or stale targets. Targets are local to the current
process; never persist them across restarts. `Snapshot.Revision` counts
paints for observation; it does not expire targets. `Inspect` itself does
not rebuild a frame.

The tree is semantic, rather than every layout box. Layout-only containers
and `RoleNone` are flattened. Invisible and inert content, including the
background of a modal dialog, is omitted. Only built virtualized rows are
included, and embedded web pages are not traversed. Bounds are transformed
axis-aligned boxes in DIPs relative to the native content surface, before
clipping and occlusion. `Offscreen` describes built content outside a scroll
viewport; it does not detect another desktop window covering the app.

`Actions` lists each control's supported operations. `Press` activates a
control semantically: it can toggle a checkbox even when another element
covers it. `Click` sends a single primary pointer down/up at its center and
rejects covered, clipped, off-surface or embedded-page targets with
`ErrCovered`. It does not move the system cursor or detect desktop window
occlusion. Existing `ClickAt`, keyboard and scrolling APIs remain available.
`Focus` can select a choosable row. `SetValue`, `SelectText` (rune offsets)
and `InsertText` edit through the control's normal input path; `Increment`
and `Decrement` use its keyboard behavior. `ScrollIntoView` reveals a built
target, while `Expand` and `Collapse` operate on expandable tree items.
Unsupported actions return `ErrUnsupported`; disabled controls return
`ErrDisabled`. Read-only fields allow selection but reject editing. Password
values and selections are redacted, including disabled password fields.
No operation reads or writes arbitrary application memory.

### Keyboard, text and scrolling

`tt.Input` sends keyboard, text and precise scrolling through the normal
input paths, including the code and rich-text editors. Focus a control
with `Perform` first for keyboard input:

```go
err = tt.Input(automation.Input{Kind: automation.KeyInput, Key: "CmdOrCtrl+A"})
// Check err before sending the next input.
err = tt.Input(automation.Input{Kind: automation.TextInput, Text: "Replacement text"})
```

Key input uses menu accelerator syntax, such as `Tab`, `Shift+Tab`,
`Escape` or `CmdOrCtrl+S`, and sends a press followed by a release. It does
not type characters; text input commits the supplied text at the current
selection. Unsupported keys and malformed chords return an error.
`automation.ScrollInput` takes `X`, `Y`, `DX` and `DY` in surface DIPs;
positive `DY` scrolls down. Scroll inside a list to build distant virtualized
rows before resolving them. Coordinates must be finite and on the surface;
embedded web pages are not driven. `tt.Input` accepts the same requests;
the existing tester keyboard and pointer helpers remain available.

Keep tester calls on their driving goroutine, outside view construction
and control callbacks. `Perform` and `Input` reject reentrant calls and
settle frames without advancing the simulated clock. These headless APIs
work in production-tag builds and open no listener. For live windows use
the [automation plugin](../plugins/automation.md), which owns the
development-only socket and window automation.

## What a tester does

- **Pointer.** `tt.Click`, `tt.ClickAt`, `tt.ClickWith(ui.Shift, "Row 3")`,
  `tt.Press`, `tt.Move` and `tt.Release`, `tt.Scroll`, which scrolls at
  once as a touchpad does, and `tt.Wheel`, a notch of a mouse wheel, which
  glides there as the clock advances (`tt.Advance`).
- **Keyboard.** `tt.Key(ui.Cmd, ui.KeyS)` presses a key, `tt.TypeKey` a key
  with the text it types, and `tt.Type` types text into the focused
  element. `tt.Compose` shows the composition of an input method,
  `tt.Command` performs an edit command of the menus, such as `"copy"`, and
  `tt.TextCaret` returns where input methods would compose.
- **Menus.** `tt.RightClick` opens a context menu, which `tt.Menu` lists and
  `tt.ChooseMenuItem("Move to", "Archive")` chooses from.
- **The window.** `tt.SetSize`, `tt.SetScale`, `tt.SetZoom` (the content's
  [zoom](windows.md#zoom), as `Context.SetZoom`; the tester's coordinates,
  of `Find`, `ClickAt`, `Move` and `TextCaret`, stay the window's), `tt.SetDark`,
  `tt.SetPreferences` and `tt.SetDirection` (lay it out
  [right to left](right-to-left.md)) change the window and the desktop's
  settings;
  `tt.SetFocused` takes the keyboard from the window and gives it back.
- **What it shows.** `tt.Texts`, `tt.HasText`, `tt.Find` (the box of a
  text), `tt.Focused`, `tt.Cursor`, `tt.Clipboard`, `tt.OpenedURLs` and
  `tt.Announcements`, what the view asked screen readers to read out;
  `tt.FailOpenURL` makes the links it opens fail, as those no app opens.
- **Frames.** `tt.Frame` draws a frame, for what changed outside the view,
  and `tt.Image()` is the last frame, for snapshots; `ui.Render` draws a
  view once at a given scale. Inputs and `tt.Frame` run the frames the
  view asks for until it asks for no more, or 20 of them:
  `tt.FramePending` tells whether it still asks, as a view that animates
  or never settles does.
- **Time.** A tester has a clock of its own, `tt.Now()`, which stands
  still until `tt.Advance(d)` moves it on by `d`, running the frames due
  meanwhile at their times: one each refresh while something moves, and
  those of `c.After` and `Painter.After` when they come due. What depends
  on time follows it: animations, `Context.After`, `Context.Now`, the
  clicks that make a double click, the typing that undoes in one step,
  the caret's blinking, tooltips and toasts. So a test reads the same on
  a busy machine as on a fast one: never sleep to let something happen,
  advance the clock. `Advance` runs only the frames the view asks for:
  after changing what the view shows from outside it, call `tt.Frame`
  first. The undo steps of an `editor.Buffer` and a `richtext.Document`
  use the system's clock.

  ```go
  tt := ui.NewTester(app.view, 320, 240)
  tt.Click("More")
  tt.Advance(100 * time.Millisecond) // half way through a 200 ms animation
  tt.Advance(time.Second)            // and done
  if tt.FramePending() {
  	t.Error("the panel keeps asking for frames")
  }
  ```

## Testing the built app

A tester runs a view inside `go test`. To test the app as built, with its
real windows, from another process, as an end-to-end test runner or an
agent does, import the [automation plugin](../plugins/automation.md) into
a test build: it reads the elements of every window as a tester's
`Find` does, by their labels or by their `Key`, acts on them, sends
input, and waits for frames as `FramePending` tells.
