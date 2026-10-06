# Hosting platform views

`Window.NewNativeView` and `ui.HostView` embed a platform control inside a
MyGo layout: an `NSView` on macOS, a GTK 3 widget on Linux, or a child HWND
on Windows. This is an adapter API for controls that already exist in the
platform toolkit or a native library. Ordinary MyGo widgets remain the
portable choice for interfaces drawn and tested entirely in Go.

Run the demonstration with:

```sh
CGO_ENABLED=0 go run ./examples/native-host
```

It places an actual `NSTextField`, `GtkEntry`, or Win32 `EDIT` beside MyGo
buttons. It shows native editing, updates from Go, hiding without losing
state, disabling, and a MyGo modal dialog. The adapter sources in
`examples/native-host/control` demonstrate purego/Objective-C delegates,
GTK signals, and Win32 parent notifications, with one shared callback per
signature and instance routing. No JavaScript or Bun is involved.

## Creating and placing a control

Create the control after its window exists, then keep the `*mygo.NativeView`
in the state its view builds from:

```go
win := mygo.NewWindow(mygo.WindowOptions{Content: ui.View(app.view)})
app.control, err = win.NewNativeView(mygo.NativeViewOptions{
    Create:  createPlatformControl,
    Update:  updatePlatformControl,
    Dispose: disconnectPlatformControl,
})
```

The hooks take `mygo.NativeViewContext`. `Create` returns `(uintptr, error)`;
`Update` and `Dispose` return nothing. Implement adapters in files with
platform build constraints and use purego or syscall for native calls.
`Context.Platform` identifies the active backend. `Parent` is MyGo's
clipping container; `View` is zero in `Create`, and the control's handle in
later hooks. `Bounds` and `VisibleBounds` are surface coordinates in DIPs,
with a top-left origin, and `Scale` is device pixels per DIP.

Place the control as a leaf in the view:

```go
ui.Row(c).Fill().Gap(12).Children(func() {
    ui.Button(c, "Refresh")
    ui.HostView(c, app.control).Key("control").Grow(1).Height(40)
})
```

Give the element a size, a percentage, or `Grow`; it has no intrinsic
measurement of its platform control. Use a keyed element when siblings
can move. Place a control only once per frame and only in its owning
window. A misplaced or destroyed control produces an empty box. Put
MyGo borders and padding on a surrounding container.

`ui.Tester` lays out the element as an empty placeholder without calling
native hooks. Core integration tests can use the fake backend; a native
adapter itself needs tests against its actual toolkit.

## Ownership and the main thread

All hooks run on the main thread. `NativeView.Update(fn)`, `Focus`,
`MoveFocus`, `Destroy`, `IsDestroyed`, and `Window` are safe from any
goroutine. `Update` waits for `fn` to finish and requests a layout frame.
Use it for later native calls instead of retaining a context and using
its raw handles on a goroutine:

```go
go func() {
    value := loadValue()
    _ = app.control.Update(func(ctx mygo.NativeViewContext) {
        setPlatformValue(ctx.View, value)
    })
}()
```

An update to application state used by the view still goes through
`Window.Update`. Hooks and native callbacks must return promptly and must
never wait for work that needs the main thread. An options `Update` hook
runs after each placement, and once when an omitted element is hidden;
it should synchronize state without requesting another frame itself.

The adapter supplies a new view with this ownership:

| Platform | `Create` returns | MyGo's container and disposal |
|---|---|---|
| macOS | An unattached `NSView` with an owned +1 reference | A flipped, layer-backed `NSView`; MyGo removes and releases the control |
| Linux | A parentless GTK **3** widget with a floating or owned reference | A `GtkLayout` with a clipping bin window; MyGo destroys and unreferences the control |
| Windows | A `WS_CHILD` HWND created on the main thread with the supplied `Parent` | A child HWND; destroying it destroys the adopted child too |

After adoption, the adapter must not reparent, release, or destroy the
returned view or MyGo's parent. Use `Dispose` to disconnect callbacks and
release auxiliary resources while both native handles are still valid.
It runs exactly once, before native disposal, including on window close.
`Create` must clean up auxiliary resources when it returns an error;
`Dispose` is not called for a failed creation or attachment. The failed
container is removed (Windows consequently destroys children created
under that parent).

Omitting the element hides it and retains its native state, useful for
tabs and router pages. To release it, call `Destroy` or close its window.
Updates after disposal return an error; focusing a disposed, hidden,
fully clipped out, or disabled control does nothing.

## Layout and composition

MyGo sizes the entire control to the element's layout box and clips it to
the intersection of rectangular ancestor clips and the surface. Scrolling
moves the full control behind that clip; it does not resize the control
to just the visible part. GTK allocates a widget at least its native
minimum size, so choose sizes that fit its toolkit, theme, font, and
language. Windows geometry is snapped to device pixels at the window DPI.

Native controls occupy a drawing plane above MyGo's scene. If a later
element's hit-test rectangle intersects a hosted control, MyGo hides the
whole control until the overlap goes away. Controls behind the active
MyGo modal scope hide too. This lets ordinary widgets, popovers and dialogs
draw and receive input over a hosted area without invisible native controls
intercepting their input. Hiding retains the control's state.

Rounded masks, rotations, scaling effects, inherited opacity, and MyGo
materials do not affect the native subtree. Custom `DrawOver` painting
without a corresponding element rectangle cannot occlude a native view.
Use a separate layout region for native content, or omit it while such
drawing is visible. Captures of MyGo content render only the scene and
leave out native controls. A specialized plugin can implement its own
native capture if needed.

## Focus, input, and accessibility

Each hosted subtree is one MyGo tab stop by default. Tab and Shift+Tab
move between it and surrounding MyGo widgets, and a click focusing a
native descendant updates MyGo's focus. Ordinary pointer events, text
editing, native input methods, and non-Tab keys are handled by the native
control; they do not become MyGo `HandleInput` events or MyGo shortcuts.
MyGo Edit menu roles are forwarded to the focused native control using
AppKit responder actions, GTK key bindings, or Win32 edit messages, within
the editing capabilities of that control.
Disabled ancestors disable native input. Hiding or closing a control gives
its keyboard focus back to the MyGo surface.

For a compound control, supply an options `Focus(ctx, backward)` hook to
select the appropriate entry descendant. Set `ManagesTab` when the native
subtree owns internal Tab navigation, and call `MoveFocus(backward)` when
it should leave the subtree. The default macOS and Windows focus targets
the returned root; GTK also tries its native child focus traversal.

Native callbacks can update app state on the main thread and call
`Window.Invalidate`. On Windows, the options `Message` hook receives
messages sent to the supplied parent, including `WM_COMMAND` and
`WM_NOTIFY`. Return `handled=false` for default processing. Pointer-valued
message parameters are valid only while the hook runs. On macOS and Linux,
use the toolkit's delegates/signals; keep callback functions shared and
route through instance data as the example does.

`HostView.Label` names the containing MyGo accessibility group. The
control's own accessibility implementation supplies its native roles,
names, values, text ranges, and actions below that group. MyGo grafts that
subtree into its logical layout: AppKit accessibility children, ATK native
objects (with the physical GTK overlay exposing only the MyGo surface),
and Windows UI Automation HWND override providers. Hidden controls leave
the logical accessibility tree. A control with no native accessibility
implementation does not acquire one from being hosted.

Native API tree traversal is covered by AppKit and GTK GUI tests;
VoiceOver, Orca, Narrator and NVDA behavior still needs verification with
the chosen control and application. Windows GUI verification requires a
Windows desktop session.

## Compatible adapters

The root must be an in-process view compatible with the toolkit MyGo uses
and able to live under the supplied parent. GTK 4 widgets, top-level
windows, cross-process HWNDs, shared/reparented views, and controls needing
their own UI thread or event loop are outside this API's contract. Native
popup windows or panels created by a control remain its responsibility.

This API can support optional PDF, video, or image components: an adapter
creates its platform viewer, updates it in the hooks, exposes its native
accessibility, and disposes its subscriptions and auxiliary resources.
Core hosting does not load specialized viewer libraries or define their
document/playback APIs. Embedded system webviews remain a separate API.
