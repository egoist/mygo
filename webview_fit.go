package mygo

import (
	"encoding/json"

	"github.com/egoist/mygo/internal/bridge"
	"github.com/egoist/mygo/internal/platform"
)

// A web view whose size follows its page's content (ui's FitContent,
// WebView.OnContentSizeChanged) has its page measure the content as a
// window that takes its page's size does (fit_content.go), but at the
// width the view gives the web view, and without the bridge: the page may
// be untrusted, or have no bridge at all. The backend runs bridge.FitScript
// in an isolated world of the page's top frame, which posts the size to a
// handler of that world alone (platform.WebView.MeasureContent), so that the
// page's own scripts can neither tell a size nor change how it is measured;
// what the bridge posts never reaches it. The core turns the size into DIPs
// at the page's zoom, as for a window (fitContent.fit), tells the listeners
// and has the window's view build again, which lays the web view's element
// out at the new height.
//
// Applying a height feeds nothing back: the script measures the root
// element's height, which is its content's (height: auto) and not the
// viewport's, or how far the content scrolls when it overflows, so that a
// web view laid out at the height it measured measures the same again and
// tells nothing. What may still go back and forth, as content that a
// vertical scrollbar narrows at one height and not at the other, the
// script marks: a size measured right after the viewport's height alone
// changed, that is the size told before the last one, holds at the taller
// of the two, as native UI's flyouts hold (ui's measureFit).

// viewFit is what the page of a web view whose size follows its content
// keeps, made as it starts measuring, so that apps that measure no page
// link none of it: tell takes the sizes its isolated world tells
// (WindowHandler.ContentSize, tellContentSize). Main thread only, but
// listeners.
type viewFit struct {
	tell func(msg string)
	// listeners are OnContentSizeChanged's, which read size.
	listeners listeners[func()]
	// measuring tells that the platform measures the page
	// (platform.WebView.MeasureContent); shown, that ui's FitContent
	// shows the web view at the content's height, which the window's
	// view builds again for.
	measuring, shown bool
	// size is the content's size last told, in DIPs, once told, and
	// before the one told before it.
	size, before platform.Size
	told         bool
}

// maxContentSizeMessage bounds what the backend may deliver as the size
// of a page's content: the script posts a few numbers.
const maxContentSizeMessage = 512

// OnContentSizeChanged calls fn with the size of the content of the web
// view's page in DIPs, at the width the window's view shows the web view,
// whenever it changes: as the page loads, as its images and fonts come,
// as its scripts change it, and as the web view gets another width or the
// page another zoom. The first call has the page measure its content from
// then on, which ui's FitContent does too: a script in an isolated world
// of the page, which needs no bridge, so an untrusted page (and HTML loaded
// with LoadHTML) tells its size too, and which the page's own scripts can
// neither reach nor fake. The height is the content's, not the web view's:
// content taller than the web view is as tall as it scrolls. fn runs on
// the main thread, as other events do.
//
//	reader.OnContentSizeChanged(func(width, height float64) {
//		log.Printf("the message is %v DIPs tall", height)
//	})
//
// A web view that never showed has no width to lay out at, and tells no
// size until it shows. Where the platform has no isolated worlds (macOS
// before 11, WebKitGTK before 2.22) fn is never called. A page whose root
// element is as tall as its viewport, as with html { height: 100% },
// measures the web view's height: content taller than that scrolls, so
// that it grows, but it does not shrink.
func (v *WebView) OnContentSizeChanged(fn func(width, height float64)) (off func()) {
	var f *viewFit
	onMain(func() { f = v.w.measureContent() })
	if f == nil {
		return func() {} // destroyed
	}
	return f.listeners.add(func() { fn(float64(f.size.Width), float64(f.size.Height)) }, false)
}

// ContentSize returns the size of the content of the web view's page in
// DIPs, as the page last measured it (OnContentSizeChanged); ok is false
// before it told one, and where the platform can't measure it.
func (v *WebView) ContentSize() (width, height float64, ok bool) {
	onMain(func() {
		if f := v.w.viewFit; f != nil && f.told {
			width, height, ok = float64(f.size.Width), float64(f.size.Height), true
		}
	})
	return width, height, ok
}

// SurfaceFitHeight returns the height of the content of the web view's
// page in DIPs, for package ui to make its element as tall (FitContent),
// and has the page measure it from then on, the first time: 0 until the
// page told it. measuring is false where the platform can't measure it,
// and for a web view destroyed before it measured. Main thread only.
func (v *WebView) SurfaceFitHeight() (height float64, measuring bool) {
	f := v.w.measureContent()
	if f == nil || !f.measuring {
		return 0, false
	}
	f.shown = true
	if f.told {
		height = float64(f.size.Height)
	}
	return height, true
}

// measureContent has the page of a web view measure its content from now
// on, the first time, and returns what it keeps of it; nil for a web view
// destroyed before it measured. Main thread only.
func (w *Window) measureContent() *viewFit {
	if f := w.viewFit; f != nil {
		return f
	}
	n, ok := w.native.(*webViewNative)
	if !ok {
		return nil
	}
	f := &viewFit{tell: w.tellContentSize}
	w.viewFit = f
	f.measuring = n.web.MeasureContent(bridge.FitScript)
	return f
}

// tellContentSize takes the size of the content of a web view's page as
// the script in its isolated world measured it, in CSS pixels, and tells
// the listeners and the window's view when its size in DIPs changed.
// Main thread only.
func (w *Window) tellContentSize(msg string) {
	f, n := w.viewFit, w.native
	if f == nil || n == nil || len(msg) > maxContentSizeMessage {
		return
	}
	var m struct {
		W  float64 `json:"w"`
		H  float64 `json:"h"`
		SH float64 `json:"sh"` // a horizontal scrollbar's room
		R  int     `json:"r"`  // measured as the viewport's height alone changed
	}
	if json.Unmarshal([]byte(msg), &m) != nil {
		return
	}
	s, ok := (&fitContent{}).fit(contentSize{Width: m.W, Height: m.H, HorizontalScrollbar: m.SH}, n.Zoom(), 0)
	if !ok || f.told && s == f.size {
		return
	}
	if m.R != 0 && f.told && s == f.before && s.Height <= f.size.Height {
		// Laid out at the height it took, the content measures the size
		// before it again: the taller holds.
		return
	}
	f.before, f.size, f.told = f.size, s, true
	fire(&f.listeners)
	if h := w.host; h != nil && f.shown {
		h.Invalidate()
	}
}
