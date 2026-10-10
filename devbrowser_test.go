package mygo

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

// devBrowserServer serves browser tabs of a frontend made of files.
func devBrowserServer(t *testing.T, files fstest.MapFS) *httptest.Server {
	t.Helper()
	SetFrontend(files)
	t.Cleanup(func() { SetFrontend(nil) })
	srv := httptest.NewServer(devBrowserHandler())
	t.Cleanup(srv.Close)
	return srv
}

// testTab is a browser tab: the bridge it loaded and the events it hears.
type testTab struct {
	t      *testing.T
	base   string
	secret string
	cancel context.CancelFunc
	msgs   chan map[string]any
}

func tabGet(t *testing.T, url string, header ...string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", url, nil)
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	return res, string(b)
}

// openTab loads the bridge, as the page of a tab does, and connects to
// its events.
func openTab(t *testing.T, srv *httptest.Server) *testTab {
	t.Helper()
	res, js := tabGet(t, srv.URL+"/__mygo/bridge.js", "Sec-Fetch-Site", "same-origin")
	if res.StatusCode != 200 || !strings.Contains(js, `"endpoint":"/__mygo/"`) {
		t.Fatalf("bridge.js: %d %.200s", res.StatusCode, js)
	}
	m := regexp.MustCompile(`"secret":"([^"]+)"`).FindStringSubmatch(js)
	if m == nil {
		t.Fatal("no secret in the bridge")
	}
	ctx, cancel := context.WithCancel(context.Background())
	tab := &testTab{t: t, base: srv.URL, secret: m[1], cancel: cancel, msgs: make(chan map[string]any, 256)}
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/__mygo/events?s="+tab.secret, nil)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	ev, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if ev.StatusCode != 200 || ev.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("events: %d %s", ev.StatusCode, ev.Header.Get("Content-Type"))
	}
	go func() {
		defer ev.Body.Close()
		sc := bufio.NewScanner(ev.Body)
		sc.Buffer(nil, 1<<20)
		var data strings.Builder
		for sc.Scan() {
			line := sc.Text()
			if v, ok := strings.CutPrefix(line, "data: "); ok {
				data.WriteString(v)
				continue
			}
			if line != "" || data.Len() == 0 {
				continue
			}
			var batch []map[string]any
			if err := json.Unmarshal([]byte(data.String()), &batch); err != nil {
				panic(fmt.Sprintf("bad batch %q: %v", data.String(), err))
			}
			data.Reset()
			for _, m := range batch {
				tab.msgs <- m
			}
		}
	}()
	return tab
}

// post sends messages as the tab's bridge does, and returns the status.
func (tab *testTab) post(msgs ...string) int {
	tab.t.Helper()
	return tab.postWith(tab.base, msgs...)
}

func (tab *testTab) postWith(origin string, msgs ...string) int {
	tab.t.Helper()
	for i, m := range msgs {
		msgs[i] = tab.secret + m
	}
	body, _ := json.Marshal(msgs)
	req, _ := http.NewRequest("POST", tab.base+"/__mygo/post?s="+tab.secret, strings.NewReader(string(body)))
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		tab.t.Fatal(err)
	}
	res.Body.Close()
	return res.StatusCode
}

// wait returns the next message the tab hears that matches pred.
func (tab *testTab) wait(pred func(m map[string]any) bool) map[string]any {
	tab.t.Helper()
	timeout := time.After(3 * time.Second)
	for {
		select {
		case m := <-tab.msgs:
			if pred(m) {
				return m
			}
		case <-timeout:
			tab.t.Fatal("message not received")
			return nil
		}
	}
}

func (tab *testTab) call(id int, method string, args ...any) map[string]any {
	tab.t.Helper()
	if args == nil {
		args = []any{}
	}
	a, _ := json.Marshal(args)
	if code := tab.post(fmt.Sprintf(`{"t":"call","id":%d,"k":"tok","m":%q,"a":%s}`, id, method, a)); code != http.StatusNoContent {
		tab.t.Fatalf("post: %d", code)
	}
	return tab.wait(func(m map[string]any) bool { return m["t"] == "reply" && m["id"] == float64(id) })
}

func TestDevBrowserServesPagesWithTheBridge(t *testing.T) {
	srv := devBrowserServer(t, fstest.MapFS{
		"index.html": {Data: []byte(`<!doctype html><html lang="en"><head><title>x</title></head><body></body></html>`)},
		"app.js":     {Data: []byte(`console.log("<head>")`)},
	})
	res, body := tabGet(t, srv.URL+"/")
	want := `<head><script src="/__mygo/bridge.js"></script><title>`
	if res.StatusCode != 200 || !strings.Contains(body, want) {
		t.Errorf("index: %d %q, want %q in it", res.StatusCode, body, want)
	}
	if res.ContentLength != int64(len(body)) {
		t.Errorf("Content-Length %d, body %d bytes", res.ContentLength, len(body))
	}
	// Client-side routes get the page too.
	if _, body := tabGet(t, srv.URL+"/settings"); !strings.Contains(body, want) {
		t.Errorf("route: %q", body)
	}
	if _, body := tabGet(t, srv.URL+"/app.js"); body != `console.log("<head>")` {
		t.Errorf("script changed: %q", body)
	}
	// The page the browser has keeps the bridge.
	_, _ = tabGet(t, srv.URL+"/")
	res, _ = tabGet(t, srv.URL+"/index.html", "If-Modified-Since", time.Now().Add(time.Hour).UTC().Format(http.TimeFormat))
	if res.StatusCode != http.StatusNotModified && res.StatusCode != http.StatusOK {
		t.Errorf("conditional request: %d", res.StatusCode)
	}
}

func TestInjectBridge(t *testing.T) {
	for in, want := range map[string]string{
		`<html><HEAD lang="x"><title>`: `<html><HEAD lang="x">` + bridgeTag + `<title>`,
		`<html><body>`:                 `<html>` + bridgeTag + `<body>`,
		`<p>hi`:                        bridgeTag + `<p>hi`,
		`<header></header>`:            bridgeTag + `<header></header>`,
	} {
		if got := string(injectBridge([]byte(in))); got != want {
			t.Errorf("injectBridge(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDevBrowserCalls(t *testing.T) {
	bindForTest(t, "Svc", &testService{})
	w, _ := readyWindow(t, WindowOptions{})
	srv := devBrowserServer(t, fstest.MapFS{})
	tab := openTab(t, srv)

	if r := tab.call(1, "Svc.Hello", "tab"); r["ok"] != true || r["v"] != "hello tab" || r["k"] != "tok" {
		t.Errorf("Hello: %v", r)
	}
	if r := tab.call(2, "Svc.Fail"); r["ok"] != false || r["e"] != "nope" {
		t.Errorf("Fail: %v", r)
	}
	// Calls report the first window that shows a page as theirs.
	var first *Window
	for _, x := range Windows() {
		if x.content == nil && !x.IsDestroyed() {
			first = x
			break
		}
	}
	if r := tab.call(3, "Svc.Caller"); r["v"] != float64(first.ID()) {
		t.Errorf("Caller: %v, want window %d (created %d)", r, first.ID(), w.ID())
	}
}

func TestDevBrowserEvents(t *testing.T) {
	ev := newEventForTest[progress](t, "test:browser")
	readyWindow(t, WindowOptions{})
	srv := devBrowserServer(t, fstest.MapFS{})
	tab := openTab(t, srv)
	if tab.post(`{"t":"dom-ready"}`) != http.StatusNoContent {
		t.Fatal("dom-ready")
	}
	isEvent := func(done float64) func(m map[string]any) bool {
		return func(m map[string]any) bool {
			p, _ := m["p"].(map[string]any)
			return m["t"] == "event" && m["n"] == "test:browser" && p["done"] == done
		}
	}
	if err := ev.Broadcast(progress{Done: 1, Total: 2}); err != nil {
		t.Fatal(err)
	}
	tab.wait(isEvent(1))
	// Events emitted to the window the tab stands in for.
	var target *Window
	for _, x := range Windows() {
		if x.content == nil && !x.IsDestroyed() {
			target = x
			break
		}
	}
	if err := ev.Emit(target, progress{Done: 2, Total: 2}); err != nil {
		t.Fatal(err)
	}
	tab.wait(isEvent(2))
}

func TestDevBrowserChannels(t *testing.T) {
	s := &streamer{sent: make(chan error, 8), stopped: make(chan error, 1)}
	bindForTest(t, "Stream", s)
	srv := devBrowserServer(t, fstest.MapFS{})
	tab := openTab(t, srv)
	tab.post(`{"t":"call","id":1,"k":"tok","m":"Stream.Count","a":[3,7]}`)
	var got []any
	for {
		m := tab.wait(func(m map[string]any) bool { return m["t"] == "chan" || m["t"] == "reply" })
		if m["t"] == "reply" {
			if m["ok"] != true {
				t.Fatalf("reply: %v", m)
			}
			break
		}
		if m["c"] != float64(7) {
			t.Fatalf("channel %v", m["c"])
		}
		if m["end"] == true {
			got = append(got, "end")
		} else {
			got = append(got, m["p"])
		}
	}
	if fmt.Sprint(got) != "[0 1 2 end]" {
		t.Errorf("values: %v", got)
	}

	// A tab that goes away cancels its calls, as a page navigating does.
	tab.post(`{"t":"call","id":2,"k":"tok","m":"Stream.Wait","a":[8]}`)
	tab.wait(func(m map[string]any) bool { return m["t"] == "chan" && m["p"] == "ready" })
	tab.cancel()
	select {
	case err := <-s.stopped:
		if err != context.Canceled {
			t.Errorf("Wait stopped with %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the call outlived its tab")
	}
	if code := tab.post(`{"t":"dom-ready"}`); code != http.StatusGone {
		t.Errorf("post to an ended session: %d", code)
	}
}

func TestDevBrowserRejectsOtherSites(t *testing.T) {
	bindForTest(t, "Svc", &testService{})
	srv := devBrowserServer(t, fstest.MapFS{"index.html": {Data: []byte("<head>")}})
	tab := openTab(t, srv)

	req, _ := http.NewRequest("GET", srv.URL+"/", nil)
	req.Host = "evil.example:80" // DNS rebinding
	if res, err := http.DefaultClient.Do(req); err != nil || res.StatusCode != http.StatusForbidden {
		t.Errorf("foreign Host: %v %v", res.StatusCode, err)
	}
	if res, _ := tabGet(t, srv.URL+"/__mygo/bridge.js", "Sec-Fetch-Site", "cross-site"); res.StatusCode != http.StatusForbidden {
		t.Errorf("bridge.js for another site: %d", res.StatusCode)
	}
	if res, _ := tabGet(t, srv.URL+"/__mygo/events?s="+tab.secret, "Origin", "https://evil.example"); res.StatusCode != http.StatusForbidden {
		t.Errorf("events for another site: %d", res.StatusCode)
	}
	call := `{"t":"call","id":1,"k":"tok","m":"Svc.Hello","a":["x"]}`
	if code := tab.postWith("https://evil.example", call); code != http.StatusForbidden {
		t.Errorf("post from another site: %d", code)
	}
	if code := tab.postWith("", call); code != http.StatusForbidden {
		t.Errorf("post without Origin: %d", code)
	}
	if res, _ := tabGet(t, srv.URL+"/__mygo/events?s=unknown", "Sec-Fetch-Site", "same-origin"); res.StatusCode != http.StatusNoContent {
		t.Errorf("events of an unknown session: %d", res.StatusCode)
	}
	// A second connection does not take the tab's events.
	if res, _ := tabGet(t, srv.URL+"/__mygo/events?s="+tab.secret, "Sec-Fetch-Site", "same-origin"); res.StatusCode != http.StatusNoContent {
		t.Errorf("second events connection: %d", res.StatusCode)
	}
	// Messages without the session's secret are ignored.
	body := `["{\"t\":\"call\",\"id\":9,\"k\":\"tok\",\"m\":\"Svc.Hello\",\"a\":[\"x\"]}"]`
	req, _ = http.NewRequest("POST", srv.URL+"/__mygo/post?s="+tab.secret, strings.NewReader(body))
	req.Header.Set("Origin", srv.URL)
	if res, err := http.DefaultClient.Do(req); err != nil || res.StatusCode != http.StatusNoContent {
		t.Fatalf("post: %v", err)
	}
	if r := tab.call(10, "Svc.Hello", "y"); r["v"] != "hello y" {
		t.Errorf("Hello: %v", r)
	}
	select {
	case m := <-tab.msgs:
		t.Errorf("unexpected message %v", m)
	default:
	}
}

func TestIsLoopbackHost(t *testing.T) {
	for host, want := range map[string]bool{
		"localhost:5199": true, "LOCALHOST": true, "127.0.0.1:1": true, "[::1]:8080": true, "127.1.2.3": true,
		"example.com": false, "localhost.example.com:80": false, "192.168.1.2:5199": false, "": false,
	} {
		if got := isLoopbackHost(host); got != want {
			t.Errorf("isLoopbackHost(%q) = %v", host, got)
		}
	}
}

func TestDevServerProxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/main.js" {
			w.Header().Set("Content-Type", "text/javascript")
			io.WriteString(w, "export {}")
			return
		}
		w.Header().Set("Content-Type", "text/html")
		var out io.Writer = w
		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			w.Header().Set("Content-Encoding", "gzip")
			gz := gzip.NewWriter(w)
			defer gz.Close()
			out = gz
		}
		io.WriteString(out, "<!doctype html><html><head>")
		w.(http.Flusher).Flush() // chunked
		io.WriteString(out, `<script type="module" src="/main.js"></script></head></html>`)
	}))
	defer upstream.Close()
	target, _ := url.Parse(upstream.URL)
	srv := httptest.NewServer(devServerProxy(target))
	defer srv.Close()

	// As a browser asks, ready to decompress itself.
	res, body := tabGet(t, srv.URL+"/", "Accept-Encoding", "gzip, br")
	want := `<head><script src="/__mygo/bridge.js"></script><script type="module" src="/main.js">`
	if res.StatusCode != 200 || !strings.Contains(body, want) || res.ContentLength != int64(len(body)) || res.Header.Get("Content-Encoding") != "" {
		t.Errorf("page: %d, Content-Length %d, Content-Encoding %q, %q", res.StatusCode, res.ContentLength, res.Header.Get("Content-Encoding"), body)
	}
	if _, body := tabGet(t, srv.URL+"/main.js"); body != "export {}" {
		t.Errorf("script: %q", body)
	}
}
