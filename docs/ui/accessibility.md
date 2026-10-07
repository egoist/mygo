# Accessibility

Assistive technology, such as VoiceOver on macOS, Orca on Linux and
Narrator or NVDA on Windows, reads a window's native UI as it reads other
apps: the widgets with their roles, names, values and states, the texts,
and the elements that take the focus, as the content changes. It acts on
them as the keyboard and the pointer would: it presses buttons, checks
boxes, moves sliders, edits text inputs and moves the focus.

## Names

Widgets describe themselves, and the text inside an element names it, as a
button's does. Elements without text need a `Label`, which also finds them
in [tests](testing.md):

```go
ui.Slider(c, &app.volume, 0, 100).Label("Volume")
ui.TextInput(c, &app.query).Placeholder("Search").Label("Search")
ui.Box(c).Size(24, 24).Draw(drawIcon).Label("Unread messages")
```

A [field](form.md) names its control with its label.

## Text ranges

[Text inputs and areas](text-input.md), read-only inputs, and [selectable
text](text.md) expose their caret and single contiguous selection. Assistive
technology can read ranges, move by characters, words, wrapped visual lines
or paragraphs, set the selection, find visible text, inspect range bounds,
and scroll a range into view. Ordinary `Text` and `RichText` expose reading
and range geometry too; `.Selectable()` enables selection and copying:

```go
ui.TextArea(c, &app.notes).Label("Notes")
ui.TextInput(c, &app.identifier).Label("Identifier").ReadOnly(true)
ui.RichText(c,
    ui.Span{Text: "Result: ", Weight: 600},
    ui.Span{Text: app.result},
).Selectable()
```

Reading follows logical text order, including bidirectional text. Character
navigation uses extended grapheme clusters, so combining accents, emoji
families and CRLF stay together. Native UTF-16 offsets on macOS and Windows,
and character offsets in ATK/AT-SPI, are converted through the paragraph's
rune/byte/UTF-16 index; partial bytes or surrogate pairs round to the rune's
start. Selection endpoints inside a grapheme round to its start. An empty
selection identifies the caret, and bounds may have separate rectangles for
the visual fragments of a bidirectional range.

Read-only text permits selection, copying and scrolling; setting its value
remains unavailable. Disabled text can be read but cannot be selected or
edited. Passwords expose their secure role and permitted value-setting
action, with no text, character count, caret, selection, range bounds or
text-change payloads. Password text never enters the range provider.

| Platform | Native text support |
|---|---|
| Windows | UI Automation `Text`/`Text2` and `ITextRangeProvider`: document, selection, caret and visible ranges; cloning, comparisons, navigation, text search, bounds, inline-link children and scrolling; `TextChanged` and `TextSelectionChanged` events |
| macOS | NSAccessibility text attributes and parameterized queries: strings, character and line ranges, caret line, selection setters, visible character range, range/point bounds; value and selected-text notifications |
| Linux | `AtkText`, bridged by GTK to AT-SPI: text and character queries, caret/selection setters, character/word/visual-line/paragraph ranges, point offsets, character/range extents, bounded ranges and substring scrolling; text, selection and caret signals |

Rich text provides its plain text, shaped geometry, selection and inline-link
relationships. Font/style attribute queries, sentence units, disjoint
selections, annotations and RTF extraction are not implemented. UIA returns
its reserved unsupported value for unimplemented attributes and promotes
unsupported format/page navigation to the next supported text unit. ATK
returns no sentence range. AppKit returns a single enclosing rectangle for
range bounds, and a single enclosing interval for visible text, as its API
requires.

Text areas keep paragraph virtualization when accessibility is enabled.
Reading and offset conversion need no layout; navigation shapes only the
paragraph being queried, and visible/bounds queries inspect only the
viewport. Queries outside the viewport use temporary paragraph layouts and
keep the scroll anchor and layout cache unchanged. An exact global visual
line number or range by line number needs to measure the prefix, so those
explicit ordinal queries can be more expensive in very large documents.

### Verifying provider support

Run `go run ./examples/accessibility-text` for a window containing editable,
read-only, selectable rich text, inline links and a password. Automated
tests cover Unicode, wrapped lines, bidirectional geometry, selection,
virtualization and disposal. The GUI tests query the native providers:

```sh
MYGO_E2E=1 go test ./internal/e2e -run TextAccessibility
```

Separate clients also verify the public system accessibility APIs and their
notifications, beyond calling the provider's methods directly:

```sh
# macOS, from a terminal already granted Accessibility permission:
go build -o /tmp/accessibility-text ./examples/accessibility-text
swift scripts/test-text-ax.swift /tmp/accessibility-text

# Linux, with python3-pyatspi, GTK and a virtual display installed:
go build -o /tmp/accessibility-text ./examples/accessibility-text
dbus-run-session -- xvfb-run -a python3 scripts/test-text-atspi.py /tmp/accessibility-text
```

The macOS AX queries and notifications and Linux ATK/AT-SPI queries and change
signals have been exercised from separate clients in desktop sessions.
Windows COM tests and GUI-provider
tests are included for the Windows CI runner; local verification from a
macOS host is limited to cross-compilation. Spoken navigation with VoiceOver,
Orca, Narrator and NVDA still needs manual screen-reader verification.

## Roles

Other elements get a role from what they do: one that is clickable and
takes the focus is a button, a scroll container a scroll area, and one with
a `Label`, or that takes the focus, a group. `Role` sets it for an element
drawn as a widget it is not built from, and `ui.RoleNone` leaves an element
out but not its children:

```go
toggle := ui.Box(c).Size(36, 20).Focusable().Role(ui.RoleSwitch).Label("Wi-Fi")
```

Besides the roles of MyGo's widgets, `ui.RoleHeading` is the title of a
section, ranked with `Level`, and `ui.RoleMenu`, `ui.RoleMenuBar`,
`ui.RoleMenuItem`, `ui.RoleMenuItemCheckBox` and `ui.RoleMenuItemRadio`
make menus drawn in the window, as a drop-down menu or a menu bar of your
own:

```go
ui.Text(c, "Appearance").Bold().Role(ui.RoleHeading).Level(2)
```

## States

The bases tell assistive technology what their widgets show. A widget of
your own tells it with these, as ARIA's states do on the web:

| Method | What it tells |
|---|---|
| `Checked(on)`, `Mixed()` | whether a check box, switch, radio button, toggle or menu item is on, or partly on |
| `Expanded(open)` | whether what the element opens shows, as a popup or a section |
| `Value(s)` | its value, as the choice a button opening a popup shows |
| `Range(lo, hi, value)`, `Step(step)` | the range and value of a slider, progress bar, meter or stepper, and how far the keys move it |
| `Level(n)` | the rank of a heading, or how deep an item of a tree is |
| `ActiveDescendant(e)` | the option that has the focus while the element keeps it, as the one the arrows are on in a menu or a list |

```go
all := ui.CheckboxBase(c, &app.all).Label("Select all")
if app.some {
	all.Mixed()
}

date := ui.ButtonBase(c).Role(ui.RolePopUpButton).Label("Due").Value(app.due.Format("Jan 2")).Expanded(app.picking)
```

## Descriptions and errors

`Description` tells more than the name, as help text read after it, as a
[tooltip](tooltip.md) does, and `Error` marks a value as invalid, for a
message read with it; a [field](form.md) gives its control the texts below
it.

## Lists and tables

A [list](list.md)'s rows are list items, and a [table](table.md)'s rows,
named by the text inside them, and each says which of all the rows it is,
"5 of 10,000", although the list builds only those in view. A list choosing
its rows gives assistive technology the focus on the row chosen: Up and
Down are read as they move the choice, and focusing a row chooses it. A
screen reader moving out of view asks the list to scroll there, which
builds the rows it reaches. `Label` names a list or a table.

## Announcements

News that does not move the focus, as a search done or a file saved,
reaches screen readers with `c.Announce(text)`, which they read once, after
what they are reading: VoiceOver's and Orca's announcements, and a live
region that Narrator and NVDA read on Windows. [Toasts](toast.md) announce
their text, and a [router](navigation.md) the title of a page shown while
the focus stays outside it.

```go
if ui.Button(c, "Search").Clicked() {
	app.results = search(app.query)
	c.Announce(fmt.Sprintf("%d results", len(app.results)))
}
```

MyGo describes frames only once assistive technology asked, so apps pay
nothing for it otherwise.

Each component's page says what assistive technology sees of it.
