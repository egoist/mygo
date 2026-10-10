# Web view

`ui.WebView` shows a web page in a window of native UI: a document the web
engine renders beside native controls, a page per tab, a preview of HTML
the app makes. The page is the system's web view (WKWebView, WebView2,
WebKitGTK), made with `Window.NewWebView`, and works as a window's page
does: it loads the app's frontend, calls bound Go methods with the typed
client and hears events. What the view paints after the web view shows
over the page: a menu opening from a button above it, a select's options,
a dialog and its dimmed backdrop, tooltips, toasts, a badge over a corner.

```go
type browser struct {
	win  *mygo.Window
	docs *mygo.WebView
	menu bool
}

func (app *browser) view(c *ui.Context) {
	ui.Column(c).Fill().AlignItems(ui.Stretch).Children(func() {
		ui.Row(c).Padding(8).Gap(8).Children(func() {
			menu := ui.Button(c, "Menu ▾")
			if menu.Clicked() {
				app.menu = !app.menu
			}
			// The popover shows over the page.
			ui.Popover(c, menu, &app.menu, func() { ui.Text(c, "Over the page") })
		})
		ui.WebView(c, app.docs).Grow(1).Radius(8)
	})
}

func main() {
	app := &browser{}
	mygo.App.WhenReady(func() {
		app.win = mygo.NewWindow(mygo.WindowOptions{Title: "Docs", Content: ui.View(app.view)})
		app.docs, _ = app.win.NewWebView(mygo.WebViewOptions{URL: "/docs"})
		app.win.Invalidate()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
```

## Making web views

`Window.NewWebView` makes a web view of a window showing `Content`, which
it closes with. Its options are the page's (`Page`, as a window's
`PageOptions`), the `URL` to load, a `BackgroundColor` to fill it with
until the page paints, and `Transparent`, which shows what is behind the
web view through the transparent parts of its page: the window's
background or material, as native UI paints nothing under a web view.
`WebView.Page` returns its page, whose methods are a window's page's:
`LoadURL`, `LoadHTML`, `Eval`, `GoBack`, `OnDidNavigate`, `OnWillNavigate`
and the others. `Focus` gives it the keyboard, `CapturePage` a screenshot
of its page, and `Destroy` closes it.

Make a web view once, as a window, not in the view: the view only shows
it. One the view does not build hides and keeps its page, so a view of tabs
builds the web view of the tab shown, and switching back finds the page
where it was.

```go
if app.tab == "docs" {
	ui.WebView(c, app.docs).Grow(1)
} else {
	ui.WebView(c, app.changelog).Grow(1)
}
```

## Its box

A web view has no size of its own. The element stretches across its
container (`AlignSelf(Stretch)`) and takes the room along it with `Grow`,
or a size. The page shows in the element's content box: inside its border
and padding, which paint around it with the element's background, rounded
as its `Radius` makes the border's inner edge. Scroll containers and clips
around it cut it as they cut any element, rounded corners included, and a
web view scrolled out of view hides. `Opacity` fades the page with the
element, as a page of a `Router` fading in does: the page blends over what
is painted under the element.

```go
ui.WebView(c, app.preview).Height(320).Radius(12).Border(1, t.Border)
```

## What shows over it

The web view is under the window's native UI: the element cuts a hole
through what the window paints, where the page shows. Everything painted
after the element paints over the page, as it would over any element: its
children, elements absolutely positioned over it, and the overlay's
popovers, menus, selects, comboboxes, dialogs, tooltips and toasts.
Shadows and translucent backgrounds blend over the page.

```go
ui.WebView(c, app.page).Grow(1).Children(func() {
	// A badge over the page's corner.
	ui.Text(c, "Draft").Absolute().Top(8).Right(8).Padding(2, 8).Radius(8).Background(t.Accent)
})
```

The pointer goes to the page where it shows and nothing painted over it
takes the pointer: a dialog's backdrop or a select's keeps the pointer
from the page while open, a popover keeps it only over its panel, and an
element with `PassThrough` lets it through. Files dragged there drop on
the page. A press on the page closes the popovers it is outside of
(`PressedOutside`), and takes the keyboard from the native UI, which gets
it back as the user clicks it. The page sets the cursor over it.

Tab stops at the web view among the native UI's controls: its page takes
the keyboard at its first element, or its last with Shift+Tab, Tab moves
through its elements, and past its last one goes on to the control after
the web view. `Focus` gives the page the keyboard where it had it.

Assistive technology finds the page inside the web view's element, where
it is among the native UI: VoiceOver and Orca read and move through it as
through the controls around it.

## Its page

The page of a web view works as a window's does (see
[Bindings](../bindings.md)). `mygo.CallerPage(ctx)` returns the page that
called a bound method, and `Page.WebView` its web view, while
`mygo.CallerWindow(ctx)` returns the window the web view is in, which the
page's window controls (`mygo.window.minimize()`, drag regions) act on.
`Event.Emit` to the window reaches the pages of its web views,
`Event.EmitPage` one page, and `Broadcast` every page.

```go
func (s *Notes) Save(ctx context.Context, text string) error {
	page := mygo.CallerPage(ctx) // the web view's
	...
}
```

The page's `window.close()` leaves the window open.

## Limits

The page is the system's web view, which the window composites with the
native UI; MyGo does not draw it. As with SwiftUI's `WebView`:

- Effects reading what is behind them, as the glass plugin's blur, see
  none of the page: it is not part of the window's frames.
- `Window.CapturePage` of the window shows a hole where the page is;
  `WebView.CapturePage` captures the page.

On Windows, WebView2 keeps the elements of a page it shows this way under
a window of its own: Narrator finds the web view in place, and the page's
elements in that window, not inside it.

## Testing

In a `Tester`, `WebViewAt` returns the web view that takes the pointer at a
point of the window, as the last frame showed it: the one under it, unless
what is painted over it takes the pointer there. `ClickAt` and `Press` over
a web view press the page, which the view hears of as the platform tells
it.
