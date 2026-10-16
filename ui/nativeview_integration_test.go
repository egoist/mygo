package ui

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/surface"
)

// nativePressWindow builds a window whose content holds a native view, a
// text input, the view the tests press, and a text input after it. Tab
// reaches the inputs and the views, and label reports what the content's
// focus is on after the last frame. Both views report the surface that
// takes their views, where the keyboard the content takes back goes.
func nativePressWindow(t *testing.T) (tt *Tester, label func() string, view, other *testNativeView, provider *testNativeProvider) {
	t.Helper()
	provider = &testNativeProvider{}
	view = &testNativeView{name: "view", view: &testPlatformView{handle: 7}, provider: provider}
	other = &testNativeView{name: "other", view: &testPlatformView{handle: 8}, provider: provider}
	var before, after string
	tt = coreNewTester(func(c *context) {
		coreBox(c).Fill().Children(func() {
			coreNativeView(c, other).Height(60)
			coreTextInput(c, &before).Label("Before")
			coreNativeView(c, view).Height(60)
			coreTextInput(c, &after).Label("After")
		})
	}, 200, 260)
	label = func() string {
		if s := tt.rt.states[tt.rt.focused]; s != nil && s.nativeView != nil {
			if s.nativeView == NativeHostView(view) {
				return "view"
			}
			if s.nativeView == NativeHostView(other) {
				return "other"
			}
			return "another native view"
		}
		for _, l := range []string{"Before", "After"} {
			if tt.Focused(l) {
				return l
			}
		}
		return "nothing"
	}
	return tt, label, view, other, provider
}

// focusNativeViewTabbing tabs the window's focus to the native view the
// label names, as the keyboard does, and fails when Tab does not reach it.
func focusNativeViewTabbing(t *testing.T, tt *Tester, label func() string, want string) {
	t.Helper()
	for i := 0; i < 4; i++ {
		if label() == want {
			return
		}
		tt.Key(0, KeyTab)
	}
	t.Fatalf("Tab left the content's focus on %q: want %q", label(), want)
}

// nativeViewCenter is the centre of the placed native view v, where a press
// of it lands: it fails when the frame did not place v, or something else
// takes the pointer there.
func nativeViewCenter(t *testing.T, tt *Tester, v NativeHostView) (x, y float32) {
	t.Helper()
	p, ok := placedNative(tt, v)
	if !ok {
		t.Fatalf("the frame does not show the native view %v", v)
	}
	x = float32(p.frame.X + p.frame.W/2)
	y = float32(p.frame.Y + p.frame.H/2)
	if got := tt.NativeViewAt(x, y); got != v {
		t.Fatalf("the native view at %v, %v is %v: want %v", x, y, got, v)
	}
	return x, y
}

// A press of a native view the content's focus is already on keeps that
// focus: the view reported the keyboard first (Focused), as AppKit does for
// a press on one of its controls, and the press must not drop the focus, or
// the end of the frame hands the keyboard back to the content and steals it
// from the view that was pressed. The other order, the press arriving
// first, leaves the same view focused once it reports the keyboard; Tab
// still hands the keyboard on from the view, the content's own UI still
// takes it, and the right button still moves nothing.
func TestNativeViewPressKeepsItsFocus(t *testing.T) {
	t.Run("the view focused before the press", func(t *testing.T) {
		tt, label, view, _, provider := nativePressWindow(t)
		focusNativeViewTabbing(t, tt, label, "view")
		x, y := nativeViewCenter(t, tt, view)
		took := provider.takeFocus
		tt.ClickAt(x, y)
		if got := label(); got != "view" {
			t.Errorf("the press of the focused native view left the content's focus on %q: want the view", got)
		}
		if nativeKeyboard(tt.rt) != NativeHostView(view) {
			t.Errorf("nativeFocus is %v after the press: want the view", nativeKeyboard(tt.rt))
		}
		tt.Frame()
		if got := label(); got != "view" {
			t.Errorf("a frame after the press left the content's focus on %q: want the view", got)
		}
		if nativeKeyboard(tt.rt) != NativeHostView(view) {
			t.Errorf("nativeFocus is %v after the frame: want the view to keep the keyboard", nativeKeyboard(tt.rt))
		}
		if provider.takeFocus != took {
			t.Error("the frame took the keyboard back from the native view it was pressed on")
		}
		// Tab hands the keyboard on from the view, as ever.
		took = provider.takeFocus
		nativeHostOf(tt.rt).tabOutView(NativeHostView(view), false)
		tt.Frame()
		if got := label(); got != "After" {
			t.Errorf("Tab out of the native view focused %q: want After", got)
		}
		if nativeKeyboard(tt.rt) != nil || provider.takeFocus != took+1 {
			t.Errorf("nativeFocus %v after Tab out (the view's surface took the keyboard %d times): want the content to have it", nativeKeyboard(tt.rt), provider.takeFocus-took)
		}
	})

	t.Run("the press before the view focused", func(t *testing.T) {
		tt, label, _, other, provider := nativePressWindow(t)
		focusNativeViewTabbing(t, tt, label, "view")
		// The press of the other view arrived first, as when that view had
		// no keyboard yet: the content gives its own focus up, and the view
		// reports Focused after.
		x, y := nativeViewCenter(t, tt, other)
		tt.ClickAt(x, y)
		if got := label(); got != "nothing" {
			t.Errorf("the press of another native view left the content's focus on %q: want it given up", got)
		}
		nativeHostOf(tt.rt).focus(NativeHostView(other))
		took := provider.takeFocus
		tt.Frame()
		if got := label(); got != "other" {
			t.Errorf("the content's focus is %q after the view reported the keyboard: want other", got)
		}
		if nativeKeyboard(tt.rt) != NativeHostView(other) {
			t.Errorf("nativeFocus is %v after the frame: want the other view to keep the keyboard", nativeKeyboard(tt.rt))
		}
		if provider.takeFocus != took {
			t.Error("the frame took the keyboard back from the native view that reported it")
		}
	})

	t.Run("the content's own UI and the right button", func(t *testing.T) {
		tt, label, view, _, provider := nativePressWindow(t)
		focusNativeViewTabbing(t, tt, label, "view")
		// Clicking the content's own UI takes the keyboard back, as it does
		// from a view that has it.
		took := provider.takeFocus
		if err := tt.Click("After"); err != nil {
			t.Fatal(err)
		}
		if got := label(); got != "After" {
			t.Fatalf("clicking the content's own UI focused %q: want After", got)
		}
		tt.Frame()
		if nativeKeyboard(tt.rt) != nil || provider.takeFocus != took+1 {
			t.Errorf("nativeFocus %v after the click (the view's surface took the keyboard %d times): want the content to have it", nativeKeyboard(tt.rt), provider.takeFocus-took)
		}
		// The right button of a press on the native view leaves the
		// content's focus where it is: only the left button moves it.
		x, y := nativeViewCenter(t, tt, view)
		tt.RightClickAt(x, y)
		if got := label(); got != "After" {
			t.Errorf("a right press of the native view left the content's focus on %q: want After", got)
		}
	})
}

// testPlatformWebView is the platform page of a placed test web view: the
// tests never call a method of it, so the interface stands in for one.
type testPlatformWebView struct{ platform.WebView }

// placedTestWebView stands for a *mygo.WebView whose page the surface
// actually places, unlike testWebView, whose page no surface supports.
type placedTestWebView struct {
	name string
	view *testPlatformWebView
}

func (v *placedTestWebView) SurfaceWebView(*surface.Conn) platform.WebView { return v.view }

// placedSurface is a window surface that records what the host places on
// it, and takes native views (platform.NativeViewSurface).
type placedSurface struct {
	*testSurface
	webCalls, nativeCalls, takeFocusCalls int
	web                                   []platform.WebViewPlacement
	native                                []platform.NativeViewPlacement
}

func (s *placedSurface) Size() (float64, float64, float64) { return 200, 200, 1 }

func (s *placedSurface) PlaceWebViews(views []platform.WebViewPlacement) {
	s.webCalls++
	s.web = slices.Clone(views)
}

func (s *placedSurface) PlaceNativeViews(views []platform.NativeViewPlacement) {
	s.nativeCalls++
	s.native = slices.Clone(views)
}

func (s *placedSurface) TakeFocus() { s.takeFocusCalls++ }

func (s *placedSurface) NewNativeView(func(platform.NativeViewHost) (platform.NativeView, error)) (platform.NativeView, error) {
	return nil, platform.ErrUnsupported
}

// The window hands the surface the web views and the native views of a frame
// with the order the frame painted them, so that a window mixing both keeps
// the last painted on top, and places them again when only those orders
// change. It reads the placements the surface hears, not the engine's own
// numbers.
func TestNativeViewWebViewPlacementOrder(t *testing.T) {
	page := &placedTestWebView{name: "page", view: &testPlatformWebView{}}
	view := &testNativeView{name: "view", view: &testPlatformView{handle: 7}}
	s := &placedSurface{testSurface: &testSurface{}}
	h := &windowHost{conn: &surface.Conn{Surface: s}}
	nativeFirst := true
	h.rt = newRuntime(func(c *context) {
		coreColumn(c).Fill().Children(func() {
			if nativeFirst {
				coreNativeView(c, view).Height(60)
				coreWebView(c, page).Height(60)
			} else {
				coreWebView(c, page).Height(60)
				coreNativeView(c, view).Height(60)
			}
		})
	}, h)
	h.event(platform.SurfaceEvent{Kind: platform.SurfaceFrame})
	if len(s.web) != 1 || len(s.native) != 1 {
		t.Fatalf("the surface heard %d web views and %d native views: want one of each", len(s.web), len(s.native))
	}
	// The native view painted first, the page over it.
	if s.native[0].Order >= s.web[0].Order {
		t.Errorf("the surface heard native order %d and web order %d: want the native view painted before the page", s.native[0].Order, s.web[0].Order)
	}

	// The content paints them the other way around: the surface hears the
	// page painted before the native view.
	webCalls, nativeCalls := s.webCalls, s.nativeCalls
	nativeFirst = false
	h.event(platform.SurfaceEvent{Kind: platform.SurfaceFrame})
	if len(s.web) != 1 || len(s.native) != 1 {
		t.Fatalf("the surface heard %d web views and %d native views: want one of each", len(s.web), len(s.native))
	}
	if s.webCalls == webCalls && s.nativeCalls == nativeCalls {
		t.Error("painting the page before the native view placed nothing again")
	}
	if s.web[0].Order >= s.native[0].Order {
		t.Errorf("the surface heard web order %d and native order %d: want the page painted before the native view", s.web[0].Order, s.native[0].Order)
	}

	// The same boxes, only the orders swapped back: the placements are not
	// the same, so the surface hears them again.
	w, n := s.web[0], s.native[0]
	webCalls, nativeCalls = s.webCalls, s.nativeCalls
	h.placeWebViews([]placedWebView{{v: page, frame: w.Frame, clip: w.Clip, covers: w.Covers, order: n.Order}})
	nativeHostOf(h.rt).placeViews([]placedNativeView{{v: view, frame: n.Frame, clip: n.Clip, covers: n.Covers, order: w.Order}})
	if s.webCalls != webCalls+1 || s.nativeCalls != nativeCalls+1 {
		t.Errorf("swapping only the orders placed %d web views and %d native views again: want one of each", s.webCalls-webCalls, s.nativeCalls-nativeCalls)
	}
	if len(s.web) != 1 || len(s.native) != 1 {
		t.Fatalf("the surface heard %d web views and %d native views: want one of each", len(s.web), len(s.native))
	}
	if s.web[0].Order != n.Order || s.native[0].Order != w.Order {
		t.Errorf("the surface heard web order %d and native order %d: want %d and %d", s.web[0].Order, s.native[0].Order, n.Order, w.Order)
	}

	// And placing those same views again writes nothing.
	webCalls, nativeCalls = s.webCalls, s.nativeCalls
	h.placeWebViews([]placedWebView{{v: page, frame: w.Frame, clip: w.Clip, covers: w.Covers, order: n.Order}})
	nativeHostOf(h.rt).placeViews([]placedNativeView{{v: view, frame: n.Frame, clip: n.Clip, covers: n.Covers, order: w.Order}})
	if s.webCalls != webCalls || s.nativeCalls != nativeCalls {
		t.Error("placing the same views again reached the surface")
	}
}

// The same for a window mixing a native view and a web view: the first frame
// places both, in the order it painted them, and frames identical to it
// place neither again. Painting them the other way around places both again,
// each with the new frame's order, and frames identical to that one write
// nothing. The engine's own frames make the orders, so a per-frame counter
// that never restarts fails the identical ones.
func TestNativeViewWebViewIdenticalFramesPlaceOnce(t *testing.T) {
	page := &placedTestWebView{name: "page", view: &testPlatformWebView{}}
	view := &testNativeView{name: "view", view: &testPlatformView{handle: 7}}
	s := &placedSurface{testSurface: &testSurface{}}
	h := &windowHost{conn: &surface.Conn{Surface: s}}
	nativeFirst := true
	h.rt = newRuntime(func(c *context) {
		coreColumn(c).Fill().Children(func() {
			if nativeFirst {
				coreNativeView(c, view).Height(60)
				coreWebView(c, page).Height(60)
			} else {
				coreWebView(c, page).Height(60)
				coreNativeView(c, view).Height(60)
			}
		})
	}, h)
	frame := func() { h.event(platform.SurfaceEvent{Kind: platform.SurfaceFrame}) }
	// one returns the single placement of each kind the surface heard, or
	// fails the test.
	one := func() (platform.WebViewPlacement, platform.NativeViewPlacement) {
		t.Helper()
		if len(s.web) != 1 || len(s.native) != 1 {
			t.Fatalf("the surface heard %d web views and %d native views: want one of each", len(s.web), len(s.native))
		}
		return s.web[0], s.native[0]
	}

	frame()
	if s.webCalls != 1 || s.nativeCalls != 1 {
		t.Fatalf("the surface heard %d web placements and %d native placements for the first frame: want one of each", s.webCalls, s.nativeCalls)
	}
	w, n := one()
	if n.Order >= w.Order {
		t.Errorf("the first frame heard native order %d and web order %d: want the native view painted first", n.Order, w.Order)
	}

	// Identical frames place neither view again, at the same orders.
	webCalls, nativeCalls := s.webCalls, s.nativeCalls
	frame()
	frame()
	if s.webCalls != webCalls || s.nativeCalls != nativeCalls {
		t.Errorf("identical frames placed %d web views and %d native views again: want none", s.webCalls-webCalls, s.nativeCalls-nativeCalls)
	}
	if w2, n2 := one(); w2.Order != w.Order || n2.Order != n.Order {
		t.Errorf("identical frames heard web order %d and native order %d: want %d and %d", w2.Order, n2.Order, w.Order, n.Order)
	}

	// Painting the page before the native view places both again, each with
	// the order of the new frame: the page first now.
	nativeFirst = false
	frame()
	if s.webCalls != webCalls+1 || s.nativeCalls != nativeCalls+1 {
		t.Errorf("painting the page first placed %d web views and %d native views: want one of each", s.webCalls-webCalls, s.nativeCalls-nativeCalls)
	}
	w, n = one()
	if w.Order >= n.Order {
		t.Errorf("after the reorder the surface heard web order %d and native order %d: want the page painted first", w.Order, n.Order)
	}

	// And frames identical to the reordered one write nothing.
	webCalls, nativeCalls = s.webCalls, s.nativeCalls
	frame()
	frame()
	if s.webCalls != webCalls || s.nativeCalls != nativeCalls {
		t.Errorf("identical reordered frames placed %d web views and %d native views again: want none", s.webCalls-webCalls, s.nativeCalls-nativeCalls)
	}
	if w2, n2 := one(); w2.Order != w.Order || n2.Order != n.Order {
		t.Errorf("identical reordered frames heard web order %d and native order %d: want %d and %d", w2.Order, n2.Order, w.Order, n.Order)
	}
}

// testNativeProvider is the surface a view reports itself
// (platform.NativeViewSurface), which the tests reach through the view.
type testNativeProvider struct {
	placed     []platform.NativeViewPlacement
	placeCalls int
	takeFocus  int
}

func (p *testNativeProvider) NewNativeView(func(platform.NativeViewHost) (platform.NativeView, error)) (platform.NativeView, error) {
	return nil, platform.ErrUnsupported
}

func (p *testNativeProvider) PlaceNativeViews(views []platform.NativeViewPlacement) {
	p.placeCalls++
	p.placed = slices.Clone(views)
}

func (p *testNativeProvider) TakeFocus() { p.takeFocus++ }

// optInNativeView stands for a *mygo.NativeView: it reports the surface
// that takes its views, and its platform side, on a window whose own
// surface is not one.
type optInNativeView struct {
	view     *testPlatformView
	provider *testNativeProvider
}

func (v *optInNativeView) SurfaceNativeView(*surface.Conn) platform.NativeView {
	if v.view == nil || v.view.destroyed {
		return nil
	}
	return v.view
}

func (v *optInNativeView) SurfaceNativeViewHost(*surface.Conn) platform.NativeViewSurface {
	return v.provider
}

// The native views of a window reach the surface through the optional
// accessor the views themselves carry (mygo.NativeView's
// SurfaceNativeViewHost, here), so a surface that implements no
// platform.NativeViewSurface of its own still hears them: naming one never
// depends on a default surface.
func TestNativeViewProviderOptIn(t *testing.T) {
	provider := &testNativeProvider{}
	view := &optInNativeView{view: &testPlatformView{handle: 7}, provider: provider}
	s := &testSurface{}
	if _, ok := any(s).(platform.NativeViewSurface); ok {
		t.Fatal("the test surface takes native views itself: the test proves nothing")
	}
	h := &windowHost{conn: &surface.Conn{Surface: s}}
	h.rt = newRuntime(func(c *context) {
		coreBox(c).Fill().Children(func() {
			coreNativeView(c, view).Height(100)
		})
	}, h)
	h.event(platform.SurfaceEvent{Kind: platform.SurfaceFrame})
	if len(provider.placed) != 1 {
		t.Fatalf("the view's surface heard %d placements: want one", len(provider.placed))
	}
	if p := provider.placed[0]; p.View != view.view || p.Frame.H != 100 {
		t.Errorf("the view's surface heard %+v: want the view where the frame shows it", p)
	}
	// The keyboard goes back through the same surface.
	nativeHostOf(h.rt).takeBackFocus()
	if provider.takeFocus != 1 {
		t.Errorf("the view's surface took the keyboard %d times: want once", provider.takeFocus)
	}
}

// A window whose app view reports the keyboard, a press over it, or Tab
// leaving it, through the host the content hears it from (mygo.NativeViewHost,
// carried by the connection's native view state): the window routes the
// reports to the native runtime of its content, which knows the view by where
// the last frame placed it and by nothing else.
func TestNativeViewReportedEvents(t *testing.T) {
	provider := &testNativeProvider{}
	view := &optInNativeView{view: &testPlatformView{handle: 7}, provider: provider}
	var input string
	h := &windowHost{conn: &surface.Conn{Surface: &testSurface{}}}
	h.rt = newRuntime(func(c *context) {
		coreBox(c).Fill().Children(func() {
			coreNativeView(c, view).Height(60)
			coreTextInput(c, &input).Label("After")
		})
	}, h)
	h.event(platform.SurfaceEvent{Kind: platform.SurfaceFrame})
	if len(provider.placed) != 1 {
		t.Fatalf("the view's surface heard %d placements: want one", len(provider.placed))
	}
	if s := nativeHostOf(h.rt); s == nil {
		t.Fatal("the frame built a native view and installed no native runtime")
	}
	b := nativeState(t, h.conn)

	// The view reports the keyboard: the content shows it focused.
	b.ReportFocus(view)
	if got := nativeKeyboard(h.rt); got != NativeHostView(view) {
		t.Errorf("nativeFocus is %v after the view reported Focused: want the view", got)
	}
	if st := h.rt.states[h.rt.focused]; st == nil || st.nativeView != NativeHostView(view) {
		t.Error("the content's focus did not move to the element showing the view that reported Focused")
	}

	// A press the view reports in its own space lands in the content where
	// the frame placed it (0, 0 here), and keeps it focused: the view
	// reported the keyboard before the press.
	b.ReportPress(view, 10, 20)
	if h.rt.pointerX != 10 || h.rt.pointerY != 20 {
		t.Errorf("the pointer is at %v, %v after the view reported a press at 10, 20: want it there", h.rt.pointerX, h.rt.pointerY)
	}
	if got := nativeKeyboard(h.rt); got != NativeHostView(view) {
		t.Errorf("nativeFocus is %v after the press of the focused view: want the view", got)
	}

	// Tab leaving the view: the view's surface takes the keyboard back, and
	// the content's focus moves on from the element showing it.
	b.ReportTabOut(view, false)
	if got := nativeKeyboard(h.rt); got != nil {
		t.Errorf("nativeFocus is %v after the view reported TabbedOut: want nothing", got)
	}
	if provider.takeFocus != 1 {
		t.Errorf("the view's surface took the keyboard %d times after TabbedOut: want once", provider.takeFocus)
	}

	// The report of a view the frame did not place moves nothing, however
	// valid the view still is.
	x := h.rt.pointerX
	b.ReportPress(&optInNativeView{view: &testPlatformView{handle: 99}, provider: provider}, 1, 2)
	if h.rt.pointerX != x {
		t.Errorf("a press of a view the frame did not place moved the pointer to %v: want %v", h.rt.pointerX, x)
	}
}

// The surface a window's views report (nativeViewSurfaceHost) keeps hearing
// them once the content hides every one: the window's own surface is not a
// platform.NativeViewSurface (testSurface, which implements none), so the
// empty placement of the frame that shows nothing must still reach the
// surface the views named, and so must the keyboard the hidden view gives
// back. A frame that goes on showing none places nothing again, and the view
// that shows again is the same object on the same surface.
func TestNativeViewProviderHidden(t *testing.T) {
	provider := &testNativeProvider{}
	view := &optInNativeView{view: &testPlatformView{handle: 7}, provider: provider}
	shown := true
	s := &testSurface{}
	if _, ok := any(s).(platform.NativeViewSurface); ok {
		t.Fatal("the test surface takes native views itself: the test proves nothing")
	}
	h := &windowHost{conn: &surface.Conn{Surface: s}}
	h.rt = newRuntime(func(c *context) {
		coreBox(c).Fill().Children(func() {
			if shown {
				coreNativeView(c, view).Height(100)
			}
		})
	}, h)
	h.event(platform.SurfaceEvent{Kind: platform.SurfaceFrame})
	if len(provider.placed) != 1 {
		t.Fatalf("the view's surface heard %d placements: want one", len(provider.placed))
	}
	// The control takes the keyboard, as a click on it does, and reports
	// it: the content's native runtime holds it.
	nativeState(t, h.conn).ReportFocus(view)
	if nativeKeyboard(h.rt) != NativeHostView(view) {
		t.Fatalf("nativeFocus is %v after the view reported the keyboard: want the view", nativeKeyboard(h.rt))
	}

	// The content stops building it: the surface hears the empty placement,
	// which hides the view, and takes the keyboard back from it.
	calls := provider.placeCalls
	shown = false
	h.event(platform.SurfaceEvent{Kind: platform.SurfaceFrame})
	if len(provider.placed) != 0 {
		t.Errorf("the view's surface still hears %d placements after the content hid the view: want none", len(provider.placed))
	}
	if provider.placeCalls != calls+1 {
		t.Errorf("hiding the view placed %d times: want once", provider.placeCalls-calls)
	}
	if nativeKeyboard(h.rt) != nil {
		t.Errorf("nativeFocus is %v after the content hid the view: want the content to have the keyboard", nativeKeyboard(h.rt))
	}
	if provider.takeFocus != 1 {
		t.Errorf("the view's surface took the keyboard %d times after the view was hidden: want once", provider.takeFocus)
	}

	// Frames that go on showing none place nothing again.
	calls = provider.placeCalls
	h.event(platform.SurfaceEvent{Kind: platform.SurfaceFrame})
	h.event(platform.SurfaceEvent{Kind: platform.SurfaceFrame})
	if provider.placeCalls != calls {
		t.Errorf("frames showing no view placed %d times: want none", provider.placeCalls-calls)
	}
	// And an explicit Focus of the hidden view, as mygo.NativeView.Focus
	// is, takes nothing.
	nativeHostOf(h.rt).focusExplicit(h.conn, view, false)
	if len(view.view.focuses) != 0 {
		t.Errorf("an explicit Focus of a hidden view focused it %v", view.view.focuses)
	}
	if nativeKeyboard(h.rt) != nil {
		t.Errorf("an explicit Focus of a hidden view took the keyboard: %v", nativeKeyboard(h.rt))
	}

	// Shown again, it is the same platform object where the frame shows it.
	shown = true
	h.event(platform.SurfaceEvent{Kind: platform.SurfaceFrame})
	if len(provider.placed) != 1 {
		t.Fatalf("the view's surface heard %d placements after the view showed again: want one", len(provider.placed))
	}
	if p := provider.placed[0]; p.View != view.view || p.Frame.H != 100 {
		t.Errorf("the view's surface heard %+v after the view showed again: want the same view where the frame shows it", p)
	}

	// Destroyed while hidden, it goes away for good: the frame places
	// nothing, and Focus of it stays inert.
	shown = false
	h.event(platform.SurfaceEvent{Kind: platform.SurfaceFrame})
	view.view.destroyed = true
	calls = provider.placeCalls
	h.event(platform.SurfaceEvent{Kind: platform.SurfaceFrame})
	if provider.placeCalls != calls {
		t.Errorf("the frame of a destroyed view placed %d times: want none", provider.placeCalls-calls)
	}
	nativeHostOf(h.rt).focusExplicit(h.conn, view, true)
	if len(view.view.focuses) != 0 {
		t.Errorf("an explicit Focus of a destroyed view focused it %v", view.view.focuses)
	}

	// A window that let the content go reaches none of it again: the
	// surface the hidden views named is not called for a window that is
	// gone.
	h.detached = true
	if p := nativeHostOf(h.rt).provider(); p != nil {
		t.Error("a detached window reports the surface that takes its native views")
	}
}

// focusLabel moves the content's focus to the element labeled label, as a
// click on it does, and returns its id: these tests drive a window host, not
// a Tester.
func focusLabel(t *testing.T, h *windowHost, label string) uint64 {
	t.Helper()
	for _, n := range h.rt.labels {
		if n.text == label {
			h.rt.focused = n.id
			return n.id
		}
	}
	t.Fatalf("no element labeled %q in the last frame", label)
	return 0
}

// The window's own callbacks (the connection's native view state, its
// ReportFocus and ReportTabOut; mygo.NativeViewHost.Focused and TabOut) are
// asynchronous: a view the content just hid, and that stayed alive, may
// still report the keyboard it held, or Tab leaving it. A report of a view
// the last frame does not show moves nothing, as the surface's own events
// already do: the content keeps its own focus and the keyboard, and no Tab
// advance happens. The reports of a view the frame does show still count.
func TestNativeViewStaleReports(t *testing.T) {
	provider := &testNativeProvider{}
	hidden := &optInNativeView{view: &testPlatformView{handle: 7}, provider: provider}
	placed := &optInNativeView{view: &testPlatformView{handle: 8}, provider: provider}
	showHidden := true
	var text string
	s := &testSurface{}
	conn := &surface.Conn{Surface: s}
	h := &windowHost{conn: conn}
	h.rt = newRuntime(func(c *context) {
		coreBox(c).Fill().Children(func() {
			if showHidden {
				coreNativeView(c, hidden).Height(60)
			}
			coreNativeView(c, placed).Height(60)
			coreTextInput(c, &text).Label("After")
		})
	}, h)
	h.event(platform.SurfaceEvent{Kind: platform.SurfaceFrame})
	if len(provider.placed) != 2 {
		t.Fatalf("the view's surface heard %d placements: want the two the frame shows", len(provider.placed))
	}
	// The content stops building one of them: it stays alive, and the
	// surface hears the one view the frame still shows.
	showHidden = false
	h.event(platform.SurfaceEvent{Kind: platform.SurfaceFrame})
	if len(provider.placed) != 1 || provider.placed[0].View != placed.view {
		t.Fatalf("the view's surface heard %+v: want the view the frame still shows", provider.placed)
	}
	if hidden.SurfaceNativeView(conn) == nil || nativeViewGone(hidden) {
		t.Fatal("the hidden view is destroyed: the test proves nothing about a valid view the frame does not show")
	}

	// The content's own control has the keyboard, as a click on it does.
	after := focusLabel(t, h, "After")
	took := provider.takeFocus

	// The hidden view reports the keyboard it held: the content keeps its
	// own focus, and the keyboard.
	b := nativeState(t, conn)
	b.ReportFocus(NativeHostView(hidden))
	if h.rt.focused != after {
		t.Errorf("a report of a view the frame does not show moved the content's focus from %d to %d", after, h.rt.focused)
	}
	if got := nativeKeyboard(h.rt); got != nil {
		t.Errorf("nativeFocus is %v after a hidden view reported the keyboard: want none", got)
	}
	// Tab leaving it moves nothing on either: the content's focus stays
	// where it is, and no keyboard comes back through the surface.
	b.ReportTabOut(NativeHostView(hidden), false)
	if h.rt.focused != after {
		t.Errorf("Tab out of a view the frame does not show moved the content's focus from %d to %d", after, h.rt.focused)
	}
	if provider.takeFocus != took {
		t.Errorf("a view the frame does not show had the surface take the keyboard back %d times", provider.takeFocus-took)
	}

	// A view the frame does show still takes the keyboard through the same
	// callback, and Tab out of it still gives the keyboard back.
	b.ReportFocus(NativeHostView(placed))
	if nativeKeyboard(h.rt) != NativeHostView(placed) {
		t.Errorf("nativeFocus is %v after a view the frame shows reported the keyboard: want the view", nativeKeyboard(h.rt))
	}
	took = provider.takeFocus
	b.ReportTabOut(NativeHostView(placed), true)
	if nativeKeyboard(h.rt) != nil {
		t.Errorf("nativeFocus is %v after Tab left a view the frame shows: want none", nativeKeyboard(h.rt))
	}
	if provider.takeFocus != took+1 {
		t.Errorf("Tab out of a view the frame shows took the keyboard back %d times: want once", provider.takeFocus-took)
	}
}
