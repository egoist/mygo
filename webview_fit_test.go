//go:build !js

package mygo

import (
	"fmt"
	"slices"
	"testing"

	"github.com/egoist/mygo/internal/bridge"
	"github.com/egoist/mygo/internal/fake"
	"github.com/egoist/mygo/ui"
)

// contentSizeMsg is what the script measuring a page posts: its content's
// size in CSS pixels, in a viewport vh tall, resized marking a measure
// right after the viewport's height alone changed.
func contentSizeMsg(w, h, vh float64, resized bool) string {
	r := 0
	if resized {
		r = 1
	}
	return fmt.Sprintf(`{"w":%v,"h":%v,"sh":0,"vw":%v,"vh":%v,"r":%d}`, w, h, w, vh, r)
}

// A web view whose element fits its content (ui's FitContent) has its
// page measure it in an isolated world, and its element takes the height
// the page tells, in DIPs at the page's zoom, inside its padding, over a
// Height it has; the listeners hear each change, and what the page's
// bridge posts changes nothing.
func TestWebViewFitContent(t *testing.T) {
	var reader, fixed *WebView
	w, _, s := contentWindow(t, func(c *ui.Context) {
		ui.Scroll(c).Fill().Children(func() {
			ui.Text(c, "Header").Height(40)
			if fixed != nil {
				ui.WebView(c, fixed).Height(120).FitContent()
			}
			if reader != nil {
				ui.WebView(c, reader).FitContent().Padding(4)
			}
		})
	})
	frame := func() {
		t.Helper()
		w.Invalidate()
		onMain(func() {})
		onMain(func() { s.Frame() })
	}
	r, err := w.NewWebView(WebViewOptions{ScrollToContainer: true, Page: PageOptions{Untrusted: true}})
	if err != nil {
		t.Fatal(err)
	}
	fr := s.WebViews()[0]
	var told [][2]float64
	r.OnContentSizeChanged(func(width, height float64) { told = append(told, [2]float64{width, height}) })
	if !slices.Equal(fr.Measures, []string{bridge.FitScript}) {
		t.Fatalf("OnContentSizeChanged measured the page with %d scripts", len(fr.Measures))
	}
	w.Update(func() { reader = r })
	frame()
	height := func(fv *fake.WebView) float64 {
		t.Helper()
		if !fv.Shown {
			t.Fatal("the web view does not show")
		}
		return fv.Placement.Frame.H
	}
	if h := height(fr); h != 1 {
		t.Errorf("before the page told its size: %v DIPs tall, want 1, for the page to lay out at its width", h)
	}
	if _, _, ok := r.ContentSize(); ok {
		t.Error("a content size before the page told one")
	}
	if fr.Placement.Frame.W != 292 {
		t.Errorf("the page lays out %v DIPs wide, want 292 inside the padding", fr.Placement.Frame.W)
	}

	tell := func(msg string) {
		t.Helper()
		onMain(func() { fr.TellContentSize(msg) })
		frame()
	}
	tell(contentSizeMsg(292, 480.5, 1, false))
	if h := height(fr); h != 481 {
		t.Errorf("the content's height: %v DIPs, want 481", h)
	}
	if cw, ch, ok := r.ContentSize(); !ok || cw != 292 || ch != 481 {
		t.Errorf("ContentSize %v, %v, %v", cw, ch, ok)
	}
	// As tall as it measured, the page measures the same: nothing changes.
	tell(contentSizeMsg(292, 480.5, 481, true))
	if !slices.Equal(told, [][2]float64{{292, 481}}) {
		t.Errorf("listeners heard %v", told)
	}

	// The bridge's channel, which an untrusted page has too, is not the
	// world's.
	page(fr.Window, `{"t":"content-size","w":10,"h":10}`)
	onMain(func() { fr.H.Message(`{"w":10,"h":10}`) })
	frame()
	if h := height(fr); h != 481 {
		t.Errorf("the bridge's message resized the web view to %v", h)
	}
	// Nor does anything but a size.
	for _, bad := range []string{`{"w":-1,"h":10}`, `not json`, `{"w":10,"h":10,"x":"` + string(make([]byte, maxContentSizeMessage)) + `"}`} {
		tell(bad)
	}
	if h := height(fr); h != 481 {
		t.Errorf("a bad message resized the web view to %v", h)
	}

	// An image came: it follows. A page zoomed lays out in fewer CSS
	// pixels.
	tell(contentSizeMsg(292, 600, 481, false))
	if h := height(fr); h != 600 {
		t.Errorf("the content grown: %v DIPs, want 600", h)
	}
	r.Page().SetZoomFactor(2)
	tell(contentSizeMsg(146, 310, 300, false))
	if h := height(fr); h != 620 {
		t.Errorf("the page zoomed: %v DIPs, want 620", h)
	}
	r.Page().SetZoomFactor(1)

	// Content that measures A at one height and B at the other, as with a
	// scrollbar narrowing it at one, holds at the taller.
	tell(contentSizeMsg(292, 500, 620, false))
	tell(contentSizeMsg(275, 520, 500, true))
	tell(contentSizeMsg(292, 500, 520, true))
	if h := height(fr); h != 520 {
		t.Errorf("going back and forth: %v DIPs, want 520", h)
	}
	tell(contentSizeMsg(275, 520, 520, true))
	if h := height(fr); h != 520 {
		t.Errorf("going back and forth: %v DIPs, want 520", h)
	}
	// The content changing back on its own is no loop.
	tell(contentSizeMsg(292, 500, 520, false))
	if h := height(fr); h != 500 {
		t.Errorf("the content shrank: %v DIPs, want 500", h)
	}
	want := [][2]float64{{292, 481}, {292, 600}, {292, 620}, {292, 500}, {275, 520}, {292, 500}}
	if !slices.Equal(told, want) {
		t.Errorf("listeners heard %v, want %v", told, want)
	}

	// Where the platform can't measure, the element keeps its Height.
	f, err := w.NewWebView(WebViewOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ff := s.WebViews()[1]
	onMain(func() { ff.NoIsolatedWorlds = true })
	w.Update(func() { fixed = f })
	frame()
	if h := height(ff); h != 120 || len(ff.Measures) != 1 {
		t.Errorf("a web view that can't measure: %v DIPs, measured %d times", h, len(ff.Measures))
	}
	if _, _, ok := f.ContentSize(); ok {
		t.Error("a content size where the platform can't measure")
	}
	onMain(func() { ff.TellContentSize(contentSizeMsg(300, 50, 120, false)) })
	frame()
	if h := height(ff); h != 120 {
		t.Errorf("a web view that can't measure took %v DIPs", h)
	}

	// The page measures once, however many frames and listeners.
	r.OnContentSizeChanged(func(float64, float64) {})
	if len(fr.Measures) != 1 {
		t.Errorf("measured %d times", len(fr.Measures))
	}
	r.Destroy()
	onMain(func() { fr.TellContentSize(contentSizeMsg(292, 100, 500, false)) })
	if len(told) != len(want) {
		t.Error("a destroyed web view told a size")
	}
}

// A web view without FitContent or listeners measures nothing.
func TestWebViewFitContentOptIn(t *testing.T) {
	show := true
	_, v, _, fv := webViewWindow(t, &show)
	if len(fv.Measures) != 0 {
		t.Error("a web view measured its page unasked")
	}
	if _, _, ok := v.ContentSize(); ok || len(fv.Measures) != 0 {
		t.Error("ContentSize measured the page")
	}
}
