//go:build !js

package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/wait"
)

// gatePages serves the given pages of a loopback server, which in production
// mode (and with no TrustedOrigins) is a page the app does not trust.
func gatePages(t *testing.T, pages map[string]string) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := pages[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

// popupPage is the page the opener's popups show first.
func popupPage(title string) string {
	return `<!doctype html><title>` + title + `</title><p>` + title + `</p>`
}

// TestBridgeAboutBlankPopupGetsNoBridge: a page the app does not trust holds
// popups that show about:blank documents, which have its origin. None of them
// has the bridge, and no call of the page through them makes Go run a method.
func TestBridgeAboutBlankPopupGetsNoBridge(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("popups of WebKitGTK are independent windows, with no relation to their opener (window.open does not return them)")
	}
	t.Setenv("MYGO_ENV", "production")
	origin := gatePages(t, map[string]string{
		"/":       popupPage("opener"),
		"/popupB": popupPage("popupB"),
		"/popupC": popupPage("popupC"),
	})

	// reach is what the opener's script does with the popup it holds: it finds
	// no bridge, or its call is rejected.
	const reach = `(async () => { try {
	  const p = window.pop; const t = typeof p.mygo;
	  if (t !== 'object') return 'no mygo: ' + t;
	  try { await p.mygo.call('Probe.Touch'); return 'ran'; } catch (e) { return 'rejected: ' + e.message; }
	} catch (e) { return 'threw ' + e.name + ': ' + e.message; } })()`

	// open makes a window of an untrusted page, which allows its popups.
	open := func(t *testing.T) (*mygo.Window, func() *mygo.Window) {
		t.Helper()
		w := newWindow(t, mygo.WindowOptions{Hidden: true})
		created := make(chan *mygo.Window, 4)
		off := mygo.App.OnWindowCreated(func(c *mygo.Window) { created <- c })
		t.Cleanup(off)
		w.Page().SetWindowOpenHandler(func(req mygo.WindowOpenRequest) *mygo.WindowOptions {
			return &mygo.WindowOptions{Width: 320, Height: 240, Hidden: true}
		})
		if err := w.Page().LoadURL(origin + "/"); err != nil {
			t.Fatal(err)
		}
		waitFor(t, w, "document.title === 'opener'")
		if got, _ := mygo.EvalAs[string](w.Page(), "typeof window.mygo"); got != "undefined" {
			t.Fatalf("the untrusted page has window.mygo: %q", got)
		}
		return w, func() *mygo.Window {
			t.Helper()
			select {
			case c := <-created:
				t.Cleanup(c.Destroy)
				return c
			case <-wait.Context(t).Done():
				t.Fatal("no popup window was created")
				return nil
			}
		}
	}
	// check evaluates expr in the opener, once the popup settled, and checks
	// what it found and that Go ran nothing.
	check := func(t *testing.T, w *mygo.Window, what, expr string) {
		t.Helper()
		probe.n.Store(0)
		got, err := mygo.EvalAs[string](w.Page(), expr)
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		if got != "no mygo: undefined" && !strings.HasPrefix(got, "rejected") {
			t.Errorf("%s: the popup's document has the bridge: %q", what, got)
		}
		if n := probe.n.Load(); n != 0 {
			t.Errorf("%s: Go ran a bound method %d time(s) for a page that is not trusted", what, n)
		}
	}

	t.Run("open about:blank", func(t *testing.T) {
		w, next := open(t)
		if _, err := w.Page().Eval("void (window.pop = window.open('about:blank'))"); err != nil {
			t.Fatal(err)
		}
		next()
		waitFor(t, w, "pop.location.href === 'about:blank' && pop.document.readyState === 'complete'")
		check(t, w, "window.open('about:blank')", reach)
	})

	t.Run("location set to about:blank", func(t *testing.T) {
		w, next := open(t)
		if _, err := w.Page().Eval("void (window.pop = window.open(location.origin + '/popupB'))"); err != nil {
			t.Fatal(err)
		}
		b := next()
		waitForPage(t, b.Page(), "document.title === 'popupB'")
		if _, err := w.Page().Eval("void (window.pop.location = 'about:blank')"); err != nil {
			t.Fatal(err)
		}
		eventually(t, "the popup is at about:blank", func() bool { return b.Page().URL() == "about:blank" })
		waitFor(t, w, "pop.location.href === 'about:blank' && pop.document.readyState === 'complete'")
		check(t, w, "pop.location = 'about:blank'", reach)
	})

	t.Run("document.write", func(t *testing.T) {
		w, next := open(t)
		if _, err := w.Page().Eval("void (window.pop = window.open(location.origin + '/popupC'))"); err != nil {
			t.Fatal(err)
		}
		c := next()
		waitForPage(t, c.Page(), "document.title === 'popupC'")
		if _, err := w.Page().Eval("void (window.pop.location = 'about:blank')"); err != nil {
			t.Fatal(err)
		}
		eventually(t, "the popup is at about:blank", func() bool { return c.Page().URL() == "about:blank" })
		waitFor(t, w, "pop.location.href === 'about:blank' && pop.document.readyState === 'complete'")
		// The opener's own script runs in the popup's document: whatever
		// the popup has of the bridge, it has for that script.
		const script = `(async () => { try {
		  const p = window.pop; p.document.open();
		  p.document.write('<script>window.__r = typeof window.mygo; window.__call = window.mygo ? window.mygo.call("Probe.Touch").then(() => "ran", (e) => "rejected: " + e.message) : Promise.resolve("no mygo: " + typeof window.mygo)<\/script>');
		  p.document.close();
		  return await p.__call;
		} catch (e) { return 'threw ' + e.name + ': ' + e.message; } })()`
		check(t, w, "document.write in the popup", script)
	})
}

// TestPopupOfATrustedPageKeepsTheBridge: a popup of a trusted page that
// shows a page of a trusted origin has the bridge and calls Go, though the
// popup began as an about:blank document the bridge did not trust: no
// document of the popup starts the bridge off for the next one.
func TestPopupOfATrustedPageKeepsTheBridge(t *testing.T) {
	switch runtime.GOOS {
	case "linux":
		t.Skip("popups of WebKitGTK are independent pages, with no relation to their opener")
	case "windows":
		t.Skip("on WebView2 a call of the first document of a popup of a page at TrustedOrigins gets no reply, and the bridge finds no channel there (it fails on upstream/main too: only popups of the app's schemes are loaded again, see newWindowRequested)")
	}
	t.Setenv("MYGO_ENV", "production")
	origin := gatePages(t, map[string]string{"/": popupPage("opener"), "/popup": popupPage("popup")})
	trusted := mygo.PageOptions{TrustedOrigins: []string{origin}}
	w := newWindow(t, mygo.WindowOptions{Hidden: true, Page: trusted})
	created := make(chan *mygo.Window, 2)
	off := mygo.App.OnWindowCreated(func(c *mygo.Window) { created <- c })
	t.Cleanup(off)
	w.Page().SetWindowOpenHandler(func(req mygo.WindowOpenRequest) *mygo.WindowOptions {
		return &mygo.WindowOptions{Width: 320, Height: 240, Hidden: true, Page: trusted}
	})
	if err := w.Page().LoadURL(origin + "/"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, w, "document.title === 'opener' && typeof window.mygo === 'object'")
	if _, err := w.Page().Eval("void window.open(location.origin + '/popup')"); err != nil {
		t.Fatal(err)
	}
	var child *mygo.Window
	select {
	case child = <-created:
		t.Cleanup(child.Destroy)
	case <-wait.Context(t).Done():
		t.Fatal("no popup window was created")
	}
	waitForPage(t, child.Page(), "document.title === 'popup' && typeof window.mygo === 'object'")
	got, err := mygo.EvalAs[string](child.Page(), "mygo.call('Greeter.Greet', 'popup')")
	if err != nil || got != "Hello, popup!" {
		t.Errorf("a call from the popup = %q, %v", got, err)
	}
}

// TestBridgeOwnAboutDocumentKeepsBridge: the documents the app makes itself,
// from HTML with no base URL, are about:blank pages too, and have the bridge,
// also after a reload.
func TestBridgeOwnAboutDocumentKeepsBridge(t *testing.T) {
	t.Setenv("MYGO_ENV", "production")
	w := newWindow(t, mygo.WindowOptions{Hidden: true})
	w.Page().LoadHTML("<p>own</p>", "")
	waitFor(t, w, "document.querySelector('p') && typeof window.mygo === 'object'")
	greet := func(what, name string) {
		t.Helper()
		got, err := mygo.EvalAs[string](w.Page(), "mygo.call('Greeter.Greet', "+jsString(name)+")")
		if err != nil || got != "Hello, "+name+"!" {
			t.Errorf("%s: call = %q, %v", what, got, err)
		}
	}
	greet("the app's own document", "own")
	if _, err := w.Page().Eval("void (window.__first = true)"); err != nil {
		t.Fatal(err)
	}
	w.Page().Reload()
	waitFor(t, w, "window.__first === undefined && document.querySelector('p') && typeof window.mygo === 'object'")
	greet("after a reload", "reloaded")
}

// TestUntrustedPageCannotTakeReceive: a page that is not trusted cannot put
// its own window.__mygo in the place of the bridge's: it is pinned before the
// page's scripts run, and inert, so what Go would send to a bridge is never
// handed to the page's code.
func TestUntrustedPageCannotTakeReceive(t *testing.T) {
	t.Setenv("MYGO_ENV", "production")
	origin := gatePages(t, map[string]string{"/": `<!doctype html><html><head><script>
window.__leaks = [];
const fake = (name) => ({
  receive() { window.__leaks.push(name + ':receive') },
  deliver() { window.__leaks.push(name + ':deliver') },
  find() { window.__leaks.push(name + ':find'); return { matches: 99, active: 1 } },
  stopFind() {},
});
try { window.__mygo = fake('assign'); } catch (e) { window.__assignThrew = true }
try { Object.defineProperty(window, '__mygo', { value: fake('define') }); } catch (e) { window.__defineThrew = true }
</script><title>pinned</title></head><body><p>pinned</p></body></html>`})
	w := newWindow(t, mygo.WindowOptions{Hidden: true})
	if err := w.Page().LoadURL(origin + "/"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, w, "document.title === 'pinned'")
	got, err := mygo.EvalAs[string](w.Page(), `(() => {
	  let threw = '';
	  try {
	    window.__mygo.receive([{ t: 'event', n: 'x', p: 1 }]);
	    window.__mygo.deliver({ m: [{ t: 'event', n: 'x', p: 1 }] });
	  } catch (e) { threw = String(e) }
	  const d = Object.getOwnPropertyDescriptor(window, '__mygo');
	  return JSON.stringify({ leaks: window.__leaks, threw, configurable: d && d.configurable, writable: d && d.writable, mygo: typeof mygo });
	})()`)
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Leaks        []string
		Threw        string
		Configurable *bool
		Writable     *bool
		Mygo         string
	}
	if err := json.Unmarshal([]byte(got), &s); err != nil {
		t.Fatal(err, got)
	}
	if len(s.Leaks) != 0 {
		t.Errorf("the page's own window.__mygo took what was handed to the bridge: %v", s.Leaks)
	}
	if s.Threw != "" {
		t.Errorf("the inert window.__mygo threw: %s", s.Threw)
	}
	if s.Configurable == nil || *s.Configurable || s.Writable == nil || *s.Writable {
		t.Errorf("window.__mygo is not pinned: %s", got)
	}
	if s.Mygo != "undefined" {
		t.Errorf("typeof mygo = %q in an untrusted page", s.Mygo)
	}
}

// gateService is a bound service whose method stays running until the test
// lets it return.
type gateService struct {
	entered  chan struct{}
	release  chan struct{}
	returned chan struct{}
}

// Slow blocks until the test releases it.
func (g *gateService) Slow() string {
	g.entered <- struct{}{}
	<-g.release
	defer func() { g.returned <- struct{}{} }()
	return "RESULT-OF-A-TRUSTED-CALL"
}

var (
	gate     = &gateService{entered: make(chan struct{}, 1), release: make(chan struct{}), returned: make(chan struct{}, 1)}
	bindGate sync.Once
)

// TestReplyOfTrustedCallDoesNotReachUntrustedPage: what Go sends for a page
// the app trusts (the reply of its call, its events) is never evaluated in the
// page the window shows by the time Go sends it, if that one is not trusted,
// also when that page made a window.__mygo of its own first.
func TestReplyOfTrustedCallDoesNotReachUntrustedPage(t *testing.T) {
	t.Setenv("MYGO_ENV", "production")
	bindGate.Do(func() { mygo.BindAs("BridgeGate", gate) })
	origin := gatePages(t, map[string]string{"/": `<!doctype html><html><head><script>
window.__leaks = [];
try { window.__mygo = { receive(m) { window.__leaks.push(m) }, deliver(m) { window.__leaks.push(m) } }; } catch (e) {}
</script><title>leaky</title></head><body><p>leaky</p></body></html>`})

	// leaked settles the untrusted page after the window showed it, and returns
	// what its own window.__mygo was handed.
	leaked := func(t *testing.T, w *mygo.Window) string {
		t.Helper()
		got, err := mygo.EvalAs[string](w.Page(), `(async () => {
		  await 1; await new Promise((r) => setTimeout(r, 0));
		  return JSON.stringify(window.__leaks);
		})()`)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	toLeaky := func(t *testing.T, w *mygo.Window) {
		t.Helper()
		if err := w.Page().LoadURL(origin + "/"); err != nil {
			t.Fatal(err)
		}
		waitFor(t, w, "document.title === 'leaky'")
		eventually(t, "Go hears of the page", func() bool { return w.Page().URL() == origin+"/" })
	}
	trustedWindow := func(t *testing.T) *mygo.Window {
		t.Helper()
		w := newWindow(t, mygo.WindowOptions{Hidden: true})
		w.Page().LoadHTML("<p>trusted</p>", "")
		waitFor(t, w, "document.querySelector('p') && typeof window.mygo === 'object'")
		return w
	}

	t.Run("reply", func(t *testing.T) {
		w := trustedWindow(t)
		if _, err := w.Page().Eval("void (window.__call = mygo.call('BridgeGate.Slow').catch(() => 'rejected'))"); err != nil {
			t.Fatal(err)
		}
		select {
		case <-gate.entered:
		case <-wait.Context(t).Done():
			t.Fatal("the method never ran")
		}
		toLeaky(t, w)
		gate.release <- struct{}{}
		select {
		case <-gate.returned:
		case <-wait.Context(t).Done():
			t.Fatal("the method never returned")
		}
		if got := leaked(t, w); got != "[]" {
			t.Errorf("the reply of a trusted page's call reached the page that is not trusted: %s", got)
		}
	})

	t.Run("event", func(t *testing.T) {
		w := trustedWindow(t)
		stop := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				ticked.Emit(w, Tick{N: i})
			}
		}()
		stopped := false
		stopEmitting := func() {
			if !stopped {
				stopped = true
				close(stop)
				wg.Wait()
			}
		}
		defer stopEmitting()
		toLeaky(t, w)
		stopEmitting()
		if got := leaked(t, w); got != "[]" {
			if len(got) > 300 {
				got = got[:300] + "…"
			}
			t.Errorf("events of Go reached the page that is not trusted: %s", got)
		}
	})
}

// TestFindInPageIgnoresPageDefinedMygo: FindInPage in a page that is not
// trusted counts matches itself, and does not take them from a window.__mygo
// the page defined.
func TestFindInPageIgnoresPageDefinedMygo(t *testing.T) {
	t.Setenv("MYGO_ENV", "production")
	origin := gatePages(t, map[string]string{"/": `<!doctype html><html><head><script>
window.__mygo = { find() { return { matches: 99, active: 1 } }, stopFind() {} };
</script><title>searched</title></head><body><p>text one</p><p>text two</p><p>text three</p></body></html>`})
	w := newWindow(t, mygo.WindowOptions{Width: 400, Height: 300, Hidden: true})
	if err := w.Page().LoadURL(origin + "/"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, w, "document.title === 'searched' && document.readyState === 'complete'")
	res, err := w.Page().FindInPage("text", mygo.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Matches != 3 {
		t.Errorf("FindInPage found %d matches (active %d), want the real 3, not what the page's own find answers", res.Matches, res.Active)
	}
}
