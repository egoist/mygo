package updater

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
)

//go:embed page.html
var pageHTML string

// The size of the update window's page, for status views and for views
// with release notes.
const (
	statusWidth, statusHeight   = 480, 148
	releaseWidth, releaseHeight = 620, 440
)

func size(release bool) (width, height int) {
	if release {
		return releaseWidth, releaseHeight
	}
	return statusWidth, statusHeight
}

// openWindow creates the update window of s. It shows once its page is
// ready, in front when the user asked for the check.
func openWindow(s *session) {
	v, _ := s.current()
	width, height := size(v.Release)
	win := mygo.NewWindow(mygo.WindowOptions{
		Title:             "Software Update",
		Width:             width,
		Height:            height,
		UseContentSize:    true,
		Hidden:            true,
		DisableResize:     true,
		DisableMaximize:   true,
		DisableFullScreen: true,
		BackgroundColor:   "light-dark(#ececec, #1e1e1e)",
	})
	// A menu of its own, empty, rather than the app's menu bar (Linux,
	// Windows).
	win.SetMenu(mygo.NewMenu(nil))
	s.mu.Lock()
	s.win = win
	s.release = s.view.Release // the view may have changed since
	resize := s.release != v.Release
	user := s.user
	v = s.view
	s.mu.Unlock()
	if resize {
		fit(win, v.Release)
	}
	win.OnClosed(s.cancel)
	win.OnWillNavigate(func(e *mygo.NavigateEvent) {
		// Links of the release notes open in the browser.
		e.PreventDefault()
		if e.UserInitiated && safeURL(e.URL) {
			go mygo.Shell.OpenExternal(e.URL)
		}
	})
	win.OnReadyToShow(func() {
		if user {
			win.Show()
			win.Focus()
		} else {
			win.ShowInactive()
		}
	})
	win.LoadHTML(page(s.u.iconURL(), v), "")
}

// fit gives the window the size of a status or release view.
func fit(win *mygo.Window, release bool) {
	win.SetContentSize(size(release))
	win.Center()
}

// page returns the page of the update window, showing v until the page
// watches the session.
func page(icon string, v view) string {
	nonce := make([]byte, 16)
	rand.Read(nonce)
	initial, _ := json.Marshal(v, jsontext.EscapeForHTML(true), jsontext.EscapeForJS(true))
	return strings.NewReplacer(
		"{{nonce}}", base64.StdEncoding.EncodeToString(nonce),
		"{{icon}}", icon,
		"{{view}}", string(initial),
	).Replace(pageHTML)
}

// iconURL returns the icon of the update window as a data URL, or "".
func (u *updater) iconURL() string {
	png := u.opts.Icon
	if png == nil {
		dir, err := resourcesDir()
		if err != nil {
			return ""
		}
		if png, err = os.ReadFile(filepath.Join(dir, "icon.png")); err != nil {
			return ""
		}
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
}

// service is what the update window's page calls.
type service struct{ u *updater }

var errNotUpdateWindow = errors.New("only the update window may call the updater")

// session returns the session whose window made the call.
func (sv *service) session(ctx context.Context) (*session, error) {
	win := mygo.CallerWindow(ctx)
	sv.u.mu.Lock()
	s := sv.u.session
	sv.u.mu.Unlock()
	if s == nil || win == nil {
		return nil, errNotUpdateWindow
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.win != win {
		return nil, errNotUpdateWindow
	}
	return s, nil
}

// Watch streams the views of the session to the update window.
func (sv *service) Watch(ctx context.Context, views *mygo.Channel[view]) error {
	s, err := sv.session(ctx)
	if err != nil {
		return err
	}
	for {
		v, changed := s.current()
		if err := views.Send(v); err != nil {
			return nil // the page went away
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return nil
		}
	}
}

// Fit gives the window of a status view the height its page needs.
func (sv *service) Fit(ctx context.Context, height int) error {
	s, err := sv.session(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	win, release := s.win, s.release
	s.mu.Unlock()
	if !release {
		win.SetContentSize(statusWidth, min(max(height, 80), 600))
	}
	return nil
}

// Respond answers the prompt of a view with a button, and tells whether
// updates may be installed automatically from now on.
func (sv *service) Respond(ctx context.Context, prompt int, a action, automaticDownloads bool) error {
	s, err := sv.session(ctx)
	if err != nil {
		return err
	}
	s.respond(prompt, a, automaticDownloads)
	return nil
}
