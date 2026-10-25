//go:build !js

package e2e

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/wait"
)

// domServer serves pages that load resources the test holds back until it
// lets them go, so that a page's DOM is ready while its load goes on, for as
// long as the test wants and not for a time of its own.
type domServer struct {
	// URL and Other are the server's two origins: 127.0.0.1 and localhost.
	URL, Other string
	// held gets the path of each held request that reached the server, and
	// seen those of /seen/<what>, which a page requests to tell the test
	// that a script of it ran.
	held, seen chan string
	// release lets the held resources answer; it can be called again.
	release func()
}

const domFrame = `<!doctype html><title>frame</title><p>frame</p><script>fetch("/seen/frame")</script>`

func newDOMServer(t *testing.T) *domServer {
	t.Helper()
	s := &domServer{held: make(chan string, 16), seen: make(chan string, 16)}
	released := make(chan struct{})
	var once sync.Once
	s.release = func() { once.Do(func() { close(released) }) }
	tell := func(ch chan string, path string) {
		select {
		case ch <- path:
		default:
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case strings.HasPrefix(path, "/hold/"):
			tell(s.held, path)
			select {
			case <-released:
			case <-r.Context().Done():
				return
			}
			if strings.HasSuffix(path, ".js") {
				w.Header().Set("Content-Type", "text/javascript")
				io.WriteString(w, "window.blocker = 'ran'")
				return
			}
			w.Header().Set("Content-Type", "image/svg+xml")
			io.WriteString(w, `<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"/>`)
		case strings.HasPrefix(path, "/seen/"):
			tell(s.seen, path)
			w.WriteHeader(http.StatusNoContent)
		case path == "/frame":
			w.Header().Set("Content-Type", "text/html")
			io.WriteString(w, domFrame)
		case path == "/image":
			// DOMContentLoaded does not wait for an image, the load does.
			w.Header().Set("Content-Type", "text/html")
			io.WriteString(w, `<!doctype html><title>image</title><p>image</p><img src="/hold/image.svg">`)
		case path == "/forged":
			// A page that dispatches the DOMContentLoaded events of its own
			// before the real one, makes its document claim to be done,
			// keeps the events from reaching listeners after it, and has
			// frames of its origin and of another one, whose DOM is ready
			// long before its own, as a script it waits for holds it.
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<!doctype html><title>forged</title>
<script>
addEventListener("DOMContentLoaded", (e) => e.stopImmediatePropagation(), true);
document.addEventListener("DOMContentLoaded", (e) => e.stopImmediatePropagation(), true);
document.dispatchEvent(new Event("DOMContentLoaded", { bubbles: true }));
window.dispatchEvent(new Event("DOMContentLoaded"));
document.dispatchEvent(new Event("readystatechange"));
Object.defineProperty(document, "readyState", { get: () => "complete" });
fetch("/seen/forged");
</script>
<iframe src="/frame"></iframe>
<iframe src="%s/frame"></iframe>
<script src="/hold/block.js"></script>
<p>parsed</p>`, s.Other)
		default:
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<!doctype html><title>plain</title><p>plain %s</p>`, r.URL.RawQuery)
		}
	}))
	t.Cleanup(server.Close)
	t.Cleanup(s.release)
	u, _ := url.Parse(server.URL)
	s.URL, s.Other = server.URL, "http://localhost:"+u.Port()
	return s
}

// domEvents counts what a window's page reported.
type domEvents struct{ ready, finished atomic.Int32 }

func watchDOM(w *mygo.Window) *domEvents {
	e := &domEvents{}
	w.Page().OnDOMReady(func() { e.ready.Add(1) })
	w.Page().OnDidFinishLoad(func() { e.finished.Add(1) })
	return e
}

// domWindow returns a hidden window, whose pages are those of s as an app
// that trusts s would have them, or not.
func domWindow(t *testing.T, s *domServer, trusted bool) *mygo.Window {
	t.Helper()
	// A loopback server is the app's own in development.
	t.Setenv("MYGO_ENV", "production")
	opts := mygo.PageOptions{}
	if trusted {
		opts.TrustedOrigins = []string{s.URL}
	}
	return newWindow(t, mygo.WindowOptions{Hidden: true, Page: opts})
}

// settle returns once the page answered, which is after the messages it
// posted before reached Go, as the page answers on the same road.
func settle(t *testing.T, w *mygo.Window) {
	t.Helper()
	if _, err := w.Page().Eval("1"); err != nil {
		t.Fatal(err)
	}
}

// TestDOMReadyDoesNotWaitForTheLoad: OnDOMReady runs when the DOM of the
// page is ready, whether or not the page may call Go, and not when its last
// resource arrives: with an image the server holds back, the DOM is ready
// and the load still goes on.
func TestDOMReadyDoesNotWaitForTheLoad(t *testing.T) {
	for _, trusted := range []bool{false, true} {
		t.Run(fmt.Sprintf("trusted=%v", trusted), func(t *testing.T) {
			s := newDOMServer(t)
			w := domWindow(t, s, trusted)
			ev := watchDOM(w)
			if err := w.Page().LoadURL(s.URL + "/image"); err != nil {
				t.Fatal(err)
			}
			wait.For(t, "the request of the image", s.held)
			eventually(t, "OnDOMReady while the image is held", func() bool { return ev.ready.Load() >= 1 })
			if n := ev.finished.Load(); n != 0 {
				t.Errorf("the load finished %d times while its image was held", n)
			}
			s.release()
			eventually(t, "the load to finish", func() bool { return ev.finished.Load() >= 1 })
			settle(t, w)
			if n := ev.ready.Load(); n != 1 {
				t.Errorf("OnDOMReady ran %d times for one page", n)
			}
		})
	}
}

// TestDOMReadyOncePerDocument: every document, of a load, a reload, a link
// or a script navigating, gets one OnDOMReady, neither more nor fewer, for a
// page that is trusted and for one that is not.
func TestDOMReadyOncePerDocument(t *testing.T) {
	for _, trusted := range []bool{false, true} {
		t.Run(fmt.Sprintf("trusted=%v", trusted), func(t *testing.T) {
			s := newDOMServer(t)
			w := domWindow(t, s, trusted)
			ev := watchDOM(w)
			loaded := func(what string, n int32) {
				t.Helper()
				eventually(t, what+" to finish", func() bool { return ev.finished.Load() >= n })
				eventually(t, "OnDOMReady of "+what, func() bool { return ev.ready.Load() >= n })
				settle(t, w)
				if got := ev.ready.Load(); got != n || ev.finished.Load() != n {
					t.Fatalf("%s: OnDOMReady ran %d times and the load finished %d, want %d each", what, got, ev.finished.Load(), n)
				}
			}
			if err := w.Page().LoadURL(s.URL + "/plain?load"); err != nil {
				t.Fatal(err)
			}
			loaded("the first load", 1)
			w.Page().Reload()
			loaded("the reload", 2)
			if _, err := w.Page().Eval("void (location.href = '/plain?script')"); err != nil {
				t.Fatal(err)
			}
			loaded("the navigation of the page", 3)
			if err := w.Page().LoadURL(s.URL + "/plain?again"); err != nil {
				t.Fatal(err)
			}
			loaded("the next load", 4)
		})
	}
}

// TestForgedDOMContentLoadedIsIgnored: a page of another site cannot make
// Go believe that its DOM is ready before it is: not by dispatching the
// event itself, nor by claiming its document is done, nor by keeping the
// real event from the listeners that follow it; and the frames in it, of its
// origin or of another, do not tell for it. Its DOM is ready when the parser
// is past the script the server holds, and the page's tricks do not keep
// that from Go either.
func TestForgedDOMContentLoadedIsIgnored(t *testing.T) {
	s := newDOMServer(t)
	w := domWindow(t, s, false)
	ev := watchDOM(w)
	if err := w.Page().LoadURL(s.URL + "/forged"); err != nil {
		t.Fatal(err)
	}
	// The forgeries ran, and both frames loaded, while the parser waits.
	wait.For(t, "the script held back", s.held)
	seen := map[string]int{}
	for range 3 {
		seen[wait.For(t, "the page, and its frames, to tell that they ran", s.seen)]++
	}
	if seen["/seen/forged"] != 1 || seen["/seen/frame"] != 2 {
		t.Fatalf("seen = %v", seen)
	}
	settle(t, w)
	if n := ev.ready.Load(); n != 0 {
		t.Fatalf("OnDOMReady ran %d times before the DOM of the page was ready", n)
	}
	s.release()
	eventually(t, "OnDOMReady once the parser went on", func() bool { return ev.ready.Load() >= 1 })
	eventually(t, "the load to finish", func() bool { return ev.finished.Load() >= 1 })
	settle(t, w)
	if n := ev.ready.Load(); n != 1 {
		t.Errorf("OnDOMReady ran %d times for one page", n)
	}
}
