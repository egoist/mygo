# Native view hosting

MyGo draws its own UI, and hosts a real control of the platform inside it: an
`NSTextView` in an `NSScrollView` on macOS, a multiline `EDIT` on Windows, a
`GtkEntry` on Linux. The control draws and edits itself — its caret, its
selection, its scrolling and its input method are the system's — and MyGo
draws everything around it and leaves a hole where the view belongs.

```sh
go run ./examples/native-view-hosting
```

The example is a small form. One of MyGo's own text inputs comes first, the
platform's control sits in the middle, and another of MyGo's inputs comes
last — built in that order, so Tab can be watched crossing MyGo's focus ring,
the real keyboard focus the native view takes, and MyGo's ring again. Each
input carries a label and a placeholder where it is drawn, so which one is
which does not depend on their order alone. A status line shows what the view
reported, and the buttons hide, show, destroy, create and explicitly focus it
(`Focus(back)` is the public way in: the example never calls into its own
adapter). **Popover over the view** opens a MyGo panel drawn over the
platform's control. MyGo paints what comes after the view over it, and it takes
the pointer there as well: the buttons in the panel are clicked by MyGo, and
the platform's control only hears the presses on the part the panel leaves
uncovered. The panel's counter is how many presses the panel itself took, and
its close button shuts it without the control ever hearing the press. Hiding a
view does not destroy it,
so the text the user typed is still there when it is shown again; `Destroy`
releases the control, and `Create` makes a new one with no text.

## How it is put together

| file | what it is |
| --- | --- |
| `main.go` | the app: the window, the layout, and what the host reported |
| `host.go` | the shared part: `textView`, and `adapter`, which is what MyGo hosts |
| `coords.go` | the arithmetic that turns AppKit's point into the root-local DIPs `PointerDown` asks for |
| `coords_window.go` | the same for the `GdkWindow`-relative point GTK reports |
| `text_darwin.go` | AppKit through purego: `MyGoExampleTextView`, a subclass of `NSTextView` |
| `text_windows.go` | user32 through `syscall`: an `EDIT` control, subclassed |
| `text_windows_geom.go` | the Windows arithmetic: a client-area point to the DIPs of the frame MyGo placed, current DPI and measured border included |
| `text_linux.go` | GTK 3 through purego: a `GtkEntry` |
| `text_other.go` | the browser and the unsupported platforms: an error saying so |

The factory `mygo.NewNativeView(win, mygo.NativeViewOptions{Create: ...})`
creates the control in the per-view container MyGo made for it
(`NativeViewHost.Parent`) and returns it as a `mygo.NativeViewAdapter` whose
`NativeHandle` is the `NSView*`/`HWND`/`GtkWidget*` the factory made. MyGo positions, clips and
hides that handle and puts `ui.NativeView(c, v)` where the hole belongs; the
adapter keeps its control's ownership, its focus and what it reports. The app
never calls into the adapter: `mygo.NativeView.Focus(back)` moves the keyboard
to the view the way a press on it would, and the two focus buttons call that.
Native input methods (IME) are entirely the platform's: nothing composes text
in Go. See [the native views guide](../../docs/native-views.md) for the API
and its guarantees.

## What the native view reports, and what it does not

- `Focused` when the control took keyboard focus: on macOS
  `becomeFirstResponder`, on Windows `WM_SETFOCUS`, on Linux
  `focus-in-event`.
- `TabOut(back)` when Tab (or Shift+Tab) is pressed at the edge of the view.
  The example takes those keys away from the platform's own focus chain so
  MyGo, which knows the focus order of the controls it draws, moves focus on.
- `PointerDown(x, y)` when the pointer goes down on the control, in logical
  DIPs from the top-left of the root MyGo hosts — the view the factory
  returned, not the platform's own event coordinates. MyGo notes the press (a
  popover over the view closes on it) and handles none of the control's own:
  the platform still places the caret, selects and drags the text itself. Where MyGo paints over the view —
  the panel above, the view's own children — MyGo takes the press as it does
  for its own elements, and the control hears nothing: it only gets the presses
  on the part of itself that is still uncovered.

## Restrictions

- The view is a control of the same process, in the window's own container.
  It is not a way to embed another app's window, a video surface or another
  process's rendering.
- The platform draws the control itself: the element's radius, shadow,
  opacity and transform are not applied to it, and MyGo shows only the part
  of it inside the element's content box, cut by the scroll containers and
  clips around it (`ui/nativeview.go` says the same). It is a rectangle the
  platform draws; what MyGo paints after the view — children, popovers, menus,
  dialogs, tooltips — shows over it, and MyGo takes the pointer there too: a
  button in a panel painted over the control is clicked like any other button
  of the app, and the control only gets the presses on the part it still shows.
- A native view and web content in one window are mixed by the platform's own
  compositing. The opaque case is what MyGo targets; transparent web content
  blended over a native view is not promised, because on Windows the frame of
  the content is composited over the view's clip window with DirectComposition,
  so a translucent page there shows the platform's composition, not the page
  behind it.
- The host's `Parent` is the platform handle of the container MyGo made for
  this view and lends the factory: a framework-owned clip or container, not the
  app's whole window and not a view the app made (macOS: a clip view MyGo
  makes, and MyGo puts the app's view in it unless the factory already did;
  Linux: the host adds the widget to the parent it gave and takes it out again;
  Windows: the `EDIT` needs its parent at creation, so the factory passes it
  straight to `CreateWindowExW`).
- A view has one placement: one element per view instance. Destroying a view
  releases the control for good (`NativeHandle` answers 0 afterwards).
- The browser (`GOOS=js GOARCH=wasm`) and the platforms MyGo keeps compiling
  through `internal/unsupported` have no native controls: `NewNativeView`
  returns an error matching `errors.ErrUnsupported`, and this example shows
  the message instead of failing. The app still runs.

### The contract this example assumes

- **Coordinates.** `NativeViewHost.PointerDown` is `(x, y)` **in logical DIPs
  relative to the root MyGo hosts** — the view the factory returned — with
  `(0, 0)` at its own top left, not the platform's own event coordinates. The
  content moves it into its own space with the frame the view shows in
  (`ui/nativeview.go`'s `pressLocal` adds that frame's origin, and the content
  hit-tests in its own top-left coordinates). Each platform's event
  coordinates are somewhere else, and the files below hold the arithmetic that
  does not need a native event:
  - **macOS** converts the window point with `convertPoint:fromView:` into the
    `NSScrollView` MyGo hosts — the root — and not into the `NSTextView`
    document view inside it. A document point carries the scroll offset in it
    (the text view's frame moves under the scroll view as the document
    scrolls), so the same spot on screen would report a different `y` after
    scrolling; the scroll view's own coordinates do not move. `coords.go`'s
    `localDIPs` then takes the root's bounds origin off
    (its scroll offset, zero for a scroll view whose content does not scroll it)
    and, because an `NSScrollView` is not flipped and AppKit counts `y` upward
    from the bottom of its bounds, inverts `y` against the bounds height —
    asking `isFlipped` rather than assuming, so a flipped handle still comes out
    right. The result is unaffected by how far the document is scrolled.
  - **Windows** gets a point in the control's client area, in device pixels of
    the control's own window: MyGo sizes the control's whole **window
    rectangle** to the frame the content placed the view at
    (`internal/windows/nativeview.go`'s `placeNative`), so a root with a
    border — this example's `EDIT` has `WS_BORDER` — has its client area
    strictly inside that frame, and the message's point is not a point of the
    frame yet. `text_windows_geom.go`'s `pressDIPs` adds the client area's own
    screen origin, takes the frame's off, and divides by the DPI the window
    has **now**, `GetDpiForWindow(hwnd) / 96`, asked on the message itself: a
    window dragged to a display with another scale would otherwise report
    every press in the scale it was created with. The distance between the two
    origins is asked of the system (`ClientToScreen` against `GetWindowRect`),
    never assumed — `SM_CXBORDER`'s width is not promised — and the scale the
    factory heard (`NativeViewHost.Scale`, an initial snapshot) is only the
    fallback where the system cannot answer. `internal/e2e`'s Windows fixture
    measures the press against that same frame and current DPI.
  - **Linux** gets `gdk_event_get_coords`, which is in the coordinates of the
    `GdkWindow` that delivered the event, not necessarily the widget's own: a
    windowless widget such as `GtkEntry` makes a private text-area window,
    inset from the entry by its CSS border and padding, and the press arrives
    on that. `coords_window.go`'s `eventToViewDIPs` adds the delivering
    window's origin and takes the root's off. Every one of those is logical —
    GDK divides X11's device pixels by the window's scale factor itself — so
    nothing multiplies by a scale factor.
- **Callbacks.** The platform callbacks are made once per signature, never per
  view (`objc.RegisterClass` methods, one `syscall.NewCallback`, three
  `purego.NewCallback`s), and a view is found by the object the platform hands
  the callback: `self`, the `HWND`, or the signal's user data. Nothing is ever
  freed per view.
- **Ownership.** macOS: the `NSTextView` and `NSScrollView` are `alloc`/`init`
  and are released in `destroy` (with an autorelease pool around creation).
  Windows: the `EDIT` is `DestroyWindow`-ed after its original window
  procedure is put back. Linux: `gtk_entry_new` is a floating reference, sunk
  with `g_object_ref_sink`, and `destroy` calls `gtk_widget_destroy` and then
  `g_object_unref` — exactly once, after MyGo has detached the entry.
- **Lifecycle.** MyGo calls the adapter's `Destroy` exactly once on the main
  thread, when the app destroys the view (`NativeView.Destroy`, what this
  example's Destroy button calls) or when its window closes; a destroyed view
  cannot be shown again, which is why the example makes a new one instead.
  Hiding a view is not destroying it: `ui.NativeView` is simply not built, and
  the platform keeps the control and its text.
- **Element identity.** `ui.NativeView` builds an element like any other, so
  when siblings before it are added, removed or reordered, give the element a
  key — `c.Key(...)` before construction, or `ui.NativeView(c, v).Key(...)` —
  and it follows the view as the frame changes, as
  [Keys](../../docs/ui/views.md#keys) describes. What the control holds — its
  text, caret, selection and scroll — does not depend on that: those belong to
  the platform's object, not to the element, and only `Destroy` loses them.
- **Threading.** The factory, `NativeHandle`, `Focus` and `Destroy` run on the
  main thread, as do the platform's own callbacks, so nothing here locks the
  control's state; the registries that route a callback to its view do.
- **The container is the host's.** The factory makes its control inside
  `NativeViewHost.Parent`, the per-view container MyGo made for this view and
  lends it for the view's lifetime — a framework-owned clip or container, never
  the app's whole window, and never the app's to release — and never inside the
  window itself: the macOS file refuses a zero `Parent` instead of
  making a control nothing hosts, the Linux file puts its widget there, and the
  Windows control is created with it as its parent and must be a **direct
  child** of it (MyGo checks `GetParent`, because a control nested under an
  intermediate window would be placed at that window's offset; descendants
  inside the returned root are its own business). The handle is the factory's
  own fresh root, not a control that is already in another window or another
  parent. A control MyGo cannot attach fails the creation with a message rather
  than showing nothing.
- **`Scale` is an initial snapshot.** `NativeViewHost.Scale` is the window's
  scale when the factory ran, not a live value that follows the window to
  another display; a platform that needs the current scale asks for it (Windows
  asks `GetDpiForWindow` on each event) and keeps `Scale` only as its fallback.
  The host's callbacks are another matter: `Focused`, `TabOut` and
  `PointerDown` are set before the factory runs, so they can be called at any
  point in it, but a view is not live until the factory returns and MyGo has
  hosted the handle — a report that arrives before then reaches no window and is
  ignored, so a control that reports while it is being created needs no answer.

## Validation

Two kinds of check, and which one a claim rests on matters.

Without a display, `go test ./examples/native-view-hosting` covers the
adapter's forwarding to the control and the coordinate arithmetic the platform
files call: `coords_test.go`, `coords_window_test.go` (including the GTK
event-window inset) and `text_windows_geom_test.go` (the Windows frame and
border arithmetic). `GOOS=darwin go test -tags nativegeom ./internal/windows/`
decides which handles MyGo will host, the direct-child rule included, on a
host that is not Windows. Neither shows a control.

The GUI tests in `internal/e2e` make a real control of each platform's toolkit
and drive it with the platform's own input:

```sh
MYGO_E2E=1 go test ./internal/e2e -run TestNativeView
```

They cover the control in the element's box; a click giving it the keyboard,
and `Focus(back)` doing so programmatically; Tab and Shift+Tab across it; a
MyGo panel over it taking the click while the control hears nothing; hide
and show keeping its text; resize and partial clip; `Destroy` once, and a
window close doing the same; a failing factory cleaning up; and two native
views over each other — and a native view over an embedded page — in both
paint orders. Three of them come from a real problem or a real fixture:

- `TestNativeViewPointerCoordinates` (Linux) is the regression: a `GtkEntry`
  makes a private text-area window, inset from the entry by its CSS border and
  padding, so `gdk_event_get_coords` is not the widget's own coordinates, and
  forwarding the raw event point missed the press by that inset. The test
  forces a large padding, presses a known point of the text area, and runs
  again at `GDK_SCALE` 1 and 2: the reported DIPs must be the widget's own at
  both scales.
- The Windows fixture is the bordered root and keyboard one: it gives the
  control a `WS_BORDER`, so a client-area point and a frame point differ,
  presses inside the frame and compares the report with where the system says
  the pointer was, and types through `SendInput` packets, whose layout
  `TestWindowsInputLayout` and `TestWindowsTypePackets` check without a
  window.
- On macOS, `TestNativeViewDarwinKeepsMarkedText` checks that the marked text
  the control's own input client composed survives MyGo's frames: the
  marked-text protocol, not a physical input method, so it certifies no IME
  behavior.

A real GUI session is required: macOS a desktop session; Linux a container
with WebKitGTK 4.1 (GTK 3), `xvfb` and `dbus` (`dbus-run-session -- xvfb-run
-a ./e2e.test`, see "Testing" in
[the architecture guide](../../docs/architecture.md#testing)). The Linux
input cases are X11, through XTEST, so a Wayland session is not covered.
Windows needs Windows with the WebView2 Runtime, which the fixture's own input
hooks need.

The cross-builds (`GOOS=linux`, `GOOS=windows`, `GOOS=js GOARCH=wasm`,
`GOOS=freebsd`, all with `CGO_ENABLED=0`) and `go vet` prove the platform
files compile, not that they run.

### Which backends host a view today

- macOS (`internal/darwin/nativeview.go`), Linux
  (`internal/linux/nativeview.go`) and Windows
  (`internal/windows/nativeview.go`) each implement the backend side of
  native-view hosting (`platform.NativeViewSurface`), and the fake surface
  serves the unit tests. Each makes a container of its own for the view and
  hands that to the factory as `Parent`; on macOS that container is a clip view
  MyGo makes, and the app's view may already be in it or still detached (this
  example leaves its `NSScrollView` detached).
- the browser (`GOOS=js GOARCH=wasm`) and the platforms MyGo keeps compiling
  through `internal/unsupported` have no native controls: there
  `NewNativeView` returns an error matching `errors.ErrUnsupported`, and this
  example shows the message instead of failing. The app still runs.
