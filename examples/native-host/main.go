// Native-host puts an actual NSTextField, GtkEntry or Win32 EDIT beside
// MyGo widgets, with native input, clipped layout and safe lifetime hooks.
//
//	CGO_ENABLED=0 go run ./examples/native-host
package main

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/examples/native-host/control"
	"github.com/egoist/mygo/ui"
)

type app struct {
	win                    *mygo.Window
	field                  *mygo.NativeView
	text                   string
	show, disabled, dialog bool
}

func (a *app) view(c *ui.Context) {
	ui.Column(c).Fill().Padding(24).Gap(16).Children(func() {
		ui.Text(c, "A platform control inside a MyGo layout").FontSize(20).Bold()
		ui.Text(c, "Tab between the MyGo buttons and the native text field. Resize the window, hide the field, or open a dialog.")
		ui.Button(c, "Before the native field")
		ui.Box(c).Height(48).Clip().Children(func() {
			if a.show {
				ui.HostView(c, a.field).Key("field").Height(36).WidthPercent(100).Label("Platform text field").Disabled(a.disabled)
			}
		})
		ui.Textf(c, "Native text: %s", a.text)
		ui.Row(c).Gap(8).Children(func() {
			if ui.Button(c, "Set from Go").Clicked() {
				_ = a.field.Update(func(ctx mygo.NativeViewContext) {
					control.SetText(ctx, "Updated safely from Go")
					a.text = control.Text(ctx)
				})
			}
			if ui.Button(c, "Hide / show").Clicked() {
				a.show = !a.show
			}
			if ui.Button(c, "Enable / disable").Clicked() {
				a.disabled = !a.disabled
			}
			if ui.Button(c, "Open dialog").Clicked() {
				a.dialog = true
			}
		})
	})
	ui.Modal(c, &a.dialog, func() {
		ui.Text(c, "MyGo dialog").Bold()
		ui.Text(c, "The native field hides while this modal layer is open, then returns with its text intact.")
		if ui.Button(c, "Close").Clicked() {
			a.dialog = false
		}
	})
}

func main() {
	a := &app{show: true, text: "Edit with the platform's native text services"}
	mygo.App.WhenReady(func() {
		a.win = mygo.NewWindow(mygo.WindowOptions{Title: "Native-view hosting", Width: 700, Height: 410, Content: ui.View(a.view)})
		var err error
		a.field, err = a.win.NewNativeView(control.Options(a.text, func(text string) { a.text = text; a.win.Invalidate() }))
		if err != nil {
			log.Fatal(err)
		}
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
