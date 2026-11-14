// Desktopcontrol lists the windows of the desktop and shows a screenshot of
// the one chosen (plugins/desktopcontrol). Without Screen Recording it
// offers to guide the user to System Settings (plugins/permissionflow).
//
//	go run ./examples/desktopcontrol
//
// Run from a terminal, macOS gives the permission to the terminal: build
// the app (go run ./cmd/mygo build examples/desktopcontrol) to see it
// given to it.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/desktopcontrol"
	"github.com/egoist/mygo/plugins/permissionflow"
	"github.com/egoist/mygo/ui"
)

// model is the state the window shows, and how it reaches the system,
// which the test replaces.
type model struct {
	windows []desktopcontrol.Window
	err     error // of listing the windows

	chosen  desktopcontrol.Window
	shot    ui.ImageSource
	shotErr error
	busy    bool

	list    func() ([]desktopcontrol.Window, error)
	capture func(desktopcontrol.Window)
	guide   func()
}

func (m *model) refresh() { m.windows, m.err = m.list() }

func (m *model) view(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Children(func() {
		ui.Column(c).Padding(16, 16, 8).Gap(4).Children(func() {
			ui.Text(c, "Windows").FontSize(20).Bold()
			ui.Text(c, "Choose a window to see a screenshot of it.").TextColor(t.TextMuted)
		})
		switch {
		case errors.Is(m.err, desktopcontrol.ErrScreenRecording):
			ui.Column(c).Grow(1).Center().Gap(12).Children(func() {
				ui.Text(c, "Seeing other apps' windows takes Screen Recording.").TextColor(t.TextMuted)
				if ui.PrimaryButton(c.Key("guide"), "Guide to Screen Recording").Disabled(m.busy).Clicked() {
					m.busy = true
					m.guide()
				}
			})
			return
		case m.err != nil:
			ui.Column(c).Grow(1).Center().Children(func() {
				ui.Text(c, m.err.Error()).TextColor(t.Danger)
			})
			return
		}
		ui.Row(c).Grow(1).Padding(0, 16, 16).Gap(12).Children(func() {
			ui.Scroll(c).Width(260).Children(func() {
				ui.Column(c).Gap(4).Children(func() {
					for _, w := range m.windows {
						m.windowRow(c, w)
					}
				})
			})
			ui.Column(c).Grow(1).Center().Radius(t.Radius).Background(t.Surface).Border(1, t.Border).Children(func() {
				switch {
				case m.shotErr != nil:
					ui.Text(c, m.shotErr.Error()).TextColor(t.Danger)
				case m.shot != nil:
					w, h := ui.ImageSize(m.shot)
					if w > 480 {
						w, h = 480, h*480/w
					}
					ui.Image(c, m.shot).Width(w).Height(h)
				case m.busy:
					ui.Text(c, "Capturing…").TextColor(t.TextMuted)
				default:
					ui.Text(c, "No window chosen").TextColor(t.TextMuted)
				}
			})
		})
	})
}

// title names a window in the list: its title, else its app.
func title(w desktopcontrol.Window) string {
	if w.Title == "" {
		return w.App
	}
	return w.App + " — " + w.Title
}

func (m *model) windowRow(c *ui.Context, w desktopcontrol.Window) {
	key := c.Key(fmt.Sprintf("window-%d-%d", w.ID, w.PID))
	if ui.Button(key, title(w)).Label("Capture " + title(w)).Disabled(m.busy).Clicked() {
		m.chosen, m.shot, m.shotErr, m.busy = w, nil, nil, true
		m.capture(w)
	}
}

func main() {
	m := &model{list: desktopcontrol.Windows}
	var win *mygo.Window
	m.capture = func(w desktopcontrol.Window) {
		// ScreenCaptureKit may need the main thread to finish: capture off it.
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			data, err := desktopcontrol.CaptureWindow(ctx, w, desktopcontrol.CaptureOptions{MaxDimension: 960, Quality: 0.8})
			var shot ui.ImageSource
			if err == nil {
				shot, err = ui.DecodeImage(data)
			}
			win.Update(func() {
				if m.chosen == w {
					m.shot, m.shotErr = shot, err
				}
				m.busy = false
			})
		}()
	}
	m.guide = func() {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			_, _ = permissionflow.Guide(ctx, permissionflow.ScreenRecording, permissionflow.GuideOptions{})
			win.Update(func() {
				m.busy = false
				m.refresh()
			})
		}()
	}
	if !desktopcontrol.Available() {
		log.Fatal("desktopcontrol needs macOS 14 or later")
	}
	mygo.App.WhenReady(func() {
		m.refresh()
		win = mygo.NewWindow(mygo.WindowOptions{
			Title:   "Desktop Control",
			Width:   820,
			Height:  560,
			Content: ui.View(m.view),
		})
		// Windows open and close meanwhile.
		go func() {
			for range time.Tick(2 * time.Second) {
				win.Update(m.refresh)
			}
		}()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
