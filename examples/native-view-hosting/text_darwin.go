//go:build darwin

package main

import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// This file drives AppKit through purego, the way internal/darwin does: no
// cgo, and no generated headers to keep in step (AGENTS.md). The control is
// a real editable NSTextView in an NSScrollView: AppKit owns its text, its
// caret, its selection and its input method; MyGo hosts the scroll view.

// NSPoint, NSSize and NSRect are C typedefs of these layouts, and CGFloat
// is a double on every macOS MyGo builds for.
type nsPoint struct{ X, Y float64 }
type nsSize struct{ Width, Height float64 }
type nsRect struct {
	Origin nsPoint
	Size   nsSize
}

var (
	msgSendAddr      uintptr
	msgSendSuperAddr uintptr

	// objc_msgSend has no fixed signature, so one typed function is
	// registered per argument shape this file sends (AGENTS.md: callbacks
	// are scarce, these are not, but they are still made once).
	msgRectArg  func(obj uintptr, cmd objc.SEL, r nsRect) uintptr
	msgRect0    func(obj uintptr, cmd objc.SEL) nsRect
	msgPoint0   func(obj uintptr, cmd objc.SEL) nsPoint
	msgPointArg func(obj uintptr, cmd objc.SEL, p nsPoint, view uintptr) nsPoint
	msgSizeArg  func(obj uintptr, cmd objc.SEL, s nsSize) uintptr
	msgFloatArg func(obj uintptr, cmd objc.SEL, v float64) uintptr

	poolPush func() uintptr
	poolPop  func(uintptr)

	appKitOnce sync.Once
)

func loadAppKit() {
	appKitOnce.Do(func() {
		libObjC := mustDlopen("/usr/lib/libobjc.A.dylib")
		mustDlopen("/System/Library/Frameworks/Foundation.framework/Foundation")
		mustDlopen("/System/Library/Frameworks/AppKit.framework/AppKit")
		msgSendAddr = mustDlsym(libObjC, "objc_msgSend")
		msgSendSuperAddr = mustDlsym(libObjC, "objc_msgSendSuper")
		// Methods returning structs larger than 16 bytes use a different
		// entry point on amd64 (internal/darwin does the same for NSRect).
		stret := msgSendAddr
		if runtime.GOARCH == "amd64" {
			stret = mustDlsym(libObjC, "objc_msgSend_stret")
		}
		purego.RegisterFunc(&msgRectArg, msgSendAddr)
		purego.RegisterFunc(&msgRect0, stret)
		purego.RegisterFunc(&msgPoint0, msgSendAddr)
		purego.RegisterFunc(&msgPointArg, msgSendAddr)
		purego.RegisterFunc(&msgSizeArg, msgSendAddr)
		purego.RegisterFunc(&msgFloatArg, msgSendAddr)
		purego.RegisterLibFunc(&poolPush, libObjC, "objc_autoreleasePoolPush")
		purego.RegisterLibFunc(&poolPop, libObjC, "objc_autoreleasePoolPop")
	})
}

func mustDlopen(path string) uintptr {
	h, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		panic("native-view-hosting: dlopen " + path + ": " + err.Error())
	}
	return h
}

func mustDlsym(h uintptr, name string) uintptr {
	p, err := purego.Dlsym(h, name)
	if err != nil {
		panic("native-view-hosting: dlsym " + name + ": " + err.Error())
	}
	return p
}

var (
	namesMu   sync.Mutex
	classes   = map[string]uintptr{}
	selectors = map[string]objc.SEL{}
)

func class(name string) uintptr {
	namesMu.Lock()
	defer namesMu.Unlock()
	if c, ok := classes[name]; ok {
		return c
	}
	c := uintptr(objc.GetClass(name))
	if c == 0 {
		panic("native-view-hosting: no Objective-C class " + name)
	}
	classes[name] = c
	return c
}

func sel(name string) objc.SEL {
	namesMu.Lock()
	defer namesMu.Unlock()
	if s, ok := selectors[name]; ok {
		return s
	}
	s := objc.RegisterName(name)
	selectors[name] = s
	return s
}

// send is objc_msgSend. //go:uintptrescapes keeps a Go pointer passed as an
// argument alive and unmoved across the call.
//
//go:uintptrescapes
func send(obj uintptr, selector string, args ...uintptr) uintptr {
	a := make([]uintptr, 0, len(args)+2)
	a = append(a, obj, uintptr(sel(selector)))
	a = append(a, args...)
	r, _, _ := purego.SyscallN(msgSendAddr, a...)
	return r
}

// sendBool is objc_msgSend for a method that answers BOOL, which is a char:
// only the low byte of the return value means anything (internal/darwin reads
// it the same way).
func sendBool(obj uintptr, selector string) bool {
	return send(obj, selector)&0xff != 0
}

// objcSuper is struct objc_super, which objc_msgSendSuper takes first.
type objcSuper struct {
	receiver   uintptr
	superClass uintptr
}

// sendSuper runs the superclass implementation of a method this file
// overrides. The struct is pinned: Go's collector never moves heap objects,
// but it must not collect the struct while objc_msgSendSuper reads it
// (AGENTS.md: pin what a native call reads).
func sendSuper(self uintptr, className, selector string, args ...uintptr) uintptr {
	var pin runtime.Pinner
	defer pin.Unpin()
	sup := &objcSuper{
		receiver:   self,
		superClass: uintptr(objc.Class(class(className)).SuperClass()),
	}
	pin.Pin(sup)
	a := make([]uintptr, 0, len(args)+2)
	a = append(a, uintptr(unsafe.Pointer(sup)), uintptr(sel(selector)))
	a = append(a, args...)
	r, _, _ := purego.SyscallN(msgSendSuperAddr, a...)
	return r
}

// nsString makes an NSString from a Go string. stringWithUTF8String: copies
// the bytes, so they only have to live through the call: NUL-terminate them
// and let //go:uintptrescapes on send keep them pinned.
func nsString(s string) uintptr {
	b := make([]byte, len(s)+1)
	copy(b, s)
	return send(class("NSString"), "stringWithUTF8String:", uintptr(unsafe.Pointer(&b[0])))
}

const textViewClassName = "MyGoExampleTextView"

var (
	textViewOnce sync.Once
	viewsMu      sync.Mutex
	views        = map[uintptr]*darwinText{}
)

// darwinText is one control: the NSTextView AppKit edits and the
// NSScrollView that shows it, which is the handle MyGo positions.
type darwinText struct {
	text   uintptr
	scroll uintptr
	host   host

	mu        sync.Mutex
	destroyed bool
}

// registerTextViewClass makes the class of every text control this example
// creates, once. Its three methods are registered once too, never per view:
// purego callbacks are scarce (AGENTS.md), and self routes each call back to
// the view it belongs to.
func registerTextViewClass() {
	textViewOnce.Do(func() {
		_, err := objc.RegisterClass(textViewClassName, objc.GetClass("NSTextView"), nil, nil, []objc.MethodDef{
			// AppKit calls this when focus enters the view: report it, and
			// keep AppKit's own answer.
			{Cmd: sel("becomeFirstResponder"), Fn: func(self objc.ID, _ objc.SEL) bool {
				if v := viewOf(uintptr(self)); v != nil {
					v.host.focus()
				}
				return sendSuper(uintptr(self), textViewClassName, "becomeFirstResponder") != 0
			}},
			// The press reaches the view before it starts editing: report
			// where it landed, in view-local DIPs, with (0, 0) at the top
			// left of the view MyGo hosts, and let AppKit place the caret as
			// usual.
			{Cmd: sel("mouseDown:"), Fn: func(self objc.ID, _ objc.SEL, ev objc.ID) {
				v := viewOf(uintptr(self))
				if v != nil {
					// locationInWindow is in the window's coordinates.
					// Converting it into the text view would be a document
					// point: once the document scrolls, its frame moves under
					// the scroll view, so the same spot on screen reports a
					// different Y. Convert into the scroll view MyGo hosts
					// instead, which does not move, and let localDIPs take
					// its bounds origin (its scroll offset) off and honour
					// isFlipped: AppKit counts Y upward unless a view is
					// flipped, and an NSScrollView is not.
					p := msgPointArg(v.scroll, sel("convertPoint:fromView:"),
						msgPoint0(uintptr(ev), sel("locationInWindow")), 0)
					b := msgRect0(v.scroll, sel("bounds"))
					local := localDIPs(
						point{X: p.X, Y: p.Y},
						bounds{X: b.Origin.X, Y: b.Origin.Y, Width: b.Size.Width, Height: b.Size.Height},
						sendBool(v.scroll, "isFlipped"))
					v.host.press(local.X, local.Y)
				}
				sendSuper(uintptr(self), textViewClassName, "mouseDown:", uintptr(ev))
			}},
			// Tab is a command, not a character. Reporting it here is what
			// makes TabOut the view's edge: AppKit's key view loop would
			// otherwise walk the window's views behind MyGo's back.
			{Cmd: sel("doCommandBySelector:"), Fn: func(self objc.ID, _ objc.SEL, cmd objc.SEL) {
				switch cmd {
				case sel("insertTab:"):
					if v := viewOf(uintptr(self)); v != nil {
						v.host.tab(false)
					}
					return
				case sel("insertBacktab:"):
					if v := viewOf(uintptr(self)); v != nil {
						v.host.tab(true)
					}
					return
				}
				sendSuper(uintptr(self), textViewClassName, "doCommandBySelector:", uintptr(cmd))
			}},
		})
		if err != nil {
			panic("native-view-hosting: registering " + textViewClassName + ": " + err.Error())
		}
	})
}

func viewOf(self uintptr) *darwinText {
	viewsMu.Lock()
	defer viewsMu.Unlock()
	return views[self]
}

// newTextView makes the platform's text control in the host's window. The
// factory runs on the main thread, and so do handle, focus and destroy.
func newTextView(h host) (textView, error) {
	if h.parent == 0 {
		return nil, fmt.Errorf("the window has no content view to host it (parent is nil)")
	}
	loadAppKit()
	registerTextViewClass()

	// alloc/init objects are owned (AGENTS.md): the pool drains the
	// temporary strings and fonts AppKit hands back, and destroy releases
	// the two views this function owns.
	pool := poolPush()
	defer poolPop(pool)

	frame := nsRect{Size: nsSize{Width: 320, Height: 160}}
	scroll := msgRectArg(send(class("NSScrollView"), "alloc"), sel("initWithFrame:"), frame)
	text := msgRectArg(send(class(textViewClassName), "alloc"), sel("initWithFrame:"), frame)

	// The canonical scrollable text view: it grows downwards, its container
	// does not wrap, and its width follows the scroll view.
	msgSizeArg(text, sel("setMinSize:"), nsSize{})
	msgSizeArg(text, sel("setMaxSize:"), nsSize{Width: 1e9, Height: 1e9})
	send(text, "setVerticallyResizable:", 1)
	send(text, "setHorizontallyResizable:", 0)
	send(text, "setAutoresizingMask:", 2) // NSViewWidthSizable
	container := send(text, "textContainer")
	msgSizeArg(container, sel("setContainerSize:"), nsSize{Width: 1e9, Height: 1e9})
	send(container, "setWidthTracksTextView:", 1)

	// Editable text drawn and edited by AppKit: the caret, the selection,
	// the key handling and the input method are all the system's.
	send(text, "setEditable:", 1)
	send(text, "setRichText:", 0)
	send(text, "setString:", nsString("Type here: this text is edited by the system."))
	send(text, "setFont:", msgFloatArg(class("NSFont"), sel("systemFontOfSize:"), 13))

	send(scroll, "setHasVerticalScroller:", 1)
	send(scroll, "setBorderType:", 2) // NSBezelBorder
	send(scroll, "setDrawsBackground:", 1)
	send(scroll, "setDocumentView:", text)

	v := &darwinText{text: text, scroll: scroll, host: h}
	viewsMu.Lock()
	views[text] = v
	viewsMu.Unlock()
	return v, nil
}

// handle is the scroll view MyGo positions: the text view is its document
// view, and MyGo only ever knows about the view the factory made.
func (v *darwinText) handle() uintptr { return v.scroll }

// focus gives the view first responder status. The text and the selection
// the user left stay; one control is both the first and the last of the
// view, so back changes nothing here.
func (v *darwinText) focus(back bool) {
	if win := send(v.text, "window"); win != 0 {
		send(win, "makeFirstResponder:", v.text)
	}
}

// destroy releases what newTextView made, exactly once. MyGo detaches the
// handle from the parent it put it in and calls this on the main thread.
func (v *darwinText) destroy() {
	v.mu.Lock()
	if v.destroyed {
		v.mu.Unlock()
		return
	}
	v.destroyed = true
	v.mu.Unlock()

	viewsMu.Lock()
	delete(views, v.text)
	viewsMu.Unlock()

	pool := poolPush()
	defer poolPop(pool)
	send(v.scroll, "removeFromSuperview")
	// documentView is a strong property (AppKit): setDocumentView: took a
	// reference of its own, and setting it to nil gives that one back. The
	// two releases below balance the scroll view's alloc and the text
	// view's, one each.
	send(v.scroll, "setDocumentView:", 0)
	send(v.scroll, "release")
	send(v.text, "release")
	v.text, v.scroll = 0, 0
}
