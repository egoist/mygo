# DesktopControl

The desktopcontrol plugin reads and drives the local, logged-in desktop:
its displays and windows, JPEG screenshots of them, synthesized input
aimed at one display, process or window, and the static labels and
buttons of dialogs. It is what a remote desktop, an agent that operates
other apps, or an accessibility tool needs from the system. It is all Go,
through purego, on macOS 14 and later: no JavaScript package, and
nothing to `mygo.Use`. Pages reach it through the app's own bindings.
Elsewhere every function returns `ErrUnsupported`, and `Available`
reports false.

It opens no sockets, starts nothing when imported, and grants no remote
access. Consent, sessions, limits on input and whatever the app sends
elsewhere are the app's.

```go
import "github.com/egoist/mygo/plugins/desktopcontrol"

wins, err := desktopcontrol.Windows()
if errors.Is(err, desktopcontrol.ErrScreenRecording) {
	// ask with permissionflow.Guide(ctx, permissionflow.ScreenRecording, ...)
}
for _, w := range wins {
	jpeg, err := desktopcontrol.CaptureWindow(ctx, w, desktopcontrol.CaptureOptions{MaxDimension: 1280, Quality: 0.7})
	// ...
}
```

[examples/desktopcontrol](../../examples/desktopcontrol) lists the
windows of the desktop and shows a screenshot of the one chosen.

## Permissions

The plugin never prompts: it reports a permission the app lacks with an
error, and the app asks for it with
[permissionflow](permissionflow.md), whose `Guide` takes the user to
System Settings.

| Function | Needs | Without it |
| --- | --- | --- |
| `Displays`, `Locked`, `Wake`, `ApplicationInfo` | — | — |
| `Windows`, `CaptureDisplay`, `CaptureWindow` | Screen Recording | `ErrScreenRecording` |
| `Post`, `PostToDisplay`, `PostToProcess`, `PostToWindow` | Accessibility | `ErrAccessibility` |
| `ReadDialog`, `Press` | Accessibility | `ErrAccessibility` |

Run from a terminal, macOS gives the permissions to the terminal; a built
app asks for its own.

## Displays and windows

`Displays` lists the active displays, the main one first. A display is
its `ID` and its `UUID`: the system gives a numeric ID to another screen
once one is unplugged, so the functions that take a `Display` check both
and return `ErrDisplayUnavailable` when they no longer match.

`Windows` lists the visible windows from front to back. A window is its
`ID` and its `PID`, since an ID may be reused once its app exits; a gone
or mismatched window is `ErrWindowUnavailable`. `Layer` is the window
level, 0 for ordinary windows.

Bounds are in points of the global display space, with the origin at the
top left of the main display, as input takes them.

`ApplicationInfo` reads an app bundle's name, identifier and, when asked,
its icon as a PNG, without launching it.

## Screenshots

`CaptureDisplay` and `CaptureWindow` return a JPEG through
ScreenCaptureKit, its longest edge at most `MaxDimension` pixels, at
`Quality` from 0 to 1. A window is captured alone, never its display
in its place. Call them off the main thread, which ScreenCaptureKit may
need to finish. Canceling the context returns at once; the system's late
result is discarded.

## Input

An `Input` is one action: `Move`, `Click`, `DoubleClick`, `RightClick`,
`Drag` (from `X`, `Y` to `ToX`, `ToY`), `Scroll` (`DeltaX`, `DeltaY` in
pixels), `Text`, typed as keys and never through the clipboard, or `Key`,
a virtual key code of the Mac keyboard with `Modifiers`.

- `Post` sends it as local input arrives.
- `PostToDisplay` checks the display's identity first, and that the
  points are within it.
- `PostToProcess` sends `Text` and `Key` to one process alone.
- `PostToWindow` aims at one window. A pointer action needs the window
  topmost under its point, `Text` and `Key` need it in front; when it
  isn't, it is raised first, and only when its geometry names it alone.
  It never falls back to another window or to the whole display. A
  `Move` never raises; `Text` stops with `ErrWindowUnavailable` once the
  window leaves the front.

Invalid input (coordinates that aren't finite, modifiers on anything but
`Key`, empty or invalid UTF-8 text) is refused before anything is sent.

## Dialogs

`ReadDialog` reads a window's title, static labels and buttons, at most
eight, through accessibility. It never reads editable text or secure
fields, and skips app navigation and documents: scroll areas, outlines,
tables, toolbars, menu bars and web areas. `Press` presses the window's
one enabled button with that label, and refuses an ambiguous or disabled
one.

## The session

`Locked` reports whether the console shows the lock screen. `Wake`
declares user activity, as a key press would: a sleeping display lights
up and the idle timer restarts. It changes no setting and holds nothing.

## Tests

`go test ./plugins/desktopcontrol` reads displays, windows, app metadata
and dialogs, and skips what the system or its permissions don't allow; it
never sends input.
