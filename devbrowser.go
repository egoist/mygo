package mygo

import (
	"bytes"
	"crypto/rand"
	"encoding/json/v2"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/internal/bridge"
)

// While `mygo dev` runs the app, the app also serves its frontend to
// regular browsers at MYGO_DEV_BROWSER (a loopback address such as
// 127.0.0.1:5199), with the bridge in front of every HTML page, so that a
// browser tab calls Go, streams channels and hears events as a window's
// page does: its developer tools and extensions work on the real app.
//
// The server forwards everything to the dev server (devUrl), or serves the
// frontend the mygo scheme serves when there is none. Under /__mygo/ it
// serves the bridge (bridge.js), which starts a session for the tab, the
// messages for the tab as server-sent events (events), takes the tab's
// messages in POST requests (post), and tells a tab whose app went away
// that it is back (ping).
const devBrowserPath = "/__mygo/"

// devBrowserEnv is how `mygo dev` tells the app where to serve browsers.
const devBrowserEnv = "MYGO_DEV_BROWSER"

var devBrowser struct {
	sync.Mutex
	tabs   map[string]*browserTab
	nextID int
}

// browserTab is a session of a browser tab: the page that loaded
// bridge.js.
type browserTab struct {
	link    *pageLink
	created time.Time

	mu        sync.Mutex
	batches   []string      // for the tab, each a JSON array of messages
	wake      chan struct{} // signaled when batches gets one
	connected bool          // to events, which is then the tab's
}

// unconnectedTabTTL is how long a tab may take to connect to events after
// loading the bridge.
const unconnectedTabTTL = time.Minute

// startDevBrowser starts the server of browser tabs if `mygo dev` asked
// for one.
func startDevBrowser() {
	addr := os.Getenv(devBrowserEnv)
	os.Unsetenv(devBrowserEnv) // not meant for child processes
	if addr == "" || !launchedByDev() {
		return
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("mygo: cannot serve the app to browsers: %v", err)
		return
	}
	go func() {
		srv := &http.Server{Handler: devBrowserHandler(), ReadHeaderTimeout: 10 * time.Second}
		if err := srv.Serve(ln); err != nil {
			log.Printf("mygo: serving the app to browsers: %v", err)
		}
	}()
	go sweepBrowserTabs()
}

// devBrowserHandler serves browser tabs.
func devBrowserHandler() http.Handler {
	frontend := browserFrontend()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only to this machine, whatever name resolves to it: DNS
		// rebinding must not let a web site in.
		if !isLoopbackHost(r.Host) {
			http.Error(w, "mygo dev serves the app on localhost only", http.StatusForbidden)
			return
		}
		name, ok := strings.CutPrefix(r.URL.Path, devBrowserPath)
		if !ok {
			frontend.ServeHTTP(w, r)
			return
		}
		// Bound methods are not for other sites the browser has open.
		if !sameOrigin(r) {
			http.Error(w, "cross-origin request", http.StatusForbidden)
			return
		}
		switch name {
		case "bridge.js":
			serveBridge(w)
		case "events":
			serveEvents(w, r)
		case "post":
			servePost(w, r)
		case "ping":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})
}

// isLoopbackHost reports whether host (of a Host header) names this
// machine.
func isLoopbackHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// sameOrigin reports whether r comes from a page of this server, or from
// no page at all (the address bar). Browsers send Origin with POST
// requests and cross-origin ones, and Sec-Fetch-Site with every request.
func sameOrigin(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "", "same-origin", "none":
	default:
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return r.Method == http.MethodGet
	}
	return strings.EqualFold(origin, "http://"+r.Host)
}

// browserFrontend returns the handler of everything but the bridge: the
// dev server, or what the mygo scheme serves.
func browserFrontend() http.Handler {
	if d := devURL(); d != "" {
		if target, err := url.Parse(d); err == nil && target.Host != "" {
			return devServerProxy(target)
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Protocol.mu.RLock()
		h := Protocol.handlers[frontendScheme]
		Protocol.mu.RUnlock()
		if h == nil {
			h = frontendHandler()
		}
		iw := &injectingWriter{ResponseWriter: w}
		h.ServeHTTP(iw, r)
		iw.finish()
	})
}

// devServerProxy forwards requests to the dev server, WebSocket upgrades
// (hot reload) included, and adds the bridge to its HTML pages.
func devServerProxy(target *url.URL) http.Handler {
	return &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			r.Out.Host = target.Host
			// Without the browser's Accept-Encoding, the transport asks
			// for gzip itself and decompresses, so pages can be read to
			// add the bridge.
			r.Out.Header.Del("Accept-Encoding")
		},
		ModifyResponse: func(res *http.Response) error {
			if !isHTML(res.Header) || res.Body == nil {
				return nil
			}
			b, err := io.ReadAll(res.Body)
			res.Body.Close()
			if err != nil {
				return err
			}
			b = injectBridge(b)
			res.Body = io.NopCloser(bytes.NewReader(b))
			res.ContentLength = int64(len(b))
			res.TransferEncoding = nil
			res.Header.Set("Content-Length", strconv.Itoa(len(b)))
			res.Header.Del("Etag")
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			http.Error(w, "mygo: the dev server at "+target.String()+" does not answer: "+err.Error(), http.StatusBadGateway)
		},
	}
}

func isHTML(h http.Header) bool {
	return strings.HasPrefix(strings.ToLower(h.Get("Content-Type")), "text/html")
}

var (
	headTag = regexp.MustCompile(`(?i)<head(\s[^>]*)?>`)
	htmlTag = regexp.MustCompile(`(?i)<html(\s[^>]*)?>`)
)

// bridgeTag loads the bridge before the page's own scripts: a classic
// script, which runs as soon as it is parsed.
const bridgeTag = `<script src="` + devBrowserPath + `bridge.js"></script>`

// injectBridge adds the bridge to an HTML page, at the start of its head.
func injectBridge(page []byte) []byte {
	at := 0
	if loc := headTag.FindIndex(page); loc != nil {
		at = loc[1]
	} else if loc := htmlTag.FindIndex(page); loc != nil {
		at = loc[1]
	}
	out := make([]byte, 0, len(page)+len(bridgeTag))
	out = append(out, page[:at]...)
	out = append(out, bridgeTag...)
	return append(out, page[at:]...)
}

// injectingWriter adds the bridge to the HTML pages of a handler.
type injectingWriter struct {
	http.ResponseWriter
	code    int
	html    bool
	decided bool
	buf     bytes.Buffer
}

func (w *injectingWriter) WriteHeader(code int) {
	if w.decided {
		return
	}
	w.decided, w.code = true, code
	// Not a 304, whose page the browser has, with the bridge.
	w.html = code == http.StatusOK && isHTML(w.Header())
	if !w.html {
		w.ResponseWriter.WriteHeader(code)
		return
	}
	w.Header().Del("Content-Length")
	w.Header().Del("Etag")
}

func (w *injectingWriter) Write(b []byte) (int, error) {
	if !w.decided {
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", http.DetectContentType(b))
		}
		w.WriteHeader(http.StatusOK)
	}
	if w.html {
		return w.buf.Write(b)
	}
	return w.ResponseWriter.Write(b)
}

// finish sends a buffered page, with the bridge.
func (w *injectingWriter) finish() {
	if !w.html {
		return
	}
	b := injectBridge(w.buf.Bytes())
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	w.ResponseWriter.WriteHeader(w.code)
	_, _ = w.ResponseWriter.Write(b)
}

// serveBridge starts a session for a tab and sends it the bridge
// configured for it.
func serveBridge(w http.ResponseWriter) {
	secret := rand.Text()
	// Calls report the first window that shows a page as theirs, so that
	// CallerWindow, dialogs attached to it, Emit and the window controls
	// work as in that window.
	var win *Window
	for _, x := range Windows() {
		if x.content == nil && !x.IsDestroyed() {
			win = x
			break
		}
	}
	tab := &browserTab{created: time.Now(), wake: make(chan struct{}, 1)}
	devBrowser.Lock()
	devBrowser.nextID++
	name := "browser tab " + strconv.Itoa(devBrowser.nextID)
	tab.link = newPageLink(win, name, tab.deliver)
	if devBrowser.tabs == nil {
		devBrowser.tabs = map[string]*browserTab{}
	}
	devBrowser.tabs[secret] = tab
	devBrowser.Unlock()

	id := 0
	if win != nil {
		id = win.id
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, bridge.Script(bridge.Config{
		Platform: jsPlatform(),
		WindowID: id,
		Version:  Version,
		Secret:   secret,
		Endpoint: devBrowserPath,
	}))
}

// deliver queues a batch of messages, a __mygo.receive([...]) script, for
// the tab.
func (t *browserTab) deliver(js string) {
	batch := strings.TrimSuffix(strings.TrimPrefix(js, receivePrefix[:len(receivePrefix)-1]), receiveSuffix[1:])
	t.mu.Lock()
	t.batches = append(t.batches, batch)
	t.mu.Unlock()
	select {
	case t.wake <- struct{}{}:
	default:
	}
}

func lookupTab(r *http.Request) *browserTab {
	devBrowser.Lock()
	defer devBrowser.Unlock()
	return devBrowser.tabs[r.URL.Query().Get("s")]
}

// endTab ends the session of a tab, as a window's page ends when it
// navigates: its calls are canceled and its channels closed.
func endTab(secret string, t *browserTab) {
	devBrowser.Lock()
	if devBrowser.tabs[secret] == t {
		delete(devBrowser.tabs, secret)
	}
	devBrowser.Unlock()
	t.link.end()
}

// serveEvents streams the messages for a tab, until it goes away.
func serveEvents(w http.ResponseWriter, r *http.Request) {
	secret := r.URL.Query().Get("s")
	t := lookupTab(r)
	if t == nil {
		// Unknown, e.g. from before mygo dev rebuilt the app: 204 stops
		// EventSource from reconnecting, and the tab reloads.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	t.mu.Lock()
	taken := t.connected
	t.connected = true
	t.mu.Unlock()
	if taken {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	defer endTab(secret, t)
	flusher, _ := w.(http.Flusher)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if _, err := io.WriteString(w, ": connected\n\n"); err != nil {
		return
	}
	for {
		t.mu.Lock()
		batches := t.batches
		t.batches = nil
		t.mu.Unlock()
		var out []byte
		for _, b := range batches {
			// Each line of an event's data is a data field.
			for line := range strings.SplitSeq(b, "\n") {
				out = append(out, "data: "...)
				out = append(out, line...)
				out = append(out, '\n')
			}
			out = append(out, '\n')
		}
		if len(out) > 0 {
			if _, err := w.Write(out); err != nil {
				return
			}
		}
		if flusher != nil {
			flusher.Flush()
		}
		select {
		case <-t.wake:
		case <-r.Context().Done():
			return
		}
	}
}

// servePost takes the messages of a tab, a JSON array of the strings its
// bridge posted, in order.
func servePost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	secret := r.URL.Query().Get("s")
	t := lookupTab(r)
	if t == nil {
		http.Error(w, "unknown session", http.StatusGone)
		return
	}
	var msgs []string
	if err := json.UnmarshalRead(r.Body, &msgs); err != nil {
		http.Error(w, "malformed messages", http.StatusBadRequest)
		return
	}
	for _, msg := range msgs {
		msg, ok := strings.CutPrefix(msg, secret)
		if !ok {
			continue
		}
		var m linkMessage
		if t.link.receive(msg, true, &m) {
			continue
		}
		// The rest concerns a window: a tab has no title bar to drag or
		// files dropped from the system.
		if m.T == "dom-ready" {
			t.link.ready()
			postMain(t.link.flush)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// sweepBrowserTabs ends the sessions of tabs that loaded the bridge but
// never connected, e.g. pages closed while they loaded.
func sweepBrowserTabs() {
	for range time.Tick(unconnectedTabTTL / 2) {
		var stale map[string]*browserTab
		devBrowser.Lock()
		for secret, t := range devBrowser.tabs {
			t.mu.Lock()
			old := !t.connected && time.Since(t.created) > unconnectedTabTTL
			t.mu.Unlock()
			if old {
				if stale == nil {
					stale = map[string]*browserTab{}
				}
				stale[secret] = t
			}
		}
		devBrowser.Unlock()
		for secret, t := range stale {
			endTab(secret, t)
		}
	}
}

// browserTabsOf returns the links of the tabs whose calls report w as
// theirs; with all, those of every tab.
func browserTabsOf(w *Window, all bool) []*pageLink {
	devBrowser.Lock()
	defer devBrowser.Unlock()
	var out []*pageLink
	for _, t := range devBrowser.tabs {
		if all || t.link.w == w {
			out = append(out, t.link)
		}
	}
	return out
}
