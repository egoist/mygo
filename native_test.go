//go:build !js

package mygo

import (
	"context"
	"errors"
	"testing"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/surface"
	"github.com/egoist/mygo/ui"
)

type pluginViewSpy struct {
	platform.WebView
	closed     int
	closedMain bool
}

func (s *pluginViewSpy) Close() {
	s.closed++
	s.closedMain = isMainThread()
}

// TestNativePluginOnDesktop checks what a phone plugin's native handle
// does on a desktop: it is not available, calling it is refused with
// ErrUnsupported (after checking that its arguments can be encoded as
// JSON), and event listeners register until they are turned off.
func TestNativePluginOnDesktop(t *testing.T) {
	p := Native("location")
	if p.Available() {
		t.Error("a desktop reports native plugins as available")
	}

	if err := p.Call(context.Background(), "current", nil, nil); !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("Call with no args = %v, want %v", err, errors.ErrUnsupported)
	}
	if err := p.Call(context.Background(), "current", map[string]string{"accuracy": "high"}, nil); !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("Call with args = %v, want %v", err, errors.ErrUnsupported)
	}

	// Arguments that cannot be encoded fail before reaching the backend.
	if err := p.Call(context.Background(), "current", make(chan int), nil); err == nil || errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("Call with unmarshalable args = %v, want a JSON error", err)
	}

	// Calling from the main thread still settles rather than blocking.
	if err := onMainValue(func() error { return p.Call(context.Background(), "current", nil, nil) }); !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("Call on the main thread = %v, want %v", err, errors.ErrUnsupported)
	}

	// On adds the listener under the plugin and event name, and the
	// returned function removes it again (and tolerates a second call).
	key := "location\x00fix"
	off := p.On("fix", func([]byte) {})
	t.Cleanup(off)
	nativeEvents.Lock()
	n := len(nativeEvents.on[key])
	nativeEvents.Unlock()
	if n != 1 {
		t.Fatalf("after On, %d listeners are registered, want 1", n)
	}
	off()
	off()
	nativeEvents.Lock()
	n = len(nativeEvents.on[key])
	nativeEvents.Unlock()
	if n != 0 {
		t.Errorf("after off, %d listeners stay registered, want 0", n)
	}
}

// TestNewPluginView checks making a plugin's native view: bad arguments
// fail before touching the window, a window without native UI has no
// content to place it in, and a desktop's surface does not support one.
func TestNewPluginView(t *testing.T) {
	pageWin, _ := testWindow(t, WindowOptions{Hidden: true})
	if _, err := NewPluginView(pageWin, "map", make(chan int)); err == nil || errors.Is(err, errNoContent) || errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("NewPluginView with unmarshalable args = %v, want a JSON error", err)
	}
	if _, err := NewPluginView(pageWin, "map", nil); !errors.Is(err, errNoContent) {
		t.Errorf("NewPluginView on a page window = %v, want %v", err, errNoContent)
	}

	uiWin := NewWindow(WindowOptions{Hidden: true, Content: ui.View(func(*ui.Context) {})})
	t.Cleanup(uiWin.Destroy)
	if _, err := NewPluginView(uiWin, "map", map[string]int{"zoom": 10}); !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("NewPluginView on a desktop surface = %v, want %v", err, errors.ErrUnsupported)
	}
}

// TestPluginViewSurfaceAndDestroy checks that a plugin view only hands
// its native view to its own window's content, and that Destroy closes
// it on the main thread.
func TestPluginViewSurfaceAndDestroy(t *testing.T) {
	w, _ := testWindow(t, WindowOptions{Hidden: true})
	other, _ := testWindow(t, WindowOptions{Hidden: true})
	spy := &pluginViewSpy{}
	pv := &PluginView{w: w, view: spy, ID: 7}

	if got := (*PluginView)(nil).SurfaceWebView(&surface.Conn{Window: w}); got != nil {
		t.Errorf("nil PluginView gave %v, want nil", got)
	}
	if got := pv.SurfaceWebView(&surface.Conn{Window: other}); got != nil {
		t.Errorf("SurfaceWebView for another window gave %v, want nil", got)
	}
	if got := pv.SurfaceWebView(&surface.Conn{Window: w}); got != spy {
		t.Errorf("SurfaceWebView for its own window = %v, want the view", got)
	}

	pv.Destroy()
	if spy.closed != 1 || !spy.closedMain {
		t.Errorf("Destroy closed %d times (on main: %v), want once on the main thread", spy.closed, spy.closedMain)
	}
}
