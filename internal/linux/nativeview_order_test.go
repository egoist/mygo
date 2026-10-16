//go:build linux && (amd64 || arm64)

package linux

import (
	"fmt"
	"slices"
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

// The tests here are about the order of the view windows in the surface's
// layer: a container maps a window over its siblings as it shows it, so a
// view that goes away and comes back is the top one whatever order the
// content painted it in. Putting the content's order back is what keeps
// the pointer, and the pixels, with the view the content painted last, and
// only a surface that made a native view does it (nativeview.go): the
// web-only path places and shapes its pages as it always did, which the
// last test here pins. They read the calls that map, unmap and restack the
// windows, in order, and the order the surface remembers (ordered), which
// is what decides whether a restack is needed at all.

// The handles of the ordering tests' second view: their own, so that the
// ledger says which window went over which.
const (
	testWrapB    = ptr(0x6100)
	testBoxB     = ptr(0x7100)
	testControlB = ptr(0x5100)
	// testWebLower and testWebUpper are the GtkWidgets of two pages.
	testWebLower = ptr(0x3100)
	testWebUpper = ptr(0x3200)
)

// orderClip is the box the placed views share: they overlap, as the
// content's views painted one over the other always may.
var orderClip = platform.RectF{X: 10, Y: 10, W: 100, H: 100}

// ordering is the GTK the ordering tests see: which widgets are visible,
// and the ordered ledger of the calls that map, unmap and restack their
// windows. Every window in these tests is its widget's own
// (gtkWidgetGetWindow answers the widget), so the ledger reads by the
// widget MyGo shows, hides or moves, as the content's view does.
type ordering struct {
	g       *gtkStub
	visible map[ptr]bool
	ops     []string
}

func newOrdering(t *testing.T) *ordering {
	t.Helper()
	o := &ordering{g: newGTKStub(t), visible: map[ptr]bool{}}
	stubGTKFunc(t, &gtkWidgetGetVisible, func(w ptr) bool { return o.visible[w] })
	stubGTKFunc(t, &gtkWidgetShow, func(w ptr) {
		o.visible[w] = true
		o.ops = append(o.ops, fmt.Sprintf("gtk_widget_show %#x", w))
	})
	stubGTKFunc(t, &gtkWidgetHide, func(w ptr) {
		o.visible[w] = false
		o.ops = append(o.ops, fmt.Sprintf("gtk_widget_hide %#x", w))
	})
	stubGTKFunc(t, &gdkWindowRestack, func(w, sibling ptr, above bool) {
		o.ops = append(o.ops, fmt.Sprintf("gdk_window_restack %#x over %#x above=%t", w, sibling, above))
	})
	stubGTKFunc(t, &gtkWidgetGetWindow, func(w ptr) ptr { return w })
	stubGTKFunc(t, &gtkLayoutMove, func(l, w ptr, x, y int32) {})
	return o
}

// take returns the calls recorded since the last take, which is what one
// step of a test made.
func (o *ordering) take() []string {
	ops := o.ops
	o.ops = nil
	return ops
}

// newSecondView makes a second native view of the same window, with
// layouts and a control of its own.
func newSecondView(t *testing.T, g *gtkStub, s *surface) platform.NativeView {
	t.Helper()
	g.layouts = []ptr{testWrapB, testBoxB}
	a := &fakeAdapter{handle: testControlB}
	g.widget(testControlB, "GtkEntry", "GtkWidget")
	v, err := nativeViewProvider(t, s).NewNativeView(func(platform.NativeViewHost) (platform.NativeView, error) { return a, nil })
	if err != nil {
		t.Fatalf("NewNativeView (the second view): %v", err)
	}
	return v
}

// TestOrderViewsStacksAgainAfterTheLowerViewComesBack is the review's
// case: two native views, and the content hides the lower one (A), then
// shows it again without remaking it. Showing A's clip maps its window
// over B's, so the order the surface remembered is not the order the
// windows are in any more, and the check that skips a restack must not
// trust it: B ends over A, where the content painted it.
func TestOrderViewsStacksAgainAfterTheLowerViewComesBack(t *testing.T) {
	o := newOrdering(t)
	_, s, _, a := newFakeView(t, o.g)
	b := newSecondView(t, o.g, s)
	ns := nativeViewProvider(t, s)
	place := func(v platform.NativeView, order int) platform.NativeViewPlacement {
		return platform.NativeViewPlacement{View: v, Clip: orderClip, Frame: orderClip, Order: order}
	}

	// The content paints A first and B over it: B's window goes over A's.
	ns.PlaceNativeViews([]platform.NativeViewPlacement{place(a, 0), place(b, 1)})
	want := []string{
		"gtk_widget_show 0x7000", "gtk_widget_show 0x6000", // A: its box, then its clip
		"gtk_widget_show 0x7100", "gtk_widget_show 0x6100", // then B's
		"gdk_window_restack 0x6100 over 0x6000 above=true",
	}
	if d := diffOps(want, o.take()); d != "" {
		t.Fatalf("placing two views: %s", d)
	}
	if !slices.Equal(s.ordered, []ptr{testWrap, testWrapB}) {
		t.Fatalf("the surface remembers the order %v, want [0x6000 0x6100]", s.ordered)
	}

	// The content paints A no more: A's clip goes away and one window is
	// left, whose place among others says nothing any more. The order of
	// the two windows must not stay, though: it is what the next frame
	// with two windows compares against.
	ns.PlaceNativeViews([]platform.NativeViewPlacement{place(b, 1)})
	if d := diffOps([]string{"gtk_widget_hide 0x6000"}, o.take()); d != "" {
		t.Fatalf("hiding the lower view: %s", d)
	}
	if len(s.ordered) != 0 {
		t.Fatalf("the surface still remembers the order %v with one view shown", s.ordered)
	}

	// The content paints A again, under B. The same two windows in the
	// same order they were, and A's window was just mapped over B's: B
	// must go back over it.
	ns.PlaceNativeViews([]platform.NativeViewPlacement{place(a, 0), place(b, 1)})
	want = []string{
		"gtk_widget_show 0x6000",
		"gdk_window_restack 0x6100 over 0x6000 above=true",
	}
	if d := diffOps(want, o.take()); d != "" {
		t.Fatalf("showing the lower view again: %s", d)
	}

	// A frame that changes nothing restacks nothing: what the surface
	// remembers is the windows' own order, and the content's frames are
	// not a reason to call into GDK.
	ns.PlaceNativeViews([]platform.NativeViewPlacement{place(a, 0), place(b, 1)})
	if got := o.take(); len(got) != 0 {
		t.Fatalf("a frame that changed nothing made %v", got)
	}
}

// TestPlaceWebViewsPlacesAndShapesThePagesLikeTheOriginal is the web-only
// path's regression test: a surface that made no native view places and
// shapes its pages with the code every app has always linked, and links
// none of the feature. Its pages' windows are not re-ordered among
// themselves (upstream's behaviour: an order of view windows exists only
// under the area of a surface that made a native view, nativeview.go), and
// the path keeps no order of its own: showing a page again shows it, calls
// nothing into GDK to re-order windows, and leaves s.ordered empty and
// orderDirty false for the next frame.
func TestPlaceWebViewsPlacesAndShapesThePagesLikeTheOriginal(t *testing.T) {
	o := newOrdering(t)
	stubGTKFunc(t, &gtkWidgetHasFocus, func(ptr) bool { return false })
	w := &window{win: testWin}
	s := &surface{w: w, area: testArea, web: testWeb, layers: testLayers}
	lower, upper := &window{host: w, web: testWebLower}, &window{host: w, web: testWebUpper}
	w.webViews = []*window{lower, upper}
	place := func(v *window, order int) platform.WebViewPlacement {
		return platform.WebViewPlacement{WebView: v, Frame: orderClip, Clip: orderClip, Order: order}
	}
	noOrderState := func(step string) {
		t.Helper()
		if len(s.ordered) != 0 || s.orderDirty {
			t.Fatalf("%s left order state: ordered %v, orderDirty %t", step, s.ordered, s.orderDirty)
		}
	}

	// The content paints the lower page first: both pages are shown where
	// the content shows them, and the area's input shape lets the pointer
	// through to them.
	s.PlaceWebViews([]platform.WebViewPlacement{place(lower, 0), place(upper, 1)})
	want := []string{
		"gtk_widget_show 0x3100",
		"gtk_widget_show 0x3200",
	}
	if d := diffOps(want, o.take()); d != "" {
		t.Fatalf("placing two pages: %s", d)
	}
	noOrderState("placing two pages")

	// The content shows the upper page only: the lower one's window goes
	// away and its page keeps its state.
	s.PlaceWebViews([]platform.WebViewPlacement{place(upper, 1)})
	if d := diffOps([]string{"gtk_widget_hide 0x3100"}, o.take()); d != "" {
		t.Fatalf("hiding the lower page: %s", d)
	}
	noOrderState("hiding the lower page")

	// The content shows the lower page again, under the upper one: it is
	// mapped over the upper page's window, and the web-only path leaves
	// the windows as the container mapped them. Nothing puts the content's
	// order back, as nothing did upstream: only a surface that made a
	// native view has an order under its area to put back.
	s.PlaceWebViews([]platform.WebViewPlacement{place(lower, 0), place(upper, 1)})
	if d := diffOps([]string{"gtk_widget_show 0x3100"}, o.take()); d != "" {
		t.Fatalf("showing the lower page again: %s", d)
	}
	noOrderState("showing the lower page again")
}

// TestPlaceNativeViewsHidesAnEmptyClip checks the boundary of the
// backend's call, not the core's: a placement that shows no pixel of the
// view, an empty clip (the core may hand one over for an element of no
// size), hides the view, which keeps its state, rather than showing a
// window of no size over the others. The windows backend's placement does
// the same (hideNative).
func TestPlaceNativeViewsHidesAnEmptyClip(t *testing.T) {
	o := newOrdering(t)
	_, s, _, a := newFakeView(t, o.g)
	ns := nativeViewProvider(t, s)
	ns.PlaceNativeViews([]platform.NativeViewPlacement{{View: a, Clip: orderClip, Frame: orderClip}})
	if !o.visible[testWrap] {
		t.Fatal("a placed view's clip is not shown")
	}
	o.take()

	empty := orderClip
	empty.W = 0
	ns.PlaceNativeViews([]platform.NativeViewPlacement{{View: a, Clip: empty, Frame: orderClip}})
	if d := diffOps([]string{"gtk_widget_hide 0x6000"}, o.take()); d != "" {
		t.Fatalf("an empty clip: %s", d)
	}
	var placed int
	if p := s.extOf(); p != nil {
		placed = len(p.placed)
	}
	if placed != 1 {
		t.Errorf("the surface placed %d views, want the one the content placed", placed)
	}
	if len(s.ordered) != 0 {
		t.Errorf("the surface remembers the order %v with nothing shown", s.ordered)
	}

	// The view keeps its state: a placement that no longer shows it hides
	// an already hidden view nowhere.
	ns.PlaceNativeViews(nil)
	if got := o.take(); len(got) != 0 {
		t.Errorf("hiding a view the content placed no more made %v", got)
	}
	ns.PlaceNativeViews([]platform.NativeViewPlacement{{View: a, Clip: orderClip, Frame: orderClip}})
	if got := o.take(); !slices.Equal(got, []string{"gtk_widget_show 0x6000"}) {
		t.Errorf("showing the view again made %v, want its clip shown", got)
	}
}
