// Command native-view-hosting shows MyGo hosting a real text control of the
// platform: an NSTextView in an NSScrollView on macOS, a multiline EDIT on
// Windows, a GtkEntry on Linux. MyGo draws the window and the controls
// around it and hosts the native one in the element that asks for it: the
// control draws and edits itself, with the platform's own caret, selection
// and input method.
//
//	go run ./examples/native-view-hosting
//
// The text stays with the platform: hiding the view does not destroy it (the
// status line says what the view reported), and the buttons make and destroy
// it again. See README.md for what a native view can and cannot be, and
// docs/native-views.md for the API this example uses.
package main

import (
	"errors"
	"fmt"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// The assertion keeps this example honest about the public interface it
// implements: mygo calls exactly these three methods, on the main thread.
var _ mygo.NativeViewAdapter = (*adapter)(nil)

type app struct {
	win    *mygo.Window
	native *mygo.NativeView
	shown  bool
	// popover is whether the MyGo panel drawn over the native view is open,
	// and clicks is how many presses that panel has taken itself: MyGo gets
	// the presses where it paints over the platform's control, so a control
	// inside the panel is clicked like any other MyGo control, and the
	// platform's control never hears those presses.
	popover bool
	clicks  int
	// before and after are MyGo's own text inputs. One is drawn above the
	// native view and one below it, so the focus ring and Tab can be seen
	// crossing between MyGo's inputs and the platform's control.
	before string
	after  string
	status string
}

func main() {
	mygo.App.WhenReady(func() {
		a := &app{shown: true, status: "No native view has been made yet."}
		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title:   "Native View Hosting",
			Width:   760,
			Height:  560,
			Content: ui.View(a.view),
		})
		a.make()
		a.win.Invalidate()
	})
	mygo.App.Run()
}

// make creates the native control, or says why it could not: the browser and
// the platforms MyGo keeps compiling through internal/unsupported report
// errors.ErrUnsupported, and the example shows that instead of failing.
func (a *app) make() {
	v, err := mygo.NewNativeView(a.win, mygo.NativeViewOptions{
		Create: func(h *mygo.NativeViewHost) (mygo.NativeViewAdapter, error) {
			view, err := newTextView(myHost(h, a))
			if err != nil {
				return nil, err
			}
			return &adapter{view}, nil
		},
	})
	if err != nil {
		a.native = nil
		if errors.Is(err, errors.ErrUnsupported) {
			a.status = "This platform hosts no native views: " + err.Error()
		} else {
			a.status = "The native view could not be made: " + err.Error()
		}
		return
	}
	a.native = v
	a.status = fmt.Sprintf("A native text control, handle %#x. Type into it, and press Tab at its end.",
		v.NativeHandle())
}

// myHost is what the platform control calls to report. mygo sets the
// callbacks of the host it hands to the factory before it runs the factory,
// so h.Focused, h.TabOut and h.PointerDown are all there by the time the
// control is created; a view is not live until the factory has returned and
// mygo has put the handle in the window, though, and what the callbacks report
// before then reaches no window: a factory that creates a control which
// reports at once does not have to answer it.
func myHost(h *mygo.NativeViewHost, a *app) host {
	return host{
		platform: h.Platform,
		parent:   h.Parent,
		scale:    h.Scale,
		focused: func() {
			a.note("The native control took focus.")
			h.Focused()
		},
		tabOut: func(back bool) {
			if back {
				a.note("Shift+Tab at the start of the native view: the app moves focus on.")
			} else {
				a.note("Tab at the end of the native view: the app moves focus on.")
			}
			h.TabOut(back)
		},
		pointerDown: func(x, y float64) {
			a.note(fmt.Sprintf("Pointer down at (%.0f, %.0f) in the view's own coordinates.", x, y))
			h.PointerDown(x, y)
		},
	}
}

// note records what the native control reported. The platform and mygo call
// it on the main thread, so it may touch the app's state; Invalidate asks for
// the frame that shows it.
func (a *app) note(what string) {
	a.status = what
	if a.win != nil {
		a.win.Invalidate()
	}
}

func (a *app) view(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Padding(16).Gap(12).Children(func() {
		ui.Text(c, "Native View Hosting").FontSize(20).Bold()
		ui.Text(c, a.status).FontSize(13).TextColor(t.TextMuted)

		// MyGo's own inputs sit one on each side of the platform's control,
		// and they are built in that order: Tab moves Before -> the native
		// view -> After. Both carry a label where they are drawn, so which
		// one is which does not depend on the order alone.
		ui.Field(c, "Before: a text input MyGo draws itself", func() {
			ui.TextInput(c, &a.before).Placeholder("MyGo edits this one")
		}).Description("Tab reaches this input, then the platform's control, then the input below it.")

		var hole ui.Element
		if a.native != nil && a.shown {
			// The hole: MyGo paints everything around it, and the platform's
			// control draws and edits itself inside it.
			hole = ui.NativeView(c, a.native).Grow(1)
		} else {
			hole = ui.Column(c).Fill().Center().Children(func() {
				ui.Text(c, "No native view is shown here.").TextColor(t.TextMuted)
			})
		}

		ui.Field(c, "After: another text input MyGo draws itself", func() {
			ui.TextInput(c, &a.after).Placeholder("MyGo edits this one too")
		}).Description("The platform keeps the view's text while it is hidden; Destroy takes it away for good.")

		ui.Row(c).Gap(8).Wrap().AlignItems(ui.Center).Children(func() {
			label := "Hide"
			if !a.shown {
				label = "Show"
			}
			if ui.Button(c.Key("toggle"), label).Clicked() {
				a.shown = !a.shown
				if a.shown {
					a.note("Shown again: the platform kept the text while the view was hidden.")
				} else {
					a.note("Hidden: the view is not destroyed, so its text is still there.")
				}
			}
			if ui.Button(c.Key("destroy"), "Destroy").Clicked() {
				if a.native != nil {
					// Destroy runs on the main thread and releases exactly
					// what the factory made; the view cannot be shown again.
					a.native.Destroy()
					a.native = nil
					a.shown = true
					a.note("Destroyed: the native control is gone. Create makes a new one, with no text.")
				}
			}
			if ui.Button(c.Key("create"), "Create").Clicked() {
				if a.native == nil {
					a.make()
				}
			}
			// MyGo's own focus API, not the adapter's: Focus moves the
			// keyboard the way a press on the view would, with back telling
			// the view whether the keyboard came from behind it (Shift+Tab)
			// or from in front of it. It is only ever called from a press:
			// no frame focuses the view on its own, or the app would take
			// the keyboard back from the user every frame.
			forward := ui.Button(c.Key("focus"), "Focus view")
			backward := ui.Button(c.Key("focus-back"), "Focus view backward")
			if forward.Clicked() && a.native != nil {
				a.native.Focus(false)
			}
			if backward.Clicked() && a.native != nil {
				a.native.Focus(true)
			}
			ui.Text(c, "Tab moves between MyGo's own inputs and the native view.").FontSize(12).TextColor(t.TextMuted)

			// The panel is anchored to this button and drawn over the native
			// view, which is where MyGo paints: the view is a hole the
			// platform draws in, and what a frame builds after it shows over
			// it. What MyGo paints over the view takes the presses there too,
			// the way it does over the web's content: the platform's control
			// only gets the clicks on the part of itself the panel leaves
			// uncovered. The buttons in the panel are there to be clicked,
			// and the counter says that they were. Escape, a press outside
			// the panel, or this button again, closes it, and the press that
			// closed it goes on to what is under the pointer.
			popover := ui.Button(c.Key("popover"), "Popover over the view")
			if popover.Clicked() {
				a.popover = !a.popover
			}
			ui.PopoverBase(c, popover, &a.popover, func(panel ui.Element) {
				panel.AttachTo(hole, ui.AnchorTopLeft, ui.AnchorTopLeft).
					Width(240).Padding(12).Radius(10).
					Background(t.Background).Border(1, t.Border)
				panel.Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.18))
				ui.Column(c).Gap(8).Children(func() {
					ui.Text(c, "A MyGo popover").FontSize(13).Bold()
					ui.Text(c, "Where this panel covers the platform's control, MyGo takes the press: the control only hears the clicks on the part it still shows.").
						FontSize(12).TextColor(t.TextMuted)
					if ui.Button(c.Key("popover-count"), fmt.Sprintf("Clicked %d times", a.clicks)).Clicked() {
						a.clicks++
						a.note(fmt.Sprintf("The MyGo popover took press %d; the platform's control never heard it.", a.clicks))
					}
					if ui.Button(c.Key("popover-close"), "Close the popover").Clicked() {
						a.popover = false
						a.note("The panel closed itself: that press went to MyGo, not to the control it covers.")
					}
				})
			})
		})
	})
}
