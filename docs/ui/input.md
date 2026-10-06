# Input

Elements report what the user did to them since the last frame, as you
build them: ask, and handle it where the element is built.

```go
card := ui.Column(c).Padding(12).Radius(8).Focusable()
if card.Hovered() {
	card.Background(t.SurfaceHover)
}
if card.DoubleClicked() {
	app.open(item)
}
```

## The pointer

`Hovered`, `Pressed`, `Clicked`, `DoubleClicked`, `RightClicked`, `Dragged`
(how far the pointer moved since the last frame while pressing the
element) and `PointerPosition`. `ClickModifiers` returns the modifier keys
held for the last click, as Shift for a Shift-click. `PassThrough` lets the
pointer through to what is below.

Elements that take the pointer give it to the innermost under it: a button
in a clickable row takes its own clicks.

While the pointer presses an element, the elements it was over as the
press began, as the row around a button, stay `Hovered` as long as it is
over them, as in CSS, and the others hover no more: dragging over other
elements does not light them up. A button that shows while its row is
hovered stays as it is pressed, to take its click:

```go
row := ui.Row(c).Padding(8)
hovered := row.Hovered()
row.Children(func() {
	ui.Text(c, item.Title).Grow(1)
	if hovered && ui.Button(c, "Remove").Clicked() {
		removed = item.ID
	}
})
```

## Contacts, pen input and capture

`HandleInput` reports every contact as it arrives. `InputEvent.Pointer` has
an `ID` stable for that contact within the window, a `Device` (`PointerMouse`,
`PointerTouch`, `PointerPen`, or `PointerTouchpad`), and `Primary` and
`Contact`. Mouse ID is zero. IDs can be reused after a contact ends: key
active contacts by ID and remove them on Up or Cancel. Multiple fingers
can be active and captured by different elements at once.

`X` and `Y` are DIPs relative to the element, including outside its box
while captured. Pixel scale does not change those coordinates. Pen
`Pressure` is 0–1 and `TiltX`/`TiltY` are degrees from -90 to 90 along the
screen axes; `HasPressure` and `HasTilt` distinguish unsupported axes from
valid zero readings (`HasTilt` requires both axes). `Eraser` identifies the eraser end when reported.
A stationary pen can change pressure without changing position.

Taking Down captures that contact implicitly. `InputPointerCapture` and
`InputPointerCaptureLost` bracket capture; Up releases it. Explicit
`element.CapturePointer(id)` additionally prevents touch gestures and
scroll containers from taking that contact. `ReleasePointer(id)` releases
it without completing a click. These methods, like other element methods,
run while building the view or in a callback on the UI thread.

The OS cancelling a contact, losing capture or focus, closing the window,
or removing/disabling its element produces `InputPointerCancel` and
capture loss. A cancelled press produces no `Clicked` or typed drop.
For compatibility with existing handlers, an unhandled Cancel also sends
Up with `Cancelled` set; handle Cancel to use only the richer lifecycle.
Enter and Leave describe surface boundaries and preserve capture.

The first direct touch or pen contact also drives the existing click,
focus, drag and text-selection APIs. Additional contacts go to raw input
handlers. Touchpad contacts are **indirect**: `X`/`Y` locate the cursor when
the sequence began, while `NormalizedX`/`NormalizedY` are 0–1 coordinates
on the pad, with a top-left origin. They do not click or become a second
mouse pointer. Native trackpad gestures are used separately, so a gesture
is not synthesized again from these contacts.

```go
pad := ui.Box(c).Size(300, 200)
pad.HandleInput(func(ev ui.InputEvent) bool {
	switch ev.Kind {
	case ui.InputPointerDown:
		pad.CapturePointer(ev.Pointer.ID) // independent strokes, no scrolling
		app.beginStroke(ev.Pointer.ID, ev.X, ev.Y)
	case ui.InputPointerMove:
		app.extendStroke(ev.Pointer.ID, ev.X, ev.Y, ev.Pointer.Pressure)
	case ui.InputPointerUp, ui.InputPointerCancel:
		app.endStroke(ev.Pointer.ID)
	default:
		return false
	}
	return true
})
```

## Pan, pinch and rotation

`element.Gestures(kinds, fn)` offers `GesturePan`, `GesturePinch` and
`GestureRotation` together. A callback returning true to `GestureBegin`
claims the requested transformations for the sequence. A callback leaving
Begin offers the gesture to ancestors, innermost first, then scrolling for
pan. Updates stay with their owner, even outside its bounds. Return values
on updates do not transfer ownership. End and Cancel finish the sequence.

```go
pad.Gestures(ui.GesturePan|ui.GesturePinch|ui.GestureRotation,
	func(ev ui.GestureEvent) bool {
		if ev.Phase == ui.GestureUpdate {
			app.x += ev.DX
			app.y += ev.DY
			app.zoom *= ev.Scale
			app.angle += ev.Rotation
		}
		return true
	})
```

The focal point `X`/`Y` is relative to the element. `DX`/`DY` follow the
fingers in DIPs; scrolling moves content by their negative. `Scale` is an
incremental multiplier (1 means no change), and `Rotation` is incremental
clockwise radians. `TotalX`, `TotalY`, `TotalScale`, `TotalRotation` accumulate
since Begin. Begin and terminal events have neutral deltas. `Contacts` is
zero when the OS does not report its count.

Direct touch recognition waits for movement (6 DIPs of pan, 4 DIPs of
separation, or 0.04 radians). Pan uses the contacts' center, and pinch and
rotation the pair with the lowest IDs. Contact-count changes end the old
gesture and reset its baseline to avoid jumps. Removing or disabling a
gesture owner cancels it until the current contacts end. A claimed gesture cancels
ordinary presses and implicit captures; scrolling cancels a pending tap
as it starts. Explicit capture, scrollbar thumbs, and a one-finger slider,
text selection or typed drag retain their interactions. Touch scrolling
keeps its initial scroll owner until the contact count changes. Native
trackpad pan that no gesture handler takes stays ordinary precise scrolling
along its initial hit chain, including `HandleInput`. Mouse wheels retain
their existing scrolling behavior.

| Input | macOS | Linux (GTK 3) | Windows |
|---|---|---|---|
| Touch contacts | Indirect `NSTouch` trackpad contacts; Macs do not supply touchscreen input through this path | GDK touchscreen sequences, with separate contact IDs | Win32 `WM_POINTER` touch contacts, Windows 8+ |
| Pen | AppKit tablet-point events, device ID, pressure and tilt | GDK tablet source and available pressure/tilt axes | `GetPointerPenInfo`, pressure/tilt availability masks |
| Trackpad gestures | AppKit magnification, rotation and phased precise scroll as pan | GDK pinch/swipe, GTK 3.18+; phased smooth scroll when the driver supplies stop events (3.20+) | Precision touchpads retain wheel scrolling; this backend does not expose their raw contacts or pinch/rotation |
| Direct touch gestures | No direct touch source | Recognized in Go from contacts | Recognized in Go from contacts |

Hardware, drivers and the compositor determine which events and axes
arrive. GDK touchpad gestures commonly require Wayland support. No backend
invents pressure or tilt for a mouse. AppKit/GDK normalized tilt axes are
scaled to degrees; Windows reports degrees directly. Native device IDs are
opaque and local to a surface, not persistent hardware identifiers.
The fake backend and `ui.Tester` accept the same contact and gesture event
model; unsupported platforms still compile and cannot create native windows.

Run `go run ./examples/gallery` and open **Input** for a transform pad,
contact markers and pen telemetry, with an explicit capture toggle.
`Tester.Pointer` sends contact events in window DIPs, and `Tester.Gesture`
sends recognized gestures; use `Pointer` to test direct-touch recognition.

The translators follow [AppKit trackpad events](https://developer.apple.com/library/archive/documentation/Cocoa/Conceptual/EventOverview/HandlingTouchEvents/HandlingTouchEvents.html),
[GDK touch sequences](https://docs.gtk.org/gdk3/struct.EventTouch.html),
[GDK pinch events](https://docs.gtk.org/gdk3/struct.EventTouchpadPinch.html),
and [Win32 pointer input](https://learn.microsoft.com/en-us/windows/win32/inputmsg/wm-pointerdown).

## The keyboard focus

`Focusable` elements take the focus when clicked, and Tab and Shift+Tab
move it between them, with a focus ring when it moves by keyboard.
`AutoFocus` gives an element the focus when it appears, such as the first
field of a dialog, and `Focus` keeps it there while you call it; `Focused`,
`FocusVisible` and `FocusWithin` report it. Enter and Space press a focused
button or link, and Space toggles a focused check box, switch, toggle or
radio button.

## Focus groups

The controls of a toolbar, a radio group, a segmented control or a tab list
are one stop of Tab, as they are natively: Tab moves the focus into the
group, to the control that had it last (or the radio button or tab chosen),
and on out of it, while the arrows move it among them, Home and End to the
first and last. `FocusGroup` does it for a container of your own, with the
arrows `ui.Horizontal`, `ui.Vertical` or both; a slider or a text input
inside keeps the arrows it takes.

```go
ui.Row(c).Gap(4).FocusGroup(ui.Horizontal).Children(func() {
	for _, tool := range tools {
		if ui.Button(c, tool.Name).Clicked() {
			app.tool = tool
		}
	}
})
```

## Shortcuts

`c.Shortcut(ui.Cmd, ui.KeyS)` reports a key pressed with exactly those
modifiers anywhere in the window, and `Element.Shortcut` only while the
element or one inside it has the focus, which comes first. A focused button
or link keeps Enter and Space, and a toggle Space, so
`c.Shortcut(0, ui.KeyEnter)` presses a dialog's default button wherever
else the focus is.

```go
if c.Shortcut(ui.Cmd, ui.KeyS) {
	app.save()
}
```

`ui.Cmd` is Command on macOS and Ctrl elsewhere. `ui.KeyBack` and
`ui.KeyForward` are the back and forward buttons of a mouse and the keys of
keyboards that have them, which a [router](navigation.md) takes. Shortcuts
of [menus](../menus.md) still work, and the Edit menu's roles (cut, copy,
paste, select all, undo, redo) act on the focused text input.

A focused text input takes the editing keys of the platform first: on
macOS, Option and Command with the arrows and Backspace, and Control with
A, E, B, F, N, P, D, H and K, as in other Mac apps; elsewhere, it leaves Alt
and the arrows, which go back and forward, and the function keys.

## Input methods

Text inputs take text composed with input methods, which see the text
around the caret: macOS's press and hold replaces the letter it accents,
Japanese input methods convert typed text again, and others predict from
what comes before.

## Dropped files

`DroppedFiles` returns the paths of the files dropped on the element from
Finder, Explorer or a file manager, and `FileDragOver` reports files dragged
over it, to show it would take them. The window takes files only over such
elements, or anywhere when it has `OnFileDrop` listeners, which get the
files no element takes, with where they were dropped:

```go
zone := ui.Column(c).Size(260, 80).Border(1, t.Border)
if files := zone.DroppedFiles(); files != nil {
	app.files = files
}
if zone.FileDragOver() {
	zone.Border(2, t.Accent)
}
```

Values dragged within the window are [drag and drop](drag-and-drop.md).

## Every key, as it comes

Widgets that take every key themselves, as the
[terminal](../plugins/terminal.md) does, get their input as it comes with
`HandleInput`, before the next frame: keys pressed and released, text typed
and composed while they have the focus, the edit commands of the menus,
and the pointer pressed on them, moving over them or scrolling over them.
The function reports whether it took the event; one it leaves goes on to
shortcuts, Tab, context menus and scroll containers as usual, and keys that
the window or an element around the focus handles with `Shortcut` go there
first. `TextCaret` turns on the system's input methods for such an element
while it has the focus, composing at the caret it gives. `c.ReadClipboard`,
`c.WriteClipboard` and `c.OpenURL` copy, paste and open links for them;
`c.OpenURLThen` opens a link too, and its function gets what came of it a
moment later, as the system opens it, with an error when no app could,
and the view builds a frame anew:

```go
ui.Box(c).Fill().Focusable().HandleInput(func(ev ui.InputEvent) bool {
	switch ev.Kind {
	case ui.InputKeyDown:
		return app.key(ev.Mods, ev.Key)
	case ui.InputText:
		app.insert(ev.Text)
		return true
	}
	return false
}).TextCaret(app.caretRect())

c.OpenURLThen(url, func(err error) {
	if err != nil {
		c.Toast("No app opens " + url)
	}
})
```

## See also

- [Context menus](context-menu.md) and [menu buttons](menu-button.md).
- [Tooltips](tooltip.md).
- Custom title bars: [windows with native UI](windows.md).
