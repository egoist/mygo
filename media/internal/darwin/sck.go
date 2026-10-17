//go:build darwin

package darwin

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// This file drives ScreenCaptureKit: what displays macOS offers to capture,
// whether this app may capture one, and the stream that hands over its
// pictures as CoreMedia samples, the same shape a camera's arrive in.
//
// ScreenCaptureKit needs macOS 12.3, and the screen-recording question the
// system asks before an app may see other apps' windows.

// SCStreamOutputTypeScreen is the stream output kind a screen is.
const SCStreamOutputTypeScreen = 0

var (
	screenOnce sync.Once
	screenErr  error

	preflightScreen func() bool
	requestScreen   func() bool

	// ScreenCaptureKit counts a display in points, and a Retina display has
	// twice as many pixels: CGDisplayCopyDisplayMode and its accessors say
	// how many.
	displayCopyMode      func(display uint32) uintptr
	displayModePixelWide func(mode uintptr) uint
	displayModePixelHigh func(mode uintptr) uint

	screenDelegateOnce  sync.Once
	screenDelegateClass objc.Class
	screenDelegateErr   error

	// screenRoutes finds the capture a stream delegate belongs to, the way
	// cameraRoutes finds the camera an output's samples belong to.
	screenRoutes sync.Map // uintptr -> *ScreenCapture
)

// LoadScreenCapture loads ScreenCaptureKit and the part of CoreGraphics that
// asks whether an app may record the screen.
func LoadScreenCapture() error {
	screenOnce.Do(func() {
		if screenErr = Load(); screenErr != nil {
			return
		}
		if _, err := purego.Dlopen("/System/Library/Frameworks/ScreenCaptureKit.framework/ScreenCaptureKit", purego.RTLD_LAZY|purego.RTLD_GLOBAL); err != nil {
			screenErr = err
			return
		}
		cg, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			screenErr = err
			return
		}
		addr, err := purego.Dlsym(cg, "CGPreflightScreenCaptureAccess")
		if err != nil {
			screenErr = err
			return
		}
		purego.RegisterFunc(&preflightScreen, addr)
		if addr, err = purego.Dlsym(cg, "CGRequestScreenCaptureAccess"); err != nil {
			screenErr = err
			return
		}
		purego.RegisterFunc(&requestScreen, addr)
		if addr, err = purego.Dlsym(cg, "CGDisplayCopyDisplayMode"); err != nil {
			screenErr = err
			return
		}
		purego.RegisterFunc(&displayCopyMode, addr)
		if addr, err = purego.Dlsym(cg, "CGDisplayModeGetPixelWidth"); err != nil {
			screenErr = err
			return
		}
		purego.RegisterFunc(&displayModePixelWide, addr)
		if addr, err = purego.Dlsym(cg, "CGDisplayModeGetPixelHeight"); err != nil {
			screenErr = err
			return
		}
		purego.RegisterFunc(&displayModePixelHigh, addr)
	})
	return screenErr
}

// displaySize is a display's size in pixels, rather than the points
// ScreenCaptureKit reports.
func displaySize(display uint32) (w, h int) {
	mode := displayCopyMode(display)
	if mode == 0 {
		return 0, 0
	}
	defer CFReleaseItem(mode)
	w, h = int(displayModePixelWide(mode)), int(displayModePixelHigh(mode))
	if w <= 0 || h <= 0 {
		return 0, 0
	}
	return w, h
}

// ScreenAccessAllowed is whether macOS lets this app record the screen,
// without asking.
func ScreenAccessAllowed() bool {
	if err := LoadScreenCapture(); err != nil {
		return false
	}
	return preflightScreen()
}

// RequestScreenAccess asks the person to let the app record the screen where
// macOS has not asked yet, and reports the answer. macOS asks this once per
// app: changing the answer later needs the app quit, so this hands back what
// the system says now rather than waiting for a new answer.
func RequestScreenAccess() bool {
	if err := LoadScreenCapture(); err != nil {
		return false
	}
	return requestScreen()
}

// OpenScreenSettings shows the part of System Settings where the person can
// change the app's answer about recording the screen.
func OpenScreenSettings() error {
	if err := Load(); err != nil {
		return err
	}
	workspace := SendID(Class("NSWorkspace"), Sel("sharedWorkspace"))
	if workspace == 0 {
		return fmt.Errorf("media: no system settings to show")
	}
	SendBool1(workspace, Sel("openURL:"), URL("x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture"))
	return nil
}

// A ScreenDisplay is a display ScreenCaptureKit offers.
type ScreenDisplay struct {
	// ID is the display's CGDirectDisplayID.
	ID uint32
	// Width and Height are its size in pixels.
	Width, Height int
}

// ScreenDisplays lists the displays the system offers for capture, without
// asking for permission to capture them.
func ScreenDisplays() ([]ScreenDisplay, error) {
	content, err := shareableContent()
	if err != nil {
		return nil, err
	}
	defer Release(content)
	var out []ScreenDisplay
	WithPool(func() {
		displays := SendID(content, Sel("displays"))
		n := ArrayCount(displays)
		out = make([]ScreenDisplay, 0, n)
		for i := 0; i < n; i++ {
			d := ArrayAt(displays, i)
			id := uint32(SendUint(d, Sel("displayID")))
			w, h := displaySize(id)
			if w == 0 || h == 0 {
				w, h = SendInt(d, Sel("width")), SendInt(d, Sel("height"))
			}
			out = append(out, ScreenDisplay{ID: id, Width: w, Height: h})
		}
	})
	if len(out) == 0 {
		return nil, fmt.Errorf("media: macOS lists no displays")
	}
	return out, nil
}

// shareableContent asks ScreenCaptureKit what it can share. macOS answers on a
// queue of its own, so this waits for the answer.
func shareableContent() (uintptr, error) {
	if err := LoadScreenCapture(); err != nil {
		return 0, err
	}
	type answer struct {
		content uintptr
		problem error // the system's error, said while it was valid
	}
	done := make(chan answer, 1)
	block := objc.NewBlock(func(b objc.Block, content, errRef uintptr) {
		if content != 0 {
			Retain(content)
		}
		// The error belongs to the system for the length of this callback:
		// ask what it means here, not after the callback returned.
		done <- answer{content, errorOf(errRef)}
	})
	handler := block.Copy()
	SendVoid1(Class("SCShareableContent"), Sel("getShareableContentWithCompletionHandler:"), uintptr(handler))
	handler.Release()
	block.Release()
	a := <-done
	if a.problem != nil {
		if a.content != 0 {
			// The system gave both: the content is ours to release,
			// since the caller gets the error instead.
			Release(a.content)
		}
		return 0, fmt.Errorf("media: macOS will not share the screen: %w", a.problem)
	}
	if a.content == 0 {
		return 0, fmt.Errorf("media: macOS shared no screens")
	}
	return a.content, nil
}

// errorOf says what a completion callback's NSError means, as a Go error, or
// nil when there was none. It must be called inside the callback: the error
// belongs to the system and may not outlive it. A Go error is returned even
// when the system's description is empty, so that an error object is never
// mistaken for success by its text.
func errorOf(errRef uintptr) error {
	if errRef == 0 {
		return nil
	}
	return fmt.Errorf("%s", ErrorString(errRef))
}

// A ScreenCapture is a running ScreenCaptureKit stream. fn is called with each
// sample buffer on the capture queue, and stop when the stream stops by
// itself, with the system's reason.
type ScreenCapture struct {
	stream   uintptr
	filter   uintptr
	config   uintptr
	delegate uintptr
	fn       func(sample uintptr)
	stop     func(err uintptr)

	mu     sync.Mutex
	closed bool
}

// OpenScreen starts capturing the display with the system's own display id,
// and calls fn with every sample buffer it makes.
//
// fn runs on the capture queue, never on the main thread, and must return
// quickly: the sample buffer is good only until it returns.
func OpenScreen(displayID uint32, fn func(sample uintptr), onStop func(err uintptr)) (*ScreenCapture, error) {
	if err := LoadScreenCapture(); err != nil {
		return nil, err
	}
	content, err := shareableContent()
	if err != nil {
		return nil, err
	}
	class, err := screenDelegate()
	if err != nil {
		Release(content)
		return nil, err
	}
	queue, err := CaptureQueue()
	if err != nil {
		Release(content)
		return nil, err
	}

	c := &ScreenCapture{fn: fn, stop: onStop}
	var openErr error
	WithPool(func() {
		defer Release(content)
		displays := SendID(content, Sel("displays"))
		var display uintptr
		for i := 0; i < ArrayCount(displays); i++ {
			d := ArrayAt(displays, i)
			if uint32(SendUint(d, Sel("displayID"))) == displayID {
				display = d
				break
			}
		}
		if display == 0 {
			openErr = fmt.Errorf("media: no screen with display id %d", displayID)
			return
		}

		w, h := displaySize(displayID)
		if w == 0 || h == 0 {
			w, h = SendInt(display, Sel("width")), SendInt(display, Sel("height"))
		}
		excluded := SendID(Class("NSArray"), Sel("array"))
		c.filter = SendID2(Alloc(Class("SCContentFilter")), Sel("initWithDisplay:excludingWindows:"), display, excluded)
		c.config = SendID(Alloc(Class("SCStreamConfiguration")), Sel("init"))
		if c.filter == 0 || c.config == 0 {
			openErr = fmt.Errorf("media: macOS refused the screen's filter")
			return
		}
		// The pictures come in the format the UI draws: the system converts
		// them, as a decoder does, rather than this package converting
		// twenty-five of them a second on the CPU.
		SendVoid1(c.config, Sel("setWidth:"), uintptr(w))
		SendVoid1(c.config, Sel("setHeight:"), uintptr(h))
		SendVoid1(c.config, Sel("setPixelFormat:"), PixelFormat32BGRA)
		SendVoid1(c.config, Sel("setShowsCursor:"), 1)
		SendVoid1(c.config, Sel("setQueueDepth:"), 5)

		c.delegate = SendID(Alloc(uintptr(class)), Sel("init"))
		c.stream = SendID3(Alloc(Class("SCStream")), Sel("initWithFilter:configuration:delegate:"), c.filter, c.config, c.delegate)
		if c.stream == 0 {
			openErr = fmt.Errorf("media: macOS refused a screen stream")
			return
		}
		screenRoutes.Store(c.delegate, c)
		var errRef uintptr
		SendVoid4(c.stream, Sel("addStreamOutput:type:sampleHandlerQueue:error:"), c.delegate, uintptr(SCStreamOutputTypeScreen), queue, uintptr(unsafe.Pointer(&errRef)))
		if errRef != 0 {
			openErr = fmt.Errorf("media: the screen refused a stream output: %s", ErrorString(errRef))
			return
		}

		// Starting runs the system's own capture, and answers on a queue of
		// its own: wait for it rather than hand out a stream that may not run.
		started := make(chan error, 1)
		block := objc.NewBlock(func(b objc.Block, startErr uintptr) {
			// The error belongs to the system for the length of this
			// callback: ask what it means here, not after it returned.
			started <- errorOf(startErr)
		})
		handler := block.Copy()
		SendVoid1(c.stream, Sel("startCaptureWithCompletionHandler:"), uintptr(handler))
		handler.Release()
		block.Release()
		if problem := <-started; problem != nil {
			openErr = fmt.Errorf("media: macOS did not start capturing the screen: %w", problem)
		}
	})
	if openErr != nil {
		c.Close()
		return nil, openErr
	}
	return c, nil
}

// Close stops the stream, waits for the system to stop calling back, and frees
// what it held. It is safe to call more than once.
func (c *ScreenCapture) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()

	screenRoutes.Delete(c.delegate)
	WithPool(func() {
		if c.stream != 0 {
			stopped := make(chan uintptr, 1)
			block := objc.NewBlock(func(b objc.Block, stopErr uintptr) { stopped <- stopErr })
			handler := block.Copy()
			SendVoid1(c.stream, Sel("stopCaptureWithCompletionHandler:"), uintptr(handler))
			handler.Release()
			block.Release()
			<-stopped
		}
		for _, p := range []uintptr{c.stream, c.filter, c.config, c.delegate} {
			if p != 0 {
				Release(p)
			}
		}
	})
	return nil
}

// screenDelegate is the class the stream calls back into, made once: the
// runtime has no way to free a class, so every stream shares one.
func screenDelegate() (objc.Class, error) {
	screenDelegateOnce.Do(func() {
		var protocols []*objc.Protocol
		if proto := objc.GetProtocol("SCStreamOutput"); proto != nil {
			protocols = append(protocols, proto)
		}
		screenDelegateClass, screenDelegateErr = objc.RegisterClass("MyGoScreenOutput", objc.GetClass("NSObject"), protocols, nil, []objc.MethodDef{
			{
				Cmd: objc.RegisterName("stream:didOutputSampleBuffer:ofType:"),
				Fn: func(self objc.ID, _cmd objc.SEL, stream objc.ID, sample uintptr, typ int) {
					if v, ok := screenRoutes.Load(uintptr(self)); ok {
						v.(*ScreenCapture).fn(sample)
					}
				},
			},
			{
				Cmd: objc.RegisterName("stream:didStopWithError:"),
				Fn: func(self objc.ID, _cmd objc.SEL, stream objc.ID, errRef uintptr) {
					if v, ok := screenRoutes.Load(uintptr(self)); ok {
						v.(*ScreenCapture).stop(errRef)
					}
				},
			},
		})
	})
	return screenDelegateClass, screenDelegateErr
}
