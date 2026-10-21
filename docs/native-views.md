# Native views

A window whose `WindowOptions.Content` is MyGo's own UI can host a real
control of the platform inside it: an `NSView` on macOS, an `HWND` on
Windows, a `GtkWidget` on Linux. The app makes the control with the
platform's own toolkit, MyGo leaves a hole for it where the content places
it with `ui.NativeView`, and the platform draws and edits it there: its
caret, its selection, its scrolling and its input method are the system's,
and nothing about it is composed in Go.

It is a control of the same process, in a container MyGo makes inside the
window. It is **not** a region of a web page — a window whose content is a
page shows the page, not native views — not a cross-process handle (another
app's window, an out-of-process renderer, a video surface of another
process), and not a widget kit: MyGo draws the UI, and the native type
behind the view is the app's to write for each platform. The object has to be
a control the platform's own toolkit made in this process; MyGo promises
nothing about arbitrary GPU or video surfaces — an OpenGL area, a video
overlay, a layer of its own — whose pixels it cannot cut or clip, and a
backend may refuse such a control instead of hosting it (Linux does, below).

## Making one

```go
v, err := mygo.NewNativeView(win, mygo.NativeViewOptions{
	Create: func(h *mygo.NativeViewHost) (mygo.NativeViewAdapter, error) {
		// h.Parent is the container MyGo made for this view and lends it
		// for the view's lifetime; h.Scale is device pixels per DIP as the
		// window's scale was when the factory ran. Make the platform's own
		// type here, with its own toolkit (see "Writing the platform code"
		// below).
		return newTerminalView(h)
	},
})
if err != nil {
	if errors.Is(err, errors.ErrUnsupported) {
		// The browser and the unsupported platforms: run without the view.
	} else {
		return err
	}
}

// In the content of the window win shows:
ui.NativeView(c, v).Grow(1)
```

`win` must be a window that shows `WindowOptions.Content`, the native UI
MyGo draws itself. `mygo.NewNativeView` returns no view for a nil window,
one that is gone or one without content; it returns an error matching
`errors.ErrUnsupported` on the browser (`GOOS=js GOARCH=wasm`) and on every
platform MyGo keeps compiling through `internal/unsupported`. It is never a
silent no-op: an app that asks for a native view on a platform that has
none is told so, and can keep running without it.

`ui.NativeView(c, v)` places the view in the element's content box. A
native view has no size of its own: the element stretches across its
container and takes the room along it with `Grow` or a size. The view shows
while a frame builds its element, and hides when one does not, keeping its
state: the control of the tab that is not shown stays alive.

## The factory and the host

`NativeViewOptions.Create` runs once, on the main thread, with a
`*mygo.NativeViewHost` filled in before it runs:

| field | what it is |
| --- | --- |
| `Platform` | `"darwin"`, `"windows"` or `"linux"` |
| `Parent` | the platform handle of the container MyGo made for this view and lends the factory: an `NSView*`, an `HWND`, a `GtkWidget*`. It is framework-owned for the view's lifetime — a clip view or a container MyGo made, not the app's whole window — and the factory must not take it over, release it or remove the control it put in it |
| `Scale` | the window's scale **when the factory ran**: how many device pixels a DIP was. An initial snapshot, not the window's current scale: a platform that needs the scale now asks for it (Windows' `GetDpiForWindow`), and keeps this only as a fallback |
| `Focused()` | the view took the keyboard, as a click on one of its controls does |
| `TabOut(back)` | Tab moved past the view's last control, or Shift+Tab past its first (`back`) |
| `PointerDown(x, y)` | the pointer went down on the view, in **logical DIPs from the top-left of the root MyGo hosts** — the view the factory returned, not the platform's own event coordinates (see "Focus, pointer and input") |

The three callbacks are set before the factory runs, so a factory may call them
at any point of its own; a view is not live until the factory has returned and
MyGo has hosted its handle, though, and a report that arrives before then
reaches no window: it is ignored, so a control that reports focus, Tab or a
press during its own creation needs no answer and changes nothing. They are
safe from any goroutine and answer nothing. What they report after the view was
destroyed, or its window closed, does nothing either.

The factory returns the view's `NativeViewAdapter`, or an error. A factory
that makes its control and then fails must return the control it made
along with the error: MyGo destroys it, once.

## The adapter

The adapter is what MyGo hosts, and it stays the app's object.

```go
type NativeViewAdapter interface {
	NativeHandle() uintptr // NSView* / HWND / GtkWidget*
	Focus(back bool)
	Destroy()
}
```

- `NativeHandle` is read **once**, while the factory runs, and must be the
  view's own handle for as long as the view lives: MyGo shows the view at
  that handle and never asks for another one. A handle of 0 fails the
  creation.
- `Focus(back)` gives the view the keyboard, at its first control that
  takes the focus, or its last when `back`, as Tab and Shift+Tab do.
  `mygo.NativeView.Focus(back)` calls it.
- `Destroy` releases the control. MyGo calls it once, on the main thread,
  and never again.

The app owns the native object, and MyGo owns neither end of its life: the
factory makes it, `Destroy` releases it. MyGo positions, clips and hides
the handle — where the view shows, and how much of it — and never creates,
paints or frees the native object. MyGo wraps the adapter in a small view
of its own that answers the handle it read once; the app's object stays the
app's, and the app may keep using it after MyGo destroyed the view.

Each backend attaches the app's view itself, and fails the creation rather
than taking a control from somewhere else:

- macOS makes a clip view of its own and hands it to the factory as
  `Parent`.
- Linux puts the widget in the container MyGo made for it.
- Windows needs the control created with `Parent` as its parent, and the
  handle must be a **direct child** of it: MyGo checks `GetParent`, because
  a control nested under an intermediate window would be placed at that
  window's offset. Descendants inside the returned root are the control's
  own business.

In every case, the handle must be the factory's own fresh root: a control
that is already in another window or another parent is refused, and the
creation fails with a message saying so.

**The handle is the root MyGo hands the frame to.** MyGo places that view
where the element is — it sets its frame to the element's box, it does not
lay it out — so the view itself has to honor the frame it is given. A
control that sizes itself to its own content is not that root: as the root
it answers the element's box with the size of its content (an empty
AppKit `NSTextView` with the default `isVerticallyResizable` is about 14
DIPs tall), and the frame the host set is lost. Wrap a self-sizing
document view in a stable container that honors the frame — an
`NSScrollView` around the text view, as
[`examples/native-view-hosting`](../examples/native-view-hosting/README.md)
does — or turn the root's own self-sizing off before its first text is set
or laid out (`setVerticallyResizable:NO` and `setHorizontallyResizable:NO`
on a text view used as the root).

## Lifecycle

- **One placement, one window.** A view belongs to the window it was made
  for, and one element per view instance shows it. A frame that builds
  `ui.NativeView` twice for one view logs it once and shows the view in the
  first of them; a `ui.Tester` panics there, so a test fails where an app
  only gets the warning. A view made for a window whose content went away
  while the factory ran is destroyed and reported as an error.
- **Hidden is not destroyed.** While a frame does not build the element,
  the view is hidden and the platform keeps everything about it: the text
  the user typed, the caret, the selection, the scroll position. Showing it
  again costs a frame.
- **Element identity is immediate UI, the control's state is the
  platform's.** `ui.NativeView` builds an element like any other, so when
  siblings before it are added, removed or reordered, give the element a key
  — `c.Key(...)` before construction, or `ui.NativeView(c, v).Key(...)` — and
  its identity follows it as the frame changes, as [Keys](ui/views.md#keys)
  describes. The control does not depend on that: hiding, showing or moving
  the view keeps the text the user typed, the caret, the selection and the
  scroll inside it, because they belong to the platform's object and not to
  the element. Only `Destroy` loses them.
- **Destroyed once.** `mygo.NativeView.Destroy` releases the view for good
  and the content stops showing it; the window closing does the same for
  the views of its content. A destroyed view cannot be shown again —
  `IsDestroyed` answers true, `NativeHandle` answers 0 and `ui.NativeView`
  shows nothing — so an app that wants the control back makes a new one.
  Call `Destroy` from an event handler or a bound method, not while a frame
  is building the view.
- **Focus.** `mygo.NativeView.Focus(back)` moves the keyboard to the view
  as a press on it would, and is safe from any goroutine. It does nothing
  for a view the frame does not show, one that was destroyed, or one whose
  window closed.
- **Failures clean up.** When the factory fails, returns no view, returns
  one without a handle, or returns one the platform will not attach, MyGo
  destroys what the factory made, once, and no view is registered. After
  that, the callbacks of that host are inert, and the app's control is
  released exactly as `Destroy` would have released it.

## Focus, pointer and input

A native view is a hole in what MyGo draws, and the platform's control owns
everything inside it:

- **Focus is native.** The platform's own focus, caret, selection, IME
  composition and key handling are the control's; nothing of it is
  forwarded to Go. The element is in the content's focus order, so Tab
  reaches the view and `TabOut` is how it leaves, which is why a control
  that wants Tab inside itself should not report it.
- **The pointer is the root's own DIPs.** `PointerDown(x, y)` is the only
  pointer report, and it is a point in logical DIPs from the top-left of the
  root MyGo hosts — the view the factory returned — not the coordinates the
  platform's own event carries. Each platform's event coordinates are
  somewhere else, and the adapter converts:
  - **macOS** converts the event into that root, not into a document view
    inside an `NSScrollView`: a document point carries the document's scroll
    offset, so the same spot on screen would report a different `y` after
    scrolling. Then the root's bounds origin comes off, and `isFlipped`
    decides whether `y` inverts against the bounds height: the arithmetic
    the example's `coords.go` holds, tested by `coords_test.go` without a
    display.
  - **Windows** gets a point in the control's client area, in device pixels
    of the control's own window. MyGo sizes that whole **window rectangle**
    to the element's frame (a `WS_BORDER`ed root has its client area strictly
    inside it), so the message's point is not a point of the frame yet.
    `pressDIPs` (`coords.go`) adds the client area's own screen
    origin (`ClientToScreen`) and takes the window rectangle's off
    (`GetWindowRect`), then divides by the DPI the window has **now**
    (`GetDpiForWindow(hwnd) / 96`), not by the scale the factory heard:
    `localDIPs = (clientPoint + ClientToScreen({0, 0}) - GetWindowRect(root).topLeft) / currentScale`.
    The two origins are asked of the system, never assumed, and `Scale` is
    only the fallback where the system cannot answer.
  - **Linux** gets `gdk_event_get_coords`, in the coordinates of the
    `GdkWindow` that delivered the event — not always the widget's own: a
    windowless widget such as `GtkEntry` makes a private text-area window
    inset from it by its CSS border and padding. `eventToViewDIPs`
    (`coords.go`) adds that window's origin and takes the hosted
    view's off, both in the coordinates of their common window:
    `localDIPs = eventPoint + eventWindowOriginInCommonWindow - rootWidgetOriginInCommonWindow`.
    Every one of those is a logical unit — GDK reports event coordinates,
    window positions and allocations in application pixels, dividing X11's
    device pixels by the scale factor itself — so nothing multiplies by a
    scale and no screen coordinate is involved.

  MyGo notes the press — popovers close as they do for a press outside them —
  and handles none of it: the platform still places the caret, selects and
  drags.
- **The view takes the pointer where it shows and MyGo does not paint over
  it.** What MyGo paints over the control — its children, a popover, a menu —
  takes the press there, as it does over web content, and the control hears
  nothing: it gets the presses on the part of itself that is still uncovered.
  A button in a panel drawn over a native view is clicked like any other
  button of the app.

## What MyGo draws, and what the platform draws

The view is a rectangle the platform draws. MyGo shows the part of it
inside the element's content box, cut by the scroll containers and clips
around it, and paints what comes after the element over the view: its
children, popovers, menus, dialogs, tooltips and toasts. That paint takes the
pointer with it: where MyGo covers the control, MyGo gets the press, and the
control only gets the presses on the part it still shows (see above).

- The element's radius, shadow, opacity and transform are not applied to
  the control. There are no promises about rounding it, rotating it,
  applying an effect to it, blending it, or treating it as something the
  GPU renderer draws with: MyGo cuts the pixels it shows.
- MyGo's clip is visual. The platform keeps drawing and scrolling its own
  content inside the control; MyGo only cuts the part of it that shows.
- A native view and web content in one window are mixed by the platform's
  own compositing. The opaque case is what MyGo targets. Transparent web
  content blended over a native view is not promised: on Windows the frame
  of the content is composited over the view's clip window with
  DirectComposition, so a translucent page there shows the platform's
  composition, not the page behind it.
- Windows hosts a view only where DirectComposition is available, since
  that is what shows the content's frames over the app's control; the
  creation fails with a message saying so otherwise.

## Writing the platform code

The example's platform files are the worked version of all of this, one per
platform, and they are what to copy:

- `text_darwin.go` — AppKit through purego: a class registered once
  (`MyGoExampleTextView`, a subclass of `NSTextView`), with
  `becomeFirstResponder`, `mouseDown:` and `doCommandBySelector:` reporting
  through the host.
- `text_windows.go` — user32 through `syscall`: an `EDIT` control created
  with `Parent` and subclassed with one window procedure.
- `text_linux.go` — GTK 3 through purego: a `GtkEntry`, three
  `purego.NewCallback`s created once and a signal's user data finding the
  control.
- `text_other.go` — the browser and the unsupported platforms: an error
  matching `errors.ErrUnsupported`.
- `coords.go` — the arithmetic a press goes through on its way to
  `PointerDown`, all three platforms' versions, free of native events and so
  tested by `coords_test.go` without a display: macOS takes the bounds origin
  off (the scroll offset of a scrolled view) and asks `isFlipped` whether `y`
  is inverted against the bounds height; GTK, whose press arrives in the
  coordinates of the `GdkWindow` that delivered it, adds that window's origin
  and takes the root's off, the raw-point mistake held as a regression; and
  Windows takes a client-area point plus the client area's screen origin, less
  the frame's, over the window's current scale, the bordered, borderless and
  fractional-scale cases included.

Three rules the reader has to keep, from [AGENTS.md](../AGENTS.md):

- **No cgo.** Everything builds with `CGO_ENABLED=0`: purego on macOS and
  Linux, `syscall` on Windows.
- **Callbacks are scarce.** Make them once per signature at startup, never
  per view or per call, and route each call back to its control with the
  object the platform hands over (`self`, the `HWND`, a signal's user data,
  or a map from those).
- **Objects are owned.** An object that is `alloc`/`init`-ed on macOS, or
  `gtk_entry_new`'d on Linux (a floating reference), is released exactly
  once in `Destroy`, and a Windows control is destroyed once after its
  original procedure is put back.

On Linux, MyGo refuses a control it cannot host with clipping that would
hold rather than hosting it anyway: a `GtkGLArea` (OpenGL in a surface of
its own), a `GtkSocket` and a `GtkPlug` (another process's window, another
window's widget), and any subclass of them, as well as a `GtkWindow`, a
widget of MyGo's own, and a widget that is already another widget's child.

## The example

[`examples/native-view-hosting`](../examples/native-view-hosting/README.md)
is a small form that hosts a text control on each platform: MyGo's own
inputs on both sides of it, the status line that shows what the view
reported, buttons that hide, show, destroy, create and focus it, and a MyGo
panel painted over the control. The panel is interactive, which is what makes
the hit testing visible: a button in it counts its own presses and another
closes it, both clicked by MyGo with the platform's control under the panel
hearing none of them. It is the complete sample for this page:

```sh
go run ./examples/native-view-hosting
```

Its README states the contract the platform files assume (coordinates,
callbacks, ownership, lifecycle).

## Validation

It matters which kind of check a claim rests on: the arithmetic and the
policy are tested where no display is needed, the hosting itself only in a
real GUI session, and a cross-build proves neither.

**Without a display.** `go test ./...` runs the platform-neutral parts and
the platform arithmetic that takes no event:

- `go test ./examples/native-view-hosting` covers the adapter's forwarding
  to the control, and in `coords_test.go` the coordinate arithmetic the
  platform files call — including the GTK event-window inset and the Windows
  frame-and-border arithmetic the regressions below are about.
- `GOOS=darwin go test -tags nativegeom ./internal/windows/` decides which
  handles MyGo will host, the direct-child rule included, on a host that is
  not Windows.

Neither shows a control.

**The real GUI suite**, `internal/e2e`, makes a control with each platform's
own toolkit and drives it with the platform's own input:

```sh
MYGO_E2E=1 go test ./internal/e2e -run TestNativeView  # the native-view cases
MYGO_E2E=1 go test ./internal/e2e                      # the whole GUI suite
```

The `TestNativeView*` cases cover the control in the element's box; a click
giving it the keyboard, and `Focus` doing so programmatically; Tab and
Shift+Tab across it; a popover over it taking the click while the control
hears nothing; hide and show keeping its text; resize and partial clip; the
composition device taken away and the control still placed (Windows);
`Destroy` once, and a window close doing the same; a failing factory
cleaning up; and two native views over each other — and a native view over
an embedded page — in both paint orders. Three of them are about a real
problem rather than a hypothesis:

- `TestNativeViewPointerCoordinates` (Linux) is a regression: a `GtkEntry`
  is windowless and makes a private text-area window, so the `GdkWindow`
  that delivered a press is inset from the widget by its CSS border and
  padding, and forwarding the raw event coordinates missed the press by that
  inset. The test forces a large padding, presses a known point of the text
  area, and runs the binary again at `GDK_SCALE` 1 and 2: the reported DIPs
  must be the widget's own at both scales.
- The Windows fixture is a bordered root and a keyboard one: it gives
  the control a `WS_BORDER`, so its client area is strictly inside the frame
  MyGo places and a client-area point and a frame point differ, presses
  inside it and compares the report with where the system says the pointer
  was, and types through the `SendInput` packets that
  `TestWindowsInputLayout` and `TestWindowsTypePackets` hold to the layout
  the headers describe.
- On macOS, `TestNativeViewDarwinKeepsMarkedText` drives the control's own
  input client as an input method does and checks the marked text survives
  MyGo's frames: the marked-text protocol, not a physical input method, so
  it certifies no IME behavior. `TestNativeViewDarwinKeepsTheFrameItWasPlacedAt`
  makes AppKit lay the control's text out after the host placed it and
  checks the control keeps the element's own frame, exactly, with its text
  and with the text taken back out.

The suite needs a real GUI session:

- **macOS**: a desktop session; the tests inject AppKit's own events.
- **Linux**: a container with WebKitGTK 4.1 (GTK 3), `xvfb` and `dbus`,
  run as `dbus-run-session -- xvfb-run -a ./e2e.test` (see "Testing" in
  [the architecture guide](architecture.md#testing)). Its input is X11
  through XTEST, so the native-view input cases need an X11 session and a
  Wayland one is not covered.
- **Windows**: Windows with the WebView2 Runtime, which the fixture's own
  input hooks need.

Cross-building (`GOOS=linux`, `GOOS=windows`, `GOOS=js GOARCH=wasm`,
`GOOS=freebsd`, all with `CGO_ENABLED=0`) and `go vet` prove that the
platform files compile; that is not a runtime check.
