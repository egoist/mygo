//go:build !js

package mygo

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo/internal/bridge"
	"github.com/egoist/mygo/internal/fake"
	"github.com/egoist/mygo/internal/platform"
)

// popupOf makes the window w opens for a request of its page, as the
// backend does, and returns the popup with its fake native window.
func popupOf(t *testing.T, w *Window, fw *fake.Window, opts WindowOptions) (*Window, *fake.Window) {
	t.Helper()
	w.Page().SetWindowOpenHandler(func(WindowOpenRequest) *WindowOptions { return &opts })
	onMain(func() { fw.H.NewWindow(platform.NewWindowRequest{URL: "about:blank", Native: 1}) })
	wins := fb.Windows()
	pw := wins[len(wins)-1]
	var popup *Window
	windows.Lock()
	for _, c := range windows.list {
		if c.native != nil && c.popup {
			popup = c
		}
	}
	windows.Unlock()
	if popup == nil {
		t.Fatal("the open handler made no popup")
	}
	t.Cleanup(popup.Destroy)
	return popup, pw
}

// TestAboutDocumentsOfTheApp: a document at about:blank has the origin of the
// page that made it, so it is trusted when the app made it, not for its
// scheme: the one LoadHTML loads, and the first of a window, but not one a
// page made by navigating its window to about:blank.
func TestAboutDocumentsOfTheApp(t *testing.T) {
	bindForTest(t, "About", &testService{})
	w, fw := testWindow(t, WindowOptions{})
	id := 100
	calls := func(what string, want bool) {
		t.Helper()
		id++
		if m := call(t, fw, id, "About.Hello", "x"); m["ok"] != want {
			t.Errorf("%s: a call ok = %v, want %v (%v)", what, m["ok"], want, m["e"])
		}
	}
	calls("the window's first document, a blank page", true)

	onMain(func() { fw.H.NavigationCommitted("about:blank") })
	calls("a page that navigated to about:blank", false)
	onMain(func() { fw.H.NavigationCommitted("about:blank#a") })
	calls("a blank page with a fragment", false)
	onMain(func() { fw.H.NavigationCommitted("about:srcdoc") })
	calls("about:srcdoc", false)

	w.Page().LoadHTML("<p>app</p>", "")
	onMain(func() { fw.H.NavigationCommitted("about:blank") })
	calls("the HTML the app loaded", true)

	onMain(func() { fw.H.NavigationCommitted("about:blank") })
	calls("a page that replaced the app's document with about:blank", false)

	// The HTML of an app that gives a base URL is at that URL, which is trusted by its own rule.
	w.Page().LoadHTML("<p>app</p>", "https://evil.example/")
	onMain(func() { fw.H.NavigationCommitted("https://evil.example/") })
	calls("HTML at an origin that is not trusted", false)
}

// TestAboutDocumentsOfPopups: a popup shows what the page that opened it
// makes, so no about: page of it is trusted, the one it opens with included,
// and the bridge in it knows no about: schemes. A popup that shows a page of
// a trusted origin is still trusted.
func TestAboutDocumentsOfPopups(t *testing.T) {
	bindForTest(t, "Popup", &testService{})
	w, fw := testWindow(t, WindowOptions{})
	popup, pfw := popupOf(t, w, fw, WindowOptions{Page: PageOptions{TrustedOrigins: []string{"https://partner.example"}}})

	var cfg struct {
		Trust bridge.Trust `json:"trust"`
	}
	src := pfw.UserScripts()[0].Source
	if err := json.Unmarshal([]byte(strings.TrimSuffix(src[strings.LastIndex(src, "})(")+3:], ");")), &cfg); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(cfg.Trust.Schemes, "about") || !slices.Contains(cfg.Trust.Schemes, "file") {
		t.Errorf("the schemes a popup's bridge trusts: %v", cfg.Trust.Schemes)
	}
	if bridgeAllows(cfg.Trust, "about:blank") || !bridgeAllows(cfg.Trust, "https://partner.example/") {
		t.Errorf("the pages a popup's bridge trusts: %+v", cfg.Trust)
	}

	id := 200
	calls := func(url string, want bool) {
		t.Helper()
		id++
		onMain(func() { pfw.H.NavigationCommitted(url) })
		sender := url
		s := secret(pfw)
		onMain(func() {
			pfw.H.Message(s+`{"t":"call","id":`+strconv.Itoa(id)+`,"k":"tok","m":"Popup.Hello","a":["x"]}`, sender)
		})
		m := received(t, pfw, func(m map[string]any) bool { return m["t"] == "reply" && m["id"] == float64(id) })
		if m["ok"] != want {
			t.Errorf("popup at %s: a call ok = %v, want %v (%v)", url, m["ok"], want, m["e"])
		}
	}
	calls("about:blank", false) // the first document of the popup, which the opener can write to
	calls("about:blank", false)
	calls("https://evil.example/", false)
	calls("https://partner.example/", true)
	calls("about:blank", false)
	if popup.trusted {
		t.Error("the popup at about:blank is trusted")
	}
}

// TestBridgeTrustOfPopups: the rule the bridge applies and isTrusted agree for
// the windows pages open.
func TestBridgeTrustOfPopups(t *testing.T) {
	w := &Window{popup: true}
	tr := w.bridgeTrust()
	for _, u := range []string{"about:blank", "about:srcdoc", "file:///x", "https://evil.example/"} {
		if got, want := bridgeAllows(tr, u), w.isTrusted(u); got != want {
			t.Errorf("%s: the bridge allows: %v, isTrusted: %v", u, got, want)
		}
	}
	if w.isTrusted("about:blank") {
		t.Error("about:blank is trusted in a popup")
	}
}

// TestDeliveryNamesTheDocument: what the page is sent goes to the document
// whose dom-ready the core took, which any other document ignores.
func TestDeliveryNamesTheDocument(t *testing.T) {
	ev := newEventForTest[string](t, "gate:doc")
	bindForTest(t, "Doc", &testService{})
	w, fw := testWindow(t, WindowOptions{})
	docs := func() []string {
		var out []string
		for _, s := range fw.Scripts() {
			if d, msgs, ok := delivery(t, s); ok {
				for _, m := range msgs {
					if m["n"] == "gate:doc" {
						out = append(out, d+":"+m["p"].(string))
					}
				}
			}
		}
		return out
	}
	// A reply before the page's dom-ready names no document: it names its own.
	if m := call(t, fw, 1, "Doc.Hello", "x"); m["k"] != "tok" {
		t.Errorf("reply: %v", m)
	}
	for _, s := range fw.Scripts() {
		if d, _, ok := delivery(t, s); ok && d != "" {
			t.Errorf("a delivery before dom-ready names the document %q", d)
		}
	}

	page(fw, `{"t":"dom-ready","k":"doc-1"}`)
	_ = ev.Emit(w, "one")
	received(t, fw, func(m map[string]any) bool { return m["p"] == "one" })
	if got := docs(); !slices.Equal(got, []string{"doc-1:one"}) {
		t.Errorf("events of the first document: %q", got)
	}

	// A navigation: the next document is another, and what was held for it
	// goes to it once it is ready.
	onMain(func() { fw.H.NavigationCommitted("mygo://localhost/next") })
	_ = ev.Emit(w, "two")
	page(fw, `{"t":"dom-ready","k":"doc-2"}`)
	received(t, fw, func(m map[string]any) bool { return m["p"] == "two" })
	if got := docs(); !slices.Equal(got, []string{"doc-1:one", "doc-2:two"}) {
		t.Errorf("events of both documents: %q", got)
	}
}

// TestNavigationDropsTheOutbox: what was queued for a page that is ready is
// not evaluated in the page the window shows after it: an event emitted
// as the navigation committed, which flush has not sent yet.
func TestNavigationDropsTheOutbox(t *testing.T) {
	ev := newEventForTest[string](t, "gate:outbox")
	w, fw := testWindow(t, WindowOptions{})
	page(fw, `{"t":"dom-ready","k":"doc-1"}`)
	fw.ClearScripts()
	onMain(func() {
		_ = ev.Emit(w, "FOR-THE-PAGE-THAT-WENT")
		fw.H.NavigationCommitted("https://evil.example/")
	})
	onMain(func() {})
	for _, s := range fw.Scripts() {
		if strings.Contains(s, "FOR-THE-PAGE-THAT-WENT") {
			t.Errorf("an event of the page that went was evaluated after the navigation: %s", s)
		}
	}
	// What waits for the next page's dom-ready is for that page.
	onMain(func() { fw.H.NavigationCommitted("mygo://localhost/") })
	_ = ev.Emit(w, "FOR-THE-NEXT-PAGE")
	page(fw, `{"t":"dom-ready","k":"doc-2"}`)
	received(t, fw, func(m map[string]any) bool { return m["p"] == "FOR-THE-NEXT-PAGE" })
}

type gateSlow struct{ ran atomic.Int32 }

func (s *gateSlow) Secret(context.Context) string { s.ran.Add(1); return "RESULT-OF-A-CALL" }

// TestReplyOfAGonePageIsNotQueued: a method that returns after its page went
// has nobody to reply to: the page that came after it is another, and
// may not be trusted.
func TestReplyOfAGonePageIsNotQueued(t *testing.T) {
	svc := &gateSlow{}
	bindForTest(t, "GateSlow", svc)
	w, fw := testWindow(t, WindowOptions{})
	raw := `{"t":"call","id":1,"k":"tok","m":"GateSlow.Secret","a":[]}`

	// The control: a call of the page showing is answered.
	handleCall(w, w.pageContext(), raw, true)
	m := received(t, fw, func(m map[string]any) bool { return m["t"] == "reply" && m["id"] == float64(1) })
	if m["v"] != "RESULT-OF-A-CALL" {
		t.Fatalf("the reply of the page's own call: %v", m)
	}
	fw.ClearScripts()

	// The call runs as the window navigates; its context is the page's.
	ctx, cancel := context.WithCancel(w.pageContext())
	cancel() // NavigationCommitted cancels it
	onMain(func() { fw.H.NavigationCommitted("https://evil.example/") })
	handleCall(w, ctx, raw, true)
	onMain(func() {})
	if svc.ran.Load() != 2 {
		t.Fatalf("the method ran %d times, want 2", svc.ran.Load())
	}
	for _, s := range fw.Scripts() {
		if strings.Contains(s, "RESULT-OF-A-CALL") {
			t.Errorf("the reply of a page that went was evaluated: %s", s)
		}
	}
}

// TestProtocolChangeIsJudgedAgain: the decision for the last sender is kept
// only while the schemes the app serves are the same.
func TestProtocolChangeIsJudgedAgain(t *testing.T) {
	w, fw := testWindow(t, WindowOptions{})
	effects := watchEffects(w)
	if err := Protocol.Handle("gatescheme", http.NotFoundHandler()); err != nil {
		t.Fatal(err)
	}
	const sender = "gatescheme://localhost/"
	onMain(func() { fw.H.NavigationCommitted(sender) })
	pageAt(fw, sender, `{"t":"dom-ready"}`)
	onMain(func() {})
	if effects.domReady.Load() != 1 {
		t.Fatalf("a page of a scheme the app serves: OnDOMReady ran %d times", effects.domReady.Load())
	}
	Protocol.Unhandle("gatescheme")
	pageAt(fw, sender, `{"t":"dom-ready"}`)
	onMain(func() {})
	if effects.domReady.Load() != 1 {
		t.Errorf("a sender of a scheme the app stopped serving was still trusted: OnDOMReady ran %d times", effects.domReady.Load())
	}
}

// TestEmptySenderIsNotTrusted: a message without a sender URL is nobody's,
// whatever came before it, also where TrustedOrigins trusts every page.
func TestEmptySenderIsNotTrusted(t *testing.T) {
	w, fw := testWindow(t, WindowOptions{Page: PageOptions{TrustedOrigins: []string{"*"}}})
	effects := watchEffects(w)
	onMain(func() { fw.H.NavigationCommitted("https://anything.example/") })
	for _, from := range []string{"", "https://anything.example/", ""} {
		pageAt(fw, from, `{"t":"dom-ready"}`)
	}
	onMain(func() {})
	if n := effects.domReady.Load(); n != 1 {
		t.Errorf("OnDOMReady ran %d times for three messages, one with a URL", n)
	}
}

// TestSupersededLoadHTMLDoesNotMakeAboutTrusted: HTML the app loaded, and
// that another page replaced before it committed, is no document the app
// made: an about:blank the page makes after that is not the app's.
func TestSupersededLoadHTMLDoesNotMakeAboutTrusted(t *testing.T) {
	bindForTest(t, "Superseded", &testService{})
	w, fw := testWindow(t, WindowOptions{})
	w.Page().LoadHTML("<p>app</p>", "")
	onMain(func() { fw.H.NavigationCommitted("https://evil.example/") })
	onMain(func() { fw.H.NavigationCommitted("about:blank") })
	if m := call(t, fw, 1, "Superseded.Hello", "x"); m["ok"] != false {
		t.Errorf("a call from an about:blank a page made after it replaced the app's HTML: ok = %v", m["ok"])
	}
	// The HTML the app loads next is its own.
	w.Page().LoadHTML("<p>app</p>", "")
	onMain(func() { fw.H.NavigationCommitted("about:blank") })
	if m := call(t, fw, 2, "Superseded.Hello", "x"); m["ok"] != true {
		t.Errorf("a call from the app's HTML: ok = %v (%v)", m["ok"], m["e"])
	}
}
