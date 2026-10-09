package platform

// This file defines the seam for embedded web views: a Surface whose window
// can host a system web view in a sub-region of the native content, beside
// the area the framework draws into. Only the surface of a window created
// with WindowOptions.Surface can implement WebViewSurface; the ui.WebView
// element drives it. See docs/architecture.md, "Embedded web views".

// WebView is one embedded web view of a window's surface. The surface owns
// its native objects; the window sizes and loads it.
type WebView interface {
	// LoadURL navigates the web view to url, replacing its current page.
	LoadURL(url string)
	// LoadHTML shows html in the web view; baseURL resolves its relative
	// references.
	LoadHTML(html, baseURL string)
	// Reload re-fetches the web view's current page.
	Reload()
	// Eval runs javascript in the web view's current page.
	Eval(javascript string)
	// Move sizes and positions the web view at rect, in the surface's
	// coordinates.
	Move(rect RectF)
	// Destroy releases the web view's native objects.
	Destroy()
}

// WebViewOptions configures an embedded web view at creation.
type WebViewOptions struct {
	// UserScripts are evaluated in order before each page loads.
	UserScripts []UserScript
	// DevTools enables the inspector, where the engine allows it.
	DevTools bool
	// UserAgent overrides the engine's for this web view.
	UserAgent string
	// Schemes are custom URL schemes the web view may load, served by the
	// window's protocol handlers.
	Schemes []string
}

// WebViewHandler receives the events of one embedded web view. The surface
// delivers them on the window's main thread.
type WebViewHandler interface {
	// Message is called for a message the page posted to the web view's
	// script-message handler.
	Message(msg string)
	// WillNavigate is called before the web view leaves its current page;
	// returning false cancels the navigation.
	WillNavigate(url string) bool
	// Link is called for a link the page opens in a new context, such as
	// window.open or target=_blank; the handler decides where it goes.
	Link(url string)
	// LoadFinished reports a finished load; LoadFailed, a failed one.
	LoadFinished()
	LoadFailed(err error)
	// TitleChanged reports the page's current title.
	TitleChanged(title string)
}

// WebViewSurface is a Surface whose window can host embedded web views beside
// the area the framework draws into (ui.WebView).
type WebViewSurface interface {
	Surface
	// CreateWebView hosts an embedded web view at id, which is unique within
	// the window. The web view starts at zero size; Move sizes and positions
	// it and Destroy releases it.
	CreateWebView(id string, opts WebViewOptions, handler WebViewHandler) WebView
}
