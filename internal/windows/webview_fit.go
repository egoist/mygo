//go:build windows && (amd64 || arm64)

package windows

import (
	"encoding/json"
	"log"

	"github.com/egoist/mygo/internal/platform"
)

// The page of a web view whose size follows its content measures it with
// a script of the core in an isolated world of its own (FitWorld), as
// keys.js finds the keys the page leaves (webview_keys.go): the DevTools
// protocol adds the script to every document of the top frame and its
// frames in its process, in that world (Page.addScriptToEvaluateOnNewDocument
// with a worldName; the script measures in the top frame only), and runs
// it in the document the page shows already, in a world made for it there
// (Page.createIsolatedWorld, Runtime.evaluate). It tells the size through
// a binding of that world alone (Runtime.addBinding with an
// executionContextName), which WebView2 raises as Runtime.bindingCalled
// with its name: the page's scripts reach neither the world nor the
// binding.

// MeasureContent runs script in the world FitWorld of the page's
// documents, from the start, and in the one it shows now.
func (w *window) MeasureContent(script string) bool {
	if w.fitMeasuring {
		return true
	}
	if w.closed || w.host == nil || script == "" {
		return false
	}
	w.fitMeasuring = true
	w.withWebView(func() { w.measureContent(script) })
	return true
}

// measureContent adds the binding and the script, once the web view
// exists, after forwardUnhandledKeys enabled the domains and subscribed
// to Runtime.bindingCalled.
func (w *window) measureContent(script string) {
	logFailure := func(_ string, err error) {
		if err != nil {
			log.Printf("mygo: web view content size: %v", err)
		}
	}
	params := func(v any) string {
		b, _ := json.Marshal(v)
		return string(b)
	}
	w.devtools("Runtime.addBinding", params(map[string]string{"name": platform.FitHandler, "executionContextName": platform.FitWorld}), logFailure)
	w.devtools("Page.addScriptToEvaluateOnNewDocument", params(map[string]string{"source": script, "worldName": platform.FitWorld}), logFailure)
	w.devtools("Page.getFrameTree", "{}", func(res string, err error) {
		var tree struct {
			FrameTree struct {
				Frame struct {
					ID string `json:"id"`
				} `json:"frame"`
			} `json:"frameTree"`
		}
		if err != nil || json.Unmarshal([]byte(res), &tree) != nil || tree.FrameTree.Frame.ID == "" || w.closed {
			logFailure("", err)
			return
		}
		w.devtools("Page.createIsolatedWorld", params(map[string]string{"frameId": tree.FrameTree.Frame.ID, "worldName": platform.FitWorld}), func(res string, err error) {
			var world struct {
				ID int `json:"executionContextId"`
			}
			if err != nil || json.Unmarshal([]byte(res), &world) != nil || world.ID == 0 || w.closed {
				logFailure("", err)
				return
			}
			w.devtools("Runtime.evaluate", params(map[string]any{"expression": script, "contextId": world.ID}), logFailure)
		})
	})
}
