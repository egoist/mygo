//go:build !js

package e2e

import (
	"errors"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// This file is about views of one window showing over each other: what the
// content paints last takes the pointer where two of them overlap, and the
// order the frame paints them in is the order the platform stacks them.
//
// The views are real: controls of the platform's toolkit, made with
// newNativeViewFixture, or one of them and a real page in an embedded web
// view. Every click goes through the platform's own input (the native view
// click hook), and what took it is read back from the view itself: the
// control's own reports, or the page's own script.
//
// The two boxes, at known nonzero origins, and what they leave each other:
//
//	(30,30) +-----------------+
//	        |        A        |
//	        |     +-----------------+
//	        |     |   |   overlap   |
//	        +-----|---+  B          |
//	              |                 |
//	              +-----------------+
//
// A is 200x180 at (30, 30), B the same size at (130, 120): they overlap on
// x 130..230, y 120..210, and each keeps a part of its own, which a click
// must still reach.

const (
	nvOverlapWidth  = 400
	nvOverlapHeight = 320

	nvOverlapAX, nvOverlapAY = 30, 30
	nvOverlapBX, nvOverlapBY = 130, 120
	nvOverlapW, nvOverlapH   = 200, 180
)

// The points the tests click: the middle of the overlap, and a part of each
// box the other one does not cover.
var (
	nvOverlapBoth  = [2]float64{nvOverlapAX + 150, nvOverlapAY + 135} // (180, 165)
	nvOverlapAOnly = [2]float64{nvOverlapAX + 40, nvOverlapAY + 60}   // (70, 90)
	nvOverlapBOnly = [2]float64{nvOverlapBX + 150, nvOverlapBY + 130} // (280, 250)
)

// nvOverlapPage is the page of the web view in the mixed overlap: a click
// on it counts in its own script and lands in the input that fills it, so
// the test reads the winner from the page itself.
const nvOverlapPage = `<!doctype html><html><body style="margin:0;height:100vh;background:#fff"
	onmousedown="window.clicks = (window.clicks || 0) + 1">
<input id="hit" style="position:absolute;left:0;top:0;width:100%;height:100%;border:0">
<script>window.ready = true;</script></body></html>`

// nvOverlap is a window of native UI showing two views over each other: a
// real control of the platform's toolkit in box A, and in box B either a
// second control or an embedded page. bFirst is the order the frame paints
// them in: false paints A first, so B shows over it.
type nvOverlap struct {
	t *testing.T
	w *mygo.Window

	a   *mygo.NativeView
	b   *mygo.NativeView
	web *mygo.WebView

	fxA, fxB nativeViewFixture

	// bFirst is whether the frame paints B before A. A test changes it and
	// makes a frame: the same views, the same boxes, the other stacking.
	bFirst atomic.Bool
	// aHidden is whether the frame paints A at all. A test hides A and
	// shows it again without remaking it: the same control, the same box,
	// and the platform maps its window over B's when it comes back, which
	// is the order the frame painted them in only because the surface puts
	// it back (internal/linux, orderViews).
	aHidden atomic.Bool
	// frames counts the frames the content built, so that a test can wait
	// for the one after it changed bFirst.
	frames atomic.Int32
}

// newNativeViewOverlap makes the window and its two views, withPage making
// box B an embedded page instead of a second control. It skips where the
// platform has no real control to host, and fails everywhere else: an error
// here is the product's, not the platform's.
func newNativeViewOverlap(t *testing.T, withPage bool) *nvOverlap {
	t.Helper()
	o := &nvOverlap{t: t}
	var a, b *mygo.NativeView
	var page *mygo.WebView
	placeA := func(c *ui.Context) {
		if a != nil && !o.aHidden.Load() {
			ui.NativeView(c, a).Absolute().Left(nvOverlapAX).Top(nvOverlapAY).Size(nvOverlapW, nvOverlapH)
		}
	}
	placeB := func(c *ui.Context) {
		switch {
		case page != nil:
			ui.WebView(c, page).Absolute().Left(nvOverlapBX).Top(nvOverlapBY).Size(nvOverlapW, nvOverlapH)
		case b != nil:
			ui.NativeView(c, b).Absolute().Left(nvOverlapBX).Top(nvOverlapBY).Size(nvOverlapW, nvOverlapH)
		}
	}
	o.w = newWindow(t, mygo.WindowOptions{
		Title:  "Native view overlap",
		Width:  nvOverlapWidth,
		Height: nvOverlapHeight,
		Content: ui.View(func(c *ui.Context) {
			o.frames.Add(1)
			ui.Box(c).Fill().Children(func() {
				if o.bFirst.Load() {
					placeB(c)
					placeA(c)
					return
				}
				placeA(c)
				placeB(c)
			})
		}),
	})
	eventually(t, "a frame", func() bool { return o.frames.Load() > 0 })

	make := func(slot *nativeViewFixture) func(*mygo.NativeViewHost) (mygo.NativeViewAdapter, error) {
		return func(host *mygo.NativeViewHost) (mygo.NativeViewAdapter, error) {
			f, err := newNativeViewFixture(host)
			if err != nil {
				return nil, err
			}
			*slot = f
			return f, nil
		}
	}
	va, err := mygo.NewNativeView(o.w, mygo.NativeViewOptions{Create: make(&o.fxA)})
	nvOverlapErr(t, "NewNativeView", err)
	o.a = va
	if withPage {
		v, err := o.w.NewWebView(mygo.WebViewOptions{})
		if err != nil {
			t.Fatalf("NewWebView: %v", err)
		}
		o.web = v
		v.Page().LoadHTML(nvOverlapPage, "")
		waitForPage(t, v.Page(), "window.ready")
	} else {
		vb, err := mygo.NewNativeView(o.w, mygo.NativeViewOptions{Create: make(&o.fxB)})
		nvOverlapErr(t, "NewNativeView", err)
		o.b = vb
	}
	o.w.Update(func() { a, b, page = o.a, o.b, o.web })

	eventually(t, "the control placed", func() bool {
		_, visible := o.fxA.Box()
		return visible.Width > 0 && visible.Height > 0
	})
	if !withPage {
		eventually(t, "the second control placed", func() bool {
			_, visible := o.fxB.Box()
			return visible.Width > 0 && visible.Height > 0
		})
	}
	o.w.Focus()
	return o
}

// nvOverlapErr turns the failure to make a real control into a skip where
// the platform has no fixture for one, leaving every other error a failure.
func nvOverlapErr(t *testing.T, what string, err error) {
	t.Helper()
	switch {
	case err == nil:
	case errors.Is(err, errNoNativeViewFixture):
		t.Skip("no native view fixture on this platform")
	case errors.Is(err, errors.ErrUnsupported):
		t.Skip("native views are unsupported on this platform")
	default:
		t.Fatalf("%s: %v", what, err)
	}
}

// click clicks a point of the window's surface as a user does, through the
// native view click hook, and skips where the platform cannot automate a
// press.
func (o *nvOverlap) click(p [2]float64) {
	o.t.Helper()
	(&nativeViewTest{t: o.t, w: o.w}).click(p[0], p[1])
}

// reverse makes a frame that paints the two views in the other order, with
// nothing remade: the views keep their identity and their boxes.
func (o *nvOverlap) reverse() {
	o.t.Helper()
	before := o.frames.Load()
	o.bFirst.Store(true)
	o.w.Update(func() {})
	eventually(o.t, "the frame painting the views the other way", func() bool {
		return o.frames.Load() > before
	})
}

// TestNativeViewOverlapPaintsInOrder shows two real controls over each
// other in one window and checks what takes a click on the overlap: the one
// the frame paints last, the other one everywhere else, and the other way
// round once the frame paints them in the other order. Restacking them must
// not remake, destroy or move either control.
func TestNativeViewOverlapPaintsInOrder(t *testing.T) {
	o := newNativeViewOverlap(t, false)
	wantA := mygo.Rectangle{X: nvOverlapAX, Y: nvOverlapAY, Width: nvOverlapW, Height: nvOverlapH}
	wantB := mygo.Rectangle{X: nvOverlapBX, Y: nvOverlapBY, Width: nvOverlapW, Height: nvOverlapH}
	boxA, _ := o.fxA.Box()
	boxB, _ := o.fxB.Box()
	if !nvSame(boxA, wantA) || !nvSame(boxB, wantB) {
		t.Fatalf("the controls are at %+v and %+v, want %+v and %+v", boxA, boxB, wantA, wantB)
	}
	handleA, handleB := o.a.NativeHandle(), o.b.NativeHandle()

	// What each control keeps to itself reaches it, whichever shows over
	// the other.
	o.click(nvOverlapAOnly)
	eventually(t, "the first control taking a click on its own part", func() bool {
		return o.fxA.Reports().Presses() == 1
	})
	o.click(nvOverlapBOnly)
	eventually(t, "the second control taking a click on its own part", func() bool {
		return o.fxB.Reports().Presses() == 1
	})

	// A is painted first, so B shows over it and takes the overlap.
	o.click(nvOverlapBoth)
	eventually(t, "one of the two controls taking the click on the overlap", func() bool {
		return o.fxA.Reports().Presses() > 1 || o.fxB.Reports().Presses() > 1
	})
	if got := o.fxB.Reports().Presses(); got != 2 {
		t.Errorf("the control painted last heard %d presses, want 2: the click on the overlap did not reach it", got)
	}
	if got := o.fxA.Reports().Presses(); got != 1 {
		t.Errorf("the control under the other heard %d presses, want 1: the click on the overlap went through to it", got)
	}

	// The frame paints them the other way: the same controls at the same
	// boxes, and now A takes the overlap.
	o.reverse()
	nowA, _ := o.fxA.Box()
	nowB, _ := o.fxB.Box()
	if !nvSame(nowA, wantA) || !nvSame(nowB, wantB) {
		t.Errorf("restacking moved the controls to %+v and %+v, want %+v and %+v", nowA, nowB, wantA, wantB)
	}
	if o.a.NativeHandle() != handleA || o.b.NativeHandle() != handleB {
		t.Error("restacking remade a control")
	}
	if o.a.IsDestroyed() || o.b.IsDestroyed() {
		t.Error("restacking destroyed a control")
	}
	if got := o.fxA.Reports().Destroyed() + o.fxB.Reports().Destroyed(); got != 0 {
		t.Errorf("restacking destroyed %d controls, want none", got)
	}
	o.click(nvOverlapBoth)
	eventually(t, "one of the two controls taking the click after the order reversed", func() bool {
		return o.fxA.Reports().Presses() > 1 || o.fxB.Reports().Presses() > 2
	})
	if got := o.fxA.Reports().Presses(); got != 2 {
		t.Errorf("the control painted last after the order reversed heard %d presses, want 2", got)
	}
	if got := o.fxB.Reports().Presses(); got != 2 {
		t.Errorf("the control now under the other heard %d presses, want 2: the click on the overlap went through to it", got)
	}
	// The part the other one never covered is still its own.
	o.click(nvOverlapBOnly)
	eventually(t, "the control now under the other taking a click on its own part", func() bool {
		return o.fxB.Reports().Presses() == 3
	})
}

// setA makes a frame that paints A or paints it no more, with nothing
// remade: the same control in the same box, hidden and shown again, as the
// content of a view that goes away and comes back does.
func (o *nvOverlap) setA(show bool) {
	o.t.Helper()
	before := o.frames.Load()
	o.aHidden.Store(!show)
	o.w.Update(func() {})
	eventually(o.t, "the frame painting A or not", func() bool {
		return o.frames.Load() > before
	})
	eventually(o.t, "the control hidden or shown again", func() bool {
		_, visible := o.fxA.Box()
		if show {
			return visible.Width > 0 && visible.Height > 0
		}
		return visible.Width == 0 && visible.Height == 0
	})
}

// clickOverlap clicks the point the two controls share and reports what
// each of them heard of it. Tests read who took the click, not how many
// press events one click made: a click in the same place as the one before
// it is a double (or triple) click to GTK, which delivers one
// button-press-event per click level (GDK_BUTTON_PRESS, then
// GDK_2BUTTON_PRESS, then GDK_3BUTTON_PRESS, all of them that signal), so
// the count of a click is more than one wherever the click before it was at
// the same point, and how much more depends on how fast the test got there.
func (o *nvOverlap) clickOverlap() (aPresses, bPresses int32) {
	o.t.Helper()
	aBefore, bBefore := o.fxA.Reports().Presses(), o.fxB.Reports().Presses()
	o.click(nvOverlapBoth)
	eventually(o.t, "one of the two controls hearing the click on the overlap", func() bool {
		return o.fxA.Reports().Presses() != aBefore || o.fxB.Reports().Presses() != bBefore
	})
	return o.fxA.Reports().Presses() - aBefore, o.fxB.Reports().Presses() - bBefore
}

// TestNativeViewOverlapHidesAndShowsTheLowerView is the view that goes away
// and comes back under the one over it: the frame paints A first and B over
// it, the frame paints A no more, and then paints it again without remaking
// it. The platform maps A's window over B's as it comes back, which is not
// the order the frame paints them in, so the surface must put B back over
// A: a click on their overlap keeps reaching B while A is away and after A
// is back, and A keeps its box and its identity. Then the frame paints A
// last, which is what the pointer follows: the overlap is A's, and each
// control still keeps the part the other does not cover.
func TestNativeViewOverlapHidesAndShowsTheLowerView(t *testing.T) {
	o := newNativeViewOverlap(t, false)
	wantA := mygo.Rectangle{X: nvOverlapAX, Y: nvOverlapAY, Width: nvOverlapW, Height: nvOverlapH}
	boxA, _ := o.fxA.Box()
	if !nvSame(boxA, wantA) {
		t.Fatalf("the control is at %+v, want %+v", boxA, wantA)
	}
	handleA := o.a.NativeHandle()

	// A is painted first: B shows over it and takes the overlap. This is
	// the first click of this window and no click was at this point before
	// it, so the toolkit makes one press of it.
	o.click(nvOverlapBoth)
	eventually(t, "one of the two controls taking the click on the overlap", func() bool {
		return o.fxA.Reports().Presses() > 0 || o.fxB.Reports().Presses() > 0
	})
	if got := o.fxB.Reports().Presses(); got != 1 {
		t.Errorf("the control painted last heard %d presses, want 1: the click on the overlap did not reach it", got)
	}
	if got := o.fxA.Reports().Presses(); got != 0 {
		t.Errorf("the control under the other heard %d presses, want none: the click on the overlap went through to it", got)
	}

	// The frame paints A no more: nothing of it shows, and the control over
	// it keeps the overlap while it is away.
	o.setA(false)
	if a, b := o.clickOverlap(); a != 0 || b == 0 {
		t.Errorf("while A was hidden the click on the overlap reached the hidden control %d times and the control painted last %d times, want none and at least one", a, b)
	}

	// The frame paints A again, under B. Its window is mapped over the one
	// the frame paints last, so the overlap is still B's, and A came back
	// where it was, itself.
	o.setA(true)
	nowA, _ := o.fxA.Box()
	if !nvSame(nowA, wantA) {
		t.Errorf("the control came back at %+v, want %+v", nowA, wantA)
	}
	if o.a.NativeHandle() != handleA {
		t.Error("hiding and showing the control remade it")
	}
	if o.a.IsDestroyed() || o.fxA.Reports().Destroyed() != 0 {
		t.Error("hiding and showing the control destroyed it")
	}
	if a, b := o.clickOverlap(); a != 0 || b == 0 {
		t.Errorf("after A came back the click on the overlap reached it %d times and the control painted last %d times, want none and at least one: the control that came back was left over it", a, b)
	}

	// The frame paints A last: the order the frame paints them in is the
	// order the pointer follows, so the overlap is A's now.
	o.reverse()
	nowA, _ = o.fxA.Box()
	if !nvSame(nowA, wantA) {
		t.Errorf("the order reversed moved the control to %+v, want %+v", nowA, wantA)
	}
	if a, b := o.clickOverlap(); a == 0 || b != 0 {
		t.Errorf("after the order reversed the click on the overlap reached the control painted last %d times and the other %d times, want at least one and none", a, b)
	}

	// The part the other one never covered is still its own.
	before := o.fxA.Reports().Presses()
	o.click(nvOverlapAOnly)
	eventually(t, "the control painted last taking a click on its own part", func() bool {
		return o.fxA.Reports().Presses() == before+1
	})
	if got := o.fxA.Reports().Presses(); got != before+1 {
		t.Errorf("the control painted last heard %d presses on its own part, want %d", got, before+1)
	}
}

// TestNativeViewWebViewOverlapOrder shows a real control and an embedded
// page over each other in one window, in both paint orders: the page takes
// a click on the overlap where the frame paints it last, the control takes
// it where the frame paints the control last, and each keeps the part the
// other does not cover. Nothing is reloaded and nothing is remade to
// reverse them.
func TestNativeViewWebViewOverlapOrder(t *testing.T) {
	o := newNativeViewOverlap(t, true)
	wantA := mygo.Rectangle{X: nvOverlapAX, Y: nvOverlapAY, Width: nvOverlapW, Height: nvOverlapH}
	boxA, _ := o.fxA.Box()
	if !nvSame(boxA, wantA) {
		t.Fatalf("the control is at %+v, want %+v", boxA, wantA)
	}
	handleA := o.a.NativeHandle()
	clicks := func() float64 {
		n, _ := mygo.EvalAs[float64](o.web.Page(), "window.clicks || 0")
		return n
	}
	focused := func() string {
		id, _ := mygo.EvalAs[string](o.web.Page(), "document.activeElement ? document.activeElement.id : ''")
		return id
	}

	// The page's own part, where the control does not cover it: the click
	// reaches the page and lands in the input that fills it.
	o.click(nvOverlapBOnly)
	eventually(t, "the page taking a click on its own part", func() bool { return clicks() == 1 })
	if id := focused(); id != "hit" {
		t.Errorf("the page's click went to %q, want the input filling it", id)
	}
	// And the control's own part reaches the control.
	o.click(nvOverlapAOnly)
	eventually(t, "the control taking a click on its own part", func() bool {
		return o.fxA.Reports().Presses() == 1
	})

	// The frame painted the control first: the page shows over it, so the
	// click on the overlap is the page's, and the control hears nothing.
	o.click(nvOverlapBoth)
	eventually(t, "either view taking the click on the overlap", func() bool {
		return clicks() > 1 || o.fxA.Reports().Presses() > 1
	})
	if got := clicks(); got != 2 {
		t.Errorf("the page painted last heard %v clicks, want 2: the control over it took the click", got)
	}
	if got := o.fxA.Reports().Presses(); got != 1 {
		t.Errorf("the control under the page heard %d presses, want 1: the click on the overlap went through to it", got)
	}

	// The frame paints the control last: the same control at the same box,
	// the same page, and now the control takes the overlap.
	o.reverse()
	nowA, _ := o.fxA.Box()
	if !nvSame(nowA, wantA) {
		t.Errorf("restacking moved the control to %+v, want %+v", nowA, wantA)
	}
	if o.a.NativeHandle() != handleA {
		t.Error("restacking remade the control")
	}
	if o.a.IsDestroyed() || o.web.IsDestroyed() {
		t.Error("restacking destroyed a view")
	}
	if got := o.fxA.Reports().Destroyed(); got != 0 {
		t.Errorf("restacking destroyed the control %d times, want none", got)
	}
	o.click(nvOverlapBoth)
	eventually(t, "either view taking the click after the order reversed", func() bool {
		return clicks() > 2 || o.fxA.Reports().Presses() > 1
	})
	if got := o.fxA.Reports().Presses(); got != 2 {
		t.Errorf("the control painted last after the order reversed heard %d presses, want 2", got)
	}
	if got := clicks(); got != 2 {
		t.Errorf("the page under the control heard %v clicks, want 2: the click on the overlap went through to it", got)
	}
	// The page's own part is still the page's, and still takes its input.
	o.click(nvOverlapBOnly)
	eventually(t, "the page taking a click on its own part after the order reversed", func() bool {
		return clicks() == 3
	})
	if id := focused(); id != "hit" {
		t.Errorf("the page's click went to %q, want the input filling it", id)
	}
}
