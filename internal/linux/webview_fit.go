//go:build linux && (amd64 || arm64)

package linux

import (
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"

	"github.com/egoist/mygo/internal/platform"
)

// The page of a web view whose size follows its content measures it with
// a script of the core in a script world of its own (FitWorld), in the top
// frame: WebKitGTK gives a world its own JavaScript objects over the
// page's DOM, and a script message handler registered in it
// (FitHandler) is window.webkit.messageHandlers of that world alone, so
// the page's scripts, the bridge's world, can't post to it. Bound the first
// time a page measures, so that other apps load and link none of it.

var (
	fitOnce sync.Once

	webkitUserScriptNewForWorld                    func(src *byte, frames, time int32, world *byte, allow, block ptr) ptr
	webkitUserContentManagerRegisterHandlerInWorld func(ucm ptr, name, world *byte) bool
	webkitWebViewRunJavascriptInWorld              func(wv ptr, script, world *byte, cancellable, cb, data ptr)

	// cbFitMessage handles what the script in the world posts.
	cbFitMessage ptr
)

// bindFit binds WebKitGTK's script worlds, from 2.22 on, and reports
// whether it has them.
func bindFit() bool {
	fitOnce.Do(func() {
		w := libWebKit
		if !bind(w, &webkitUserScriptNewForWorld, "webkit_user_script_new_for_world") ||
			!bind(w, &webkitUserContentManagerRegisterHandlerInWorld, "webkit_user_content_manager_register_script_message_handler_in_world") {
			return
		}
		// Before 2.40, which has webkit_web_view_evaluate_javascript.
		bind(w, &webkitWebViewRunJavascriptInWorld, "webkit_web_view_run_javascript_in_world")
		cbFitMessage = purego.NewCallback(func(ucm, result, data ptr) {
			if w := theBackend.window(data); w != nil {
				w.h.ContentSize(takeStr(jscValueToString(webkitJavascriptResultGetJSValue(result))))
			}
		})
	})
	return cbFitMessage != 0
}

// MeasureContent runs script in the world FitWorld of the page's top
// frame, in each document from the start, and in the one it shows now.
func (w *window) MeasureContent(script string) bool {
	if w.fitScript != 0 {
		return true
	}
	if w.closed || w.ucm == 0 || !bindFit() ||
		!webkitUserContentManagerRegisterHandlerInWorld(w.ucm, cs(platform.FitHandler), cs(platform.FitWorld)) {
		return false
	}
	connect(w.ucm, "script-message-received::"+platform.FitHandler, cbFitMessage, ptr(w.id))
	// The top frame, at document start; SetUserScripts adds it again.
	w.fitScript = webkitUserScriptNewForWorld(cs(script), 1, 0, cs(platform.FitWorld), 0, 0)
	webkitUserContentManagerAddScript(w.ucm, w.fitScript)
	switch {
	case webkitWebViewEvaluateJavascript != nil:
		webkitWebViewEvaluateJavascript(w.web, unsafe.StringData(script), len(script), cs(platform.FitWorld), nil, 0, 0, 0)
	case webkitWebViewRunJavascriptInWorld != nil:
		webkitWebViewRunJavascriptInWorld(w.web, cs(script), cs(platform.FitWorld), 0, 0, 0)
	}
	return true
}
