//go:build darwin

package e2e

import (
	"errors"
	"fmt"
	"math"
	"runtime"
	"sync"
	"testing"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/darwin"
)

// The macOS fixture: a real NSTextView, in this process, in the clip view
// MyGo's darwin backend makes for a native view. AppKit routes the
// window's own mouse and key events to it, and the ui only learns that it
// holds the keyboard through mygo.NativeViewHost's callbacks, exactly as
// it does from any other control. The control is the app's: the fixture
// never sets its own frame (MyGo places it), hands its text over, and
// releases it once when MyGo destroys the view.
//
// The fixture drives the control the way its own API does, and reports
// what the control does to the host: it never calls a callback under test
// to stand in for an event AppKit did not send.

// macPoint, macSize and macRect mirror the CoreGraphics structs.
type macPoint struct{ X, Y float64 }
type macSize struct{ Width, Height float64 }
type macRect struct {
	Origin macPoint
	Size   macSize
}

// macRange mirrors NSRange: a location and a length.
type macRange struct{ Location, Length uintptr }

// macNotFound is NSNotFound, the location an empty marked range reports.
const macNotFound = ^uintptr(0)

var (
	macLoadOnce sync.Once
	macMsgSend  uintptr

	// Typed variants of objc_msgSend for the calls that take or return a
	// point or a rectangle, as the darwin backend has them.
	macRectOf     func(obj objc.ID, sel objc.SEL) macRect
	macPointOf    func(obj objc.ID, sel objc.SEL) macPoint
	macInitRect   func(obj objc.ID, sel objc.SEL, r macRect) objc.ID
	macRectToView func(obj objc.ID, sel objc.SEL, r macRect, view objc.ID) macRect
	macPointFrom  func(obj objc.ID, sel objc.SEL, p macPoint, view objc.ID) macPoint
	macMarked     func(obj objc.ID, sel objc.SEL) macRange
	macSetMarked  func(obj objc.ID, sel objc.SEL, text objc.ID, selected, replacement macRange)
)

// macLoad prepares objc_msgSend and the typed variants of it once.
func macLoad() {
	macLoadOnce.Do(func() {
		lib, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			panic("e2e: " + err.Error())
		}
		macMsgSend, err = purego.Dlsym(lib, "objc_msgSend")
		if err != nil {
			panic("e2e: " + err.Error())
		}
		// A struct larger than 16 bytes comes back through a different
		// entry point on amd64.
		stret := macMsgSend
		if runtime.GOARCH == "amd64" {
			stret, err = purego.Dlsym(lib, "objc_msgSend_stret")
			if err != nil {
				panic("e2e: " + err.Error())
			}
		}
		purego.RegisterFunc(&macRectOf, stret)
		purego.RegisterFunc(&macRectToView, stret)
		purego.RegisterFunc(&macPointOf, macMsgSend)
		purego.RegisterFunc(&macPointFrom, macMsgSend)
		purego.RegisterFunc(&macInitRect, macMsgSend)
		purego.RegisterFunc(&macMarked, macMsgSend)
		purego.RegisterFunc(&macSetMarked, macMsgSend)
	})
}

var (
	macNamesMu sync.Mutex
	macSels    = map[string]objc.SEL{}
	macClasses = map[string]objc.Class{}
)

// macSel is the selector of name, registered once: RegisterName takes the
// global Objective-C lock.
func macSel(name string) objc.SEL {
	macNamesMu.Lock()
	defer macNamesMu.Unlock()
	if s, ok := macSels[name]; ok {
		return s
	}
	s := objc.RegisterName(name)
	macSels[name] = s
	return s
}

// macClass is the class of name, looked up once.
func macClass(name string) objc.Class {
	macNamesMu.Lock()
	defer macNamesMu.Unlock()
	if c, ok := macClasses[name]; ok {
		return c
	}
	c := objc.GetClass(name)
	if c == 0 {
		panic("e2e: no Objective-C class " + name)
	}
	macClasses[name] = c
	return c
}

// macSend sends an Objective-C message whose arguments are all words: ids,
// integers and booleans, as the app's own adapters do.
//
//go:uintptrescapes
func macSend(obj objc.ID, selector string, args ...uintptr) objc.ID {
	macLoad()
	call := make([]uintptr, 0, len(args)+2)
	call = append(call, uintptr(obj), uintptr(macSel(selector)))
	call = append(call, args...)
	r1, _, _ := purego.SyscallN(macMsgSend, call...)
	return objc.ID(r1)
}

// macWithPool runs fn with an autorelease pool of its own: the calls that
// make a temporary NSString or return one, as the app's do, must not grow
// the process.
func macWithPool(fn func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := macSend(objc.ID(macClass("NSAutoreleasePool")), "alloc")
	pool = macSend(pool, "init")
	defer macSend(pool, "drain")
	fn()
}

// macString copies an NSString into a Go string.
func macString(str objc.ID) string {
	if str == 0 {
		return ""
	}
	p := macSend(str, "UTF8String")
	if p == 0 {
		return ""
	}
	n := int(int64(macSend(str, "lengthOfBytesUsingEncoding:", 4)))
	s := string(unsafe.Slice(*(**byte)(unsafe.Pointer(&p)), n))
	runtime.KeepAlive(str)
	return s
}

// macNSString makes an NSString of text, as the app's adapters do.
func macNSString(text string) objc.ID {
	b := append([]byte(text), 0)
	str := macSend(objc.ID(macClass("NSString")), "stringWithUTF8String:", uintptr(unsafe.Pointer(&b[0])))
	runtime.KeepAlive(b)
	return str
}

// macInt is an integer-returning message, whose value fits an int.
func macInt(obj objc.ID, selector string) int {
	return int(int64(macSend(obj, selector)))
}

// macBool is a BOOL-returning message.
func macBool(obj objc.ID, selector string) bool { return macSend(obj, selector) != 0 }

var (
	macTextViewOnce sync.Once
	macTextView     objc.Class
)

// macTextViewClass registers MyGoE2ETextView, an NSTextView that reports
// the AppKit events that give and pass on the keyboard. A class can only
// be registered once per process.
func macTextViewClass() objc.Class {
	macTextViewOnce.Do(func() {
		cls, err := objc.RegisterClass("MyGoE2ETextView", macClass("NSTextView"), nil, nil, []objc.MethodDef{
			{Cmd: macSel("becomeFirstResponder"), Fn: macBecomeFirstResponder},
			{Cmd: macSel("insertTab:"), Fn: macInsertTab},
			{Cmd: macSel("insertBacktab:"), Fn: macInsertBacktab},
			{Cmd: macSel("mouseDown:"), Fn: macMouseDown},
		})
		if err != nil {
			panic("e2e: " + err.Error())
		}
		macTextView = cls
	})
	return macTextView
}

// macBecomeFirstResponder is where AppKit gives the control the keyboard:
// a click on it, or the ui's own NativeView.Focus, comes through here.
func macBecomeFirstResponder(self objc.ID, cmd objc.SEL) bool {
	ok := objc.SendSuper[bool](self, cmd)
	if ok {
		if f := macFixtureOf(self); f != nil {
			f.tookFocus()
		}
	}
	return ok
}

// macInsertTab is Tab at the control's last control: the platform's own
// Tab-out, which the ui turns into the focus moving on. AppKit picks it
// from the key event's characters through its own key binding dict, so a
// Shift+Tab whose characters are still U+0009 arrives here, forward:
// only insertBacktab: is the backward move.
func macInsertTab(self objc.ID, _ objc.SEL, sender objc.ID) {
	if f := macFixtureOf(self); f != nil {
		f.tabbedOut(false)
	}
}

// macInsertBacktab is Shift+Tab at the control's first control: AppKit
// sends it for NSBackTabCharacter (U+0019), and the ui moves the focus
// back with it.
func macInsertBacktab(self objc.ID, _ objc.SEL, sender objc.ID) {
	if f := macFixtureOf(self); f != nil {
		f.tabbedOut(true)
	}
}

// macMouseDown is a press on the control: the app learns where, in the
// control's own point, and then AppKit's own handling runs.
func macMouseDown(self objc.ID, cmd objc.SEL, event objc.ID) {
	if f := macFixtureOf(self); f != nil {
		p := macPointFrom(self, macSel("convertPoint:fromView:"), macPointOf(event, macSel("locationInWindow")), 0)
		f.pointerDown(p.X, p.Y)
	}
	objc.SendSuper[objc.ID](self, cmd, event)
}

// macFixtures is the control of each native view, so an AppKit callback,
// which gets only the view, finds the fixture reporting it.
var macFixtures sync.Map // objc.ID -> *macFixture

func macFixtureOf(view objc.ID) *macFixture {
	if f, ok := macFixtures.Load(view); ok {
		return f.(*macFixture)
	}
	return nil
}

// macFixture is the app's control: a real NSTextView.
type macFixture struct {
	nativeViewEvents
	view objc.ID
}

// newNativeViewFixture makes an NSTextView in the parent MyGo reports. It
// leaves the view without a frame: the backend places it. The view is the
// root MyGo hands that frame to, so it does not size itself to its own
// text (below): it keeps the frame the host gives it.
func newNativeViewFixture(host *mygo.NativeViewHost) (nativeViewFixture, error) {
	if host.Platform != "darwin" {
		return nil, fmt.Errorf("e2e: the darwin fixture on %q", host.Platform)
	}
	if host.Parent == 0 {
		return nil, errors.New("e2e: the host has no parent view")
	}
	macLoad()
	view := macInitRect(macSend(objc.ID(macTextViewClass()), "alloc"), macSel("initWithFrame:"), macRect{})
	if view == 0 {
		return nil, errors.New("e2e: cannot make an NSTextView")
	}
	// A bare NSTextView sizes itself to its own text: initWithFrame:
	// leaves isVerticallyResizable on, and this view is the root the host
	// hands the element's frame to, so a resizable one answers that frame
	// with the height of its text — an empty NSTextView is about 14 DIPs
	// tall, and the 200x180 the content placed it at comes back 14. Turn
	// the text view's own resizing off before its first string and layout,
	// so the frame the host sets is the one that stays. A view that must
	// size itself to its document belongs in a container that honors the
	// frame instead — an NSScrollView around its NSTextView, as the
	// example's text control does.
	macSend(view, "setVerticallyResizable:", 0)
	macSend(view, "setHorizontallyResizable:", 0)
	macSend(view, "setEditable:", 1)
	macSend(view, "setSelectable:", 1)
	macSend(view, "setRichText:", 0)
	macWithPool(func() {
		macSend(view, "setString:", uintptr(macNSString("")))
	})
	f := &macFixture{view: view}
	f.bind(host)
	macFixtures.Store(view, f)
	return f, nil
}

// NativeHandle is the control's NSView*.
func (f *macFixture) NativeHandle() uintptr { return uintptr(f.view) }

// Focus makes the control the window's first responder, as Tab into the
// native view does. Main thread, as the adapter's Focus is.
func (f *macFixture) Focus(back bool) {
	if f.view == 0 {
		return
	}
	win := macSend(f.view, "window")
	if win == 0 {
		return
	}
	macSend(win, "makeFirstResponder:", uintptr(f.view))
}

// Destroy is called once, after the backend detached the control: the
// fixture owns the view it allocated and releases it here. It forgets the
// view, so a later query from a test reports nothing instead of a released
// object. Main thread, as the adapter's Destroy is.
func (f *macFixture) Destroy() {
	f.destroyedControl()
	view := f.view
	if view == 0 {
		return
	}
	f.view = 0
	macSend(view, "removeFromSuperview")
	macFixtures.Delete(view)
	macSend(view, "release")
}

// Text is the control's own text. It asks AppKit on the main thread: the
// toolkit is not safe to call from a test goroutine.
func (f *macFixture) Text() (text string) {
	mygo.RunOnMain(func() {
		if f.view == 0 {
			return
		}
		macWithPool(func() { text = macString(macSend(f.view, "string")) })
	})
	return text
}

// SetText sets the control's own text, as the app does, on the main thread.
func (f *macFixture) SetText(text string) {
	mygo.RunOnMain(func() {
		if f.view == 0 {
			return
		}
		macWithPool(func() {
			macSend(f.view, "setString:", uintptr(macNSString(text)))
		})
	})
}

// HoldsKeyboard reports whether the control is the window's first
// responder, asked of AppKit on the main thread.
func (f *macFixture) HoldsKeyboard() (holds bool) {
	mygo.RunOnMain(func() {
		if f.view == 0 {
			return
		}
		win := macSend(f.view, "window")
		if win == 0 {
			return
		}
		holds = macSend(win, "firstResponder") == f.view
	})
	return holds
}

// Box is the control's box and the part of it the window shows, in the
// window surface's DIPs, which are the ui's: AppKit's own origin is the
// bottom left of the content view, so the box is converted to the content
// view and then flipped. It runs on the main thread, where AppKit is.
//
// The rect to convert is the control's bounds, which are in its own
// coordinates: a frame is in the parent's, and converting it from the
// control would add the control's own origin a second time, which shows up
// as a box shifted whenever MyGo placed the control away from the origin.
func (f *macFixture) Box() (box, visible mygo.Rectangle) {
	mygo.RunOnMain(func() {
		if f.view == 0 {
			return
		}
		macWithPool(func() {
			if macSend(f.view, "window") == 0 {
				return
			}
			content := macSend(macSend(f.view, "window"), "contentView")
			if content == 0 {
				return
			}
			height := macRectOf(content, macSel("bounds")).Size.Height
			box = macFlip(macRectToView(f.view, macSel("convertRect:toView:"), macRectOf(f.view, macSel("bounds")), content), height)
			if macBool(f.view, "isHiddenOrHasHiddenAncestor") {
				return
			}
			// visibleRect is in the control's own coordinates, so it
			// converts on its own: a clipped or hidden control reports
			// less of itself.
			visible = macFlip(macRectToView(f.view, macSel("convertRect:toView:"), macRectOf(f.view, macSel("visibleRect")), content), height)
			if visible.Width < 0 || visible.Height < 0 {
				visible = mygo.Rectangle{}
			}
		})
	})
	return box, visible
}

// macFlip converts a rectangle of the content view's coordinates, origin
// bottom left, to the surface's DIPs, origin top left.
func macFlip(r macRect, height float64) mygo.Rectangle {
	return mygo.Rectangle{
		X:      int(math.Round(r.Origin.X)),
		Y:      int(math.Round(height - r.Origin.Y - r.Size.Height)),
		Width:  int(math.Round(r.Size.Width)),
		Height: int(math.Round(r.Size.Height)),
	}
}

// mark composes text into the control through its own input client, which
// is what an input method does: the app is the one calling it, and MyGo
// must leave the composition alone. It runs on the main thread, as the
// input method does.
func (f *macFixture) mark(text string) {
	mygo.RunOnMain(func() {
		if f.view == 0 {
			return
		}
		macWithPool(func() {
			n := uintptr(0)
			for range text {
				// The tests compose BMP text, whose rune count is its
				// UTF-16 length.
				n++
			}
			macSetMarked(f.view, macSel("setMarkedText:selectedRange:replacementRange:"), macNSString(text),
				macRange{Location: n, Length: 0},
				macRange{Location: macNotFound, Length: 0})
		})
	})
}

// marked is the length of the control's marked text, 0 when nothing is
// being composed into it, asked of the control on the main thread.
func (f *macFixture) marked() (n int) {
	mygo.RunOnMain(func() {
		if f.view == 0 {
			return
		}
		r := macMarked(f.view, macSel("markedRange"))
		if r.Location == macNotFound {
			return
		}
		n = int(r.Length)
	})
	return n
}

// nvClick clicks a point of the window's content as a user does, for the
// native-view tests. It sends both halves of the click through the
// application's event queue before the application handles either, so
// AppKit itself delivers the release: a control that tracks the mouse until
// the release arrives (NSTextView does) would otherwise sit in its tracking
// loop on the main thread, the click hook having waited for the press to be
// handled before it even sent the release.
func nvClick(w *mygo.Window, x, y float64) bool {
	waitOnScreen(w)
	mygo.RunOnMain(func() { darwin.TestAppClick(w.NativeHandle(), x, y) })
	return true
}

// nvTypeInto types text into whatever holds the keyboard as the keyboard
// does, for the native-view tests. The event carries the characters, as a
// keyboard layout's do.
func nvTypeInto(w *mygo.Window, text string) (ok bool) {
	ok = true
	mygo.RunOnMain(func() {
		for _, r := range text {
			if !darwin.TestKey(w.NativeHandle(), 0, string(r)) {
				ok = false
				return
			}
		}
	})
	return ok
}

// TestNativeViewDarwinKeepsMarkedText drives the control's own input
// client as an input method does and checks MyGo leaves the composition
// alone: the marked text survives the frames MyGo builds, and the ui's own
// inputs never take it.
func TestNativeViewDarwinKeepsMarkedText(t *testing.T) {
	n := newNativeViewTest(t)
	f, ok := n.fixture.(*macFixture)
	if !ok {
		t.Fatalf("the fixture is %T, not the darwin one", n.fixture)
	}
	n.w.Focus()
	n.clickControl()
	eventually(t, "the control taking the keyboard", func() bool { return f.HoldsKeyboard() })

	f.mark("にほん")
	if got := f.marked(); got == 0 {
		t.Fatalf("the control did not take marked text: its text is %q", f.Text())
	} else if got != 3 {
		t.Errorf("the control marked %d characters, want 3", got)
	}
	// Frames go by: MyGo must not commit, clear or overwrite it.
	for i := 0; i < 3; i++ {
		at := n.frames.Load()
		n.w.Invalidate()
		eventually(t, "a frame after the marked text", func() bool { return n.frames.Load() > at })
		if got := f.marked(); got != 3 {
			t.Fatalf("MyGo disturbed the marked text: %d characters are marked, want 3", got)
		}
	}
	// The ui writes these while it builds frames, on the main thread.
	var before, after string
	mygo.RunOnMain(func() { before, after = n.before, n.after })
	if before != "" || after != "" {
		t.Errorf("the ui's own inputs took the marked text: %q, %q", before, after)
	}
	var editable int
	mygo.RunOnMain(func() {
		if f.view != 0 {
			editable = macInt(f.view, "isEditable")
		}
	})
	if editable == 0 {
		t.Error("MyGo made the control read only")
	}
}

// frameRect is the control's frame in the clip view MyGo hosts it in, asked
// of AppKit on the main thread: the frame the host set, which the control
// must keep. Box's bounds would report the same size only while nothing
// resized the control, which is exactly what this is here to tell apart.
func (f *macFixture) frameRect() (r macRect) {
	mygo.RunOnMain(func() {
		if f.view == 0 {
			return
		}
		macWithPool(func() { r = macRectOf(f.view, macSel("frame")) })
	})
	return r
}

// layoutText lays the control's own text out through its layout manager,
// which is what AppKit does after the host placed the control, and what a
// text view that sizes itself answers by resizing itself to its text. It
// reports whether the control had text to lay out at all: a call that
// found no layout manager would not reproduce anything. It runs on the
// main thread, where AppKit is.
func (f *macFixture) layoutText() (ok bool) {
	mygo.RunOnMain(func() {
		if f.view == 0 {
			return
		}
		macWithPool(func() {
			lm := macSend(f.view, "layoutManager")
			tc := macSend(f.view, "textContainer")
			if lm == 0 || tc == 0 {
				return
			}
			macSend(lm, "ensureLayoutForTextContainer:", uintptr(tc))
			ok = true
		})
	})
	return ok
}

// TestNativeViewDarwinKeepsTheFrameItWasPlacedAt places a real NSTextView
// where the content places it, at (30, 30), 200x180, and then makes AppKit
// lay its text out: a text view that sizes itself to its text answers the
// host's frame with the height of its own text there (about 14 DIPs for an
// empty one), which is not the element's box. The fixture turns the text
// view's own resizing off before its first string and layout
// (newNativeViewFixture), and this is the e2e that holds it to that: the
// control is the element's box after AppKit's own layout, with text in it
// and with the text taken back out, and its frame is the one the host set,
// exactly.
func TestNativeViewDarwinKeepsTheFrameItWasPlacedAt(t *testing.T) {
	o := newNativeViewOverlap(t, false)
	f, ok := o.fxA.(*macFixture)
	if !ok {
		t.Fatalf("the fixture is %T, not the darwin one", o.fxA)
	}
	want := mygo.Rectangle{X: nvOverlapAX, Y: nvOverlapAY, Width: nvOverlapW, Height: nvOverlapH}
	if box, visible := f.Box(); !nvSame(box, want) || !nvSame(visible, want) {
		t.Fatalf("the control is at %+v showing %+v, want the element's own %+v", box, visible, want)
	}
	placed := f.frameRect()

	// AppKit's own text layout, as it runs after a frame is placed.
	if !f.layoutText() {
		t.Fatal("the control has no layout manager to lay its text out with")
	}
	if after := f.frameRect(); after != placed {
		t.Errorf("AppKit's text layout moved the control from %+v to %+v", placed, after)
	}
	if box, visible := f.Box(); !nvSame(box, want) || box.Height != want.Height || !nvSame(visible, want) {
		t.Errorf("after AppKit laid the text out the control is at %+v showing %+v, want %+v", box, visible, want)
	}

	// The same with text in it, and again once the text is back out: the
	// control keeps the frame the host set, whatever its own text is.
	f.SetText("hello")
	if !f.layoutText() {
		t.Fatal("the control has no layout manager to lay its text out with")
	}
	if box, visible := f.Box(); !nvSame(box, want) || !nvSame(visible, want) {
		t.Errorf("with text in it the control is at %+v showing %+v, want %+v", box, visible, want)
	}
	f.SetText("")
	if !f.layoutText() {
		t.Fatal("the control has no layout manager to lay its text out with")
	}
	if after := f.frameRect(); after != placed {
		t.Errorf("taking the text out moved the control from %+v to %+v", placed, after)
	}
	if box, visible := f.Box(); !nvSame(box, want) || !nvSame(visible, want) {
		t.Errorf("with the text back out the control is at %+v showing %+v, want %+v", box, visible, want)
	}
}
