package ui

import (
	"cmp"
	"hash/maphash"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/internal/platform"
)

// WebView creates an element that shows a page in the system's web view,
// embedded in the window beside the area MyGo draws into. It is sized as a
// leaf (Width and Height, or Grow in its parent), takes no input itself —
// the page takes it, over the area it shows — and shows its background
// until the page loads.
//
// The view asks each web view what to show every frame: LoadHTML and
// LoadURL navigate it, when they ask for another page than the one it
// shows; Reload re-fetches the page it shows; and Eval runs JavaScript in
// it. Each navigation, reload and evaluation takes effect once, in the
// frame that asks it.
//
// OnMessage, OnLink, WillNavigate, OnLoadFinished, OnLoadFailed and
// OnTitleChanged watch the page: the callback the frame set last is the
// one called, on the main thread, when the event comes.
//
// The window's bridge loads in the page (window.mygo), as in a window
// showing a page, with the window's identity; Bridge(false) opts out,
// for content that must not have it. The page cannot call the window's
// bound services through the bridge: its posts, which carry the
// window's secret, stop at the web view, and OnMessage takes only the
// plain posts the page makes to the web view's script-message handler.
//
// Where the window's surface hosts no web views, the element shows its
// background in place of the page.
//
//	app.showPage = true
//	ui.WebView(c).Size(320, 240).
//		LoadHTML(app.page, base).
//		OnMessage(app.onPageMessage)
func coreWebView(c *context) *node {
	e := c.newElement(kindWebView)
	e.flags |= flagPassThrough
	e.bg = c.theme.Background
	webviewSpecOf(e)
	return e
}

// webviewKey finds the spec in the element's locals.
type webviewKey struct{}

// webviewSpec is what the frame asked of one web view element, and what
// the element last did: the navigation it asks for (load), the one-shot
// reload and evaluation, whether it loads the window's bridge, and the
// callbacks that watch the page.
type webviewSpec struct {
	id      string
	handler *webviewHandler

	load       webviewLoad // what LoadHTML or LoadURL asked for last
	loadKey    string      // its key, "" for no navigation asked
	appliedKey string      // the key the host loaded last

	bridge bool // the window's bridge in the page; true until Bridge(false)
	opts   platform.WebViewOptions
	reload bool
	eval   string

	onMessage      func(string)
	onLink         func(string)
	willNavigate   func(string) bool
	onLoadFinished func()
	onLoadFailed   func(error)
	onTitleChanged func(string)
}

// webviewLoad is a navigation of a web view, by kind.
type webviewLoad struct {
	kind    uint8 // webviewLoadHTML or webviewLoadURL
	html    string
	baseURL string
	url     string
}

const (
	webviewLoadHTML uint8 = iota + 1
	webviewLoadURL
)

var webviewKeySeed = maphash.MakeSeed()

// webviewSpecOf returns the spec of the web view element e. The id and
// handler bind lazily here, so a rekey that resets the element's state
// (Key called after construction) self-heals on the next lookup instead
// of leaving a nil handler for the host to crash on; a spec that comes
// back from a rekey loads the bridge again, as a fresh one does.
func webviewSpecOf(e *node) *webviewSpec {
	s := coreLocal[webviewSpec](e, webviewKey{}, func() webviewSpec { return webviewSpec{bridge: true} })
	if s.id == "" {
		s.id = strconv.FormatUint(e.id, 16)
		s.handler = &webviewHandler{id: e.id, rt: e.c.rt}
	}
	return s
}

// webviewKeyOf keys a navigation by what it asks for: the frames that ask
// for the same page again do not reload it. Each part is length-prefixed,
// so a NUL byte in one cannot alias another.
func webviewKeyOf(l webviewLoad) string {
	var h maphash.Hash
	h.SetSeed(webviewKeySeed)
	var n [20]byte
	part := func(s string) {
		h.Write(strconv.AppendInt(n[:0], int64(len(s)), 10))
		h.WriteString(s)
	}
	h.WriteByte(l.kind)
	part(l.html)
	part(l.baseURL)
	part(l.url)
	return strconv.FormatUint(h.Sum64(), 16)
}

// LoadHTML shows html in the web view, resolving its relative references
// against baseURL. It navigates the web view when html or baseURL change
// from what it shows, and a frame that asks for the page it shows again
// does not reload it.
func (e *node) LoadHTML(html, baseURL string) *node {
	s := webviewSpecOf(e)
	s.load = webviewLoad{kind: webviewLoadHTML, html: html, baseURL: baseURL}
	s.loadKey = webviewKeyOf(s.load)
	return e
}

// LoadURL navigates the web view to url, replacing the page it shows, as
// LoadHTML does when url changes.
func (e *node) LoadURL(url string) *node {
	s := webviewSpecOf(e)
	s.load = webviewLoad{kind: webviewLoadURL, url: url}
	s.loadKey = webviewKeyOf(s.load)
	return e
}

// Reload re-fetches the page the web view shows, as the user would from
// the browser: it takes effect once, in the frame that asks it.
func (e *node) Reload() *node {
	webviewSpecOf(e).reload = true
	return e
}

// Eval runs js in the page the web view shows: it takes effect once, in
// the frame that asks it.
func (e *node) Eval(js string) *node {
	s := webviewSpecOf(e)
	s.eval = js
	return e
}

// Bridge loads the window's bridge (window.mygo) in the page, as in a
// window showing a page, where on is true, which it is by default;
// Bridge(false) opts out, for content that must not have the window's
// identity.
func (e *node) Bridge(on bool) *node {
	webviewSpecOf(e).bridge = on
	return e
}

// OnMessage calls f for a message the page posts to the web view's
// script-message handler, as
// window.webkit.messageHandlers.mygo.postMessage does from the page. The
// posts of the window's bridge, where it loads, do not reach f.
func (e *node) OnMessage(f func(msg string)) *node {
	webviewSpecOf(e).onMessage = f
	return e
}

// OnLink calls f for a link the page asks to open in a new context, as
// window.open or target=_blank: the web view does not open it, and f
// decides where it goes, as Window.OpenURL does for a page.
func (e *node) OnLink(f func(url string)) *node {
	webviewSpecOf(e).onLink = f
	return e
}

// WillNavigate calls f before the web view leaves the page it shows, as
// Window.WillNavigate does for a page: f returning false cancels the
// navigation.
func (e *node) WillNavigate(f func(url string) bool) *node {
	webviewSpecOf(e).willNavigate = f
	return e
}

// OnLoadFinished calls f for a page the web view finished loading;
// OnLoadFailed, for one it could not load.
func (e *node) OnLoadFinished(f func()) *node {
	webviewSpecOf(e).onLoadFinished = f
	return e
}

// OnLoadFailed calls f with what failed to load the page the web view
// asked for.
func (e *node) OnLoadFailed(f func(err error)) *node {
	webviewSpecOf(e).onLoadFailed = f
	return e
}

// OnTitleChanged calls f for the title the page the web view shows sets.
func (e *node) OnTitleChanged(f func(title string)) *node {
	webviewSpecOf(e).onTitleChanged = f
	return e
}

// webviewHandler is the platform handler of one web view element: it gives
// the page's events to the callbacks the element's frame set last, read
// at the call, and does nothing where they are nil.
type webviewHandler struct {
	id uint64
	rt *engine
}

func (h *webviewHandler) spec() *webviewSpec {
	st := h.rt.states[h.id]
	if st == nil || st.locals == nil {
		return nil
	}
	spec, _ := st.locals[webviewKey{}].(*webviewSpec)
	return spec
}

// Message gives the page's posts to OnMessage. The window's bridge, where
// the web view loads it, posts its own messages prefixed with the
// window's secret, as the window's page does: the app takes only the
// rest, the plain posts the page makes to the web view's script-message
// handler.
func (h *webviewHandler) Message(msg string) {
	if secret := h.rt.host.webviewSecret(); secret != "" {
		if _, ok := strings.CutPrefix(msg, secret); ok {
			return // the window's bridge, not the page
		}
	}
	if s := h.spec(); s != nil && s.onMessage != nil {
		s.onMessage(msg)
	}
}

// WillNavigate reports true where the callback does not cancel the
// navigation.
func (h *webviewHandler) WillNavigate(url string) bool {
	if s := h.spec(); s != nil && s.willNavigate != nil {
		return s.willNavigate(url)
	}
	return true
}

func (h *webviewHandler) Link(url string) {
	if s := h.spec(); s != nil && s.onLink != nil {
		s.onLink(url)
	}
}

func (h *webviewHandler) LoadFinished() {
	if s := h.spec(); s != nil && s.onLoadFinished != nil {
		s.onLoadFinished()
	}
}

func (h *webviewHandler) LoadFailed(err error) {
	if s := h.spec(); s != nil && s.onLoadFailed != nil {
		s.onLoadFailed(err)
	}
}

func (h *webviewHandler) TitleChanged(title string) {
	if s := h.spec(); s != nil && s.onTitleChanged != nil {
		s.onTitleChanged(title)
	}
}

// webviewWant is what the frame asks of one of the window's web views:
// where to show it, and the navigation, reload or evaluation it asked for
// since the last frame.
type webviewWant struct {
	id      string
	rect    platform.RectF
	opts    platform.WebViewOptions
	load    *webviewLoad
	reload  bool
	eval    string
	handler *webviewHandler
}

// syncWebViews gives the host the web views of the frame it just built,
// in the order of their ids: each with where to show it, and the
// navigation, reload or evaluation it asked for since the last frame.
func (rt *engine) syncWebViews() {
	wants := rt.webviewWants[:0]
	rt.webviewNodes(func(e *node) {
		s := webviewSpecOf(e)
		if !rt.webviewSeen[s.id] {
			// The host destroyed this web view, as one going out of a
			// Router's kept page does: load its page again.
			s.appliedKey = ""
		}
		w := webviewWant{
			id:      s.id,
			rect:    platform.RectF{X: float64(e.x), Y: float64(e.y), W: float64(e.w), H: float64(e.h)},
			opts:    s.opts,
			handler: s.handler,
		}
		if s.bridge {
			// The window's bridge, before the frame's scripts: the
			// page's window.mygo is the window's own.
			if script := rt.host.webviewScript(); script != "" {
				w.opts.UserScripts = append([]platform.UserScript{{Source: script}}, w.opts.UserScripts...)
			}
		}
		if s.loadKey != s.appliedKey {
			w.load = &s.load
			s.appliedKey = s.loadKey
		}
		if s.reload {
			w.reload = true
			s.reload = false
		}
		if s.eval != "" {
			w.eval = s.eval
			s.eval = ""
		}
		wants = append(wants, w)
	})
	slices.SortFunc(wants, func(a, b webviewWant) int { return cmp.Compare(a.id, b.id) })
	rt.webviewWants = wants
	if rt.webviewSeen == nil {
		rt.webviewSeen = map[string]bool{}
	} else {
		clear(rt.webviewSeen)
	}
	for _, w := range wants {
		rt.webviewSeen[w.id] = true
	}
	rt.host.syncWebViews(wants)
}

// webviewNodes visits the web view elements of the frame's tree.
func (rt *engine) webviewNodes(fn func(e *node)) {
	if rt.c.root == nil {
		return
	}
	var walk func(e *node)
	walk = func(e *node) {
		for c := e; c != nil; c = c.next {
			if c.kind == kindWebView {
				fn(c)
			}
			if c.first != nil {
				walk(c.first)
			}
		}
	}
	walk(rt.c.root.first)
}
