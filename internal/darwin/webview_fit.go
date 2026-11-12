//go:build darwin

package darwin

import "github.com/egoist/mygo/internal/platform"

// The page of a web view whose size follows its content measures it with
// a script of the core in a WKContentWorld of its own (FitWorld, macOS
// 11+), in the main frame: the world has its own JavaScript objects over
// the page's DOM, and the script message handler added in it
// (FitHandler) is window.webkit.messageHandlers of that world alone, so
// the page's scripts, in the page world with the bridge, can't post to
// it. The delegate takes its messages from the main frame and that world
// only.

// addFitScript adds the user script measuring the page to the content
// controller, which SetUserScripts empties: set by MeasureContent, so
// that apps that measure no page link none of it. Pool required.
var addFitScript func(w *window)

// MeasureContent runs script in the world FitWorld of the page's main
// frame, in each document from the start, and in the one it shows now.
func (w *window) MeasureContent(script string) bool {
	if w.fitScript != "" {
		return true
	}
	if w.closed || w.foreign || w.ucc == 0 || script == "" ||
		!respondsTo(w.ucc, "addScriptMessageHandler:contentWorld:name:") {
		return false
	}
	w.fitScript = script
	addFitScript = (*window).addFitScript
	withPool(func() {
		world := fitWorld()
		send(w.ucc, "addScriptMessageHandler:contentWorld:name:", uintptr(w.delegate), uintptr(world), uintptr(nsString(platform.FitHandler)))
		w.addFitScript()
		send(w.web, "evaluateJavaScript:inFrame:inContentWorld:completionHandler:", uintptr(nsString(script)), 0, uintptr(world), 0)
	})
	return true
}

// addFitScript adds the user script measuring the page, at document
// start in the main frame. Pool required.
func (w *window) addFitScript() {
	script := send(send(class("WKUserScript"), "alloc"), "initWithSource:injectionTime:forMainFrameOnly:inContentWorld:",
		uintptr(nsString(w.fitScript)), 0, 1, uintptr(fitWorld()))
	send(w.ucc, "addUserScript:", uintptr(script))
	release(script)
}

// fitWorld returns the content world of the script measuring a page.
// Pool required.
func fitWorld() id {
	return send(class("WKContentWorld"), "worldWithName:", uintptr(nsString(platform.FitWorld)))
}
