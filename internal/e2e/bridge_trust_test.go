//go:build !js

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/wait"
)

// probeScript reports what a script of a page can see of MyGo, in one frame:
// the bridge, and the native channel it talks to Go through, which is
// webkit.messageHandlers.mygo in WebKit and chrome.webview in WebView2.
const probeScript = `
const probe = (w) => ({
  mygo: typeof w.mygo,
  __mygo: typeof w.__mygo,
  handler: typeof (w.webkit && w.webkit.messageHandlers && w.webkit.messageHandlers.mygo),
  webview: typeof (w.chrome && w.chrome.webview),
  preload: w.__preload === true,
});
`

// trustPages serves a page that probes itself and its frames of other
// origins (localhost against 127.0.0.1) and of none, and the page of the
// frame of another origin.
func trustPages(t *testing.T) (origin, other string) {
	t.Helper()
	var otherOrigin string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		switch r.URL.Path {
		case "/frame":
			fmt.Fprint(w, `<!doctype html><script>`+probeScript+`parent.postMessage(JSON.stringify(probe(window)), "*")</script>`)
		default:
			fmt.Fprint(w, `<!doctype html><title>page</title><p>page</p><script>`+probeScript+`
// The bridge accepts files dragged over the page, which it does only where it is.
const files = new DataTransfer();
files.items.add(new File(["hi"], "dragged.txt"));
const dragover = new DragEvent("dragover", { dataTransfer: files, bubbles: true, cancelable: true });
document.body.dispatchEvent(dragover);
window.result = { top: { ...probe(window), accepts: dragover.defaultPrevented } };
const reply = new Promise((resolve) => addEventListener("message", (e) => resolve(JSON.parse(e.data))));
const other = document.createElement("iframe");
other.src = "`+otherOrigin+`/frame";
document.body.append(other);
const blank = document.createElement("iframe"); // no document of its own yet
document.body.append(blank);
window.result.blank = probe(blank.contentWindow);
const srcdoc = document.createElement("iframe");
srcdoc.srcdoc = "<p>srcdoc</p>";
document.body.append(srcdoc);
srcdoc.onload = async () => {
  window.result.srcdoc = probe(srcdoc.contentWindow);
  window.result.other = await reply;
  window.done = true;
};
</script>`)
		}
	}))
	t.Cleanup(server.Close)
	u, _ := url.Parse(server.URL)
	otherOrigin = "http://localhost:" + u.Port()
	return server.URL, otherOrigin
}

// pageSees returns what the page of w, and its frames, see of MyGo.
func pageSees(t *testing.T, w *mygo.Window) map[string]map[string]any {
	t.Helper()
	waitFor(t, w, "window.done")
	raw, err := mygo.EvalAs[json.RawMessage](w.Page(), "window.result")
	if err != nil {
		t.Fatal(err)
	}
	var seen map[string]map[string]any
	if err := json.Unmarshal(raw, &seen); err != nil {
		t.Fatal(err)
	}
	return seen
}

// TestUntrustedPageSeesNoBridge: a page that is not the app's own, and not
// listed in TrustedOrigins, finds no sign of MyGo but the object Go talks to
// pages through, which the bridge pinned before the page's scripts and which
// ignores what Go sends: no window.mygo and no channel to Go, in its top
// frame and in its frames, those of other origins and those without a
// document of their own. Its preload script still runs. Go still hears that
// its DOM is ready, and nothing the page does reaches Go.
func TestUntrustedPageSeesNoBridge(t *testing.T) {
	// A loopback server is the app's own in development.
	t.Setenv("MYGO_ENV", "production")
	origin, _ := trustPages(t)
	w := newWindow(t, mygo.WindowOptions{
		Hidden: true,
		Page:   mygo.PageOptions{PreloadScript: "window.__preload = true"},
	})
	var ready atomic.Int32
	w.Page().OnDOMReady(func() { ready.Add(1) })
	if err := w.Page().LoadURL(origin + "/"); err != nil {
		t.Fatal(err)
	}
	seen := pageSees(t, w)
	for _, frame := range []string{"top", "other", "blank", "srcdoc"} {
		got := seen[frame]
		for _, name := range []string{"mygo", "__mygo", "handler", "webview"} {
			// The top frame has the bridge's own window.__mygo, which ignores what Go sends.
			if frame == "top" && name == "__mygo" {
				if got[name] != "object" {
					t.Errorf("top frame: typeof __mygo = %v, want object", got[name])
				}
				continue
			}
			if got[name] != "undefined" {
				t.Errorf("%s frame: typeof %s = %v, want undefined", frame, name, got[name])
			}
		}
	}
	if seen["top"]["preload"] != true {
		t.Error("the preload script did not run in the untrusted page")
	}
	if seen["top"]["accepts"] != false {
		t.Error("the page has the bridge's listeners")
	}
	eventually(t, "OnDOMReady", func() bool { return ready.Load() >= 1 })
	if n := ready.Load(); n != 1 {
		t.Errorf("OnDOMReady ran %d times for one page", n)
	}

	// What Go sends does not reach the page, and the app can still find in it.
	if err := ticked.Emit(w, Tick{N: 1}); err != nil {
		t.Fatal(err)
	}
	res, err := w.Page().FindInPage("page", mygo.FindOptions{})
	if err != nil || res.Matches < 1 {
		t.Errorf("FindInPage = %+v, %v", res, err)
	}
	// The bridge's window.__mygo, which the page cannot define, is what
	// FindInPage asks, and all the page has of it.
	if got, _ := mygo.EvalAs[string](w.Page(), "typeof window.__mygo"); got != "object" {
		t.Errorf("typeof window.__mygo = %q, want the bridge's object", got)
	}
	w.Page().StopFindInPage()
	if got, _ := mygo.EvalAs[string](w.Page(), "typeof window.__mygo + typeof window.mygo"); got != "objectundefined" {
		t.Errorf("typeof window.__mygo and window.mygo after finding = %q", got)
	}
}

// TestTrustedOriginsPageGetsBridge: a page of an origin of TrustedOrigins
// has the bridge and calls Go, while the channel to Go is as hidden from it,
// and from its frames, as from any page, so that no script of the page can
// post messages of its own.
func TestTrustedOriginsPageGetsBridge(t *testing.T) {
	t.Setenv("MYGO_ENV", "production")
	origin, _ := trustPages(t)
	w := newWindow(t, mygo.WindowOptions{
		Hidden: true,
		Page:   mygo.PageOptions{TrustedOrigins: []string{origin}, PreloadScript: "window.__preload = true"},
	})
	var ready atomic.Int32
	w.Page().OnDOMReady(func() { ready.Add(1) })
	if err := w.Page().LoadURL(origin + "/"); err != nil {
		t.Fatal(err)
	}
	seen := pageSees(t, w)
	if top := seen["top"]; top["mygo"] != "object" || top["__mygo"] != "object" || top["preload"] != true || top["accepts"] != true {
		t.Errorf("top frame of a trusted page: %v", top)
	}
	for _, frame := range []string{"top", "other", "blank", "srcdoc"} {
		got := seen[frame]
		if got["handler"] != "undefined" || got["webview"] != "undefined" {
			t.Errorf("%s frame: the channel to Go is visible: %v", frame, got)
		}
	}
	for _, frame := range []string{"other", "blank", "srcdoc"} {
		if got := seen[frame]; got["mygo"] != "undefined" || got["__mygo"] != "undefined" {
			t.Errorf("%s frame has the bridge: %v", frame, got)
		}
	}
	// The page calls Go, hears from Go, and Go hears its DOM is ready.
	greeting, err := mygo.EvalAs[string](w.Page(), "mygo.call('Greeter.Greet', 'trusted')")
	if err != nil || greeting != "Hello, trusted!" {
		t.Errorf("call = %q, %v", greeting, err)
	}
	waitFor(t, w, "(window.ticks = [], mygo.on('ticked', (t) => ticks.push(t.n)), true)")
	if err := ticked.Emit(w, Tick{N: 7}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, w, "ticks.includes(7)")
	if n := ready.Load(); n != 1 {
		t.Errorf("OnDOMReady ran %d times for one page", n)
	}
}

// TestNavigatingBetweenTrustedAndUntrusted: the page decides again for every
// document. One that leaves the app's own page for another origin takes
// nothing of the bridge with it, and the app's page it comes back to has all
// of it.
func TestNavigatingBetweenTrustedAndUntrusted(t *testing.T) {
	t.Setenv("MYGO_ENV", "production")
	origin, _ := trustPages(t)
	w := newWindow(t, mygo.WindowOptions{Hidden: true})
	if err := w.Page().LoadURL("app://localhost/"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, w, "window.run")
	if got, err := mygo.EvalAs[string](w.Page(), "mygo.call('Greeter.Greet', 'app')"); err != nil || got != "Hello, app!" {
		t.Fatalf("call from the app's page = %q, %v", got, err)
	}
	// The page, not Go, navigates away.
	if _, err := w.Page().Eval("void (location.href = " + jsString(origin+"/") + ")"); err != nil {
		t.Fatal(err)
	}
	seen := pageSees(t, w)
	for name, got := range seen["top"] {
		if name != "preload" && name != "accepts" && name != "__mygo" && got != "undefined" {
			t.Errorf("after navigating to another origin: typeof %s = %v", name, got)
		}
	}
	// Back to the app's page (WebView2 does not take a custom scheme from a link).
	if err := w.Page().LoadURL("app://localhost/"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, w, "window.run && typeof mygo === 'object'")
	if got, err := mygo.EvalAs[string](w.Page(), "mygo.call('Greeter.Greet', 'back')"); err != nil || got != "Hello, back!" {
		t.Errorf("call after coming back = %q, %v", got, err)
	}
}

// TestWindowOpenUntrusted: a window the page opens is a window of its own,
// with the bridge of its own, which an untrusted page it shows does not get.
func TestWindowOpenUntrusted(t *testing.T) {
	t.Setenv("MYGO_ENV", "production")
	origin, _ := trustPages(t)
	w := newWindow(t, mygo.WindowOptions{Width: 400, Height: 300})
	created := make(chan *mygo.Window, 1)
	off := mygo.App.OnWindowCreated(func(c *mygo.Window) { created <- c })
	defer off()
	w.Page().SetWindowOpenHandler(func(req mygo.WindowOpenRequest) *mygo.WindowOptions {
		return &mygo.WindowOptions{Width: 320, Height: 240}
	})
	if err := w.Page().LoadURL("app://localhost/"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, w, "window.run")
	if _, err := w.Page().Eval("void window.open(" + jsString(origin+"/") + ")"); err != nil {
		t.Fatal(err)
	}
	var child *mygo.Window
	select {
	case child = <-created:
	case <-time.After(wait.Guard):
		t.Fatal("child window not created")
	}
	defer child.Destroy()
	seen := pageSees(t, child)
	for name, got := range seen["top"] {
		if name != "preload" && name != "accepts" && name != "__mygo" && got != "undefined" {
			t.Errorf("popup of another origin: typeof %s = %v", name, got)
		}
	}
}

// TestCustomSchemeHostGetsBridge: a page of a custom scheme is the app's own
// at any host. WebView2 shows app://abc/ at http://abc.app.localhost/, which
// the bridge in the page takes for the scheme's.
func TestCustomSchemeHostGetsBridge(t *testing.T) {
	for _, host := range []string{"localhost", "abc", "a.b-c"} {
		w := newWindow(t, mygo.WindowOptions{Hidden: true})
		if err := w.Page().LoadURL("app://" + host + "/"); err != nil {
			t.Fatal(err)
		}
		waitFor(t, w, "window.run") // the page of the app, not the blank one before it
		got, err := mygo.EvalAs[string](w.Page(), "mygo.call('Greeter.Greet', 'host')")
		if err != nil || got != "Hello, host!" {
			t.Errorf("app://%s/: call = %q, %v", host, got, err)
		}
		if u := w.Page().URL(); u != "app://"+host+"/" {
			t.Errorf("URL = %q, want app://%s/", u, host)
		}
	}
}

// jsString returns s as a JavaScript string literal.
func jsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
