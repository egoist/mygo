package fake

import (
	"errors"
	"runtime"
	"sync"

	"github.com/egoist/mygo/internal/platform"
)

// fakeParent is the handle the fake reports as the parent of a native
// view: a real backend reports the window's content view.
const fakeParent = 1

// NativeView is a native view under a fake Surface (Surface.NewNativeView):
// the view the app's factory made, which the surface places and destroys
// as a real one would.
type NativeView struct {
	host *Window
	// view is what the app's factory made: a real backend attaches its
	// handle to the window, the fake only remembers it.
	view platform.NativeView

	// Placement is where the content last showed the view, while Shown;
	// Focused tells that Focus gave it the keyboard since, and Back that
	// it did so going back.
	Placement platform.NativeViewPlacement
	Shown     bool
	Focused   bool
	Back      bool
	mu        sync.Mutex
	closed    bool
}

// NativeView returns the view the app made, which the surface places.
func (v *NativeView) NativeView() platform.NativeView { return v.view }

// NewNativeView makes the platform side of a native view: it calls make
// with the host of the view, as a backend does, and keeps it for
// PlaceNativeViews. A backend that cannot make the view fails the make
// call itself; one that cannot attach the view it made fails after it, as
// FailNativeViews does.
func (s *Surface) NewNativeView(make func(platform.NativeViewHost) (platform.NativeView, error)) (platform.NativeView, error) {
	// A real backend reports the platform it runs on and the handle of
	// the window's content view; the fake stands in for the one it runs
	// on, with a pointer of its own.
	v, err := make(platform.NativeViewHost{Platform: runtime.GOOS, Parent: fakeParent, Scale: s.Scale})
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, errors.New("fake: the factory made no native view")
	}
	s.w.b.mu.Lock()
	err = s.w.b.nativeErr
	s.w.b.mu.Unlock()
	if err != nil {
		// The view was made, and the window would not take it: mygo
		// destroys it.
		return nil, err
	}
	n := &NativeView{host: s.w, view: v}
	s.mu.Lock()
	s.nativeViews = append(s.nativeViews, n)
	s.mu.Unlock()
	return n, nil
}

func (s *Surface) PlaceNativeViews(views []platform.NativeViewPlacement) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nativePlacements++
	for _, v := range s.nativeViews {
		v.Shown = false
	}
	s.nativeOrder = s.nativeOrder[:0]
	for _, p := range views {
		v, ok := p.View.(*NativeView)
		if !ok || v.host != s.w || v.IsClosed() {
			continue
		}
		v.Placement, v.Shown = p, true
		v.Placement.Covers = append([]platform.RectF(nil), p.Covers...)
		s.nativeOrder = append(s.nativeOrder, v)
	}
	for _, v := range s.nativeViews {
		if !v.Shown {
			v.Focused = false // a hidden view keeps no keyboard
		}
	}
}

// NativeViews returns the surface's native views not closed, in the order
// they were made.
func (s *Surface) NativeViews() []*NativeView {
	s.mu.Lock()
	defer s.mu.Unlock()
	var list []*NativeView
	for _, v := range s.nativeViews {
		if !v.IsClosed() {
			list = append(list, v)
		}
	}
	return list
}

// NativePlacements counts the PlaceNativeViews calls.
func (s *Surface) NativePlacements() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nativePlacements
}

// NativeViewAt returns the native view that takes the pointer at x, y: the
// last shown, in the order the content painted them, whose placement takes
// it.
func (s *Surface) NativeViewAt(x, y float64) *NativeView {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.nativeOrder) - 1; i >= 0; i-- {
		if v := s.nativeOrder[i]; v.Shown && v.Placement.At(x, y) {
			return v
		}
	}
	return nil
}

// TakeFocus gives the content the keyboard back from a native view, as
// Tab leaving its last control does.
func (s *Surface) TakeFocus() {
	s.mu.Lock()
	s.tookFocus++
	s.mu.Unlock()
}

// NativeTakeFocus counts the TakeFocus calls.
func (s *Surface) NativeTakeFocus() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tookFocus
}

// FailNativeViews makes making a native view fail with err, nil to stop.
func (b *Backend) FailNativeViews(err error) {
	b.mu.Lock()
	b.nativeErr = err
	b.mu.Unlock()
}

func (v *NativeView) NativeHandle() uintptr { return v.view.NativeHandle() }

// Focus gives the view the keyboard, as Tab does, and tells the view the
// app made, which a real backend's toolkit does.
func (v *NativeView) Focus(back bool) {
	v.mu.Lock()
	v.Focused, v.Back = true, back
	v.mu.Unlock()
	v.view.Focus(back)
}

// Destroy destroys the view the app made, once.
func (v *NativeView) Destroy() {
	v.mu.Lock()
	if v.closed {
		v.mu.Unlock()
		return
	}
	v.closed, v.Shown, v.Focused = true, false, false
	v.mu.Unlock()
	v.view.Destroy()
}

// IsClosed reports whether the view was destroyed.
func (v *NativeView) IsClosed() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.closed
}

// Host returns the window the native view is in.
func (v *NativeView) Host() *Window { return v.host }
