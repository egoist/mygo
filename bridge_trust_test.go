//go:build !js

package mygo

import (
	"encoding/json/v2"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo/internal/bridge"
	"github.com/egoist/mygo/internal/schemehost"
)

// dnsName is how trust.ts tells the hosts WebView2 serves a scheme at.
var dnsName = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$`)

// bridgeHosted is what the bridge in the page does with Trust.Hosted for
// the host of a page at http (isHosted of trust.ts).
func bridgeHosted(tr bridge.Trust, hostname string) bool {
	for _, s := range tr.Hosted {
		web := s + ".localhost"
		if hostname == web {
			return true
		}
		if host, ok := strings.CutSuffix(hostname, "."+web); ok && len(host) <= 253 && dnsName.MatchString(host) {
			return true
		}
	}
	return false
}

// bridgeAllows is what the bridge in the page does with its Trust
// (packages/bridge/src/trust.ts), for a page at rawURL.
func bridgeAllows(tr bridge.Trust, rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	scheme, host := strings.ToLower(u.Scheme), strings.ToLower(u.Host)
	if slices.Contains(tr.Schemes, scheme) || slices.Contains(tr.Origins, "*") || slices.Contains(tr.Origins, scheme+"://"+host) {
		return true
	}
	if scheme == "http" && u.Port() == "" && u.User == nil && bridgeHosted(tr, strings.ToLower(u.Hostname())) {
		return true
	}
	return tr.Loopback && (scheme == "http" || scheme == "https") && slices.Contains([]string{"localhost", "127.0.0.1", "::1"}, strings.ToLower(u.Hostname()))
}

// TestBridgeTrust: the pages that get the bridge are those isTrusted
// trusts. The cases are those of packages/bridge/src/trust.test.ts.
func TestBridgeTrust(t *testing.T) {
	if err := Protocol.Handle("trusted", http.NotFoundHandler()); err != nil {
		t.Fatal(err)
	}
	defer Protocol.Unhandle("trusted")
	t.Setenv("MYGO_DEV_URL", "http://localhost:5173")
	urls := []string{
		"https://evil.example/",
		"https://partner.example/page",
		"HTTPS://Partner.Example/page",
		"https://partner.example.evil.com/",
		"http://partner.example/",
		"trusted://localhost/",
		"trusted://abc/",
		"trusted://a.b/page?q=1",
		"mygo://localhost/index.html",
		"mygo://abc/index.html",
		"other://abc/",
		"file:///tmp/index.html",
		"about:blank",
		"data:text/html,x",
		"http://localhost:5173/",
		"http://localhost:5174/", // loopback in development
		"https://127.0.0.1:8443/",
		"http://[::1]:3000/",
		"http://192.168.1.2:5173/",
		"http://abc.trusted.localhost/", // how WebView2 serves trusted://abc/; elsewhere a server on the loopback interface
		"http://ABC.Trusted.localhost/",
		"http://abc.other.localhost/",
		"http://evilmygo.localhost/",
		"http://abc.trusted.localhost.evil.example/",
		"http://trusted.localhost:8080/",
		"http://abc.trusted.localhost:8080/",
		"http://user@abc.trusted.localhost/",
		"http://user:pw@trusted.localhost/",
		"https://user@partner.example/",
		"https://abc.trusted.localhost/",
		"http://a_b.trusted.localhost/",
		"http://-a.trusted.localhost/",
		"http://a-.trusted.localhost/",
		"http://" + strings.Repeat("a", 64) + ".trusted.localhost/",
		"http://" + strings.Repeat("a", 63) + ".trusted.localhost/",
		"ftp://localhost/",
	}
	for _, windows := range []bool{false, true} {
		defer func(v bool) { schemeHosts = v }(schemeHosts)
		schemeHosts = windows
		for _, opts := range []PageOptions{{TrustedOrigins: []string{"https://partner.example/"}}, {TrustedOrigins: []string{"*"}}, {}} {
			w := &Window{trustedOrigins: opts.TrustedOrigins}
			tr := w.bridgeTrust()
			if tr.Schemes == nil || tr.Origins == nil {
				t.Fatalf("Trust must hold arrays: %+v", tr)
			}
			for _, u := range urls {
				pageURL := u
				if windows {
					// WebView2 serves custom schemes at http://[<host>.]<scheme>.localhost,
					// and the backend maps the URL back for the core.
					pageURL = schemehost.WebURL(u, isAppScheme)
				}
				hasScheme := func(s string) bool { return windows && isAppScheme(s) }
				if got, want := bridgeAllows(tr, pageURL), w.isTrusted(schemehost.AppURL(pageURL, hasScheme)); got != want {
					t.Errorf("windows=%v %v: the bridge allows %s: %v, isTrusted: %v", windows, opts.TrustedOrigins, pageURL, got, want)
				}
			}
		}
	}
}

// TestBridgeScripts: every page gets the preload script, the bridge only
// decides for itself what trust it has, and the frames inside get the
// script that hides the channel to Go.
func TestBridgeScripts(t *testing.T) {
	_, fw := testWindow(t, WindowOptions{Page: PageOptions{PreloadScript: "window.preloaded = 1", TrustedOrigins: []string{"https://Partner.example/"}}})
	scripts := fw.UserScripts()
	if len(scripts) != 4 {
		t.Fatalf("scripts: %d, want the bridge, the preferences, the preload script and the one hiding the channel", len(scripts))
	}
	var cfg struct {
		Trust bridge.Trust `json:"trust"`
	}
	m := strings.LastIndex(scripts[0].Source, "})(")
	if err := json.Unmarshal([]byte(strings.TrimSuffix(scripts[0].Source[m+3:], ");")), &cfg); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(cfg.Trust.Origins, "https://partner.example") || !slices.Contains(cfg.Trust.Schemes, "about") {
		t.Errorf("trust in the bridge configuration: %+v", cfg.Trust)
	}
	if scripts[0].AllFrames || scripts[2].AllFrames || scripts[2].Source != "window.preloaded = 1" {
		t.Errorf("the bridge and the preload script are for the top frame: %+v", scripts)
	}
	if hide := scripts[3]; !hide.AllFrames || hide.Source != bridge.HideScript {
		t.Errorf("the last script hides the channel from every frame: %+v", hide)
	}
}

// forgeries are the messages a page that has the secret and a way to the
// native channel could post besides calls.
var forgeries = []string{
	`{"t":"dom-ready"}`,
	`{"t":"drag"}`,
	`{"t":"dblclick"}`,
	`{"t":"drop","x":1,"y":2}`,
	`{"t":"chan-ack","c":5,"k":"tok","n":1}`,
	`{"t":"chan-close","c":5,"k":"tok"}`,
	`{"t":"escape"}`, // last: a panel it closes takes the others' effects with it
}

// pageEffects counts what the messages of forgeries did.
type pageEffects struct {
	domReady, drops atomic.Int32
}

func watchEffects(w *Window) *pageEffects {
	e := &pageEffects{}
	w.Page().OnDOMReady(func() { e.domReady.Add(1) })
	w.OnFileDrop(func(*FileDropEvent) { e.drops.Add(1) })
	return e
}

// TestUntrustedMessagesAreIgnored: only trusted pages may post what is not
// a call, whatever they know: the messages do nothing for a page that is
// not trusted, nor for a sender that is not, while the page showing is.
func TestUntrustedMessagesAreIgnored(t *testing.T) {
	w, fw := testWindow(t, WindowOptions{Panel: true, Page: PageOptions{TrustedOrigins: []string{"https://partner.example"}}})
	effects := watchEffects(w)
	post := func(from string) {
		for _, msg := range forgeries {
			fw.Dropped = []string{"/tmp/file"}
			pageAt(fw, from, msg)
		}
		onMain(func() {}) // what the messages posted to the main thread has run
	}
	noEffect := func(what string) {
		t.Helper()
		if n := effects.domReady.Load(); n != 0 {
			t.Errorf("%s: OnDOMReady ran %d times", what, n)
		}
		if n := effects.drops.Load(); n != 0 {
			t.Errorf("%s: OnFileDrop ran %d times", what, n)
		}
		if fw.DragStarts != 0 || fw.TitleBarDoubleClicks != 0 {
			t.Errorf("%s: window drags %d, double clicks %d", what, fw.DragStarts, fw.TitleBarDoubleClicks)
		}
		if w.IsDestroyed() {
			t.Errorf("%s: Escape closed the panel", what)
		}
		if len(fw.Dropped) != 1 {
			t.Errorf("%s: the dropped files were taken", what)
		}
	}

	// The page is not trusted.
	onMain(func() { fw.H.NavigationCommitted("https://evil.example/") })
	post("https://evil.example/")
	noEffect("an untrusted page")

	// The page showing is trusted, the sender is not: a frame, or a page
	// that navigated away.
	onMain(func() { fw.H.NavigationCommitted("https://partner.example/") })
	post("https://evil.example/")
	post("")
	noEffect("an untrusted sender")

	// A trusted sender gets what the messages do.
	post("https://partner.example/")
	if effects.domReady.Load() != 1 || effects.drops.Load() != 1 || fw.DragStarts != 1 || fw.TitleBarDoubleClicks != 1 {
		t.Errorf("the messages of a trusted page: ready %d, drops %d, drags %d, double clicks %d",
			effects.domReady.Load(), effects.drops.Load(), fw.DragStarts, fw.TitleBarDoubleClicks)
	}
	if !w.IsDestroyed() {
		t.Error("Escape in a trusted page did not close the panel")
	}
}

// TestUntrustedChannelMessages: a page that is not trusted can neither
// close the channel of another page nor close a channel before it opens.
func TestUntrustedChannelMessages(t *testing.T) {
	s := &streamer{sent: make(chan error, 16), stopped: make(chan error, 1)}
	bindForTest(t, "UntrustedStream", s)
	w, fw := testWindow(t, WindowOptions{})
	page(fw, `{"t":"call","id":1,"k":"tok","m":"UntrustedStream.Wait","a":[9]}`)
	received(t, fw, func(m map[string]any) bool { return m["c"] == float64(9) && m["p"] == "ready" })

	pageAt(fw, "https://evil.example/", `{"t":"chan-close","c":9,"k":"tok"}`)
	pageAt(fw, "https://evil.example/", `{"t":"chan-close","c":12,"k":"tok"}`)
	onMain(func() {})
	select {
	case err := <-s.stopped:
		t.Fatalf("a page that is not trusted closed a channel: %v", err)
	default:
	}
	onMain(func() {
		if len(w.closedEarly) != 0 {
			t.Errorf("a channel was closed before it opened: %v", w.closedEarly)
		}
	})
	page(fw, `{"t":"chan-close","c":9,"k":"tok"}`)
	<-s.stopped
}

// TestUntrustedCallsFail: a call from a sender that is not trusted fails,
// whatever page is showing.
func TestUntrustedCallsFail(t *testing.T) {
	svc := &testService{}
	bindForTest(t, "SenderTrust", svc)
	_, fw := testWindow(t, WindowOptions{})
	pageAt(fw, "https://evil.example/", `{"t":"call","id":1,"k":"tok","m":"SenderTrust.Hello","a":["x"]}`)
	m := received(t, fw, func(m map[string]any) bool { return m["t"] == "reply" && m["id"] == float64(1) })
	if m["ok"] != false || svc.calls.Load() != 0 {
		t.Errorf("a call from another origin: %v, ran %d times", m, svc.calls.Load())
	}
	if m := call(t, fw, 2, "SenderTrust.Hello", "x"); m["ok"] != true {
		t.Errorf("the page's own call: %v", m)
	}
}

// TestDOMReadyWithoutBridge: a page that is not trusted has no bridge to
// tell that its DOM is ready: the backend tells, in a way the page cannot
// post to, once for each document and before the load finishes. A trusted
// page's bridge tells, and the backend's word is not what counts for it.
func TestDOMReadyWithoutBridge(t *testing.T) {
	w, fw := testWindow(t, WindowOptions{})
	effects := watchEffects(w)
	var finished, shown atomic.Int32
	w.Page().OnDidFinishLoad(func() { finished.Add(1) })
	w.OnReadyToShow(func() { shown.Add(1) })
	navigate := func(url string) { onMain(func() { fw.H.NavigationCommitted(url) }) }
	check := func(what string, ready, loads int32) {
		t.Helper()
		if n := effects.domReady.Load(); n != ready || finished.Load() != loads {
			t.Errorf("%s: OnDOMReady ran %d times, want %d, with %d loads finished, want %d", what, n, ready, finished.Load(), loads)
		}
	}

	// A page that is not trusted: the backend's word, at DOM ready and not
	// at the end of the load, once for a document. The window is ready to
	// show then too.
	navigate("https://evil.example/")
	check("a page that is not trusted, committed", 0, 0)
	onMain(fw.H.DOMContentLoaded)
	check("a page that is not trusted, DOM ready", 1, 0)
	if shown.Load() != 1 {
		t.Errorf("OnReadyToShow ran %d times at DOMContentLoaded", shown.Load())
	}
	onMain(fw.H.DOMContentLoaded)
	pageAt(fw, "https://evil.example/", `{"t":"dom-ready"}`) // a forgery, as the other tests post
	onMain(fw.H.LoadFinished)
	check("a page that is not trusted, finished", 1, 1)

	// The next document of the page, as a reload or a navigation commits one.
	navigate("https://evil.example/")
	onMain(fw.H.DOMContentLoaded)
	onMain(fw.H.LoadFinished)
	check("a page that is not trusted, again", 2, 2)
	navigate("https://other.example/")
	onMain(fw.H.DOMContentLoaded)
	onMain(fw.H.LoadFinished)
	check("another page that is not trusted", 3, 3)

	// A trusted page: its bridge tells, and the backend's word is not what
	// counts for it.
	navigate("mygo://localhost/")
	onMain(fw.H.DOMContentLoaded)
	onMain(fw.H.LoadFinished)
	check("a trusted page, before its bridge told", 3, 4)
	page(fw, `{"t":"dom-ready"}`)
	onMain(fw.H.DOMContentLoaded)
	check("a trusted page", 4, 4)
}

// TestFindScript: FindInPage asks the bridge through window.__mygo, which
// every page has (the bridge pins it at document start), trusted or not:
// the script brings nothing, and uses no object a page could define.
func TestFindScript(t *testing.T) {
	w, fw := testWindow(t, WindowOptions{})
	const want = `window.__mygo.find("x", {"backward":false,"matchCase":false,"next":false})`
	if got := w.findScript("x", FindOptions{}); got != want {
		t.Errorf("trusted page: %s", got)
	}
	onMain(func() { fw.H.NavigationCommitted("https://evil.example/") })
	if got := w.findScript("x", FindOptions{}); got != want {
		t.Errorf("untrusted page: %s", got)
	}
}
