//go:build !js

package e2e

import (
	"errors"
	"math"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// This file hosts a real control of the platform's toolkit, made by the
// app's own NativeViewOptions.Create, in a window of native UI:
//
//   - macOS: an NSTextView (nativeview_darwin_test.go)
//   - Linux: a GtkEntry (nativeview_linux_test.go)
//   - Windows: an EDIT child window (nativeview_windows_test.go)
//
// The platform files fill in newNativeViewFixture and nothing else; the
// tests here only drive the window with the platform's own input (click,
// pressTab, typing) and read the control's own state back, so no callback
// under test is called by the test itself to stand in for an event.
//
// The control is the app's: MyGo places it, resizes it, clips it, hides it
// and gives it the keyboard, and gives the keyboard back; it never touches
// the control's text or its input method. Where a platform cannot automate
// clicks or keys, a test skips; on a platform without a fixture at all,
// newNativeViewFixture returns errNoNativeViewFixture and every test here
// skips (internal/e2e/nativeview_other_test.go).
//
// The window these tests build, top to bottom:
//
//	                +-------------------+
//	                | Menu   (100x30)   |
//	                +-------------------+
//	                | Before (TextInput)|
//	                +-------------------+ <- the popover opens below the
//	                |                   |    button, 180x60, over the
//	                |   the native      |    control: a click there is
//	                |   control         |    the ui's, not the control's
//	                |                   |
//	                +-------------------+
//	                | After  (TextInput)|
//	                +-------------------+
//
// Tab goes Menu, Before, the control, After, and Shift+Tab back.

// nvWidth and nvHeight are the window's size; nvPanelWidth and
// nvPanelHeight the popover panel's, which reaches over the top of the
// control below the menu button, and nvMenuHeight the button's height.
const (
	nvWidth       = 420
	nvHeight      = 320
	nvMenuHeight  = 30
	nvPanelWidth  = 180
	nvPanelHeight = 60
)

// errNoNativeViewFixture is what a platform without a native view fixture
// returns: the tests of this file skip there.
var errNoNativeViewFixture = errors.New("e2e: no native view fixture on this platform")

// nativeViewReports counts what a control reported to its host: the host
// callbacks of mygo.NativeViewHost, which the control's own events call.
type nativeViewReports struct {
	// focused counts NativeViewHost.Focused reports: the control took the
	// keyboard, as a click on one of its controls does.
	focused atomic.Int32
	// tabOut counts NativeViewHost.TabOut reports, and back is what the
	// last one reported: Tab moved past the control's last control, or
	// Shift+Tab past its first.
	tabOut atomic.Int32
	back   atomic.Bool
	// presses counts NativeViewHost.PointerDown reports, at is where the
	// last one was, in the control's own units (math.Float64bits).
	presses atomic.Int32
	at      [2]atomic.Uint64
	// destroyed counts the control's Destroy calls: exactly one when MyGo
	// did its job, whatever else the app and the window did.
	destroyed atomic.Int32
}

// Focused reports how many times the control reported taking the keyboard.
func (r *nativeViewReports) Focused() int32 { return r.focused.Load() }

// TabbedOut reports how many times the control reported Tab out of it.
func (r *nativeViewReports) TabbedOut() int32 { return r.tabOut.Load() }

// Back reports whether the last Tab out of the control was a Shift+Tab.
func (r *nativeViewReports) Back() bool { return r.back.Load() }

// Presses reports how many pointer downs the control reported.
func (r *nativeViewReports) Presses() int32 { return r.presses.Load() }

// At is where the control reported its last pointer down: the control's own
// point. The adapter's documentation calls it the view's own pixels while
// the platform forwards it to the content as surface DIPs, so the tests
// only require it to be inside the control's box, and the conversion of a
// native event is the fixture's, explicitly.
func (r *nativeViewReports) At() (x, y float64) {
	return math.Float64frombits(r.at[0].Load()), math.Float64frombits(r.at[1].Load())
}

// Destroyed reports how many times the control's Destroy ran.
func (r *nativeViewReports) Destroyed() int32 { return r.destroyed.Load() }

// nativeViewEvents is what the platform fixtures embed: the host the app
// made the control with, and what the control reported.
type nativeViewEvents struct {
	host    *mygo.NativeViewHost
	reports nativeViewReports
}

// bind takes the host the control was made with. A fixture calls it from
// its constructor, before it returns.
func (v *nativeViewEvents) bind(host *mygo.NativeViewHost) { v.host = host }

// tookFocus reports that the control took the keyboard.
func (v *nativeViewEvents) tookFocus() {
	v.reports.focused.Add(1)
	if v.host != nil && v.host.Focused != nil {
		v.host.Focused()
	}
}

// tabbedOut reports Tab (back: Shift+Tab) moving the keyboard past the
// control.
func (v *nativeViewEvents) tabbedOut(back bool) {
	v.reports.tabOut.Add(1)
	v.reports.back.Store(back)
	if v.host != nil && v.host.TabOut != nil {
		v.host.TabOut(back)
	}
}

// pointerDown reports the pointer going down at (x, y) of the control.
func (v *nativeViewEvents) pointerDown(x, y float64) {
	v.reports.presses.Add(1)
	v.reports.at[0].Store(math.Float64bits(x))
	v.reports.at[1].Store(math.Float64bits(y))
	if v.host != nil && v.host.PointerDown != nil {
		v.host.PointerDown(x, y)
	}
}

// destroyedControl reports the control's own Destroy running: a fixture
// calls it there, once.
func (v *nativeViewEvents) destroyedControl() { v.reports.destroyed.Add(1) }

// Reports is what the control reported to its host.
func (v *nativeViewEvents) Reports() *nativeViewReports { return &v.reports }

// nativeViewFixture is a real control of the platform's toolkit, standing
// in for the app's own: mygo.NativeViewAdapter plus what the tests read of
// it.
//
// Its methods are called from the window's thread.
type nativeViewFixture interface {
	mygo.NativeViewAdapter
	// Reports is what the control reported to its host.
	Reports() *nativeViewReports
	// Text is the control's own text.
	Text() string
	// SetText sets the control's own text, as the app does.
	SetText(string)
	// HoldsKeyboard reports whether the control has the keyboard itself:
	// it is the window's first responder (macOS), has the GTK focus
	// (Linux), or the Win32 focus (Windows).
	HoldsKeyboard() bool
	// Box is the control's box and the part of it the window shows, in the
	// window surface's DIPs: both zero when the control does not show.
	Box() (box, visible mygo.Rectangle)
}

// newNativeViewFixture makes the platform's control, in the parent window
// the host reports, and starts reporting the control's events to the host.
// Each platform file of these tests defines it: nativeview_darwin_test.go,
// nativeview_linux_test.go, nativeview_windows_test.go, and
// nativeview_other_test.go where the platform has no control to make.

// nativeViewTest is a window of native UI with a real control of the
// platform's toolkit between two text inputs, under a popover the view
// opens over it.
type nativeViewTest struct {
	t       *testing.T
	w       *mygo.Window
	view    *mygo.NativeView
	fixture nativeViewFixture

	// frames counts the frames the content built.
	frames atomic.Int32
	// focused is the label of the ui element with the focus, "" for none:
	// the content records it as the frame is built.
	focused atomic.Value
	before  string
	after   string
	// open is set while the popover over the control is open, items the
	// clicks the popover took.
	open  atomic.Bool
	items atomic.Int32
	// shown reports whether the content places the control.
	shown atomic.Bool
	// tooTall places the control taller than the window, so the platform
	// has to clip it.
	tooTall atomic.Bool
}

// newNativeViewTest makes the window, waits for its first frame, makes the
// real control through the app's factory, and places it in the next frame.
func newNativeViewTest(t *testing.T) *nativeViewTest {
	t.Helper()
	n := &nativeViewTest{t: t}
	n.shown.Store(true)
	var view *mygo.NativeView
	n.w = newWindow(t, mygo.WindowOptions{
		Title:  "Native view",
		Width:  nvWidth,
		Height: nvHeight,
		Content: ui.View(func(c *ui.Context) {
			n.frames.Add(1)
			focused := ""
			ui.Column(c).Fill().AlignItems(ui.Stretch).Children(func() {
				menu := ui.Button(c, "Menu").Height(nvMenuHeight).Width(100)
				if menu.Clicked() {
					n.open.Store(true)
				}
				open := n.open.Load()
				ui.Popover(c, menu, &open, func() {
					if ui.Box(c).Size(nvPanelWidth, nvPanelHeight).Background(ui.RGB(255, 0, 0)).Clicked() {
						n.items.Add(1)
					}
				})
				if !open {
					n.open.Store(false)
				}
				// The inputs carry explicit keys: the native view's node is
				// built only while the content shows it, so without a key
				// the input after it is numbered again once the view goes,
				// and takes a new identity — and its focus — with it (see
				// ui.TestNativeViewHiddenKeepsKeyedSiblingIdentity).
				if ui.TextInput(c.Key("before"), &n.before).Label("Before").Focused() {
					focused = "Before"
				}
				if view != nil && n.shown.Load() {
					if n.tooTall.Load() {
						ui.NativeView(c, view).Height(nvHeight)
					} else {
						ui.NativeView(c, view).Grow(1)
					}
				}
				if ui.TextInput(c.Key("after"), &n.after).Label("After").Focused() {
					focused = "After"
				}
			})
			n.focused.Store(focused)
		}),
	})
	eventually(t, "a frame", func() bool { return n.frames.Load() > 0 })

	v, err := mygo.NewNativeView(n.w, mygo.NativeViewOptions{Create: func(host *mygo.NativeViewHost) (mygo.NativeViewAdapter, error) {
		fx, err := newNativeViewFixture(host)
		if err != nil {
			return nil, err
		}
		n.fixture = fx
		return fx, nil
	}})
	switch {
	case errors.Is(err, errNoNativeViewFixture):
		t.Skip("no native view fixture on this platform")
	case errors.Is(err, errors.ErrUnsupported):
		t.Skip("native views are unsupported on this platform")
	case err != nil:
		t.Fatalf("NewNativeView: %v", err)
	}
	n.view = v
	// The content closure below reads view on the main thread, where the
	// frame is built: publish it there, through Update, not from this
	// goroutine while the frame loop is running.
	n.w.Update(func() { view = v })
	eventually(t, "the control placed", n.shows)
	return n
}

// shows reports whether the window shows the control where the frame puts
// it: its visible part is not empty.
func (n *nativeViewTest) shows() bool {
	_, visible := n.fixture.Box()
	return visible.Width > 0 && visible.Height > 0
}

// focusedNow is the label of the ui element with the focus, "" for none:
// the last frame the content built.
func (n *nativeViewTest) focusedNow() string {
	s, _ := n.focused.Load().(string)
	return s
}

// click clicks a point of the window's surface as the mouse does, and skips
// where the platform cannot automate a click. It uses the native-view
// helpers, not the general click hook: a control that tracks the mouse until
// the release arrives (NSTextView does) needs the release in the
// application's own event queue, which nvClick arranges.
func (n *nativeViewTest) click(x, y float64) {
	n.t.Helper()
	if !nvClick(n.w, x, y) {
		n.t.Skip("click automation is not available on this platform")
	}
}

// clickControl clicks the centre of the control, as a user does.
func (n *nativeViewTest) clickControl() {
	n.t.Helper()
	box, _ := n.fixture.Box()
	n.click(nvAt(box, 0.5, 0.5))
}

// nvAt is a point of a box, as the click hooks take it: the box's fields are
// whole DIPs, the hooks take a point.
func nvAt(box mygo.Rectangle, fx, fy float64) (x, y float64) {
	return float64(box.X) + fx*float64(box.Width), float64(box.Y) + fy*float64(box.Height)
}

// tab presses Tab (back: Shift+Tab) as the keyboard does, and skips where
// the platform cannot automate a key.
func (n *nativeViewTest) tab(back bool) {
	n.t.Helper()
	if !pressTab(n.w, back) {
		n.t.Skip("key automation is not available on this platform")
	}
}

// typeInto types text into the control holding the keyboard, as a user
// does, and skips where the platform cannot automate keys.
func (n *nativeViewTest) typeInto(text string) {
	n.t.Helper()
	if nvTypeInto(n.w, text) {
		return
	}
	// Linux automates keys through XTest, into whatever holds the
	// keyboard: the control already does.
	keys := make([]string, 0, len(text))
	for _, r := range text {
		keys = append(keys, string(r))
	}
	if pressKeys(keys...) {
		return
	}
	n.t.Skip("key automation is not available on this platform")
}

// nvSame reports whether two rectangles are the same, to the DIP: the
// window shows all of a control it does not clip.
func nvSame(a, b mygo.Rectangle) bool {
	const epsilon = 1
	return abs(a.X-b.X) < epsilon && abs(a.Y-b.Y) < epsilon &&
		abs(a.Width-b.Width) < epsilon && abs(a.Height-b.Height) < epsilon
}

// abs is the absolute value of n.
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// TestNativeViewShowsRealControl places a real control of the platform's
// toolkit in a window of native UI: it occupies the box of the element that
// shows it, the window shows all of it, and its own text is the app's to
// read and write.
func TestNativeViewShowsRealControl(t *testing.T) {
	n := newNativeViewTest(t)
	box, visible := n.fixture.Box()
	if box.Width <= 0 || box.Height <= 0 {
		t.Fatalf("the control's box is %+v", box)
	}
	if !nvSame(visible, box) {
		t.Errorf("the control shows %+v of its box %+v, want all of it", visible, box)
	}
	// It sits where the frame put it: below the input before it, inside the
	// window, full width of the column.
	if box.Y <= 0 || box.Y+box.Height > nvHeight {
		t.Errorf("the control's box %+v is not inside the window(%dx%d)", box, nvWidth, nvHeight)
	}
	if n.view.IsDestroyed() {
		t.Error("the view is destroyed while the window shows it")
	}
	// The control's own text.
	if got := n.fixture.Text(); got != "" {
		t.Errorf("a fresh control holds %q, want no text", got)
	}
	n.fixture.SetText("hello")
	eventually(t, "the control's text", func() bool { return n.fixture.Text() == "hello" })
}

// TestNativeViewSurvivesCompositionLoss takes the GPU device away from the
// DirectComposition device of the window's pixels, which the surface of a
// window that hosts a native view goes through: the hosting of the control is
// not part of it — the platform does not build the clip it hosts the control
// in on the device — so the control is still the one the content placed, in
// the same box, and the content can still take it away and show it again.
// Whether the frames of the window come back is the ui's own renderer's
// business (it presents these windows through DirectComposition itself).
func TestNativeViewSurvivesCompositionLoss(t *testing.T) {
	n := newNativeViewTest(t)
	before, _ := n.fixture.Box()
	if !loseComposition(true) {
		t.Skip("no DirectComposition device to take away")
	}
	eventually(t, "the control still placed", n.shows)
	after, visible := n.fixture.Box()
	if !nvSame(before, after) {
		t.Errorf("the control moved from %+v to %+v when the device went away", before, after)
	}
	if !nvSame(after, visible) {
		t.Errorf("the control shows %+v of its box %+v after the device went away", visible, after)
	}
	// The content still places it: hiding it and showing it again works.
	n.shown.Store(false)
	n.w.Update(func() {})
	eventually(t, "the control hidden after the device went away", func() bool {
		_, visible := n.fixture.Box()
		return visible.Width == 0 && visible.Height == 0
	})
	n.shown.Store(true)
	n.w.Update(func() {})
	eventually(t, "the control showing again after the device went away", n.shows)
}

// TestNativeViewClickGivesItTheKeyboard clicks the control: the window's
// own event routing takes the click to the real control, which takes the
// keyboard there and reports its focus and the press to the content.
func TestNativeViewClickGivesItTheKeyboard(t *testing.T) {
	n := newNativeViewTest(t)
	n.w.Focus()
	n.clickControl()

	eventually(t, "the control taking the keyboard", func() bool { return n.fixture.HoldsKeyboard() })
	eventually(t, "the control reporting its focus", func() bool { return n.fixture.Reports().Focused() >= 1 })
	eventually(t, "the control reporting the press", func() bool { return n.fixture.Reports().Presses() >= 1 })
	// The toolkit may hand the control the keyboard more than once for one
	// click (the window becoming key, the click itself): a second report is
	// legitimate, the control simply must have reported it.
	if got := n.fixture.Reports().Focused(); got < 1 {
		t.Errorf("the control reported its focus %d times, want at least 1", got)
	}
	// The press is a point of the control itself.
	box, _ := n.fixture.Box()
	x, y := n.fixture.Reports().At()
	if x < 0 || x > float64(box.Width) || y < 0 || y > float64(box.Height) {
		t.Errorf("the press at (%v, %v) is not inside the control's box %+v", x, y, box)
	}
	// The ui did not keep the focus: the control has it.
	n.w.Invalidate()
	if got := n.focusedNow(); got != "" {
		t.Errorf("the ui shows %q focused while the control has the keyboard", got)
	}
}

// waitFrame waits for the content to build a frame after now, so a test can
// tell what a focus move left behind from what a frame does.
func (n *nativeViewTest) waitFrame() {
	n.t.Helper()
	before := n.frames.Load()
	eventually(n.t, "a frame", func() bool {
		n.w.Invalidate()
		return n.frames.Load() > before
	})
}

// TestNativeViewFocusProgrammatically moves the keyboard to the control from
// the test's own goroutine, through the public mygo.NativeView.Focus, with no
// press at all: the content shows the control focused, the real control takes
// the keyboard and types, and the ui shows none of its own elements focused.
// Tab hands the keyboard on from it, as it does after a press. A control the
// content does not show, and one the app destroyed, take the keyboard from
// nothing: Focus leaves the ui's own focus, and the keyboard, where they are.
func TestNativeViewFocusProgrammatically(t *testing.T) {
	n := newNativeViewTest(t)
	n.w.Focus()

	// No press: the app moves the keyboard to the control, which takes it
	// and can be typed into.
	n.view.Focus(false)
	eventually(t, "the control taking the keyboard", func() bool { return n.fixture.HoldsKeyboard() })
	n.typeInto("typed")
	eventually(t, "the text in the control", func() bool { return n.fixture.Text() == "typed" })
	// The ui did not keep the focus: the control has it.
	n.w.Invalidate()
	if got := n.focusedNow(); got != "" {
		t.Errorf("the ui shows %q focused while the control holds the keyboard", got)
	}

	// Tab moves the keyboard on from the control, as it does after a press.
	n.tab(false)
	eventually(t, "Tab out of the control", func() bool {
		n.w.Invalidate()
		return !n.fixture.HoldsKeyboard() && n.focusedNow() == "After"
	})

	// The content stops showing the control: the frame alone leaves the ui's
	// own focus alone, and Focus takes the keyboard from nothing.
	n.shown.Store(false)
	n.w.Update(func() {})
	eventually(t, "the control hidden", func() bool { _, visible := n.fixture.Box(); return visible.Width == 0 && visible.Height == 0 })
	n.waitFrame()
	if got := n.focusedNow(); got != "After" {
		t.Errorf("hiding the control left the ui showing %q focused, want After", got)
	}
	n.view.Focus(false)
	n.waitFrame()
	if n.fixture.HoldsKeyboard() {
		t.Error("Focus took the keyboard for a control the content does not show")
	}
	if got := n.focusedNow(); got != "After" {
		t.Errorf("Focus of a hidden control left the ui showing %q focused, want After", got)
	}

	// And the same once the view is destroyed.
	n.shown.Store(true)
	n.w.Update(func() {})
	eventually(t, "the control showing again", n.shows)
	n.view.Destroy()
	eventually(t, "the view destroyed", n.view.IsDestroyed)
	n.waitFrame()
	if got := n.focusedNow(); got != "After" {
		t.Errorf("showing and destroying the control left the ui showing %q focused, want After", got)
	}
	n.view.Focus(true)
	n.waitFrame()
	if n.fixture.HoldsKeyboard() {
		t.Error("Focus took the keyboard for a destroyed view")
	}
	if got := n.focusedNow(); got != "After" {
		t.Errorf("Focus of a destroyed view left the ui showing %q focused, want After", got)
	}
}

// TestNativeViewTabAndShiftTab tabs in and out of the control: Tab moves
// the keyboard from the input before it into the control and out to the
// input after it, Shift+Tab the other way, and the control reports each
// move to the content.
func TestNativeViewTabAndShiftTab(t *testing.T) {
	n := newNativeViewTest(t)
	n.w.Focus()
	n.clickControl()
	eventually(t, "the control taking the keyboard", func() bool { return n.fixture.HoldsKeyboard() })

	// Tab past the control's last control goes to the input after it.
	n.tab(false)
	eventually(t, "Tab out of the control", func() bool { return !n.fixture.HoldsKeyboard() })
	eventually(t, "the input after the control", func() bool { n.w.Invalidate(); return n.focusedNow() == "After" })
	if got := n.fixture.Reports().TabbedOut(); got != 1 {
		t.Errorf("the control reported Tab out %d times, want 1", got)
	}
	if n.fixture.Reports().Back() {
		t.Error("Tab out of the control was reported as Shift+Tab")
	}

	// Shift+Tab back in, and out the other side.
	n.tab(true)
	eventually(t, "Shift+Tab into the control", func() bool { return n.fixture.HoldsKeyboard() })
	if got := n.fixture.Reports().TabbedOut(); got != 1 {
		t.Errorf("Shift+Tab into the control reported Tab out %d times, want 1", got)
	}
	n.tab(true)
	eventually(t, "Shift+Tab out of the control", func() bool { return !n.fixture.HoldsKeyboard() })
	eventually(t, "the input before the control", func() bool { n.w.Invalidate(); return n.focusedNow() == "Before" })
	if got := n.fixture.Reports().TabbedOut(); got != 2 {
		t.Errorf("the control reported Tab out %d times, want 2", got)
	}
	if !n.fixture.Reports().Back() {
		t.Error("Shift+Tab out of the control was reported as Tab")
	}

	// Tab forward again, into the control and out to the input after it.
	n.tab(false)
	eventually(t, "Tab into the control again", func() bool { return n.fixture.HoldsKeyboard() })
	n.tab(false)
	eventually(t, "Tab out of the control again", func() bool { n.w.Invalidate(); return !n.fixture.HoldsKeyboard() && n.focusedNow() == "After" })
	if got := n.fixture.Reports().TabbedOut(); got != 3 {
		t.Errorf("the control reported Tab out %d times, want 3", got)
	}
	if n.fixture.Reports().Back() {
		t.Error("Tab out of the control was reported as Shift+Tab")
	}
}

// TestNativeViewPopoverTakesItsClick clicks the popover open over the
// control: the click goes to the ui, not to the real control under it, and
// a click on the control itself closes the popover and reaches it.
func TestNativeViewPopoverTakesItsClick(t *testing.T) {
	n := newNativeViewTest(t)
	box, _ := n.fixture.Box()
	if box.Height < 2*nvPanelHeight {
		t.Fatalf("the control's box %+v is too short to click under the popover", box)
	}
	n.w.Focus()
	// The popover, opened from the menu button above the control.
	n.click(50, nvMenuHeight/2)
	eventually(t, "the popover open", n.open.Load)
	// On the popover panel, over the control: the ui takes the click and
	// the control under it hears nothing.
	n.click(nvPanelWidth/2, nvMenuHeight+nvPanelHeight-10)
	eventually(t, "the click on the popover", func() bool { return n.items.Load() == 1 })
	if got := n.fixture.Reports().Presses(); got != 0 {
		t.Errorf("the control under the popover heard %d presses", got)
	}
	if n.fixture.HoldsKeyboard() {
		t.Error("the control under the popover took the keyboard")
	}
	// On the control itself, below the popover: the control takes the
	// press, and the press outside the popover closes it.
	x, y := nvAt(box, 0.5, 1)
	n.click(x, y-10)
	eventually(t, "the control taking the press", func() bool { return n.fixture.Reports().Presses() >= 1 })
	eventually(t, "the popover closing", func() bool { return !n.open.Load() })
	if got := n.items.Load(); got != 1 {
		t.Errorf("the popover took %d clicks, want 1: it took one after it closed", got)
	}
}

// TestNativeViewHideShowKeepsItsText hides the control from the frame and
// shows it again: the control is the app's, so its own text and its
// identity survive (MyGo hides the same view, it does not make another),
// and the ui reaches it again.
func TestNativeViewHideShowKeepsItsText(t *testing.T) {
	n := newNativeViewTest(t)
	n.w.Focus()
	n.clickControl()
	eventually(t, "the control taking the keyboard", func() bool { return n.fixture.HoldsKeyboard() })
	n.typeInto("kept")
	eventually(t, "the text in the control", func() bool { return n.fixture.Text() == "kept" })

	n.shown.Store(false)
	n.w.Update(func() {})
	eventually(t, "the control hidden", func() bool { _, visible := n.fixture.Box(); return visible.Width == 0 && visible.Height == 0 })
	if got := n.fixture.Text(); got != "kept" {
		t.Errorf("hiding the control lost its text: %q", got)
	}
	if n.view.IsDestroyed() {
		t.Error("hiding the control destroyed its view")
	}

	n.shown.Store(true)
	n.w.Update(func() {})
	eventually(t, "the control showing again", n.shows)
	if got := n.fixture.Text(); got != "kept" {
		t.Errorf("showing the control again lost its text: %q", got)
	}
	// It is the same control, and the ui still cycles to it.
	if h := n.view.NativeHandle(); h == 0 {
		t.Error("the control has no native handle after it showed again")
	}
	n.clickControl()
	eventually(t, "the control taking the keyboard again", func() bool { return n.fixture.HoldsKeyboard() })
	n.tab(false)
	eventually(t, "Tab out of the control after it showed again", func() bool { n.w.Invalidate(); return !n.fixture.HoldsKeyboard() && n.focusedNow() == "After" })
}

// TestNativeViewResizeAndPartialClip shortens the window under an element
// taller than it: the platform clips the control and reports the part it
// shows, and the whole control comes back when the window does.
func TestNativeViewResizeAndPartialClip(t *testing.T) {
	n := newNativeViewTest(t)
	box0, visible0 := n.fixture.Box()
	if !nvSame(box0, visible0) {
		t.Fatalf("the window does not show the whole control: box %+v visible %+v", box0, visible0)
	}
	if box0.Width <= 0 || box0.Height <= 0 {
		t.Fatalf("the control has no box: %+v", box0)
	}

	// The control's element is taller than the shortened window: what the
	// window shows is the top of the control, clipped, not all of it.
	n.tooTall.Store(true)
	n.w.SetSize(nvWidth, nvHeight/2)
	eventually(t, "the control clipped by the shorter window", func() bool {
		box, visible := n.fixture.Box()
		return visible.Height > 0 && box.Height > visible.Height
	})
	box, visible := n.fixture.Box()
	if visible.Y != box.Y || visible.Width != box.Width {
		t.Errorf("the visible part %+v of the control is not the top of its box %+v", visible, box)
	}
	if visible.Height > nvHeight/2 {
		t.Errorf("the window shows %d DIPs of the control, more than its own %d DIPs", visible.Height, nvHeight/2)
	}
	if n.view.IsDestroyed() {
		t.Error("resizing the window destroyed the control")
	}

	// Back to the layout the window was made with: the control follows and
	// the whole of it shows again.
	n.tooTall.Store(false)
	n.w.SetSize(nvWidth, nvHeight)
	eventually(t, "the whole control back after the window grew", func() bool {
		box, visible := n.fixture.Box()
		return nvSame(box, visible) && abs(box.Width-box0.Width) <= 2 && abs(box.Height-box0.Height) <= 2
	})
}

// TestNativeViewDestroyIsOnce destroys the view from the app, and from the
// window closing: the control's own Destroy runs exactly once either way, a
// later Destroy does nothing, and a destroyed view keeps no handle. The app
// destroying the view the window closed is how a view the content outlives
// its window goes.
func TestNativeViewDestroyIsOnce(t *testing.T) {
	for _, tt := range []struct {
		name    string
		destroy func(n *nativeViewTest)
	}{
		{"the app destroys it", func(n *nativeViewTest) { n.view.Destroy() }},
		{"the window closes", func(n *nativeViewTest) { n.w.Destroy() }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			n := newNativeViewTest(t)
			eventually(t, "the control showing", n.shows)

			tt.destroy(n)
			if !n.view.IsDestroyed() {
				t.Error("Destroy left the view alive")
			}
			if h := n.view.NativeHandle(); h != 0 {
				t.Errorf("a destroyed view still has its native handle %#x", h)
			}
			if got := n.fixture.Reports().Destroyed(); got != 1 {
				t.Errorf("Destroy ran the control's Destroy %d times, want 1", got)
			}
			// The app destroying the view again, and the window closing
			// again, must not destroy the control a second time.
			n.view.Destroy()
			n.w.Destroy()
			if got := n.fixture.Reports().Destroyed(); got != 1 {
				t.Errorf("a second Destroy ran the control's Destroy %d times, want 1", got)
			}
		})
	}
}

// nativeViewFailingAdapter is a factory's result that MyGo must clean up
// itself: it hands back an adapter that cannot be hosted.
type nativeViewFailingAdapter struct {
	destroyed *atomic.Int32
	handle    uintptr
}

func (a *nativeViewFailingAdapter) NativeHandle() uintptr { return a.handle }
func (a *nativeViewFailingAdapter) Focus(bool)            {}
func (a *nativeViewFailingAdapter) Destroy()              { a.destroyed.Add(1) }

// TestNativeViewFactoryFailureCleansUp fails the app's factory: the window
// reports it and destroys whatever the factory handed back, once, so a
// failed creation leaves no native object behind. It needs no platform
// fixture: the adapters are Go, and only the platform's support for native
// views is needed.
func TestNativeViewFactoryFailureCleansUp(t *testing.T) {
	t.Run("an error with an adapter", func(t *testing.T) {
		n := newNativeViewFailureTest(t)
		var destroyed atomic.Int32
		v, err := mygo.NewNativeView(n.w, mygo.NativeViewOptions{Create: func(*mygo.NativeViewHost) (mygo.NativeViewAdapter, error) {
			return &nativeViewFailingAdapter{destroyed: &destroyed, handle: 1}, errors.New("boom")
		}})
		if err == nil {
			t.Fatal("NewNativeView took an adapter its factory failed with")
		}
		if v != nil {
			t.Error("NewNativeView returned a view its factory failed")
		}
		if got := destroyed.Load(); got != 1 {
			t.Errorf("the failed adapter was destroyed %d times, want 1", got)
		}
	})
	t.Run("no adapter", func(t *testing.T) {
		n := newNativeViewFailureTest(t)
		v, err := mygo.NewNativeView(n.w, mygo.NativeViewOptions{Create: func(*mygo.NativeViewHost) (mygo.NativeViewAdapter, error) {
			return nil, nil
		}})
		if err == nil || v != nil {
			t.Errorf("NewNativeView took a factory with no adapter: %v, %v", v, err)
		}
	})
	t.Run("no native handle", func(t *testing.T) {
		n := newNativeViewFailureTest(t)
		var destroyed atomic.Int32
		v, err := mygo.NewNativeView(n.w, mygo.NativeViewOptions{Create: func(*mygo.NativeViewHost) (mygo.NativeViewAdapter, error) {
			return &nativeViewFailingAdapter{destroyed: &destroyed}, nil
		}})
		if err == nil || v != nil {
			t.Errorf("NewNativeView took an adapter with no handle: %v, %v", v, err)
		}
		if got := destroyed.Load(); got != 1 {
			t.Errorf("the adapter with no handle was destroyed %d times, want 1", got)
		}
	})
}

// newNativeViewFailureTest makes a window of native UI to fail a factory
// against, skipping where the platform has no native views at all.
func newNativeViewFailureTest(t *testing.T) *nativeViewTest {
	t.Helper()
	n := &nativeViewTest{t: t}
	var frames atomic.Int32
	n.w = newWindow(t, mygo.WindowOptions{Title: "Native view failure", Width: 200, Height: 150, Content: ui.View(func(c *ui.Context) {
		frames.Add(1)
		ui.Column(c).Fill().AlignItems(ui.Stretch).Children(func() {
			ui.Box(c).Fill()
		})
	})})
	eventually(t, "a frame", func() bool { return frames.Load() > 0 })
	// Does the platform host native views at all? A surface without support
	// reports it before it calls the factory, which is what every case here
	// needs.
	if _, err := mygo.NewNativeView(n.w, mygo.NativeViewOptions{Create: func(*mygo.NativeViewHost) (mygo.NativeViewAdapter, error) {
		return nil, nil
	}}); errors.Is(err, errors.ErrUnsupported) {
		t.Skip("native views are unsupported on this platform")
	}
	return n
}
