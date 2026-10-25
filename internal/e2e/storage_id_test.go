//go:build !js

package e2e

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// TestStorageID: pages with the same StorageID share their cookies and
// local storage, windows, web views and the windows a page opens alike;
// pages with another StorageID, or none, share none of them with them.
func TestStorageID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "<!doctype html><title>storage</title><body>storage</body>")
	}))
	defer server.Close()
	// Every run of the test makes data stores of its own.
	run := strconv.FormatInt(time.Now().UnixNano(), 36)
	idA, idB := "e2e-a-"+run, "e2e-b-"+run
	const state = "(document.cookie.match(/(?:^|; )who=([^;]*)/) || [])[1] + '|' + localStorage.getItem('who')"
	const set = "document.cookie = 'who=%[1]s; path=/; max-age=3600'; localStorage.setItem('who', '%[1]s')"
	const empty = "undefined|null"

	var mu sync.Mutex
	var shown []*mygo.WebView
	var frames atomic.Int32
	w := newWindow(t, mygo.WindowOptions{Title: "Storage", Width: 600, Height: 200, Content: ui.View(func(c *ui.Context) {
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
	open := func(id string) *mygo.WebView {
		t.Helper()
		v, err := w.NewWebView(mygo.WebViewOptions{URL: server.URL, Page: mygo.PageOptions{StorageID: id}})
		if errors.Is(err, errors.ErrUnsupported) {
			t.Skip(err)
		}
		if err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		shown = append(shown, v)
		mu.Unlock()
		w.Invalidate()
		waitForPage(t, v.Page(), "document.title === 'storage'")
		return v
	}
	check := func(what string, p *mygo.Page, want string) {
		t.Helper()
		got, err := mygo.EvalAs[string](p, state)
		if err != nil || got != want {
			t.Errorf("%s: cookie|localStorage %q (%v), want %q", what, got, err, want)
		}
	}

	a, b := open(idA), open(idB)
	check("a fresh store", a.Page(), empty)
	if _, err := a.Page().Eval(fmt.Sprintf(set, "A")); err != nil {
		t.Fatal(err)
	}
	check("the store of A", a.Page(), "A|A")
	check("the store of B after A wrote", b.Page(), empty)
	if _, err := b.Page().Eval(fmt.Sprintf(set, "B")); err != nil {
		t.Fatal(err)
	}
	check("the store of A after B wrote", a.Page(), "A|A")
	check("the store of B", b.Page(), "B|B")

	check("a web view of A made later", open(idA).Page(), "A|A")
	check("a web view with no StorageID", open("").Page(), empty)

	// A window shows a page of a store as a web view does.
	win := newWindow(t, mygo.WindowOptions{Title: "Storage B", Width: 300, Height: 200, URL: server.URL, Page: mygo.PageOptions{StorageID: idB}})
	waitFor(t, win, "document.title === 'storage'")
	check("a window of B", win.Page(), "B|B")

	// A window the page opens shares its store, whatever its options say.
	created := make(chan *mygo.Window, 4)
	defer mygo.App.OnWindowCreated(func(w *mygo.Window) { created <- w })()
	a.Page().SetWindowOpenHandler(func(mygo.WindowOpenRequest) *mygo.WindowOptions {
		return &mygo.WindowOptions{Title: "Popup of A", Width: 300, Height: 200, Page: mygo.PageOptions{StorageID: idB}}
	})
	if _, err := a.Page().Eval("window.open('" + server.URL + "/popup'), 1"); err != nil {
		t.Fatal(err)
	}
	popup := <-created
	t.Cleanup(popup.Destroy)
	waitFor(t, popup, "document.title === 'storage'")
	check("a window A opened", popup.Page(), "A|A")
}

// storagePage keeps what an account of a site keeps: a cookie and local
// storage and IndexedDB entry the page writes, and a session cookie the
// server sets with HttpOnly, which the page cannot read and /whoami tells.
// state() returns them as "cookie|localStorage|IndexedDB|HttpOnly cookie".
const storagePage = `<!doctype html><title>clear</title><body>clear<script>
const idb = () => new Promise((res, rej) => {
  const r = indexedDB.open('clear', 1);
  r.onupgradeneeded = () => r.result.createObjectStore('kv');
  r.onsuccess = () => res(r.result);
  r.onerror = () => rej(r.error);
});
async function state() {
  const db = await idb();
  const entry = await new Promise((res, rej) => {
    const q = db.transaction('kv').objectStore('kv').get('who');
    q.onsuccess = () => res(q.result === undefined ? null : q.result);
    q.onerror = () => rej(q.error);
  });
  db.close();
  const m = document.cookie.match(/(?:^|; )who=([^;]*)/);
  const sid = await (await fetch('/whoami', {cache: 'no-store'})).text();
  return [m ? m[1] : null, localStorage.getItem('who'), entry, sid].join('|');
}
async function save(who) {
  document.cookie = 'who=' + who + '; path=/; max-age=3600';
  localStorage.setItem('who', who);
  const db = await idb();
  await new Promise((res, rej) => {
    const t = db.transaction('kv', 'readwrite');
    t.objectStore('kv').put(who, 'who');
    t.oncomplete = () => res();
    t.onerror = () => rej(t.error);
  });
  db.close();
  await fetch('/login?who=' + who, {cache: 'no-store'});
}
</script></body>`

// TestClearStorage: ClearStorage deletes the cookies (HttpOnly ones too),
// local storage and IndexedDB of one StorageID, with its pages open or not,
// and leaves the other stores and the default store alone.
func TestClearStorage(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "sid", Value: r.URL.Query().Get("who"), Path: "/", MaxAge: 3600, HttpOnly: true})
	})
	mux.HandleFunc("/whoami", func(w http.ResponseWriter, r *http.Request) {
		sid := "none"
		if c, err := r.Cookie("sid"); err == nil {
			sid = c.Value
		}
		fmt.Fprint(w, sid)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, storagePage)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	run := strconv.FormatInt(time.Now().UnixNano(), 36)
	idA, idB := "e2e-clear-a-"+run, "e2e-clear-b-"+run
	const empty = "|||none"

	var mu sync.Mutex
	var shown []*mygo.WebView
	var frames atomic.Int32
	w := newWindow(t, mygo.WindowOptions{Title: "Clear storage", Width: 600, Height: 200, Content: ui.View(func(c *ui.Context) {
		frames.Add(1)
		mu.Lock()
		views := slices.Clone(shown)
		mu.Unlock()
		ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
			for i, v := range views {
				if !v.IsDestroyed() {
					ui.WebView(c.Key(strconv.Itoa(i)), v).Grow(1)
				}
			}
		})
	})})
	eventually(t, "a frame", func() bool { return frames.Load() > 0 })
	open := func(id string) *mygo.WebView {
		t.Helper()
		v, err := w.NewWebView(mygo.WebViewOptions{URL: server.URL, Page: mygo.PageOptions{StorageID: id}})
		if errors.Is(err, errors.ErrUnsupported) {
			t.Skip(err)
		}
		if err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		shown = append(shown, v)
		mu.Unlock()
		w.Invalidate()
		waitForPage(t, v.Page(), "typeof state === 'function' && document.readyState === 'complete'")
		return v
	}
	check := func(what string, v *mygo.WebView, want string) {
		t.Helper()
		got, err := mygo.EvalAs[string](v.Page(), "state()")
		if err != nil || got != want {
			t.Errorf("%s: cookie|localStorage|IndexedDB|HttpOnly cookie %q (%v), want %q", what, got, err, want)
		}
	}
	save := func(v *mygo.WebView, who string) {
		t.Helper()
		if _, err := mygo.EvalAs[any](v.Page(), "save('"+who+"')"); err != nil {
			t.Fatal(err)
		}
	}
	reload := func(v *mygo.WebView) {
		t.Helper()
		if _, err := v.Page().Eval("window.oldPage = true"); err != nil {
			t.Fatal(err)
		}
		v.Page().Reload()
		waitForPage(t, v.Page(), "typeof state === 'function' && !window.oldPage && document.readyState === 'complete'")
	}
	clearStorage := func(id string) {
		t.Helper()
		if err := mygo.App.ClearStorage(id); err != nil {
			t.Fatalf("ClearStorage(%q): %v", id, err)
		}
	}
	// The default store is the app's, which other tests use: leave it empty.
	t.Cleanup(func() { mygo.App.ClearBrowsingData() })

	a, b, d := open(idA), open(idB), open("")
	save(a, "A")
	save(b, "B")
	save(d, "D")
	check("A", a, "A|A|A|A")
	check("B", b, "B|B|B|B")
	check("the default store", d, "D|D|D|D")

	// With pages open: A's go, and what they hold in memory until they
	// reload; the others' stay.
	clearStorage(idA)
	check("B after ClearStorage(A)", b, "B|B|B|B")
	check("the default store after ClearStorage(A)", d, "D|D|D|D")
	reload(a)
	reload(b)
	reload(d)
	check("A after ClearStorage(A) and a reload", a, empty)
	check("B after ClearStorage(A) and a reload", b, "B|B|B|B")
	check("the default store after ClearStorage(A) and a reload", d, "D|D|D|D")
	a2 := open(idA)
	check("a new web view of A", a2, empty)

	// With no page of A open, and the store written again.
	save(a2, "A2")
	check("A written again", a2, "A2|A2|A2|A2")
	a.Destroy()
	a2.Destroy()
	clearStorage(idA)
	check("a web view of A made after ClearStorage(A) with no page of A open", open(idA), empty)
	reload(b)
	reload(d)
	check("B after that", b, "B|B|B|B")
	check("the default store after that", d, "D|D|D|D")

	// No store, no page: nothing to clear, and no error.
	clearStorage("e2e-clear-never-" + run)
	if err := mygo.App.ClearStorage(""); !errors.Is(err, mygo.ErrInvalidStorageID) {
		t.Errorf("ClearStorage of the empty ID = %v, want ErrInvalidStorageID", err)
	}
}
