package ui

import (
	"bytes"
	"log"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/scene"
	"github.com/egoist/mygo/internal/surface"
)

// testNativeView stands for a *mygo.NativeView in the tests of ui: its
// platform side is what SurfaceNativeView hands back, none once destroyed.
type testNativeView struct {
	name string
	view *testPlatformView
}

func (v *testNativeView) SurfaceNativeView(*surface.Conn) platform.NativeView {
	if v.view == nil || v.view.destroyed {
		return nil
	}
	return v.view
}

// IsDestroyed stands for a *mygo.NativeView the app already destroyed: the
// content shows no native view then, whatever the app builds.
func (v *testNativeView) IsDestroyed() bool { return v.view == nil || v.view.destroyed }

// testPlatformView is the platform view of a testNativeView: the adapter
// mygo.NativeView returns, and what the surface calls.
type testPlatformView struct {
	handle    uintptr
	focuses   []bool
	destroyed bool
}

func (p *testPlatformView) NativeHandle() uintptr { return p.handle }

func (p *testPlatformView) Focus(back bool) { p.focuses = append(p.focuses, back) }

func (p *testPlatformView) Destroy() { p.destroyed = true }

// nativeState returns the native view state of the connection, which the
// window's own side makes with the first view its app makes and which the
// frame then fills with the hooks a platform view reports through
// (mygo/nativeview.go, ui/nativeview.go). It fails the test for a
// connection carrying none.
func nativeState(t *testing.T, conn *surface.Conn) *surface.NativeViewBridge {
	t.Helper()
	b, _ := conn.NativeViews.(*surface.NativeViewBridge)
	if b == nil {
		t.Fatal("the connection carries no native view state")
	}
	return b
}

// nativePlacedFrames returns where the last frame showed native views, as
// the window's native runtime reported them to the headless host.
func nativePlacedFrames(tt *Tester) []placedNativeView {
	frames, _ := tt.h.nativeViews.([]placedNativeView)
	return frames
}

// nativeTabbedInto returns the native view Tab gave the keyboard last, and
// whether it did so from the view's end.
func nativeTabbedInto(tt *Tester) (NativeHostView, bool) {
	v, _ := tt.h.tabbedIntoNative.(NativeHostView)
	return v, tt.h.tabbedIntoNativeBack
}

// placedNative returns where the last frame showed v, if it did.
func placedNative(tt *Tester, v NativeHostView) (placedNativeView, bool) {
	for _, p := range nativePlacedFrames(tt) {
		if p.v == v {
			return p, true
		}
	}
	return placedNativeView{}, false
}

// nativeKeyboard returns the native view that has the keyboard, as the
// window's native runtime holds it (ui/nativeview.go).
func nativeKeyboard(rt *engine) NativeHostView {
	s := nativeHostOf(rt)
	if s == nil {
		return nil
	}
	return s.cur
}

// A native view shows in its element's content box, under what paints
// after it, which takes the pointer there: a popover over it, its own
// children.
func TestNativeViewPlacement(t *testing.T) {
	view := &testNativeView{name: "view", view: &testPlatformView{handle: 7}}
	tt := coreNewTester(func(c *context) {
		coreColumn(c).Fill().AlignItems(Stretch).Children(func() {
			coreText(c, "Toolbar").Height(40)
			coreNativeView(c, view).Grow(1).Padding(10).Border(1, RGB(0, 0, 0)).Children(func() {
				coreText(c, "Badge").Absolute().Top(20).Right(20)
			})
		})
	}, 400, 300)
	p, ok := placedNative(tt, view)
	if !ok {
		t.Fatal("the native view does not show")
	}
	if p.frame != p.clip || p.frame.X != 11 || p.frame.W != 378 || p.frame.Y+p.frame.H != 289 {
		t.Errorf("the native view shows at %+v, clipped to %+v: want its content box", p.frame, p.clip)
	}
	badge, _ := tt.Find("Badge")
	if len(p.covers) != 1 || !p.covers[0].Contains(float64(badge.X+1), float64(badge.Y+1)) {
		t.Errorf("covers %+v: want the badge's box", p.covers)
	}
	if tt.NativeViewAt(200, 200) != view || tt.NativeViewAt(badge.X+1, badge.Y+1) != nil || tt.NativeViewAt(5, 200) != nil {
		t.Error("the pointer goes to the native view where it is not, or not where it is")
	}
}

// The element paints a rectangular hole through what is under it, in its
// content box, which what paints after it covers: a platform view is a
// rectangle, and MyGo promises no rounded corners of one.
func TestNativeViewHole(t *testing.T) {
	view := &testNativeView{name: "view", view: &testPlatformView{handle: 7}}
	tt := coreNewTester(func(c *context) {
		c.Root().Background(RGB(255, 255, 255))
		coreBox(c).Fill().Padding(20).Children(func() {
			coreNativeView(c, view).Grow(1).Radius(12).Border(2, RGB(0, 0, 0)).Children(func() {
				coreBox(c).Absolute().Top(10).Left(10).Size(20, 20).Background(RGB(255, 0, 0))
			})
		})
	}, 200, 140)
	var holes []scene.Op
	for _, op := range tt.rt.scene.Ops {
		if op.Kind == scene.OpHole {
			holes = append(holes, op)
		}
	}
	if len(holes) != 1 || holes[0].Rect != (scene.Rect{X: 22, Y: 22, W: 156, H: 96}) || holes[0].Radii != ([4]float32{}) {
		t.Fatalf("holes %+v: want one rectangle in the content box, with square corners", holes)
	}
	img := tt.Image()
	at := func(x, y int) [4]uint8 {
		o := img.PixOffset(x, y)
		return [4]uint8(img.Pix[o : o+4])
	}
	if c := at(100, 70); c[3] != 0 {
		t.Errorf("the hole is %v: want transparent", c)
	}
	if c := at(22, 22); c[3] != 0 {
		t.Errorf("the hole's corner is %v: want the whole content box cut out", c)
	}
	if c := at(40, 40); c != [4]uint8{255, 0, 0, 255} {
		t.Errorf("the box over the native view is %v: want red", c)
	}
}

// One native view shows in one place: an element built twice, as by an
// exit transition, shows where it takes the pointer, and one shown twice
// at once is reported, once.
func TestNativeViewDuplicatePlacement(t *testing.T) {
	view := &testNativeView{name: "view", view: &testPlatformView{handle: 7}}
	view2 := &testNativeView{name: "view", view: &testPlatformView{handle: 8}}
	duplicate := func(c *context) {
		coreColumn(c).Fill().AlignItems(Stretch).Children(func() {
			coreNativeView(c, view).Height(40)
			coreNativeView(c, view).Height(40)
		})
	}
	// A Tester panics where the app placed one instance twice.
	func() {
		defer func() {
			msg, _ := recover().(string)
			if !strings.Contains(msg, "the same native view") {
				t.Errorf("tests do not panic on a native view placed twice: %q", msg)
			}
		}()
		coreNewTester(duplicate, 200, 200)
	}()

	var out bytes.Buffer
	log.SetOutput(&out)
	defer log.SetOutput(os.Stderr)
	tt := lenientTester(duplicate, 200, 200)
	tt.Frame()
	tt.Frame()
	if n := len(nativePlacedFrames(tt)); n != 1 {
		t.Fatalf("%d native views shown: want one of two built", n)
	}
	if p, _ := placedNative(tt, view); p.frame.Y != 0 || p.frame.H != 40 {
		t.Errorf("the native view shows at %+v: want where it first does", p.frame)
	}
	if n := strings.Count(out.String(), "the same native view"); n != 1 {
		t.Errorf("logged %d times: %s", n, out.String())
	}
	if len(tt.rt.warnings) != 1 {
		t.Errorf("warnings: %q", tt.rt.warnings)
	}

	// The one that takes the pointer is the one that shows.
	lenient := lenientTester(func(c *context) {
		coreColumn(c).Fill().AlignItems(Stretch).Children(func() {
			coreNativeView(c, view2).Height(40)
			coreNativeView(c, view2).Height(40)
		})
	}, 200, 200)
	lenient.Frame()
	if p, _ := placedNative(lenient, view2); p.frame.Y != 0 {
		t.Errorf("the native view shows at %+v: want where it first does", p.frame)
	}
}

// Web views and native views show in the order the frame paints them,
// which the host sees across both.
func TestNativeViewMixedOrder(t *testing.T) {
	page := &testWebView{"page"}
	view := &testNativeView{name: "view", view: &testPlatformView{handle: 7}}
	tt := coreNewTester(func(c *context) {
		coreColumn(c).Fill().AlignItems(Stretch).Children(func() {
			coreWebView(c, page).Height(60)
			coreNativeView(c, view).Height(60)
		})
	}, 200, 200)
	p, ok := placed(tt, page)
	if !ok {
		t.Fatal("the web view does not show")
	}
	n, ok := placedNative(tt, view)
	if !ok {
		t.Fatal("the native view does not show")
	}
	if p.order != 0 || n.order != 1 {
		t.Errorf("the web view paints %d and the native view %d: want 0 and 1", p.order, n.order)
	}
}

// A native view in a scroll container shows the part of it in view, and
// none once out of view or not built.
func TestNativeViewClipped(t *testing.T) {
	view := &testNativeView{name: "view", view: &testPlatformView{handle: 7}}
	shown := true
	var text string
	tt := coreNewTester(func(c *context) {
		coreColumn(c).Fill().AlignItems(Stretch).Children(func() {
			coreTextInput(c, &text).Label("Name")
			coreScroll(c).Grow(1).Children(func() {
				coreBox(c).Height(100)
				if shown {
					coreNativeView(c, view).Height(300)
				}
				coreBox(c).Height(400)
			})
		})
	}, 300, 300)
	p, ok := placedNative(tt, view)
	if !ok {
		t.Fatal("the native view does not show")
	}
	if p.frame.H != 300 || p.clip.Y+p.clip.H != 300 || p.clip.Y != p.frame.Y {
		t.Errorf("frame %+v clip %+v: want it clipped by the scroll container at the window's bottom", p.frame, p.clip)
	}
	// A press on the view takes the focus from the content.
	if err := tt.Click("Name"); err != nil {
		t.Fatal(err)
	}
	if !tt.Focused("Name") {
		t.Fatal("the input did not take the focus")
	}
	nativeHostOf(tt.rt).press(view, 150, 200, 0)
	if tt.Focused("Name") {
		t.Error("the input kept the focus as the native view was pressed")
	}
	tt.Scroll(150, 250, 0, 1000)
	if _, ok := placedNative(tt, view); ok {
		t.Error("the native view shows once scrolled out of view")
	}
	tt.Scroll(150, 250, 0, -1000)
	shown = false
	tt.Frame()
	if _, ok := placedNative(tt, view); ok {
		t.Error("the native view shows when not built")
	}
}

// A native view shows on whole device pixels, whatever the scale.
func TestNativeViewFractionalScale(t *testing.T) {
	view := &testNativeView{name: "view", view: &testPlatformView{handle: 7}}
	tt := coreNewTester(func(c *context) {
		coreBox(c).Fill().Children(func() {
			coreNativeView(c, view).Absolute().Left(10.4).Top(10.4).Size(100, 60)
		})
	}, 200, 200)
	tt.SetScale(1.5)
	tt.Frame()
	p, ok := placedNative(tt, view)
	if !ok {
		t.Fatal("the native view does not show")
	}
	for _, v := range []float64{p.frame.X, p.frame.Y, p.frame.X + p.frame.W, p.frame.Y + p.frame.H} {
		if got := v * 1.5; math.Abs(got-math.Round(got)) > 1e-3 {
			t.Errorf("the native view shows on %v device pixels: want whole ones", got)
		}
	}
	if p.frame.X == 10.4 {
		t.Errorf("the native view shows at %+v: want it snapped to device pixels", p.frame)
	}
}

// Tab stops at a native view to give it the keyboard, from its last
// control going back, and moves on from it as Tab leaves the view.
func TestNativeViewTab(t *testing.T) {
	view := &testNativeView{name: "view", view: &testPlatformView{handle: 7}}
	var before, after string
	tt := coreNewTester(func(c *context) {
		coreBox(c).Fill().Children(func() {
			coreTextInput(c, &before).Label("Before")
			coreNativeView(c, view).Height(100)
			coreTextInput(c, &after).Label("After")
		})
	}, 200, 200)
	label := func() string {
		if s := tt.rt.states[tt.rt.focused]; s != nil && s.nativeView != nil {
			return "native view"
		}
		for _, l := range []string{"Before", "After"} {
			if tt.Focused(l) {
				return l
			}
		}
		return "nothing"
	}
	tt.Key(0, KeyTab)
	if got := label(); got != "Before" {
		t.Fatalf("Tab focused %q: want Before", got)
	}
	tt.Key(0, KeyTab)
	tabbedView, tabbedBack := nativeTabbedInto(tt)
	if label() != "native view" || tabbedView != view || tabbedBack {
		t.Fatalf("Tab after Before focused %q and gave %v the keyboard (back %v): want the native view", label(), tabbedView, tabbedBack)
	}
	if len(view.view.focuses) != 1 || view.view.focuses[0] {
		t.Fatalf("the native view was focused %v: want once, not back", view.view.focuses)
	}
	nativeHostOf(tt.rt).tabOutView(view, false)
	tt.Frame()
	if got := label(); got != "After" {
		t.Errorf("Tab out of the native view focused %q: want After", got)
	}
	tt.h.tabbedIntoNative = nil
	tt.Key(Shift, KeyTab)
	tabbedView, tabbedBack = nativeTabbedInto(tt)
	if label() != "native view" || tabbedView != view || !tabbedBack {
		t.Fatalf("Shift+Tab from After focused %q and gave %v the keyboard (back %v): want the native view, from its end", label(), tabbedView, tabbedBack)
	}
	if n := len(view.view.focuses); n != 2 || !view.view.focuses[1] {
		t.Fatalf("the native view was focused %v: want back the second time", view.view.focuses)
	}
	nativeHostOf(tt.rt).tabOutView(view, true)
	tt.Frame()
	if got := label(); got != "Before" {
		t.Errorf("Shift+Tab out of the native view focused %q: want Before", got)
	}
	// Clicked into, the view tabs out to what follows its element.
	tt.rt.focused = 0
	nativeHostOf(tt.rt).tabOutView(view, false)
	tt.Frame()
	if got := label(); got != "After" {
		t.Errorf("Tab out of a native view clicked into focused %q: want After", got)
	}
}

// The content takes the keyboard back when the frame stops showing the
// native view that had it, and the view takes it from the content when it
// does show, as its own controls are clicked into.
func TestNativeViewFocusTakeBack(t *testing.T) {
	view := &testNativeView{name: "view", view: &testPlatformView{handle: 7}}
	shown := true
	tt := coreNewTester(func(c *context) {
		coreBox(c).Fill().Children(func() {
			if shown {
				coreNativeView(c, view).Height(100)
			}
		})
	}, 200, 200)
	tt.Key(0, KeyTab)
	if nativeKeyboard(tt.rt) != NativeHostView(view) {
		t.Fatalf("Tab did not give the native view the keyboard: %v", nativeKeyboard(tt.rt))
	}
	// The view itself takes the keyboard, and the content gives it up.
	nativeHostOf(tt.rt).press(view, 50, 50, 0)
	tt.rt.focused = 1
	tt.Frame()
	if tt.rt.focused != 0 {
		t.Error("the content kept the focus as the native view was pressed")
	}
	if !tt.h.tookBackFocus {
		t.Error("the content did not take the keyboard back")
	}
	tt.h.tookBackFocus = false
	tt.Key(0, KeyTab)
	if nativeKeyboard(tt.rt) != NativeHostView(view) {
		t.Fatalf("Tab did not give the native view the keyboard: %v", nativeKeyboard(tt.rt))
	}
	// Gone from the frame, its keyboard goes back to the content.
	shown = false
	tt.Frame()
	if nativeKeyboard(tt.rt) != nil || !tt.h.tookBackFocus {
		t.Errorf("nativeFocus %v, took back %v: want the content to take the keyboard back", nativeKeyboard(tt.rt), tt.h.tookBackFocus)
	}
	// And a view destroyed under it, still shown, does too.
	tt.h.tookBackFocus = false
	shown = true
	tt.Frame()
	tt.Key(0, KeyTab)
	if nativeKeyboard(tt.rt) == nil {
		t.Fatal("Tab did not give the native view the keyboard")
	}
	view.view.destroyed = true
	tt.Frame()
	if nativeKeyboard(tt.rt) != nil || !tt.h.tookBackFocus {
		t.Errorf("nativeFocus %v, took back %v: want the content to take the keyboard back", nativeKeyboard(tt.rt), tt.h.tookBackFocus)
	}
}

// Tab out of the only native view a frame shows still gives the content
// the keyboard, which moves on from its element.
func TestNativeViewTabOutLast(t *testing.T) {
	view := &testNativeView{name: "view", view: &testPlatformView{handle: 7}}
	tt := coreNewTester(func(c *context) {
		coreBox(c).Fill().Children(func() {
			coreNativeView(c, view).Height(100)
		})
	}, 200, 200)
	tt.Key(0, KeyTab)
	if nativeKeyboard(tt.rt) != NativeHostView(view) {
		t.Fatal("Tab did not give the native view the keyboard")
	}
	nativeHostOf(tt.rt).tabOutView(view, false)
	tt.Frame()
	if !tt.h.tookBackFocus {
		t.Error("Tab out of the native view did not give the content the keyboard back")
	}
	if nativeKeyboard(tt.rt) != nil {
		t.Errorf("the native view still has the keyboard: %v", nativeKeyboard(tt.rt))
	}
}

// nativeHoles counts the holes the last frame cut for native views.
func nativeHoles(tt *Tester) int {
	n := 0
	for _, op := range tt.rt.scene.Ops {
		if op.Kind == scene.OpHole {
			n++
		}
	}
	return n
}

// A native view the app destroyed shows nothing, whatever the content
// builds: the hole it held goes away, and Tab no longer stops at its
// element, which would trap the keyboard in a view that is gone.
func TestNativeViewDestroyed(t *testing.T) {
	view := &testNativeView{name: "view", view: &testPlatformView{handle: 7}}
	var before, after string
	tt := coreNewTester(func(c *context) {
		coreBox(c).Fill().Children(func() {
			coreTextInput(c, &before).Label("Before")
			coreNativeView(c, view).Height(100)
			coreTextInput(c, &after).Label("After")
		})
	}, 200, 200)
	if _, ok := placedNative(tt, view); !ok {
		t.Fatal("the native view does not show")
	}
	if h := nativeHoles(tt); h != 1 {
		t.Fatalf("the frame cuts %d holes: want one for the native view", h)
	}
	view.view.destroyed = true
	tt.Frame()
	if h := nativeHoles(tt); h != 0 {
		t.Errorf("the frame of a destroyed native view cuts %d holes: want none", h)
	}
	if _, ok := placedNative(tt, view); ok {
		t.Error("a destroyed native view still shows")
	}
	tt.Key(0, KeyTab)
	if !tt.Focused("Before") {
		t.Fatal("Tab focused nothing before the native view")
	}
	tt.Key(0, KeyTab)
	if tabbedView, _ := nativeTabbedInto(tt); tabbedView != nil || !tt.Focused("After") {
		t.Errorf("Tab gave %v the keyboard instead of moving to After: want the destroyed view skipped", tabbedView)
	}
}

// scaleSurface is a window surface at another scale.
type scaleSurface struct {
	*testSurface
	scale float64
}

func (s *scaleSurface) Size() (float64, float64, float64) { return 200, 100, s.scale }

// placedNativeHost returns where the last frame of a window host placed v,
// as the host reports it to the surface.
func placedNativeHost(h *windowHost, v NativeHostView) (platform.NativeViewPlacement, bool) {
	s := nativeHostOf(h.rt)
	if s == nil {
		return platform.NativeViewPlacement{}, false
	}
	for _, p := range s.frames {
		if p.v == v {
			return platform.NativeViewPlacement{View: p.v.SurfaceNativeView(s.conn), Frame: p.frame, Clip: p.clip, Covers: p.covers, Order: p.order}, true
		}
	}
	return platform.NativeViewPlacement{}, false
}

// A view reports the pointer in DIPs relative to itself: the content moves
// the point to its own space with where the view shows, device pixels and
// all, and takes no press of a view its frame does not show.
func TestNativeViewPressLocal(t *testing.T) {
	view := &testNativeView{name: "view", view: &testPlatformView{handle: 7}}
	other := &testNativeView{name: "other", view: &testPlatformView{handle: 8}}
	s := &scaleSurface{testSurface: &testSurface{}, scale: 1.5}
	h := &windowHost{conn: &surface.Conn{Surface: s}}
	h.rt = newRuntime(func(c *context) {
		coreBox(c).Fill().Children(func() {
			coreNativeView(c, view).Absolute().Left(10.4).Top(20.6).Size(100, 50)
		})
	}, h)
	h.event(platform.SurfaceEvent{Kind: platform.SurfaceFrame})
	p, ok := placedNativeHost(h, view)
	if !ok {
		t.Fatal("the native view does not show")
	}
	if p.Frame.X == 10.4 || p.Frame.Y == 20.6 {
		t.Fatalf("the native view shows at %+v: want it snapped to device pixels at scale %v", p.Frame, s.scale)
	}
	nativeHostOf(h.rt).pressLocal(view, 3, 4)
	if h.rt.pointerX != float32(p.Frame.X)+3 || h.rt.pointerY != float32(p.Frame.Y)+4 {
		t.Errorf("the press of %v, %v in the view landed at %v, %v: want it at %v, %v in the content",
			3, 4, h.rt.pointerX, h.rt.pointerY, float32(p.Frame.X)+3, float32(p.Frame.Y)+4)
	}
	if len(h.rt.downs) != 1 {
		t.Errorf("the content noted %d presses: want one", len(h.rt.downs))
	}

	// A view no frame of the window shows takes no press.
	h.rt.pointerX, h.rt.pointerY = -1, -1
	nativeHostOf(h.rt).pressLocal(other, 3, 4)
	if h.rt.pointerX != -1 || h.rt.pointerY != -1 {
		t.Errorf("a view no frame shows took a press at %v, %v", h.rt.pointerX, h.rt.pointerY)
	}
	// And neither does one the app destroyed under a frame that shows it.
	view.view.destroyed = true
	nativeHostOf(h.rt).pressLocal(view, 3, 4)
	if h.rt.pointerX != -1 || h.rt.pointerY != -1 {
		t.Errorf("a destroyed view took a press at %v, %v", h.rt.pointerX, h.rt.pointerY)
	}
}

// The focus request of a native view, mygo.NativeView.Focus, moves the
// content's focus to the view's element and gives its toolkit the keyboard.
// It reaches the content through the connection's native view state, whose
// hook only a frame that builds a native view installs, and takes nothing
// for a payload that
// is no native view, a view the window does not show, a view of another
// window, or a window that let the content go.
func TestNativeViewFocusExplicit(t *testing.T) {
	view := &testNativeView{name: "view", view: &testPlatformView{handle: 7}}
	other := &testNativeView{name: "other", view: &testPlatformView{handle: 8}}
	s := &testSurface{}
	conn := &surface.Conn{Surface: s}
	h := &windowHost{conn: conn}
	h.rt = newRuntime(func(c *context) {
		coreBox(c).Fill().Children(func() {
			coreNativeView(c, view).Height(100)
		})
	}, h)
	h.event(platform.SurfaceEvent{Kind: platform.SurfaceFrame})
	if b := nativeState(t, conn); b.RequestFocus == nil {
		t.Fatal("a frame that shows a native view installed no focus request hook")
	}

	nativeState(t, conn).RequestFocus(view, false)
	if len(view.view.focuses) != 1 || view.view.focuses[0] {
		t.Fatalf("the view heard %v: want one Focus(false)", view.view.focuses)
	}
	if nativeKeyboard(h.rt) != NativeHostView(view) {
		t.Errorf("nativeFocus is %v: want the view", nativeKeyboard(h.rt))
	}
	if s := h.rt.states[h.rt.focused]; s == nil || s.nativeView != view {
		t.Error("the content's focus did not move to the view's element: Tab would come back to the view")
	}

	// The boundary carries the view as any: a payload that is no native
	// view moves nothing, and never panics - an empty struct, an
	// uncomparable slice or map, a number, nil, the platform view of no
	// frame.
	for _, p := range []any{struct{}{}, []int{1, 2}, map[string]int{"view": 1}, 7, nil, &testPlatformView{handle: 99}} {
		nativeState(t, conn).RequestFocus(p, false)
	}
	// A native view the frame does not show moves nothing either.
	nativeState(t, conn).RequestFocus(other, false)
	if len(other.view.focuses) != 0 {
		t.Errorf("a view no frame shows was focused %v", other.view.focuses)
	}

	// A view another window shows is no view of this one: this window's
	// hook ignores it, without panic.
	conn2 := &surface.Conn{Surface: &testSurface{}}
	h2 := &windowHost{conn: conn2}
	h2.rt = newRuntime(func(c *context) {
		coreBox(c).Fill().Children(func() {
			coreNativeView(c, other).Height(100)
		})
	}, h2)
	h2.event(platform.SurfaceEvent{Kind: platform.SurfaceFrame})
	if _, ok := placedNativeHost(h2, other); !ok {
		t.Fatal("the other window does not show its native view")
	}
	nativeState(t, conn).RequestFocus(other, false)
	if len(other.view.focuses) != 0 {
		t.Errorf("another window's view was focused %v", other.view.focuses)
	}
	if len(view.view.focuses) != 1 {
		t.Errorf("the payloads moved the focus of the view: %v", view.view.focuses)
	}

	// Detaching takes the hook off the connection, and a call kept from
	// before reaches nothing: the runtime it reached is gone with the
	// window.
	stale := nativeState(t, conn).RequestFocus
	h.detach()
	if conn.NativeViews != nil {
		t.Error("a detached window kept its native view state")
	}
	stale(view, true)
	if len(view.view.focuses) != 1 {
		t.Errorf("a view of a detached window was focused again: %v", view.view.focuses)
	}
}

// A window whose content builds no native view pays nothing for them: the
// attach installs no native view hook on its connection and allocates
// nothing of the feature. Before the focus request hook replaced it, the
// attach made a per-window host map and the closing detach closure every
// time, native view or none: three allocations more (18 a window then, 15
// or 16 now; the odd one is the font preload goroutine of an attach landing
// in the window).
func TestNativeFreeContentAttach(t *testing.T) {
	const windows = 64
	contents := make([]*Content, windows)
	conns := make([]*surface.Conn, windows)
	view := func(c *context) { coreText(c, "Hello") }
	for i := range contents {
		contents[i] = coreView(view)
		conns[i] = &surface.Conn{Surface: &testSurface{}}
	}
	i := 0
	allocs := testing.AllocsPerRun(windows-8, func() {
		contents[i].AttachContent(conns[i])
		i++
	})
	// Every attach takes the same path, warm-up ones included.
	for _, conn := range conns[:i] {
		if conn.NativeViews != nil {
			t.Fatal("the attach of a content that builds no native view installed native view state")
		}
		if conn.Detach == nil {
			t.Fatal("the attach installed no detach")
		}
	}
	if allocs >= 18 {
		t.Errorf("the attach of a content that builds no native view allocates %.2f values a window: want the window's own, without the per-window host map and detach closure of the native views (18 before)", allocs)
	}
	t.Logf("native-free attach: %.2f allocations a window", allocs)
}

// A view the surface no longer has is not shown, whatever the last frame
// placed: a window takes no press of it, and gives it no keyboard.
func TestNativeViewShownGone(t *testing.T) {
	view := &testNativeView{name: "view", view: &testPlatformView{handle: 7}}
	s := &nativeHostState{conn: &surface.Conn{Surface: &testSurface{}}, views: []NativeHostView{view}}
	if !s.shown(view) {
		t.Fatal("a view that is there does not count as shown")
	}
	view.view.destroyed = true
	if s.shown(view) {
		t.Error("a view the surface no longer has counts as shown")
	}
}

// The always-linked boundary carries the native view as any, so that a
// content showing none keeps no type metadata of them: what the window's
// own callbacks report (the connection's native view state, its ReportFocus,
// ReportTabOut and ReportPress) may be no native view at all. A payload of
// another type moves nothing, and never panics - an empty struct, an
// uncomparable slice or map, a number, nil, the platform view of no frame,
// the view of another window. The view the frame does show still reaches the
// content through the same boundary.
func TestNativeViewOpaquePayloads(t *testing.T) {
	view := &testNativeView{name: "view", view: &testPlatformView{handle: 7}}
	other := &testNativeView{name: "other", view: &testPlatformView{handle: 8}}
	conn := &surface.Conn{Surface: &testSurface{}}
	h := &windowHost{conn: conn}
	h.rt = newRuntime(func(c *context) {
		coreBox(c).Fill().Children(func() {
			coreNativeView(c, view).Height(100)
		})
	}, h)
	h.event(platform.SurfaceEvent{Kind: platform.SurfaceFrame})
	if _, ok := placedNativeHost(h, view); !ok {
		t.Fatal("the native view does not show")
	}

	payloads := []any{
		struct{}{},
		[]int{1, 2},
		map[string]int{"view": 1},
		7,
		nil,
		&testPlatformView{handle: 99},
	}
	b := nativeState(t, conn)
	for _, p := range payloads {
		b.ReportFocus(p)
		b.ReportTabOut(p, false)
		b.ReportPress(p, 3, 4)
	}
	if got := nativeKeyboard(h.rt); got != nil {
		t.Errorf("nativeFocus is %v after payloads that are no native view: want none", got)
	}
	if len(h.rt.downs) != 0 {
		t.Errorf("the content noted %d presses of payloads that are no native view: want none", len(h.rt.downs))
	}

	// A native view the last frame does not show moves nothing either.
	b.ReportFocus(other)
	b.ReportTabOut(other, false)
	b.ReportPress(other, 3, 4)
	if got := nativeKeyboard(h.rt); got != nil {
		t.Errorf("nativeFocus is %v after a view no frame shows reported: want none", got)
	}
	if len(h.rt.downs) != 0 {
		t.Errorf("the content noted %d presses of a view no frame shows: want none", len(h.rt.downs))
	}

	// A view another window shows is no view of this one: this window's
	// callbacks ignore it, without panic.
	conn2 := &surface.Conn{Surface: &testSurface{}}
	h2 := &windowHost{conn: conn2}
	h2.rt = newRuntime(func(c *context) {
		coreBox(c).Fill().Children(func() {
			coreNativeView(c, other).Height(100)
		})
	}, h2)
	h2.event(platform.SurfaceEvent{Kind: platform.SurfaceFrame})
	if _, ok := placedNativeHost(h2, other); !ok {
		t.Fatal("the other window does not show its native view")
	}
	b.ReportFocus(other)
	b.ReportTabOut(other, false)
	b.ReportPress(other, 3, 4)
	if got := nativeKeyboard(h.rt); got != nil {
		t.Errorf("nativeFocus is %v after another window's view reported: want none", got)
	}
	if len(h.rt.downs) != 0 {
		t.Errorf("the content noted %d presses of another window's view: want none", len(h.rt.downs))
	}

	// The view the frame does show still reaches the content.
	b.ReportFocus(view)
	if got := nativeKeyboard(h.rt); got != NativeHostView(view) {
		t.Errorf("nativeFocus is %v after the shown view reported the keyboard: want the view", got)
	}
	b.ReportPress(view, 3, 4)
	if len(h.rt.downs) != 1 {
		t.Errorf("the content noted %d presses of the shown view: want one", len(h.rt.downs))
	}
}
