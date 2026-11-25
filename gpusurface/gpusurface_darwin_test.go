//go:build darwin && !ios

package gpusurface

import (
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// AppKit takes windows and views on the main thread only: the tests run
// on another goroutine and hand their AppKit calls to onMain.
func init() { runtime.LockOSThread() }

var mainCalls = make(chan func())

// onMain runs fn on the main thread. fn reports failures with t.Error,
// not t.Fatal, which would end the main thread's loop.
func onMain(fn func()) {
	done := make(chan struct{})
	mainCalls <- func() { fn(); close(done) }
	<-done
}

var initWindow func(obj uintptr, sel objc.SEL, r cgRect, style, backing uintptr, deferred bool) uintptr

func TestMain(m *testing.M) {
	code := make(chan int, 1)
	go func() { code <- m.Run() }()
	for {
		select {
		case fn := <-mainCalls:
			fn()
		case c := <-code:
			os.Exit(c)
		}
	}
}

// testWindow makes a window of w x h points that is never shown, its
// content view holding one subview standing for MyGo's surface view.
func testWindow(t *testing.T, w, h float64) (win, content, ui uintptr) {
	if err := load(); err != nil {
		t.Fatal(err)
	}
	if initWindow == nil {
		if _, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
			t.Fatal(err)
		}
		purego.RegisterFunc(&initWindow, lib.msgSend)
	}
	onMain(func() {
		send(class("NSApplication"), "sharedApplication")
		const styleTitled, backingBuffered = 1, 2
		win = initWindow(send(class("NSWindow"), "alloc"), objc.RegisterName("initWithContentRect:styleMask:backing:defer:"),
			cgRect{W: w, H: h}, styleTitled, backingBuffered, false)
		if win == 0 {
			return
		}
		send(win, "setReleasedWhenClosed:", false)
		content = send(win, "contentView")
		ui = lib.msgInitRect(send(class("NSView"), "alloc"), objc.RegisterName("initWithFrame:"), cgRect{W: w, H: h})
		send(ui, "setWantsLayer:", true)
		send(content, "addSubview:", ui)
	})
	if win == 0 {
		t.Fatal("cannot make a window")
	}
	t.Cleanup(func() {
		onMain(func() {
			send(win, "close")
			send(ui, "release")
			send(win, "release")
		})
	})
	return win, content, ui
}

func subviews(view uintptr) []uintptr {
	all := send(view, "subviews")
	n := int(send(all, "count"))
	out := make([]uintptr, n)
	for i := range out {
		out[i] = send(all, "objectAtIndex:", uintptr(i))
	}
	return out
}

// TestCompositeUnderContent: a composite surface's layer sits in a view
// at the back of the content view, under the views already there, that
// follows the window's size; Resize sets only the drawables, and Close
// takes the view away.
func TestCompositeUnderContent(t *testing.T) {
	win, content, ui := testWindow(t, 400, 300)
	var s *Surface
	var err error
	onMain(func() { s, err = New(Options{Window: win, Composite: true}) })
	if err != nil {
		t.Fatal(err)
	}
	onMain(func() {
		views := subviews(content)
		if len(views) != 2 || views[0] != s.host || views[1] != ui {
			t.Errorf("content subviews %#x, want [surface view %#x, UI view %#x]", views, s.host, ui)
		}
		if got := send(s.host, "layer"); got != s.layer {
			t.Errorf("the surface view hosts layer %#x, not the surface's layer %#x", got, s.layer)
		}
		lib.msgSetSize(content, objc.RegisterName("setFrameSize:"), cgSize{640, 480})
		if f := lib.msgRect(s.host, objc.RegisterName("frame")); f.W != 640 || f.H != 480 {
			t.Errorf("surface view frame %+v after the content view became 640x480", f)
		}
	})
	if err := s.Resize(1280, 960); err != nil {
		t.Fatal(err)
	}
	if w, h := s.Size(); w != 1280 || h != 960 {
		t.Fatalf("Size %dx%d after Resize(1280, 960)", w, h)
	}
	onMain(s.Close)
	onMain(func() {
		if views := subviews(content); len(views) != 1 || views[0] != ui {
			t.Errorf("content subviews after Close %#x, want only the UI view %#x", views, ui)
		}
	})
}

// testIOSurface makes a w x h BGRA IOSurface standing for a decoded frame.
func testIOSurface(t *testing.T, w, h int) uintptr {
	io, err := purego.Dlopen("/System/Library/Frameworks/IOSurface.framework/IOSurface", purego.RTLD_GLOBAL|purego.RTLD_NOW)
	if err != nil {
		t.Fatal(err)
	}
	key := func(name string) uintptr {
		p, err := purego.Dlsym(io, name)
		if err != nil {
			t.Fatal(err)
		}
		return **(**uintptr)(unsafe.Pointer(&p))
	}
	create, err := purego.Dlsym(io, "IOSurfaceCreate")
	if err != nil {
		t.Fatal(err)
	}
	var surface uintptr
	onMain(func() {
		props := send(class("NSMutableDictionary"), "dictionary")
		for k, v := range map[string]int{
			"kIOSurfaceWidth": w, "kIOSurfaceHeight": h, "kIOSurfaceBytesPerElement": 4,
			"kIOSurfaceBytesPerRow": w * 4, "kIOSurfacePixelFormat": fourccBGRA,
		} {
			send(props, "setObject:forKey:", send(class("NSNumber"), "numberWithLongLong:", uintptr(v)), key(k))
		}
		surface, _, _ = purego.SyscallN(create, props)
	})
	if surface == 0 {
		t.Fatal("cannot make an IOSurface")
	}
	t.Cleanup(func() { msg(surface, selRelease) })
	return surface
}

// TestResizeWhileWindowResizes stands for dragging a streaming window's
// edge: presenting goroutines resize their surfaces before each frame
// while the main thread resizes the windows, and busy goroutines keep the
// scheduler moving the presenting ones between threads. An autorelease
// pool popped on another thread than the one that pushed it crashes.
func TestResizeWhileWindowResizes(t *testing.T) {
	const workers, rounds = 4, 400
	frame := testIOSurface(t, 64, 64)
	type target struct {
		s       *Surface
		content uintptr
	}
	var targets [workers]target
	for i := range targets {
		win, content, _ := testWindow(t, 400, 300)
		var err error
		onMain(func() { targets[i].s, err = New(Options{Window: win, Composite: true}) })
		if err != nil {
			t.Fatal(err)
		}
		targets[i].content = content
		s := targets[i].s
		t.Cleanup(func() { onMain(s.Close) })
	}

	stop := make(chan struct{})
	var busy sync.WaitGroup
	for range 2 * runtime.GOMAXPROCS(0) {
		busy.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
					runtime.Gosched()
				}
			}
		})
	}
	dragged := make(chan struct{})
	go func() {
		defer close(dragged)
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			onMain(func() {
				for _, tg := range targets {
					lib.msgSetSize(tg.content, objc.RegisterName("setFrameSize:"), cgSize{float64(300 + i%200), float64(200 + i%150)})
				}
			})
		}
	}()

	var presented, noDrawable atomic.Int64
	var workersDone sync.WaitGroup
	for _, tg := range targets {
		workersDone.Go(func() {
			for i := range rounds {
				if err := tg.s.Resize(600+i%400, 400+i%300); err != nil {
					t.Error(err)
					return
				}
				switch err := tg.s.Present(frame); err {
				case nil:
					presented.Add(1)
				case errNoDrawable:
					noDrawable.Add(1)
				default:
					t.Error(err)
					return
				}
			}
		})
	}
	workersDone.Wait()
	close(stop)
	busy.Wait()
	<-dragged
	t.Logf("%d resizes, %d frames presented, %d without a drawable", workers*rounds, presented.Load(), noDrawable.Load())
	if presented.Load() == 0 {
		t.Error("no frame presented")
	}
}

// TestWindowedPresentKeepsAspect: out of direct mode a frame of another
// shape than the window shows whole, letterboxed, not stretched.
func TestWindowedPresentKeepsAspect(t *testing.T) {
	frame := testIOSurface(t, 64, 36)
	win, _, _ := testWindow(t, 400, 300)
	var s *Surface
	var err error
	onMain(func() { s, err = New(Options{Window: win, Composite: true}) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { onMain(s.Close) })
	if err := s.Present(frame); err != nil {
		t.Fatal(err)
	}
	dw, dh := s.Size()
	want, _ := fitRect(64, 36, uintptr(dw), uintptr(dh))
	if s.Direct() || s.fit != want || s.fit[1] >= 1 {
		t.Fatalf("windowed fit %v in %dx%d (direct %v), want %v", s.fit, dw, dh, s.Direct(), want)
	}
}
