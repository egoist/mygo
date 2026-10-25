//go:build !js

package e2e

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// popupKeepsOpener tells whether a window a page opens has window.opener on
// this platform. Linux opens independent pages (and has no StorageID).
var popupKeepsOpener = runtime.GOOS == "darwin" || runtime.GOOS == "windows"

// TestWindowOpenKeepsStorage checks that a window a page opens with
// window.open shares the data store of the page that opened it, and no
// other: the popup of a page with a StorageID reads the cookie and the
// local storage the opener wrote, writes what the opener reads, and does
// not see those of a page with another StorageID or of a page without one,
// whose popups see theirs. Openers are a window and a web view in a window
// of native UI.
func TestWindowOpenKeepsStorage(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("StorageID is not supported on Linux")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<!doctype html><title>popup-store</title><body>popup-store</body>")
	}))
	defer server.Close()
	// Every run of the test makes data stores of its own, and the cookie
	// has a name of its own: cookies are shared by every port of a host.
	run := strconv.FormatInt(time.Now().UnixNano(), 36)
	cookie := "who" + run
	state := "(document.cookie.match(/(?:^|; )" + cookie + "=([^;]*)/) || [])[1] + '|' + localStorage.getItem('who')"
	set := func(who string) string {
		return "document.cookie = '" + cookie + "=" + who + "; path=/; max-age=3600'; localStorage.setItem('who', '" + who + "')"
	}
	check := func(t *testing.T, what string, p *mygo.Page, want string) {
		t.Helper()
		got, err := mygo.EvalAs[string](p, state)
		if err != nil || got != want {
			t.Errorf("%s: cookie|localStorage %q (%v), want %q", what, got, err, want)
		}
	}
	// Only the windows a page opens count: the test makes others.
	var opening atomic.Bool
	created := make(chan *mygo.Window, 8)
	defer mygo.App.OnWindowCreated(func(w *mygo.Window) {
		if opening.Load() {
			created <- w
		}
	})()

	// open makes the page open a window and returns it once its page
	// loaded: with options of another StorageID, which the window ignores.
	n := 0
	open := func(t *testing.T, what string, opener *mygo.Page) *mygo.Window {
		t.Helper()
		n++
		opening.Store(true)
		defer opening.Store(false)
		opener.SetWindowOpenHandler(func(mygo.WindowOpenRequest) *mygo.WindowOptions {
			return &mygo.WindowOptions{Title: "Popup " + what, Width: 300, Height: 200, Page: mygo.PageOptions{StorageID: "e2e-other-" + run}}
		})
		if _, err := opener.Eval(fmt.Sprintf("void (window.popup = window.open('%s/?kid=%d', 'kid%d'))", server.URL, n, n)); err != nil {
			t.Fatal(err)
		}
		var child *mygo.Window
		select {
		case child = <-created:
		case <-time.After(20 * time.Second):
			t.Fatalf("%s: the page opened no window", what)
		}
		t.Cleanup(child.Destroy)
		waitFor(t, child, "document.title === 'popup-store' && location.search === '?kid="+strconv.Itoa(n)+"'")
		if popupKeepsOpener {
			ok, err := mygo.EvalAs[bool](child.Page(), "window.opener !== null && window.opener.location.origin === location.origin")
			if err != nil || !ok {
				t.Errorf("%s: window.opener is not the opener's window: %v, %v", what, ok, err)
			}
		}
		return child
	}

	idA, idB := "e2e-pa-"+run, "e2e-pb-"+run
	winA := newWindow(t, mygo.WindowOptions{Title: "Opener A", Width: 300, Height: 200, URL: server.URL, Page: mygo.PageOptions{StorageID: idA}})
	waitFor(t, winA, "document.title === 'popup-store'")
	winB := newWindow(t, mygo.WindowOptions{Title: "Opener B", Width: 300, Height: 200, URL: server.URL, Page: mygo.PageOptions{StorageID: idB}})
	waitFor(t, winB, "document.title === 'popup-store'")
	winD := newWindow(t, mygo.WindowOptions{Title: "Opener default", Width: 300, Height: 200, URL: server.URL})
	waitFor(t, winD, "document.title === 'popup-store'")
	for _, p := range []struct {
		who string
		pg  *mygo.Page
	}{{"A", winA.Page()}, {"B", winB.Page()}, {"D", winD.Page()}} {
		if _, err := p.pg.Eval(set(p.who)); err != nil {
			t.Fatal(err)
		}
	}

	check(t, "the opener A", winA.Page(), "A|A")
	popA := open(t, "of A", winA.Page())
	check(t, "a window A opened", popA.Page(), "A|A")
	popB := open(t, "of B", winB.Page())
	check(t, "a window B opened", popB.Page(), "B|B")
	popD := open(t, "of the default store", winD.Page())
	check(t, "a window the page without a StorageID opened", popD.Page(), "D|D")

	// The store is shared both ways, and only with the opener.
	if _, err := popA.Page().Eval(set("A2")); err != nil {
		t.Fatal(err)
	}
	check(t, "the opener A after its popup wrote", winA.Page(), "A2|A2")
	check(t, "a second window A opened", open(t, "of A again", winA.Page()).Page(), "A2|A2")
	check(t, "B after A's popup wrote", winB.Page(), "B|B")
	check(t, "the default store after A's popup wrote", winD.Page(), "D|D")
	check(t, "the popup of B after A's popup wrote", popB.Page(), "B|B")
	check(t, "the popup of the default store after A's popup wrote", popD.Page(), "D|D")

	// A web view in a window of native UI opens windows of its store too.
	t.Run("WebView", func(t *testing.T) {
		var mu sync.Mutex
		var shown []*mygo.WebView
		var frames atomic.Int32
		host := newWindow(t, mygo.WindowOptions{Title: "Popup storage", Width: 500, Height: 200, Content: ui.View(func(c *ui.Context) {
			frames.Add(1)
			mu.Lock()
			views := slices.Clone(shown)
			mu.Unlock()
			ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
				for i, v := range views {
					ui.WebView(c.Key(strconv.Itoa(i)), v).Grow(1)
				}
			})
		})})
		eventually(t, "a frame", func() bool { return frames.Load() > 0 })
		view, err := host.NewWebView(mygo.WebViewOptions{URL: server.URL, Page: mygo.PageOptions{StorageID: "e2e-pw-" + run}})
		if errors.Is(err, errors.ErrUnsupported) {
			t.Skip(err)
		}
		if err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		shown = append(shown, view)
		mu.Unlock()
		host.Invalidate()
		waitForPage(t, view.Page(), "document.title === 'popup-store'")
		if _, err := view.Page().Eval(set("W")); err != nil {
			t.Fatal(err)
		}
		check(t, "the opener web view", view.Page(), "W|W")
		check(t, "a window the web view opened", open(t, "of the web view", view.Page()).Page(), "W|W")
		check(t, "A after the web view's popup", winA.Page(), "A2|A2")
		check(t, "the default store after the web view's popup", winD.Page(), "D|D")
	})
}
