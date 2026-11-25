//go:build darwin && !ios

package gpusurface

import (
	"errors"
	"math"
	"runtime"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// Direct mode lets WindowServer scan the drawables out as they are instead
// of compositing them. It needs the layer opaque, covering the screen,
// untransformed, its drawables at the screen's native size in BGRA8 or
// BGR10A2, and nothing composited above it. The surface's view hosts the
// layer with no layer of its own, and the window's other views keep their
// place, first responder and tracking areas, only their layers hidden: the
// app still gets every key and mouse event while they show nothing.

type hiddenLayer struct {
	layer  uintptr // retained
	hidden bool
}

var (
	selIsHidden  = objc.RegisterName("isHidden")
	selSetHidden = objc.RegisterName("setHidden:")
)

// Direct reports whether direct mode is on.
func (s *Surface) Direct() bool { return s.direct.Load() }

// ScreenSize is the native size in pixels of the screen the window is on,
// or 0, 0 when it is on none. Call it on the main thread.
func (s *Surface) ScreenSize() (w, h int) {
	screen := send(s.window, "screen")
	if screen == 0 {
		return 0, 0
	}
	f := lib.msgRect(screen, objc.RegisterName("frame"))
	scale := lib.msgFloat(screen, objc.RegisterName("backingScaleFactor"))
	return int(math.Round(f.W * scale)), int(math.Round(f.H * scale))
}

// SetDirect turns direct mode on or off. On, the drawables take the
// screen's size and the layers of the content view's other subviews are hidden; off
// shows them as they were and gives the drawables the size Resize last
// set. The window should be full screen with nothing to show over the
// frames while it is on. Call it on the main thread. It needs
// Options.Composite.
func (s *Surface) SetDirect(on bool) error {
	if s.host == 0 {
		return errors.New("gpusurface: direct mode needs Options.Composite")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool, _, _ := purego.SyscallN(lib.poolPush)
	defer purego.SyscallN(lib.poolPop, pool)
	s.sizeMu.Lock()
	defer s.sizeMu.Unlock()
	if on == s.direct.Load() {
		return nil
	}
	if on {
		w, h := s.ScreenSize()
		if w <= 0 || h <= 0 {
			return errors.New("gpusurface: the window is on no screen")
		}
		// AppKit sets a new view's layer.hidden from the view on its first
		// commit: commit first, so that hiding sticks.
		send(class("CATransaction"), "flush")
		views := send(send(s.window, "contentView"), "subviews")
		for i := range int(send(views, "count")) {
			v := send(views, "objectAtIndex:", uintptr(i))
			l := send(v, "layer")
			if v == s.host || l == 0 {
				continue
			}
			send(l, "retain")
			s.hidden = append(s.hidden, hiddenLayer{layer: l, hidden: objc.Send[bool](objc.ID(l), selIsHidden)})
			objc.ID(l).Send(selSetHidden, true)
		}
		s.setDrawableSize(s.layer, w, h)
		s.direct.Store(true)
	} else {
		for i := range s.hidden {
			objc.ID(s.hidden[i].layer).Send(selSetHidden, s.hidden[i].hidden)
			release(&s.hidden[i].layer)
		}
		s.hidden = s.hidden[:0]
		s.setDrawableSize(s.layer, s.viewW, s.viewH)
		s.direct.Store(false)
	}
	send(class("CATransaction"), "flush")
	return nil
}
