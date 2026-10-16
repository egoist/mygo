//go:build !js

package mygo

import (
	"errors"
	"runtime"
	"sync"
	"testing"

	"github.com/egoist/mygo/internal/fake"
	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/surface"
	"github.com/egoist/mygo/ui"
)

// testAdapter is the platform view a test's factory builds: it records
// what the app's own view hears.
type testAdapter struct {
	handle    uintptr
	focuses   []bool
	destroys  int
	host      *NativeViewHost
	onHandle  func()
	onFocus   func()
	onDestroy func()
	// getters counts what the app's own view was asked for its platform
	// handle, which mygo reads once and never asks for again.
	getters int
}

func (a *testAdapter) NativeHandle() uintptr {
	a.getters++
	if a.onHandle != nil {
		a.onHandle()
	}
	return a.handle
}

func (a *testAdapter) Focus(back bool) {
	a.focuses = append(a.focuses, back)
	if a.onFocus != nil {
		a.onFocus()
	}
}

func (a *testAdapter) Destroy() {
	a.destroys++
	if a.onDestroy != nil {
		a.onDestroy()
	}
}

// nativeViewWindow shows a native view in a window's Content, as
// webViewWindow does for a web view.
func nativeViewWindow(t *testing.T, show *bool) (*Window, *NativeView, *fake.Surface, *fake.NativeView, *testAdapter) {
	t.Helper()
	adapter := &testAdapter{handle: 7}
	var v *NativeView
	w, _, s := contentWindow(t, func(c *ui.Context) {
		ui.Column(c).Fill().AlignItems(ui.Stretch).Children(func() {
			ui.Text(c, "Toolbar").Height(40)
			if v != nil && *show {
				ui.NativeView(c, v).Grow(1)
			}
		})
	})
	var err error
	v, err = NewNativeView(w, NativeViewOptions{Create: func(h *NativeViewHost) (NativeViewAdapter, error) {
		adapter.host = h
		return adapter, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	views := s.NativeViews()
	if len(views) != 1 {
		t.Fatalf("made %d native views: want one", len(views))
	}
	fv := views[0]
	w.Invalidate()
	onMain(func() {})
	onMain(func() { s.Frame() })
	return w, v, s, fv, adapter
}

// TestNativeViewPlaced creates a native view, shows it where its element
// is, hides it when the element goes without destroying it, and destroys
// it once.
func TestNativeViewPlaced(t *testing.T) {
	show := true
	w, v, s, fv, adapter := nativeViewWindow(t, &show)
	if v.IsDestroyed() {
		t.Fatal("the native view is destroyed")
	}
	if v.NativeHandle() != 7 {
		t.Errorf("the native view's handle is %d: want the adapter's 7", v.NativeHandle())
	}
	h := adapter.host
	if h == nil || h.Platform != runtime.GOOS || h.Parent == 0 || h.Scale != 1 {
		t.Errorf("the factory heard of %+v: want the platform, the parent and the scale", h)
	}
	if !fv.Shown || fv.Placement.Frame.Y != 40 || fv.Placement.Frame.H != 160 || fv.Placement.Frame.W != 300 {
		t.Errorf("the native view shows at %+v, shown %v: want its element's box", fv.Placement.Frame, fv.Shown)
	}
	if s.NativeViewAt(150, 100) != fv {
		t.Error("the native view does not take the pointer where it shows")
	}

	// The content hiding it hides the view, which keeps its state.
	show = false
	w.Invalidate()
	onMain(func() {})
	onMain(func() { s.Frame() })
	if fv.Shown || s.NativeViewAt(150, 100) != nil {
		t.Error("the native view shows where the content does not")
	}
	if v.IsDestroyed() || adapter.destroys != 0 {
		t.Errorf("hiding destroyed the native view %d times", adapter.destroys)
	}

	// Shown again.
	show = true
	w.Invalidate()
	onMain(func() {})
	onMain(func() { s.Frame() })
	if !fv.Shown {
		t.Error("the native view does not show again")
	}

	// Destroyed once, whatever the calls.
	v.Destroy()
	if !v.IsDestroyed() || adapter.destroys != 1 || fv.Shown {
		t.Errorf("after Destroy: destroyed %v, %d destroys, shown %v", v.IsDestroyed(), adapter.destroys, fv.Shown)
	}
	v.Destroy()
	if adapter.destroys != 1 {
		t.Errorf("Destroy destroyed the view %d times: want once", adapter.destroys)
	}
	if s.NativeViewAt(150, 100) != nil {
		t.Error("a destroyed native view still takes the pointer")
	}
}

// A native view goes in a window showing native UI.
func TestNativeViewNeedsContent(t *testing.T) {
	w, _ := testWindow(t, WindowOptions{})
	if _, err := NewNativeView(w, NativeViewOptions{Create: func(*NativeViewHost) (NativeViewAdapter, error) {
		return &testAdapter{handle: 1}, nil
	}}); !errors.Is(err, errNoContent) {
		t.Errorf("NewNativeView without Content: %v, want %v", err, errNoContent)
	}
}

// A factory that fails leaves nothing behind: mygo destroys the view it
// made, however the creation failed.
func TestNativeViewCreateFailed(t *testing.T) {
	boom := errors.New("no view for you")
	create := func(t *testing.T, create func(*NativeViewHost) (NativeViewAdapter, error)) (*Window, *fake.Surface) {
		t.Helper()
		w, _, s := contentWindow(t, func(c *ui.Context) {
			ui.Box(c).Fill()
		})
		if _, err := NewNativeView(w, NativeViewOptions{Create: create}); err == nil {
			t.Fatal("NewNativeView succeeded: want an error")
		}
		return w, s
	}

	// The factory reports an error, with a view of its own.
	adapter := &testAdapter{handle: 7}
	_, s := create(t, func(*NativeViewHost) (NativeViewAdapter, error) { return adapter, boom })
	if adapter.destroys != 1 {
		t.Errorf("the view of a failed factory was destroyed %d times: want once", adapter.destroys)
	}
	if len(s.NativeViews()) != 0 {
		t.Errorf("a failed creation left %d native views", len(s.NativeViews()))
	}

	// And no view at all.
	create(t, func(*NativeViewHost) (NativeViewAdapter, error) { return nil, boom })
	create(t, func(*NativeViewHost) (NativeViewAdapter, error) { return nil, nil })

	// A view without a platform handle is no view.
	zero := &testAdapter{}
	_, s = create(t, func(*NativeViewHost) (NativeViewAdapter, error) { return zero, nil })
	if zero.destroys != 1 || len(s.NativeViews()) != 0 {
		t.Errorf("a view without a handle: %d destroys, %d views", zero.destroys, len(s.NativeViews()))
	}

	// A backend that cannot attach it destroys it too, once.
	adapter = &testAdapter{handle: 7}
	fb.FailNativeViews(boom)
	defer fb.FailNativeViews(nil)
	_, err := nativeViewWindowErr(t, adapter)
	if !errors.Is(err, boom) {
		t.Fatalf("a backend that cannot attach the view: %v, want %v", err, boom)
	}
	if adapter.destroys != 1 {
		t.Errorf("the view of a failed attach was destroyed %d times: want once", adapter.destroys)
	}
}

// nativeViewWindowErr makes a native view in a window of its own, for a
// creation that fails.
func nativeViewWindowErr(t *testing.T, adapter *testAdapter) (*NativeView, error) {
	t.Helper()
	w, _, _ := contentWindow(t, func(c *ui.Context) { ui.Box(c).Fill() })
	return NewNativeView(w, NativeViewOptions{Create: func(*NativeViewHost) (NativeViewAdapter, error) {
		return adapter, nil
	}})
}

// Closing a window destroys its native views, once.
func TestNativeViewWindowClose(t *testing.T) {
	show := true
	w, v, _, _, adapter := nativeViewWindow(t, &show)
	w.Destroy()
	if !v.IsDestroyed() || adapter.destroys != 1 {
		t.Errorf("closing the window: destroyed %v, %d destroys", v.IsDestroyed(), adapter.destroys)
	}
}

// What the app's view tells mygo after Destroy, or from its own Destroy,
// changes nothing: the ui moved on.
func TestNativeViewCallbacksAfterDestroy(t *testing.T) {
	first, second := &testAdapter{handle: 1}, &testAdapter{handle: 2}
	var v1, v2 *NativeView
	w, _, s := contentWindow(t, func(c *ui.Context) {
		ui.Column(c).Fill().AlignItems(ui.Stretch).Children(func() {
			if v1 != nil {
				ui.NativeView(c, v1).Height(60)
			}
			if v2 != nil {
				ui.NativeView(c, v2).Height(60)
			}
		})
	})
	make := func(a *testAdapter) *NativeView {
		t.Helper()
		v, err := NewNativeView(w, NativeViewOptions{Create: func(h *NativeViewHost) (NativeViewAdapter, error) {
			a.host = h
			return a, nil
		}})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	v1 = make(first)
	v2 = make(second)
	w.Invalidate()
	onMain(func() {})
	onMain(func() { s.Frame() })

	// The first view gives the keyboard back with Tab: the content moves
	// on to the second view, which takes it.
	onMain(func() { first.host.TabOut(false) })
	onMain(func() { s.Frame() })
	if len(first.focuses) != 0 || len(second.focuses) != 1 || second.focuses[0] {
		t.Errorf("Tab out of the first view: it was focused %v, the second %v", first.focuses, second.focuses)
	}
	if n := s.NativeTakeFocus(); n != 1 {
		t.Errorf("the content took the keyboard back %d times: want once", n)
	}

	// Destroyed, its callbacks are inert.
	v1.Destroy()
	second.focuses = nil
	onMain(func() {
		first.host.TabOut(false)
		first.host.Focused()
		first.host.PointerDown(1, 2)
	})
	onMain(func() { s.Frame() })
	if len(second.focuses) != 0 {
		t.Errorf("a destroyed view's TabOut moved the keyboard to the second view: %v", second.focuses)
	}
}

// The app's view takes the keyboard from the content, and tells mygo of a
// press on it, which closes the content's popovers.
func TestNativeViewPointerDown(t *testing.T) {
	var v *NativeView
	adapter := &testAdapter{handle: 7}
	open, typed := false, ""
	w, _, s := contentWindow(t, func(c *ui.Context) {
		ui.Column(c).Fill().AlignItems(ui.Stretch).Children(func() {
			menu := ui.Button(c, "Menu").Height(30)
			if menu.Clicked() {
				open = true
			}
			ui.Popover(c, menu, &open, func() { ui.Text(c, "Item") })
			ui.TextInput(c, &typed).Label("Name")
			if v != nil {
				ui.NativeView(c, v).Grow(1)
			}
		})
	})
	var err error
	v, err = NewNativeView(w, NativeViewOptions{Create: func(h *NativeViewHost) (NativeViewAdapter, error) {
		adapter.host = h
		return adapter, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	frame := func() {
		w.Invalidate()
		onMain(func() {})
		onMain(func() { s.Frame() })
	}
	frame()
	onMain(func() {
		s.Press(30, 15) // the menu button
		s.Frame()
	})
	if !open {
		t.Fatal("the popover did not open")
	}
	fv := s.NativeViews()[0]
	if s.NativeViewAt(150, 180) != fv {
		t.Fatal("the native view does not take the pointer there")
	}
	// The view takes the keyboard, as a click on its own control does, and
	// what it reports is in its own space: (10, 20) of the view.
	onMain(func() {
		adapter.host.Focused()
		adapter.host.PointerDown(10, 20)
		s.Frame()
	})
	if open {
		t.Error("a press on the native view left the popover open")
	}
}

// A factory that calls back into mygo while it builds the view, and a
// view that does from its Destroy, neither deadlock nor panic.
func TestNativeViewReentrant(t *testing.T) {
	var v *NativeView
	adapter := &testAdapter{handle: 7}
	w, _, s := contentWindow(t, func(c *ui.Context) {
		ui.Column(c).Fill().AlignItems(ui.Stretch).Children(func() {
			if v != nil {
				ui.NativeView(c, v).Grow(1)
			}
		})
	})
	var err error
	v, err = NewNativeView(w, NativeViewOptions{Create: func(h *NativeViewHost) (NativeViewAdapter, error) {
		adapter.host = h
		// What a native view does while it is made: the content hears of
		// it before mygo knows the view.
		h.Focused()
		h.TabOut(false)
		h.PointerDown(1, 2)
		w.Invalidate()
		return adapter, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	w.Invalidate()
	onMain(func() {})
	onMain(func() { s.Frame() })
	if !s.NativeViews()[0].Shown {
		t.Error("the native view does not show")
	}

	// Its Destroy tells mygo the view left the keyboard, as the content's
	// teardown destroys it.
	adapter.onDestroy = func() { adapter.host.TabOut(false) }
	other := &testAdapter{handle: 8}
	if _, err := NewNativeView(w, NativeViewOptions{Create: func(*NativeViewHost) (NativeViewAdapter, error) {
		return other, nil
	}}); err != nil {
		t.Fatal(err)
	}
	v.Destroy()
	if adapter.destroys != 1 {
		t.Errorf("the view was destroyed %d times", adapter.destroys)
	}
}

// The content of a window shows the native views of that window, and
// reports the ones of another window, which it does not show.
func TestNativeViewOfAnotherWindow(t *testing.T) {
	var v *NativeView
	adapter := &testAdapter{handle: 7}
	other, _, otherSurface := contentWindow(t, func(c *ui.Context) {
		ui.Column(c).Fill().AlignItems(ui.Stretch).Children(func() {
			if v != nil {
				ui.NativeView(c, v).Grow(1)
			}
		})
	})
	var err error
	v, err = NewNativeView(other, NativeViewOptions{Create: func(h *NativeViewHost) (NativeViewAdapter, error) {
		adapter.host = h
		return adapter, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	other.Invalidate()
	onMain(func() {})
	onMain(func() { otherSurface.Frame() })
	if !otherSurface.NativeViews()[0].Shown {
		t.Fatal("the view does not show where it belongs")
	}

	// The content of another window builds the same view: it shows nothing
	// there, and reports it.
	w, _, s := contentWindow(t, func(c *ui.Context) {
		ui.Column(c).Fill().AlignItems(ui.Stretch).Children(func() {
			ui.NativeView(c, v).Grow(1)
		})
	})
	w.Invalidate()
	onMain(func() {})
	onMain(func() { s.Frame() })
	if n := len(s.NativeViews()); n != 0 {
		t.Errorf("the content of another window shows %d native views: want none", n)
	}
}

// noNativeSurface is a surface that cannot show native views, as the
// backends of browsers and unsupported platforms are.
type noNativeSurface struct{ platform.Surface }

// A backend without native views reports them unsupported, as apps check
// with errors.Is(err, errors.ErrUnsupported).
func TestNativeViewUnsupported(t *testing.T) {
	w := &Window{conn: &surface.Conn{Surface: noNativeSurface{}}}
	_, err := newNativeView(w, NativeViewOptions{Create: func(*NativeViewHost) (NativeViewAdapter, error) {
		return &testAdapter{handle: 1}, nil
	}})
	if !errors.Is(err, errNativeViewUnsupported) || !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("native views on a surface without them: %v, want %v", err, errNativeViewUnsupported)
	}
}

// A native view is made only with a factory, which the apps that use them
// need, and which nothing else links.
func TestNativeViewNeedsFactory(t *testing.T) {
	w, _, _ := contentWindow(t, func(c *ui.Context) { ui.Box(c).Fill() })
	if _, err := NewNativeView(w, NativeViewOptions{}); !errors.Is(err, errNativeViewNoCreate) {
		t.Errorf("NewNativeView without a factory: %v, want %v", err, errNativeViewNoCreate)
	}
}

// The constructor of the feature is free-standing, and no window is one of
// the things it reports rather than panics on: the apps that call it for a
// window that is nothing get the error of a window that is gone.
func TestNewNativeViewNilWindow(t *testing.T) {
	var w *Window
	create := func(*NativeViewHost) (NativeViewAdapter, error) { return &testAdapter{handle: 1}, nil }
	if v, err := NewNativeView(w, NativeViewOptions{Create: create}); v != nil || !errors.Is(err, errDestroyed) {
		t.Errorf("NewNativeView(no window, ...): %v, %v: want no view and %v", v, err, errDestroyed)
	}
	// The options come first, as they do for a window.
	if _, err := NewNativeView(w, NativeViewOptions{}); !errors.Is(err, errNativeViewNoCreate) {
		t.Errorf("NewNativeView(no window, no factory): %v, want %v", err, errNativeViewNoCreate)
	}
}

// The app's own view reports Tab leaving it, through the host the window
// hands its factory (mygo.NativeViewHost.TabOut), and the content moves the
// focus on from the view's element, in both directions.
func TestNativeViewBackendTabOut(t *testing.T) {
	first, second := &testAdapter{handle: 7}, &testAdapter{handle: 8}
	var v1, v2 *NativeView
	var h1, h2 *NativeViewHost
	w, _, s := contentWindow(t, func(c *ui.Context) {
		ui.Column(c).Fill().AlignItems(ui.Stretch).Children(func() {
			if v1 != nil {
				ui.NativeView(c, v1).Height(80)
			}
			if v2 != nil {
				ui.NativeView(c, v2).Height(80)
			}
		})
	})
	var err error
	v1, err = NewNativeView(w, NativeViewOptions{Create: func(h *NativeViewHost) (NativeViewAdapter, error) {
		h1 = h
		return first, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	v2, err = NewNativeView(w, NativeViewOptions{Create: func(h *NativeViewHost) (NativeViewAdapter, error) {
		h2 = h
		return second, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	w.Invalidate()
	onMain(func() {})
	onMain(func() { s.Frame() })
	views := s.NativeViews()
	if len(views) != 2 {
		t.Fatalf("made %d native views: want two", len(views))
	}

	onMain(func() { h1.TabOut(false) })
	onMain(func() {})
	if len(second.focuses) != 1 || second.focuses[0] {
		t.Errorf("Tab out of the first view: the second heard %v, want one Focus(false)", second.focuses)
	}

	onMain(func() { h2.TabOut(true) })
	onMain(func() {})
	if len(first.focuses) != 1 || !first.focuses[0] {
		t.Errorf("Shift+Tab out of the second view: the first heard %v, want one Focus(true)", first.focuses)
	}
}

// nativeViewsOf counts the native views the cleanup the view joined still
// holds, through the bridge the view keeps (NativeView.bridge): the window
// clears its connection before its cleanup runs, so the bridge outlives the
// detach that runs it. A view that never came to be joined none.
func nativeViewsOf(v *NativeView) int {
	if v == nil {
		return 0
	}
	windows.Lock()
	defer windows.Unlock()
	if v.bridge == nil {
		return 0
	}
	return len(v.bridge.Views)
}

// nativeViewRegistered reports whether the cleanup the view joined still
// holds the view itself.
func nativeViewRegistered(v *NativeView) bool {
	if v == nil {
		return false
	}
	windows.Lock()
	defer windows.Unlock()
	if v.bridge == nil {
		return false
	}
	for _, x := range v.bridge.Views {
		if x == v {
			return true
		}
	}
	return false
}

// A view that never came to be reaches no window: what the view's own
// callbacks report while the factory runs, and what the cleanup of the view
// it made reports, changes nothing of the content.
func TestNativeViewFailedIsInert(t *testing.T) {
	first, second := &testAdapter{handle: 1}, &testAdapter{handle: 2}
	var v1, v2 *NativeView
	w, _, s := contentWindow(t, func(c *ui.Context) {
		ui.Column(c).Fill().AlignItems(ui.Stretch).Children(func() {
			if v1 != nil {
				ui.NativeView(c, v1).Height(60)
			}
			if v2 != nil {
				ui.NativeView(c, v2).Height(60)
			}
		})
	})
	make := func(a *testAdapter) *NativeView {
		t.Helper()
		v, err := NewNativeView(w, NativeViewOptions{Create: func(h *NativeViewHost) (NativeViewAdapter, error) {
			a.host = h
			return a, nil
		}})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	v1, v2 = make(first), make(second)
	w.Invalidate()
	onMain(func() {})
	onMain(func() { s.Frame() })

	// A factory that fails after making its view, and the cleanup of that
	// view, both report the view leaving the keyboard.
	boom := errors.New("no view for you")
	failed := &testAdapter{handle: 3}
	failed.onDestroy = func() {
		failed.host.TabOut(false)
		failed.host.Focused()
		failed.host.PointerDown(1, 2)
	}
	if _, err := NewNativeView(w, NativeViewOptions{Create: func(h *NativeViewHost) (NativeViewAdapter, error) {
		failed.host = h
		// What a view reports while it is made, before it failed.
		h.Focused()
		h.TabOut(false)
		h.PointerDown(1, 2)
		return failed, boom
	}}); !errors.Is(err, boom) {
		t.Fatalf("the failed creation: %v, want %v", err, boom)
	}
	if failed.destroys != 1 {
		t.Errorf("the view of the failed factory was destroyed %d times: want once", failed.destroys)
	}
	onMain(func() {})
	onMain(func() { s.Frame() })
	if len(first.focuses) != 0 || len(second.focuses) != 0 {
		t.Errorf("a failed creation moved the keyboard: the first view heard %v, the second %v",
			first.focuses, second.focuses)
	}
	if n := s.NativeTakeFocus(); n != 0 {
		t.Errorf("a failed creation gave the content the keyboard back %d times: want none", n)
	}
}

// The factory runs the app, which may close the window while its native view
// is made: the view joins nothing then, and what the factory made is
// destroyed once.
func TestNativeViewFactoryClosesWindow(t *testing.T) {
	adapter := &testAdapter{handle: 7}
	w, _, s := contentWindow(t, func(c *ui.Context) { ui.Box(c).Fill() })
	v, err := NewNativeView(w, NativeViewOptions{Create: func(h *NativeViewHost) (NativeViewAdapter, error) {
		adapter.host = h
		// The app closes the window the view was being made for, as one
		// closing itself from the view's own setup does.
		w.Close()
		return adapter, nil
	}})
	if !errors.Is(err, errDestroyed) {
		t.Errorf("a factory that closed its window: %v, want %v", err, errDestroyed)
	}
	if adapter.destroys != 1 {
		t.Errorf("the view of a factory that closed its window was destroyed %d times: want once", adapter.destroys)
	}
	if n := len(s.NativeViews()); n != 0 {
		t.Errorf("a factory that closed its window left %d native views", n)
	}
	if n := nativeViewsOf(v); n != 0 {
		t.Errorf("a factory that closed its window registered %d native views", n)
	}
}

// The app's own view is asked for its platform handle once, while the factory
// runs: what the content places, focuses and destroys is the view of the
// handle read then, and the app's getter runs no more, however the view is
// used after that.
func TestNativeViewHandleReadOnce(t *testing.T) {
	show := true
	_, v, s, fv, adapter := nativeViewWindow(t, &show)
	if adapter.getters != 1 {
		t.Errorf("the app's view was asked for its handle %d times while it was made: want once", adapter.getters)
	}
	if v.NativeHandle() != 7 || fv.NativeHandle() != 7 {
		t.Errorf("the view's handle is %d, the platform's %d: want the adapter's 7", v.NativeHandle(), fv.NativeHandle())
	}
	// Placing the view again, giving it the keyboard and destroying it are
	// what the platform does with it: the app's own view answers them all.
	v.Focus(false)
	onMain(func() { s.Frame() })
	v.Destroy()
	onMain(func() { s.Frame() })
	if adapter.getters != 1 {
		t.Errorf("the app's view was asked for its handle %d times over its life: want once", adapter.getters)
	}
	if len(adapter.focuses) != 1 || adapter.focuses[0] {
		t.Errorf("the view's Focus reached the app %v times: want one backward-less focus", adapter.focuses)
	}
	if adapter.destroys != 1 {
		t.Errorf("the view's Destroy reached the app %d times: want once", adapter.destroys)
	}
	if !v.IsDestroyed() {
		t.Error("the view is not destroyed after Destroy")
	}
}

// closeOnHandleSurface records what the platform heard of the factory of a
// native view whose app getter closes the window: the error the factory
// reported, and how many views the platform held when it reported it. A view
// of a window that moved on must fail before the platform is offered anything.
type closeOnHandleSurface struct {
	*fake.Surface
	err      error
	attached int
}

func (s *closeOnHandleSurface) NewNativeView(make func(platform.NativeViewHost) (platform.NativeView, error)) (platform.NativeView, error) {
	n, err := s.Surface.NewNativeView(make)
	s.err, s.attached = err, len(s.Surface.NativeViews())
	return n, err
}

// The app's getter is the app's own code, which may close the window while
// its native view is made, as a getter that tears its own window down does:
// the view is not attached to a window that moved on, and what the factory
// made is destroyed once.
func TestNativeViewGetterClosesWindow(t *testing.T) {
	adapter := &testAdapter{handle: 7}
	w, _, s := contentWindow(t, func(c *ui.Context) { ui.Box(c).Fill() })
	rec := &closeOnHandleSurface{Surface: s}
	onMain(func() { w.conn.Surface = rec })
	adapter.onHandle = w.Close
	v, err := NewNativeView(w, NativeViewOptions{Create: func(h *NativeViewHost) (NativeViewAdapter, error) {
		adapter.host = h
		return adapter, nil
	}})
	if v != nil {
		t.Errorf("a getter that closed its window made %v", v)
	}
	if !errors.Is(err, errDestroyed) {
		t.Errorf("a getter that closed its window: %v, want %v", err, errDestroyed)
	}
	if rec.err == nil {
		t.Error("the platform attached the view of a window the getter closed: the factory did not fail first")
	}
	if rec.attached != 0 {
		t.Errorf("the platform held %d views of a window the getter closed: want none", rec.attached)
	}
	if adapter.getters != 1 {
		t.Errorf("the app's view was asked for its handle %d times: want once", adapter.getters)
	}
	if adapter.destroys != 1 {
		t.Errorf("the view of a getter that closed its window was destroyed %d times: want once", adapter.destroys)
	}
	if n := len(s.NativeViews()); n != 0 {
		t.Errorf("a getter that closed its window left %d native views", n)
	}
	if n := nativeViewsOf(v); n != 0 {
		t.Errorf("a getter that closed its window registered %d native views", n)
	}

	// The callbacks of the view that never was reach nothing: the window it
	// reported to is gone.
	onMain(func() {
		adapter.host.Focused()
		adapter.host.TabOut(true)
		adapter.host.PointerDown(1, 2)
	})
	if n := s.NativeTakeFocus(); n != 0 {
		t.Errorf("a failed view gave the content the keyboard back %d times: want none", n)
	}
	if n := adapter.destroys; n != 1 {
		t.Errorf("what the failed view reports destroyed it %d times: want once", n)
	}
}

// SurfaceNativeView answers the content of the view's own window alone: the
// connections that show no window, which used to panic, answer nil.
func TestNativeViewSurfaceNativeViewConn(t *testing.T) {
	show := true
	w, v, _, _, _ := nativeViewWindow(t, &show)
	var conn *surface.Conn
	onMain(func() { conn = w.conn })
	if n := v.SurfaceNativeView(conn); n == nil {
		t.Error("the content of the view's own window does not get the platform view")
	}
	if n := v.SurfaceNativeView(nil); n != nil {
		t.Errorf("a connection that is nothing shows the view: %v", n)
	}
	if n := v.SurfaceNativeView(&surface.Conn{}); n != nil {
		t.Errorf("a connection without a window shows the view: %v", n)
	}
	if n := v.SurfaceNativeView(&surface.Conn{Window: "not a window"}); n != nil {
		t.Errorf("a connection of another kind of window shows the view: %v", n)
	}
	if n := v.SurfaceNativeView(&surface.Conn{Window: (*Window)(nil)}); n != nil {
		t.Errorf("a connection with no window of its own shows the view: %v", n)
	}

	// A destroyed view shows nothing on the connection that showed it.
	v.Destroy()
	if n := v.SurfaceNativeView(conn); n != nil {
		t.Errorf("a destroyed view shows on its own connection: %v", n)
	}
}

// SurfaceNativeViewHost answers the content of the view's own window alone
// with the platform surface that hosts the view: the opt-in bridge the
// content asks for one, so that an app that shows no native view reaches no
// native-view hosting at all.
func TestNativeViewSurfaceNativeViewHost(t *testing.T) {
	show := true
	w, v, s, _, _ := nativeViewWindow(t, &show)
	var conn *surface.Conn
	onMain(func() { conn = w.conn })
	if n := v.SurfaceNativeViewHost(conn); n == nil {
		t.Error("the content of the view's own window does not get the hosting surface")
	} else if n != platform.NativeViewSurface(s) {
		t.Errorf("the hosting surface is %T: want the surface the view was made through (%T)", n, s)
	}
	if n := v.SurfaceNativeViewHost(nil); n != nil {
		t.Errorf("a connection that is nothing hosts the view: %v", n)
	}
	if n := v.SurfaceNativeViewHost(&surface.Conn{}); n != nil {
		t.Errorf("a connection without a window hosts the view: %v", n)
	}
	if n := v.SurfaceNativeViewHost(&surface.Conn{Window: "not a window"}); n != nil {
		t.Errorf("a connection of another kind of window hosts the view: %v", n)
	}
	if n := v.SurfaceNativeViewHost(&surface.Conn{Window: (*Window)(nil)}); n != nil {
		t.Errorf("a connection with no window of its own hosts the view: %v", n)
	}

	// A destroyed view hosts nothing on the connection that hosted it.
	v.Destroy()
	if n := v.SurfaceNativeViewHost(conn); n != nil {
		t.Errorf("a destroyed view hosts on its own connection: %v", n)
	}
}

// Focus gives the view the keyboard, and the content's focus moves to the
// view's element with it, so that Tab moves on from there.
func TestNativeViewFocus(t *testing.T) {
	first, second := &testAdapter{handle: 1}, &testAdapter{handle: 2}
	var v1, v2 *NativeView
	w, _, s := contentWindow(t, func(c *ui.Context) {
		ui.Column(c).Fill().AlignItems(ui.Stretch).Children(func() {
			if v1 != nil {
				ui.NativeView(c, v1).Height(60)
			}
			if v2 != nil {
				ui.NativeView(c, v2).Height(60)
			}
		})
	})
	var err error
	v1, err = NewNativeView(w, NativeViewOptions{Create: func(h *NativeViewHost) (NativeViewAdapter, error) {
		first.host = h
		return first, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	v2, err = NewNativeView(w, NativeViewOptions{Create: func(h *NativeViewHost) (NativeViewAdapter, error) {
		second.host = h
		return second, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	frame := func() {
		w.Invalidate()
		onMain(func() {})
		onMain(func() { s.Frame() })
	}
	frame()

	v1.Focus(false)
	onMain(func() {})
	if len(first.focuses) != 1 || first.focuses[0] {
		t.Fatalf("the view Focus focused heard %v: want one Focus(false)", first.focuses)
	}
	frame()

	// Tab moves on from the view's element: the content's focus went there
	// with Focus, and Tab without it would come back to the first view.
	onMain(func() {
		s.Send(platform.SurfaceEvent{Kind: platform.KeyPressed, Key: platform.KeyTab})
	})
	onMain(func() {})
	frame()
	if len(second.focuses) != 1 || second.focuses[0] {
		t.Errorf("Tab after Focus: the second view heard %v, the first %v", second.focuses, first.focuses)
	}
}

// Focus takes nothing for a view the content does not show, for a destroyed
// one, and for nothing at all.
func TestNativeViewFocusNotShown(t *testing.T) {
	show := false
	w, v, s, _, adapter := nativeViewWindow(t, &show)
	v.Focus(false)
	onMain(func() {})
	if len(adapter.focuses) != 0 {
		t.Errorf("a view the content does not show was focused %v", adapter.focuses)
	}

	// Shown, it takes the keyboard.
	show = true
	w.Invalidate()
	onMain(func() {})
	onMain(func() { s.Frame() })
	v.Focus(false)
	onMain(func() {})
	if len(adapter.focuses) != 1 || adapter.focuses[0] {
		t.Fatalf("a view the content shows heard %v: want one Focus(false)", adapter.focuses)
	}

	// Destroyed, it takes nothing, and neither does no view at all.
	v.Destroy()
	v.Focus(false)
	onMain(func() {})
	if len(adapter.focuses) != 1 {
		t.Errorf("a destroyed view was focused: it heard %v", adapter.focuses)
	}
	var none *NativeView
	none.Focus(false)
}

// A view whose toolkit destroys it inside Focus, as one closing its own
// window does, leaves the content's focus handling standing.
func TestNativeViewFocusDestroys(t *testing.T) {
	show := true
	w, v, s, _, adapter := nativeViewWindow(t, &show)
	adapter.onFocus = func() { v.Destroy() }
	v.Focus(false)
	onMain(func() {})
	if !v.IsDestroyed() || adapter.destroys != 1 {
		t.Errorf("the view destroyed itself inside Focus: destroyed %v, %d destroys", v.IsDestroyed(), adapter.destroys)
	}
	w.Invalidate()
	onMain(func() {})
	onMain(func() { s.Frame() })
	if n := s.NativeViewAt(150, 100); n != nil {
		t.Error("the view destroyed inside Focus still shows")
	}
}

// Destroy takes the view out of the content's next frame: the window
// redraws without the app asking, so the place the view held goes away.
func TestNativeViewDestroyInvalidates(t *testing.T) {
	show := true
	_, v, s, fv, adapter := nativeViewWindow(t, &show)
	if !fv.Shown {
		t.Fatal("the native view does not show")
	}
	before := s.NativePlacements()
	v.Destroy()
	onMain(func() {}) // the invalidation the destroy asked for
	var drew bool
	onMain(func() { drew = s.Frame() })
	if !drew {
		t.Error("a destroyed native view asked for no frame: its hole stays")
	}
	if n := s.NativePlacements(); n == before {
		t.Error("the frame after the destroy did not place the content's native views again")
	}
	if adapter.destroys != 1 {
		t.Errorf("the view was destroyed %d times: want once", adapter.destroys)
	}
}

// The backends this feature runs on destroy the native children of a window as
// the window goes, before the window's handler hears of the close: darwin and
// linux close the native views they hold, and windows destroys them as part of
// the parent window. What the app's own view does while it is destroyed — hand
// the keyboard back, Tab out, report a press — reaches no content then, and the
// app's view is destroyed once, whichever side asks for it.
func TestNativeViewBackendDestroysView(t *testing.T) {
	first, second := &testAdapter{handle: 7}, &testAdapter{handle: 8}
	var v1, v2 *NativeView
	w, _, s := contentWindow(t, func(c *ui.Context) {
		ui.Column(c).Fill().AlignItems(ui.Stretch).Children(func() {
			if v1 != nil {
				ui.NativeView(c, v1).Height(60)
			}
			if v2 != nil {
				ui.NativeView(c, v2).Height(60)
			}
		})
	})
	place := func(a *testAdapter) *NativeView {
		t.Helper()
		v, err := NewNativeView(w, NativeViewOptions{Create: func(h *NativeViewHost) (NativeViewAdapter, error) {
			a.host = h
			return a, nil
		}})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	v1, v2 = place(first), place(second)
	w.Invalidate()
	onMain(func() {})
	onMain(func() { s.Frame() })
	views := s.NativeViews()
	if len(views) != 2 {
		t.Fatalf("made %d native views: want two", len(views))
	}

	// One view tearing itself down reports leaving the keyboard, as a view
	// that is going does. The platform destroys that view before the window's
	// handler hears of the close, so the core's view has to be gone by then.
	var during struct {
		destroyed  bool
		handle     uintptr
		registered int
		own        bool
	}
	first.onDestroy = func() {
		during.destroyed = v1.IsDestroyed()
		during.handle = v1.NativeHandle()
		during.registered = nativeViewsOf(v1)
		during.own = nativeViewRegistered(v1)
		first.host.Focused()
		first.host.TabOut(false)
		first.host.PointerDown(3, 4)
	}
	onMain(func() { views[0].Destroy() }) // the platform, closing its children
	onMain(func() { s.Frame() })

	if !during.destroyed || during.handle != 0 || during.own || during.registered != 1 {
		t.Errorf("inside the app's Destroy: destroyed %v, handle %d, this view registered %v of %d: want it invalidated first",
			during.destroyed, during.handle, during.own, during.registered)
	}
	if first.destroys != 1 || first.getters != 1 {
		t.Errorf("the app's view: %d destroys, %d getters, want one of each", first.destroys, first.getters)
	}
	if len(second.focuses) != 0 {
		t.Errorf("the app's Destroy moved the keyboard to the other view: %v", second.focuses)
	}
	if n := s.NativeTakeFocus(); n != 0 {
		t.Errorf("the app's Destroy gave the content the keyboard back %d times: want none", n)
	}
	var conn *surface.Conn
	onMain(func() { conn = w.conn })
	if !v1.IsDestroyed() || v1.NativeHandle() != 0 || v1.SurfaceNativeView(conn) != nil || v1.SurfaceNativeViewHost(conn) != nil {
		t.Errorf("after the platform destroyed it: destroyed %v, handle %d", v1.IsDestroyed(), v1.NativeHandle())
	}
	if n := nativeViewsOf(v1); n != 1 {
		t.Errorf("the window registered %d views after one was destroyed: want the other", n)
	}
	if n := len(s.NativeViews()); n != 1 || !views[0].IsClosed() {
		t.Errorf("%d native views are left, the first closed %v: want the other", n, views[0].IsClosed())
	}

	// The app destroying the view, the platform destroying what it still
	// holds, and the window closing after them all reach the app's own view
	// once.
	v1.Destroy()
	onMain(func() {})
	if first.destroys != 1 {
		t.Errorf("Destroy after the platform destroyed it: %d destroys, want once", first.destroys)
	}
	v2.Destroy()
	onMain(func() {})
	if second.destroys != 1 || second.getters != 1 {
		t.Errorf("an explicit Destroy: %d destroys, %d getters, want one of each", second.destroys, second.getters)
	}
	views[1].NativeView().Destroy() // the platform, as the window goes
	onMain(func() {})
	w.Destroy()
	if first.destroys != 1 || second.destroys != 1 {
		t.Errorf("the window's close destroyed the app's views again: %d and %d times", first.destroys, second.destroys)
	}
	if n := nativeViewsOf(v1); n != 0 {
		t.Errorf("the closed window registered %d views", n)
	}
}

// The window's content, native window and connection belong to the main
// thread, which is where NewNativeView reads them and where closing the
// window writes them: a call the queue runs after the close reports a window
// that is gone and makes nothing of the app's, and one it runs before it makes
// a view that the close then destroys once. The queue orders the two, not the
// clock.
func TestNativeViewCloseOrdersCreation(t *testing.T) {
	for _, closeFirst := range []bool{false, true} {
		name := "call first"
		if closeFirst {
			name = "close first"
		}
		t.Run(name, func(t *testing.T) {
			w, _, s := contentWindow(t, func(c *ui.Context) { ui.Box(c).Fill() })
			adapter := &testAdapter{handle: 7}
			made := false
			adapter.onHandle = func() { made = true }
			var (
				v   *NativeView
				err error
			)
			call := func() {
				v, err = NewNativeView(w, NativeViewOptions{Create: func(h *NativeViewHost) (NativeViewAdapter, error) {
					adapter.host = h
					return adapter, nil
				}})
			}
			if closeFirst {
				postMain(func() { w.Close() })
				postMain(call)
			} else {
				postMain(call)
				postMain(func() { w.Close() })
			}
			onMain(func() {}) // the call and the close ran, in the order the queue took

			if closeFirst {
				if v != nil || !errors.Is(err, errDestroyed) {
					t.Errorf("a call after the close: %v, %v: want no view and %v", v, err, errDestroyed)
				}
				if made || adapter.getters != 0 || adapter.destroys != 0 {
					t.Errorf("a call after the close reached the app's view: made %v, %d getters, %d destroys",
						made, adapter.getters, adapter.destroys)
				}
				if n := nativeViewsOf(v); n != 0 {
					t.Errorf("a call after the close registered %d views", n)
				}
				return
			}
			if v == nil || err != nil {
				t.Fatalf("a call before the close: %v, %v: want a view", v, err)
			}
			if !v.IsDestroyed() || v.NativeHandle() != 0 || !made || adapter.getters != 1 {
				t.Errorf("the view of a call before the close: destroyed %v, handle %d, made %v, %d getters",
					v.IsDestroyed(), v.NativeHandle(), made, adapter.getters)
			}
			if adapter.destroys != 1 {
				t.Errorf("the close destroyed the app's view %d times: want once", adapter.destroys)
			}
			if n := nativeViewsOf(v); n != 0 {
				t.Errorf("the closed window registered %d views", n)
			}
			if n := len(s.NativeViews()); n != 0 {
				t.Errorf("the closed window left %d native views", n)
			}
			v.Destroy()
			onMain(func() {})
			if adapter.destroys != 1 {
				t.Errorf("Destroy after the close destroyed the app's view %d times: want once", adapter.destroys)
			}
		})
	}
}

// Callers of NewNativeView and the close of their window come from goroutines
// of the app's: the main thread alone reaches the window's content, native
// window and connection, so every caller either gets a view of the window it
// started with — which the close destroys once, whenever it runs — or the
// error of a window that is gone, having made nothing of the app's. The close
// waits for one creation, so the two really do meet; neither side is timed.
func TestNativeViewCallersWithClose(t *testing.T) {
	const callers = 4
	w, _, s := contentWindow(t, func(c *ui.Context) { ui.Box(c).Fill() })
	type result struct {
		adapter *testAdapter
		v       *NativeView
		err     error
	}
	results := make([]result, callers)
	for i := range results {
		results[i].adapter = &testAdapter{handle: uintptr(i + 1)}
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	first := make(chan struct{}, callers)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			a := results[i].adapter
			v, err := NewNativeView(w, NativeViewOptions{Create: func(h *NativeViewHost) (NativeViewAdapter, error) {
				a.host = h
				first <- struct{}{}
				return a, nil
			}})
			results[i].v, results[i].err = v, err
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		<-first
		w.Destroy()
	}()
	close(start)
	wg.Wait()

	made := 0
	for i, r := range results {
		if r.err != nil {
			if !errors.Is(r.err, errDestroyed) || r.v != nil {
				t.Errorf("caller %d: %v, %v: want no view and %v", i, r.v, r.err, errDestroyed)
			}
			if r.adapter.getters != 0 || r.adapter.destroys != 0 {
				t.Errorf("caller %d made the app's view of a window that is gone: %d getters, %d destroys",
					i, r.adapter.getters, r.adapter.destroys)
			}
			continue
		}
		made++
		if r.v == nil || !r.v.IsDestroyed() || r.v.NativeHandle() != 0 {
			t.Errorf("caller %d: %v: want a view the close destroyed", i, r.v)
		}
		if r.adapter.getters != 1 || r.adapter.destroys != 1 {
			t.Errorf("caller %d: %d getters, %d destroys, want one of each", i, r.adapter.getters, r.adapter.destroys)
		}
	}
	if made == 0 {
		t.Error("no caller made a view, so no creation met the close")
	}
	// Every view joined the one cleanup of the window, which the close
	// emptied: each caller's own view sees that, not merely the window's
	// connection, which the detach clears whatever the cleanup did.
	for i, r := range results {
		if n := nativeViewsOf(r.v); n != 0 {
			t.Errorf("caller %d: the closed window registered %d views", i, n)
		}
	}
	if n := len(s.NativeViews()); n != 0 {
		t.Errorf("the closed window left %d native views", n)
	}
}
