//go:build windows && (amd64 || arm64)

package windows

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unsafe"
)

// ClearStorage clears the WebView2 profile of a storage ID
// (webview2_profile.go) as ClearBrowsingData clears the default one, through
// the web view of a page that has the ID. With none open it uses a hidden
// web view made in that profile for the call and closed after it, unless the
// profile has no directory in the user data folder yet: no page ever stored
// anything there, and making the web view would make the profile.
func (a appController) ClearStorage(id string, done func(error)) {
	b := a.b
	c := &storageClear{b: b, done: done}
	if b.storageClears == nil {
		b.storageClears = map[*storageClear]struct{}{}
	}
	b.storageClears[c] = struct{}{}
	if w := b.storageWindow(id); w != nil {
		c.window = w
		clearProfile(w.webview, c.finish)
		return
	}
	if !profileStored(b.userDataFolder(), id) {
		c.finish(nil)
		return
	}
	c.clearWithHiddenView(id)
}

// A storageClear is one ClearStorage call. Closing a controller does not
// guarantee that WebView2 invokes outstanding completion handlers, so
// closing the window it uses finishes it (failStorageClears).
type storageClear struct {
	b        *Backend
	window   *window // whose web view it uses, or nil for a hidden one
	finished bool
	done     func(error)
}

func (c *storageClear) finish(err error) {
	if c.finished {
		return
	}
	c.finished = true
	delete(c.b.storageClears, c)
	c.done(err)
}

func failStorageClears(w *window, err error) {
	for c := range w.b.storageClears {
		if c.window == w {
			c.finish(err)
		}
	}
}

// storageWindow returns a window of storage ID id whose web view exists,
// among the windows and the web views in windows of native UI, or nil: the
// pages of an ID share its profile, so any one clears it.
func (b *Backend) storageWindow(id string) *window {
	usable := func(w *window) bool { return !w.closed && w.webview != 0 && w.opts.StorageID == id }
	for _, w := range b.windows {
		if usable(w) {
			return w
		}
		for _, v := range w.webViews {
			if usable(v) {
				return v
			}
		}
	}
	return nil
}

// profileStored reports whether the user data folder has a directory for
// the profile of storage ID id: WebView2 makes it in EBWebView and names it
// WV2Profile_ and the profile's name, so a directory whose name ends with
// that name is it. When the folder cannot be read, it says yes: a profile
// cleared for nothing costs more than one that is not.
func profileStored(userData, id string) bool {
	entries, err := os.ReadDir(filepath.Join(userData, "EBWebView"))
	if err != nil {
		return !errors.Is(err, fs.ErrNotExist)
	}
	for _, e := range entries {
		if e.IsDir() && isProfileDir(e.Name(), id) {
			return true
		}
	}
	return false
}

// isProfileDir reports whether a directory of EBWebView is the profile of
// storage ID id's. Windows compares directory names without regard to
// case, so the match does too.
func isProfileDir(name, id string) bool {
	return strings.HasSuffix(strings.ToLower(name), profileName(id))
}

// clearWithHiddenView clears the profile of id through a web view of the
// app's own window that nobody sees, and closes it. It closes the view from
// the message loop, not from its completion handler, which crashes it (as
// closeCookieView does).
func (c *storageClear) clearWithHiddenView(id string) {
	b := c.b
	if err := b.startEnvironment(); err != nil {
		c.finish(err)
		return
	}
	b.whenEnvironment(func() {
		if b.envErr != nil {
			c.finish(b.envErr)
			return
		}
		env10, options, err := b.profileOptions(id)
		if err != nil {
			c.finish(err)
			return
		}
		defer release(options)
		defer release(env10)
		hr := withHandler(func(hr, controller uintptr) {
			if failed(hr) || controller == 0 {
				c.finish(hresultError("clearing the data of a StorageID: creating a WebView2 controller", hr))
				return
			}
			addRef(controller)
			comCall(controller, ctlPutIsVisible, 0)
			var webview uintptr
			if hr := comCall(controller, ctlGetCoreWebView2, uintptr(unsafe.Pointer(&webview))); failed(hr) || webview == 0 {
				b.post(func() { closeHiddenView(controller, 0) })
				c.finish(hresultError("clearing the data of a StorageID: get_CoreWebView2", hr))
				return
			}
			clearProfile(webview, func(err error) {
				b.post(func() { closeHiddenView(controller, webview) })
				c.finish(err)
			})
		}, func(h uintptr) uintptr {
			return comCall(env10, env10CreateControllerWithOptions, b.appHwnd, options, h)
		})
		if failed(hr) {
			c.finish(hresultError("clearing the data of a StorageID: creating a WebView2 controller", hr))
		}
	})
}

// closeHiddenView closes the controller of a hidden web view, and releases
// it and its webview, if it has one.
func closeHiddenView(controller, webview uintptr) {
	comCall(controller, ctlClose)
	release(webview)
	release(controller)
}
