//go:build darwin

package darwin

import (
	"errors"

	"github.com/ebitengine/purego/objc"

	"github.com/egoist/mygo/internal/platform"
)

// The embedded web views of a surface's window (ui.WebView): a web view of
// their own, in a subview of the surface's view, beside the area MyGo
// draws into. Each has its own content controller and delegate, as the
// window's web view does, so messages and scripts never leak between them;
// events come back through MyGoEmbeddedWebDelegate, which finds the web
// view by its delegate.

// embeddedWebView is one embedded web view of a surface.
type embeddedWebView struct {
	id        string
	view      id // WKWebView
	ucc       id // WKUserContentController
	delegate  id // MyGoEmbeddedWebDelegate
	s         *surface
	handler   platform.WebViewHandler
	lastRect  platform.RectF
	destroyed bool
}

func (b *Backend) embedFor(delegate id) *embeddedWebView {
	ew := b.byEmbedDelegate[delegate]
	if ew == nil || ew.s.w.closed {
		return nil
	}
	return ew
}

// CreateWebView hosts an embedded web view in the surface.
func (s *surface) CreateWebView(webID string, opts platform.WebViewOptions, handler platform.WebViewHandler) platform.WebView {
	ew := &embeddedWebView{id: webID, s: s, handler: handler}
	b := s.w.b
	withPool(func() {
		cfg := alloc("WKWebViewConfiguration")
		for _, sc := range opts.Schemes {
			send(cfg, "setURLSchemeHandler:forURLScheme:", uintptr(schemeHandler()), uintptr(nsString(sc)))
		}
		prefs := send(cfg, "preferences")
		send(prefs, "setValue:forKey:", uintptr(nsBool(opts.DevTools)), uintptr(nsString("developerExtrasEnabled")))
		send(prefs, "setValue:forKey:", uintptr(nsBool(true)), uintptr(nsString("allowFileAccessFromFileURLs")))
		if respondsTo(prefs, "setElementFullscreenEnabled:") {
			send(prefs, "setElementFullscreenEnabled:", 1)
		}
		// Every embedded web view gets its own content controller, as
		// the window's does: messages, scripts and the bridge never leak
		// between web views or the window's page.
		ew.ucc = alloc("WKUserContentController")
		ew.delegate = alloc("MyGoEmbeddedWebDelegate")
		send(ew.ucc, "addScriptMessageHandler:name:", uintptr(ew.delegate), uintptr(nsString("mygo")))
		for _, sc := range opts.UserScripts {
			injection := uintptr(0) // WKUserScriptInjectionTimeAtDocumentStart
			if sc.AtDocumentEnd {
				injection = 1
			}
			script := send(send(class("WKUserScript"), "alloc"), "initWithSource:injectionTime:forMainFrameOnly:",
				uintptr(nsString(sc.Source)), injection, boolArg(!sc.AllFrames))
			send(ew.ucc, "addUserScript:", uintptr(script))
			release(script)
		}
		send(cfg, "setUserContentController:", uintptr(ew.ucc))
		ew.view = msgInitRectID(send(class("WKWebView"), "alloc"), sel("initWithFrame:configuration:"), NSRect{}, cfg)
		release(cfg)
		send(ew.view, "setNavigationDelegate:", uintptr(ew.delegate))
		send(ew.view, "setUIDelegate:", uintptr(ew.delegate))
		if respondsTo(ew.view, "setInspectable:") {
			send(ew.view, "setInspectable:", boolArg(opts.DevTools))
		}
		if opts.UserAgent != "" {
			send(ew.view, "setCustomUserAgent:", uintptr(nsString(opts.UserAgent)))
		}
		send(ew.view, "addObserver:forKeyPath:options:context:", uintptr(ew.delegate), uintptr(nsString("title")), 1, 0)
	})
	// The surface's view is flipped, so the web view's frame is in
	// top-left coordinates, as the element's box: Move sizes it.
	send(s.view, "addSubview:", uintptr(ew.view))
	b.byEmbedDelegate[ew.delegate] = ew
	s.webviews = append(s.webviews, ew)
	return ew
}

func (ew *embeddedWebView) LoadURL(url string) {
	withPool(func() {
		req := send(class("NSURLRequest"), "requestWithURL:", uintptr(nsURL(url)))
		send(ew.view, "loadRequest:", uintptr(req))
	})
}

func (ew *embeddedWebView) LoadHTML(html, baseURL string) {
	withPool(func() {
		var base id
		if baseURL != "" {
			base = nsURL(baseURL)
		}
		send(ew.view, "loadHTMLString:baseURL:", uintptr(nsString(html)), uintptr(base))
	})
}

func (ew *embeddedWebView) Reload() { send(ew.view, "reload") }

func (ew *embeddedWebView) Eval(js string) {
	withPool(func() {
		send(ew.view, "evaluateJavaScript:completionHandler:", uintptr(nsString(js)), 0)
	})
}

// Move sets the subview's frame, which the surface's flipped view takes
// in top-left coordinates; it does nothing where the frame does not
// change, as the frame asks every one.
func (ew *embeddedWebView) Move(r platform.RectF) {
	if r == ew.lastRect {
		return
	}
	ew.lastRect = r
	msgSetRect(ew.view, sel("setFrame:"), NSRect{Origin: NSPoint{X: r.X, Y: r.Y}, Size: NSSize{Width: r.W, Height: r.H}})
}

func (ew *embeddedWebView) Destroy() {
	if ew.destroyed {
		return
	}
	ew.destroyed = true
	s := ew.s
	for i, w := range s.webviews {
		if w == ew {
			s.webviews = append(s.webviews[:i], s.webviews[i+1:]...)
			break
		}
	}
	withPool(func() {
		send(ew.view, "removeObserver:forKeyPath:", uintptr(ew.delegate), uintptr(nsString("title")))
		send(ew.ucc, "removeScriptMessageHandlerForName:", uintptr(nsString("mygo")))
	})
	send(ew.ucc, "removeAllUserScripts")
	send(ew.view, "stopLoading")
	send(ew.view, "setNavigationDelegate:", 0)
	send(ew.view, "setUIDelegate:", 0)
	send(ew.view, "removeFromSuperview")
	delete(s.w.b.byEmbedDelegate, ew.delegate)
	release(ew.ucc)
	release(ew.view)
	release(ew.delegate)
}

func (ew *embeddedWebView) loadFailed(err id) {
	code := sendInt(err, "code")
	// Cancelled (-999) and "frame load interrupted" (102) happen when a
	// navigation is replaced or denied; they are not failures.
	if code == -999 || code == 102 {
		return
	}
	var desc string
	withPool(func() { desc = goString(send(err, "localizedDescription")) })
	if desc == "" {
		desc = "the web view's load failed"
	}
	ew.handler.LoadFailed(errors.New(desc))
}

func registerEmbeddedWebClasses() {
	b := func() *Backend { return theBackend }
	classDef("MyGoEmbeddedWebDelegate", "NSObject",
		[]string{"WKNavigationDelegate", "WKUIDelegate", "WKScriptMessageHandler"},
		[]objc.MethodDef{
			method("userContentController:didReceiveScriptMessage:", func(self id, _ objc.SEL, ucc, msg id) {
				ew := b().embedFor(self)
				if ew == nil {
					return
				}
				// The handler is reachable from every frame; only the
				// page's own bridge may talk to Go.
				if frame := send(msg, "frameInfo"); frame == 0 || !sendBool(frame, "isMainFrame") {
					return
				}
				body := send(msg, "body")
				if body == 0 || !sendBool(body, "isKindOfClass:", uintptr(class("NSString"))) {
					return
				}
				ew.handler.Message(goString(body))
			}),
			method("observeValueForKeyPath:ofObject:change:context:", func(self id, _ objc.SEL, keyPath, object, change id, ctx uintptr) {
				if ew := b().embedFor(self); ew != nil && goString(keyPath) == "title" {
					ew.handler.TitleChanged(goString(send(object, "title")))
				}
			}),
			method("webView:decidePolicyForNavigationAction:decisionHandler:", func(self id, _ objc.SEL, web, action id, handler uintptr) {
				allow := true
				if ew := b().embedFor(self); ew != nil {
					allow = ew.handler.WillNavigate(goString(send(send(send(action, "request"), "URL"), "absoluteString")))
				}
				callBlock(handler, boolArg(allow))
			}),
			// A link the page opens in a new context (window.open,
			// target=_blank): the web view does not open it; the content
			// decides where it goes.
			method("webView:createWebViewWithConfiguration:forNavigationAction:windowFeatures:", func(self id, _ objc.SEL, web, cfg, action, features id) id {
				if ew := b().embedFor(self); ew != nil {
					ew.handler.Link(goString(send(send(send(action, "request"), "URL"), "absoluteString")))
				}
				return 0
			}),
			method("webView:didFinishNavigation:", func(self id, _ objc.SEL, web, nav id) {
				if ew := b().embedFor(self); ew != nil {
					ew.handler.LoadFinished()
				}
			}),
			method("webView:didFailProvisionalNavigation:withError:", func(self id, _ objc.SEL, web, nav, err id) {
				if ew := b().embedFor(self); ew != nil {
					ew.loadFailed(err)
				}
			}),
			method("webView:didFailNavigation:withError:", func(self id, _ objc.SEL, web, nav, err id) {
				if ew := b().embedFor(self); ew != nil {
					ew.loadFailed(err)
				}
			}),
			// The page's dialogs, on the window that hosts the surface.
			method("webView:runJavaScriptAlertPanelWithMessage:initiatedByFrame:completionHandler:", func(self id, _ objc.SEL, web, message, frame id, handler uintptr) {
				if ew := b().embedFor(self); ew != nil {
					jsAlert(ew.s.w, goString(message), handler)
				}
			}),
			method("webView:runJavaScriptConfirmPanelWithMessage:initiatedByFrame:completionHandler:", func(self id, _ objc.SEL, web, message, frame id, handler uintptr) {
				if ew := b().embedFor(self); ew != nil {
					jsConfirm(ew.s.w, goString(message), handler)
				}
			}),
			method("webView:runJavaScriptTextInputPanelWithPrompt:defaultText:initiatedByFrame:completionHandler:", func(self id, _ objc.SEL, web, prompt, defaultText, frame id, handler uintptr) {
				if ew := b().embedFor(self); ew != nil {
					jsPrompt(ew.s.w, goString(prompt), goString(defaultText), handler)
				}
			}),
		})
}
