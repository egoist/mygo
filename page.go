package mygo

import (
	"errors"
	"fmt"
)

// Page is the web page a window shows: what it loads, the scripts it runs,
// its developer tools and its events. A window showing native UI
// (WindowOptions.Content) has none.
//
//	win := mygo.NewWindow(mygo.WindowOptions{Title: "Editor", URL: "/"})
//	win.Page().OnDOMReady(func() { log.Println("ready") })
//
// Like the window's, its methods are safe from any goroutine.
type Page struct{ w *Window }

// Page returns the web page the window shows, or nil for a window that
// shows native UI.
func (w *Window) Page() *Page {
	if w.content != nil {
		return nil
	}
	return w.pg
}

// Window returns the window that shows the page: for the page of a web
// view, the window the web view is in.
func (p *Page) Window() *Window { return p.w.top() }

// WebView returns the web view whose page this is, nil for the page of a
// window.
func (p *Page) WebView() *WebView { return p.w.webView }

// PageOptions configure the web page of a window.
type PageOptions struct {
	// PreloadScript is JavaScript injected into every page before the
	// page's own scripts, after window.mygo is available.
	PreloadScript string
	// TrustedOrigins lists extra origins, such as "https://example.com",
	// whose pages may call bound Go methods. By default only the app's own
	// content can: custom schemes registered with Protocol, file: and
	// about: pages, and loopback dev servers during development. "*"
	// trusts every origin.
	TrustedOrigins []string
	// DevTools controls the web inspector, and for a window showing
	// Content, the inspector of its native UI.
	DevTools DevTools
	// ZoomFactor of the page (default 1).
	ZoomFactor float64
	// UserAgent overrides the user agent string.
	UserAgent string
	// SafariTabFocus leaves Tab to text fields and elements with a
	// tabindex, as Safari does while the system's keyboard navigation
	// (System Settings › Keyboard › Keyboard navigation) is off (macOS).
	// By default Tab reaches links, buttons, checkboxes and selects too,
	// as it does in Chrome, Electron and WebView2.
	SafariTabFocus bool
	// UserAgentSuffix appends to the page's user agent instead of
	// replacing it, as UserAgent does: the app's name and version, such as
	// "MyApp/1.2", for the sites that read them. It appends to UserAgent
	// too, which SetUserAgent keeps as well; leading and trailing spaces
	// are trimmed. Some sites serve a bare web view, whose user agent
	// names no browser, older pages: "Version/17.0 Safari/605.1.15" makes the
	// page look like Safari.
	UserAgentSuffix string
	// ContentRules block what the page loads and hide parts of it, as
	// Page.SetContentRules does, from its first load.
	ContentRules []*ContentRules
	// StorageID gives the page a data store of its own: the cookies, local
	// storage, IndexedDB and cache it keeps, which stay from one run of the
	// app to the next. Pages with the same StorageID, windows and web views
	// alike, share one store and pages with different ones share nothing,
	// as the accounts of one site signed in side by side need. A window
	// the page opens (SetWindowOpenHandler) shares its store. The ID is
	// fixed once the page exists.
	//
	// Without one a page uses the default store, which the pages without
	// a StorageID share: App.ClearBrowsingData and Cookies act on it only.
	// App.ClearStorage clears the store of one StorageID.
	//
	// An ID is 1 to 64 letters, digits, "-", "_" and "." (capitals and
	// lowercase letters differ). NewWebView returns an error matching
	// ErrInvalidStorageID for another, and NewWindow panics with it.
	//
	// Linux, and macOS before 14, cannot keep stores apart: creating a
	// page with a StorageID fails with an error matching
	// errors.ErrUnsupported (NewWindow panics on it, as it does when it
	// cannot make a window). On Windows the WebView2 Runtime must be
	// 1.0.1210.39 or newer, or the page gets no web view and its methods
	// return that error.
	StorageID string
}

// ErrInvalidStorageID is what NewWebView returns for a StorageID of
// PageOptions that is not 1 to 64 letters, digits, "-", "_" and ".", and
// what NewWindow panics with, wrapped with the ID: errors.Is finds it.
var ErrInvalidStorageID = errors.New("mygo: invalid StorageID")

// checkStorageID returns an error matching ErrInvalidStorageID for an id
// of no data store. The empty id is the default store's.
func checkStorageID(id string) error {
	if id == "" {
		return nil
	}
	valid := len(id) <= 64
	for i := 0; valid && i < len(id); i++ {
		c := id[i]
		valid = c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.'
	}
	if valid {
		return nil
	}
	if len(id) > 32 {
		id = id[:32] + "…"
	}
	return fmt.Errorf("%w %q: it is 1 to 64 letters, digits, \"-\", \"_\" and \".\"", ErrInvalidStorageID, id)
}
