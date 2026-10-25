//go:build !js

package mygo

import (
	"context"
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/egoist/mygo/internal/bridge"
	"github.com/egoist/mygo/internal/platform"
)

// What windows say to the bridge in their pages, and what they hear from
// it. Builds for browsers, whose windows show no pages, leave it out with
// the JSON it takes (bridge_js.go).

// bridgeScript returns the script that installs the bridge in the
// window's pages.
func (w *Window) bridgeScript() string {
	return bridge.Script(bridge.Config{
		Platform: jsPlatform(),
		WindowID: w.top().id, // the window a web view is in
		Version:  Version,
		Secret:   w.secret,
		Locale:   App.Locale(),
		Dir:      appDir(),
		Trust:    w.bridgeTrust(),
	})
}

// bridgeTrust is the rule of isTrusted as data, for the bridge in the page,
// which gives its page none of itself unless the page is trusted. Keep the
// two in step (TestBridgeTrust). It cannot tell which about: document the
// app made, as the core does at NavigationCommitted: the bridge in an
// about: page of a window the app made lets Go judge.
func (w *Window) bridgeTrust() bridge.Trust {
	t := bridge.Trust{
		Schemes:  []string{"file"},
		Origins:  []string{},
		Hosted:   []string{},
		Loopback: IsDev(),
	}
	if !w.popup {
		t.Schemes = append(t.Schemes, "about")
	}
	// The frontend's scheme is among them.
	for _, s := range Protocol.schemes() {
		t.Schemes = append(t.Schemes, s)
		if schemeHosts {
			// How WebView2 names the pages of a custom scheme (schemehost).
			t.Hosted = append(t.Hosted, s)
		}
	}
	if u, err := url.Parse(devURL()); err == nil && u.Host != "" {
		t.Origins = append(t.Origins, strings.ToLower(u.Scheme+"://"+u.Host))
	}
	for _, o := range w.trustedOrigins {
		t.Origins = append(t.Origins, strings.ToLower(strings.TrimSuffix(o, "/")))
	}
	return t
}

// preferencesScriptIndex is the place of preferencesScript in a window's
// user scripts, right after the bridge, which it tells.
const preferencesScriptIndex = 1

// pageScripts are the scripts a page of the window gets at document start:
// the bridge, the preferences of the page and the preload script, which
// the top frame gets, and the one that hides the channel to Go from every
// other frame. The bridge only gives pages that are trusted a window.mygo;
// the preload script runs in all of them.
func (w *Window) pageScripts() []platform.UserScript {
	scripts := []platform.UserScript{{Source: w.bridgeScript()}, {Source: preferencesScript()}}
	if w.preload != "" {
		scripts = append(scripts, platform.UserScript{Source: w.preload})
	}
	return append(scripts, platform.UserScript{Source: bridge.HideScript, AllFrames: true})
}

// localeEvent returns the message of LocaleEvent for locale.
func localeEvent(locale string) (message, error) {
	return encodeEvent(bridge.LocaleEvent, bridge.Locale{Locale: locale, Dir: appDir()})
}

// sendLocale tells the page of a window the language the app shows, which
// the bridge configuration told it at document start: a page that started
// loading as the language changed may have had the one before. The bridge
// tells its listeners only of a language it did not have. Main thread
// only.
func (w *Window) sendLocale() {
	if w.content != nil || w.native == nil {
		return
	}
	if msg, err := localeEvent(App.Locale()); err == nil {
		w.enqueue(msg, true)
	}
}

// pagesLocaleChanged tells the pages of every window the language the app
// shows, and makes it the one of the pages they load later. Main thread
// only.
func pagesLocaleChanged(locale string) {
	msg, err := localeEvent(locale)
	if err != nil {
		return
	}
	for _, w := range pageWindows() {
		if w.content != nil || w.native == nil {
			continue
		}
		w.userScripts = w.pageScripts()
		w.native.SetUserScripts(w.userScripts)
		w.enqueue(msg, true)
	}
}

// preferencesScript tells a page the desktop's preferences at document
// start. Main thread only.
func preferencesScript() string {
	b, _ := json.Marshal(Theme.preferences())
	return bridge.PreferencesScript(b)
}

// sendPreferences tells the pages that the desktop's preferences changed.
// Main thread only.
func sendPreferences() {
	prefs, script := Theme.preferences(), preferencesScript()
	for _, w := range pageWindows() {
		w.sendPreferences(prefs, script)
	}
}

// sendPreferences tells the page of w that the preferences changed, if
// they did: the documents it loads next through its user script, which
// the backend replaces, so that their first paint has them, and the
// current one through the event. Main thread only.
func (w *Window) sendPreferences(prefs ThemePreferences, script string) {
	if w.content != nil || w.native == nil || len(w.userScripts) <= preferencesScriptIndex ||
		w.userScripts[preferencesScriptIndex].Source == script {
		return
	}
	scripts := slices.Clone(w.userScripts)
	scripts[preferencesScriptIndex].Source = script
	w.userScripts = scripts
	w.native.SetUserScripts(scripts)
	if msg, err := encodeEvent(bridge.PreferencesEvent, prefs); err == nil {
		w.enqueue(msg, true)
	}
}

// decodeResult decodes the JSON result of eval into v.
func decodeResult(raw []byte, v any) error { return json.Unmarshal(raw, v) }

const (
	// The expression form: compile errors mean code is not an expression.
	evalExpr = "try{return JSON.stringify({ok:true,v:await(\n%s\n)})}catch(e){return JSON.stringify({ok:false,e:String(e&&e.message||e)})}"
	// The statement form runs code as a function body.
	evalBody = "try{return JSON.stringify({ok:true,v:await(async()=>{\n%s\n})()})}catch(e){return JSON.stringify({ok:false,e:String(e&&e.message||e)})}"
)

// evalCanceled, a hook of tests, runs after eval delivered the
// cancellation of its context.
var evalCanceled func()

// eval returns the JSON result of code, which shares the memory of the
// page's answer.
func (w *Window) eval(ctx context.Context, code string) (jsontext.Value, error) {
	if w.content != nil {
		return nil, errNoPage
	}
	type result struct {
		raw string
		err error
	}
	// Both the cancellation and the page may answer; the first one counts
	// and the other must not block.
	ch := make(chan result, 2)
	stop := context.AfterFunc(ctx, func() {
		deliver(ch, result{err: ctx.Err()})
		if evalCanceled != nil {
			evalCanceled()
		}
	})
	defer stop()

	ran := false
	w.do(func(n platform.Window) {
		ran = true
		expr := strings.TrimRight(code, " \t\r\n;")
		n.CallAsyncFunction(fmt.Sprintf(evalExpr, expr), func(res string, err error) {
			if err == nil {
				deliver(ch, result{raw: res})
				return
			}
			if !strings.HasPrefix(err.Error(), "SyntaxError") {
				// It may have run (and then the page navigated or
				// crashed): running it again could run it twice.
				deliver(ch, result{err: err})
				return
			}
			// Not an expression: run it as a function body instead. The
			// first attempt failed to compile, so nothing ran twice.
			onMain(func() {
				if w.native == nil {
					deliver(ch, result{err: errDestroyed})
					return
				}
				w.native.CallAsyncFunction(fmt.Sprintf(evalBody, code), func(res string, err error) {
					deliver(ch, result{raw: res, err: err})
				})
			})
		})
	})
	if !ran {
		return nil, errDestroyed
	}
	r := await(ch)
	if r.err != nil {
		return nil, r.err
	}
	var out struct {
		OK bool     `json:"ok"`
		V  rawValue `json:"v"`
		E  string   `json:"e"`
	}
	if err := json.Unmarshal(stringBytes(r.raw), &out); err != nil {
		return nil, fmt.Errorf("mygo: unexpected eval result %q: %w", r.raw, err)
	}
	if !out.OK {
		return nil, &EvalError{Message: out.E}
	}
	return jsontext.Value(out.V), nil
}

// sendTitleBar tells the page of a window with a hidden title bar the room
// its controls take. The backend's script tells the first page at document
// start; this keeps the current page, and later ones, up to date. Main
// thread only.
func (w *Window) sendTitleBar() {
	if w.content != nil || !w.hiddenTitleBar || w.native == nil {
		return
	}
	if msg, err := encodeEvent(bridge.TitleBarEvent, bridge.NewTitleBar(w.native.TitleBar(), w.native.Zoom())); err == nil {
		w.enqueue(msg, true)
	}
}

// handleMessage routes a message posted by the page bridge, from the
// document at pageURL. It runs on the main thread.
func (w *Window) handleMessage(msg, pageURL string) {
	msg, ok := strings.CutPrefix(msg, w.secret)
	if !ok {
		return // not from the bridge
	}
	trusted := w.trustsSender(pageURL)
	// Calls are decoded and executed off the main thread. The page context
	// is captured here so a navigation cannot slip in between.
	if len(msg) > 12 && msg[:12] == `{"t":"call",` {
		go handleCall(w, w.pageContext(), msg, trusted)
		return
	}
	if !trusted {
		// Only the bridge of a trusted page has any business with the rest.
		// Pages that are not trusted have no channel to Go, but whatever
		// still reaches it from them stops here.
		return
	}
	var m struct {
		T string  `json:"t"`
		X float64 `json:"x"` // drop
		Y float64 `json:"y"`
		C int64   `json:"c"` // channels
		K string  `json:"k"`
		N int64   `json:"n"`
	}
	if err := json.Unmarshal(stringBytes(msg), &m); err != nil {
		return
	}
	switch m.T {
	case "dom-ready":
		w.outMu.Lock()
		w.domReady = true
		w.docToken = m.K // the document the events held are for
		w.outbox = append(w.outbox, w.held...)
		w.held = nil
		w.outMu.Unlock()
		// The script at document start may tell a later page the room of
		// the controls before a change.
		w.sendTitleBar()
		w.sendLocale()
		w.flush()
		fire(&w.onDOMReady)
		w.readyToShow()
	case "drag":
		if w.native != nil {
			w.native.StartDrag()
		}
	case "dblclick":
		if w.native != nil {
			w.native.TitleBarDoubleClicked()
		}
	case "escape":
		w.escape()
	case "drop":
		if w.native != nil {
			w.filesDropped(w.native.DroppedFiles(), int(m.X), int(m.Y))
		}
	case "chan-ack":
		if c := w.channel(m.C, m.K); c != nil {
			c.ack(m.N)
		}
	case "chan-close":
		w.pageClosedChannel(m.C, m.K)
	}
}

// trustsSender reports whether a message from the document at pageURL is a
// trusted page's: the page showing is trusted, as the last navigation
// decided, and so is the document. The decision for the last URL is kept,
// as pages post many messages, for as long as the protocols are the same.
// A message without a URL is nobody's. Main thread only.
func (w *Window) trustsSender(pageURL string) bool {
	if !w.trusted || pageURL == "" {
		return false
	}
	if gen := Protocol.generation(); pageURL != w.senderURL || gen != w.senderGen {
		w.senderURL, w.senderTrusted, w.senderGen = pageURL, w.isTrusted(pageURL), gen
	}
	return w.senderTrusted
}

// findScript returns the script with which FindInPage asks the bridge, which
// every page has, trusted or not, as window.__mygo.find: the bridge pins it
// at document start, so a page cannot answer for it.
func (w *Window) findScript(text string, opts FindOptions) string {
	t, _ := jsonv1.Marshal(text)
	o, _ := jsonv1.Marshal(map[string]bool{"matchCase": opts.MatchCase, "backward": opts.Backward, "next": opts.FindNext})
	return fmt.Sprintf("window.__mygo.find(%s, %s)", t, o)
}
