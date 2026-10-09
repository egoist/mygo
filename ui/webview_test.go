package ui

import (
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

// webviewTestApp is what the view of TestWebViewHostsAndDedups asks for.
type webviewTestApp struct {
	html     string
	reload   bool
	eval     string
	received string
	noBridge bool
	title    string
}

// TestWebViewHostsAndDedups builds a window with a web view in a known
// box, and checks that the host gets it at that box, that it asks the
// navigation once for the page it shows and again for another, that the
// one-shot reload and evaluation pass, and that the page's events reach
// the callbacks the frame set last.
func TestWebViewHostsAndDedups(t *testing.T) {
	app := &webviewTestApp{html: "one"}
	tt := coreNewTester(func(c *context) {
		e := coreWebView(c).Absolute().Left(10).Top(20).Size(120, 80).
			OnMessage(func(m string) { app.received = m }).
			OnTitleChanged(func(s string) { app.title = s })
		e.LoadHTML(app.html, "https://example.com/")
		if app.reload {
			e.Reload()
		}
		if app.eval != "" {
			e.Eval(app.eval)
		}
	}, 300, 200)

	want := func() *webviewWant {
		if len(tt.h.webviews) != 1 {
			t.Fatalf("the host got %d web views, want 1", len(tt.h.webviews))
		}
		return &tt.h.webviews[0]
	}

	w := want()
	if r := w.rect; r != (platform.RectF{X: 10, Y: 20, W: 120, H: 80}) {
		t.Errorf("the web view is at %+v, want at (10, 20, 120, 80)", r)
	}
	if l := w.load; l == nil || l.kind != webviewLoadHTML || l.html != "one" || l.baseURL != "https://example.com/" {
		t.Fatalf("the first frame did not ask for its page: %+v", l)
	}

	// Frames that ask for the page it shows again do not reload it.
	tt.Frame()
	tt.Frame()
	if w := want(); w.load != nil || w.reload || w.eval != "" {
		t.Errorf("the unchanged page was asked again: load=%+v reload=%v eval=%q", w.load, w.reload, w.eval)
	}

	// Another page is asked for.
	app.html = "two"
	tt.Frame()
	if l := want().load; l == nil || l.html != "two" {
		t.Fatalf("the changed page was not asked for: %+v", l)
	}

	// The one-shot reload and evaluation pass, and are consumed.
	app.reload, app.eval = true, "1 + 1"
	tt.Frame()
	w = want()
	if !w.reload || w.eval != "1 + 1" {
		t.Errorf("the one-shots did not pass: reload=%v eval=%q", w.reload, w.eval)
	}
	app.reload, app.eval = false, ""
	tt.Frame()
	if w := want(); w.reload || w.eval != "" {
		t.Errorf("the one-shots were not consumed: reload=%v eval=%q", w.reload, w.eval)
	}

	// The page's events reach the callbacks the frame set last.
	want().handler.Message("hi")
	want().handler.TitleChanged("a title")
	if app.received != "hi" || app.title != "a title" {
		t.Errorf("the page's events did not reach the callbacks: %q, %q", app.received, app.title)
	}
	// WillNavigate passes, where the callback does not deny.
	if !want().handler.WillNavigate("https://example.com/next") {
		t.Errorf("WillNavigate denied without a callback")
	}
}

// TestWebViewBridgeOptOut checks that the window's bridge loads in the
// web view's page by default, that Bridge(false) opts out, and that the
// bridge's posts, which carry the window's secret, do not reach
// OnMessage while the page's plain posts do.
func TestWebViewBridgeOptOut(t *testing.T) {
	app := &webviewTestApp{}
	tt := coreNewTester(func(c *context) {
		e := coreWebView(c).Absolute().Left(10).Top(20).Size(120, 80).
			OnMessage(func(m string) { app.received = m })
		e.LoadHTML(app.html, "https://example.com/")
		if app.noBridge {
			e.Bridge(false)
		}
	}, 300, 200)
	tt.h.script = "the window's bridge"
	tt.h.secret = "s1cr3t"
	tt.Frame()

	if len(tt.h.webviews) != 1 {
		t.Fatalf("the host got %d web views, want 1", len(tt.h.webviews))
	}
	if u := tt.h.webviews[0].opts.UserScripts; len(u) != 1 || u[0].Source != "the window's bridge" {
		t.Errorf("the bridge did not load by default: %+v", u)
	}

	// The bridge's posts do not reach OnMessage; the page's plain posts do.
	tt.h.webviews[0].handler.Message("s1cr3t" + `{"t":"dom-ready"}`)
	if app.received != "" {
		t.Errorf("the bridge's post reached OnMessage: %q", app.received)
	}
	tt.h.webviews[0].handler.Message("hello")
	if app.received != "hello" {
		t.Errorf("the page's post did not reach OnMessage: %q", app.received)
	}

	// Bridge(false) opts out.
	app.noBridge = true
	tt.Frame()
	if u := tt.h.webviews[0].opts.UserScripts; len(u) != 0 {
		t.Errorf("the bridge loaded after Bridge(false): %+v", u)
	}
}

// TestWebViewReloadAfterReturn checks that a web view of a Router's kept
// page, which the host destroyed while the page went out of sight, asks
// for its page again where the page comes back.
func TestWebViewReloadAfterReturn(t *testing.T) {
	router := NewRouter("/")
	router.Transition = TransitionNone
	tt := coreNewTester(func(c *context) {
		router.coreView(c, func(r *Route) {
			if r.Match("/") {
				coreWebView(c).Absolute().Left(10).Top(20).Size(120, 80).
					LoadURL("https://example.com/a")
			} else {
				coreText(c, "other")
			}
		})
	}, 300, 200)

	wantLoad := func() *webviewLoad {
		if len(tt.h.webviews) != 1 {
			t.Fatalf("the host got %d web views, want 1", len(tt.h.webviews))
		}
		return tt.h.webviews[0].load
	}
	if l := wantLoad(); l == nil || l.url != "https://example.com/a" {
		t.Fatalf("the first frame did not ask for its page: %+v", l)
	}
	id := tt.h.webviews[0].id

	// The page goes out of sight, its state, and the web view's, kept.
	router.Push("/other")
	tt.Frame()
	if len(tt.h.webviews) != 0 {
		t.Fatalf("the page going away kept its web view: %+v", tt.h.webviews)
	}

	// Coming back, the kept web view - the same one - asks for its page
	// again.
	router.Back()
	tt.Frame()
	if l := wantLoad(); l == nil || l.url != "https://example.com/a" {
		t.Fatalf("the returned web view did not ask for its page: %+v", l)
	}
	if tt.h.webviews[0].id != id {
		t.Fatal("the page coming back made another web view")
	}
}

// TestWebViewKeyAfterConstruction checks that a web view re-keyed after
// construction self-heals its spec: the host gets a web view with a
// handler, instead of the one bound before the key, which the host would
// find no state for.
func TestWebViewKeyAfterConstruction(t *testing.T) {
	tt := coreNewTester(func(c *context) {
		coreWebView(c).Key("page").LoadURL("https://example.com/a")
	}, 300, 200)
	if len(tt.h.webviews) != 1 {
		t.Fatalf("the host got %d web views, want 1", len(tt.h.webviews))
	}
	w := tt.h.webviews[0]
	if w.handler == nil {
		t.Fatal("the host got no handler for the web view")
	}
	if l := w.load; l == nil || l.url != "https://example.com/a" {
		t.Fatalf("the web view did not ask for its page: %+v", l)
	}

	// The self-healed spec is stable: another frame does not ask for the
	// page again.
	tt.Frame()
	if w := tt.h.webviews[0]; w.handler == nil || w.load != nil {
		t.Fatalf("the self-healed web view changed: no handler=%v load=%+v", w.handler == nil, w.load)
	}
}

// TestWebViewKeyNULSafe checks that a NUL byte in one part of a
// navigation cannot alias another: the parts are length-prefixed.
func TestWebViewKeyNULSafe(t *testing.T) {
	a := webviewLoad{kind: webviewLoadHTML, html: "x\x00y"}
	b := webviewLoad{kind: webviewLoadHTML, html: "x", baseURL: "y"}
	if webviewKeyOf(a) == webviewKeyOf(b) {
		t.Error("a NUL byte in one part aliased another navigation")
	}
	if webviewKeyOf(a) != webviewKeyOf(webviewLoad{kind: webviewLoadHTML, html: "x\x00y"}) {
		t.Error("the same navigation keyed differently")
	}
}
