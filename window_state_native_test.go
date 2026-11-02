//go:build !js

package mygo

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

type nativeStateTestScreen struct {
	platform.Screen
	displays []platform.Display
}

func (s nativeStateTestScreen) Displays() []platform.Display { return s.displays }

type nativeStateTestBackend struct {
	platform.Backend
	restored *platform.Rect
	accept   bool
	displays []platform.Display
}

func (b *nativeStateTestBackend) Screen() platform.Screen {
	return nativeStateTestScreen{displays: b.displays}
}
func (b *nativeStateTestBackend) RestoreWindowState(o *platform.WindowOptions, r *platform.Rect) bool {
	b.restored = r
	if !b.accept || r == nil {
		return false
	}
	o.StateBounds, o.Center = r, false
	return true
}

type nativeStateTestWindow struct {
	platform.Window
	bounds, pixels                   platform.Rect
	maximized, fullscreen, minimized bool
}

func (w *nativeStateTestWindow) Bounds() platform.Rect            { return w.bounds }
func (w *nativeStateTestWindow) WindowStateBounds() platform.Rect { return w.pixels }
func (w *nativeStateTestWindow) IsMaximized() bool                { return w.maximized }
func (w *nativeStateTestWindow) IsFullScreen() bool               { return w.fullscreen }
func (w *nativeStateTestWindow) IsMinimized() bool                { return w.minimized }

// Restoring native pixel bounds bypasses ambiguous DIP screen checks and survives a JSON round trip.
func TestNativeWindowStateRestore(t *testing.T) {
	want := Rectangle{X: 4000, Y: 100, Width: 1575, Height: 1050}
	original := WindowState{X: 2286, Y: 57, Width: 900, Height: 600, Maximized: true, WindowsBounds: &want}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var saved WindowState
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	b := &nativeStateTestBackend{accept: true, displays: []platform.Display{{Primary: true, WorkArea: platform.Rect{Width: 1920, Height: 1040}, ScaleFactor: 2}}}
	opts := &platform.WindowOptions{Width: 1180, Height: 780, Center: true, UseContentSize: true}
	restoreWindowStateFrom(saved, opts, b)
	if b.restored == nil || *b.restored != (platform.Rect{X: 4000, Y: 100, Width: 1575, Height: 1050}) {
		t.Fatalf("native bounds not restored: %+v", b.restored)
	}
	if opts.StateBounds == nil || opts.Center || opts.UseContentSize || !opts.Maximized || opts.Width != 900 || opts.Height != 600 {
		t.Fatalf("restored options = %+v", opts)
	}
}

// Legacy Windows placements use DIP coordinates only when their scale is unambiguous.
func TestNativeWindowStateFallback(t *testing.T) {
	tests := []struct {
		name     string
		bounds   *Rectangle
		displays []platform.Display
		want     platform.WindowOptions
	}{
		{
			name:     "equal scale legacy state",
			displays: []platform.Display{{Primary: true, WorkArea: platform.Rect{Width: 3840, Height: 2080}, ScaleFactor: 2}},
			want:     platform.WindowOptions{X: 100, Y: 100, Width: 2200, Height: 1200, FullScreen: true},
		},
		{
			name: "mixed scale legacy state",
			displays: []platform.Display{
				{Primary: true, WorkArea: platform.Rect{Width: 1920, Height: 1040}, ScaleFactor: 2},
				{WorkArea: platform.Rect{X: 1920, Width: 1280, Height: 720}, ScaleFactor: 1.25},
			},
			want: platform.WindowOptions{Center: true, Width: 1920, Height: 1040, FullScreen: true},
		},
		{
			name:     "disconnected display",
			bounds:   &Rectangle{X: 8000, Y: 100, Width: 2200, Height: 1200},
			displays: []platform.Display{{Primary: true, WorkArea: platform.Rect{Width: 1920, Height: 1040}, ScaleFactor: 2}},
			want:     platform.WindowOptions{Center: true, Width: 1920, Height: 1040, FullScreen: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := WindowState{X: 100, Y: 100, Width: 2200, Height: 1200, FullScreen: true}
			st.WindowsBounds = tt.bounds
			b := &nativeStateTestBackend{displays: tt.displays}
			opts := &platform.WindowOptions{}
			restoreWindowStateFrom(st, opts, b)
			if !reflect.DeepEqual(*opts, tt.want) {
				t.Fatalf("restored options = %+v, want %+v", *opts, tt.want)
			}
		})
	}
}

// Pixel placement is captured without DIP rounding and normal bounds survive maximized, fullscreen and minimized states.
func TestNativeWindowStateCapture(t *testing.T) {
	n := &nativeStateTestWindow{
		bounds: platform.Rect{X: 2286, Y: 57, Width: 900, Height: 600},
		pixels: platform.Rect{X: 4001, Y: 101, Width: 1575, Height: 1050},
	}
	saved := captureWindowState(WindowState{}, n)
	if saved.WindowsBounds == nil || *saved.WindowsBounds != (Rectangle{X: 4001, Y: 101, Width: 1575, Height: 1050}) {
		t.Fatalf("captured bounds = %+v", saved.WindowsBounds)
	}
	if got := captureWindowState(saved, n); got != saved {
		t.Fatalf("unchanged state = %+v, want %+v", got, saved)
	}
	n.bounds, n.pixels = platform.Rect{Width: 1920, Height: 1040}, platform.Rect{Width: 3840, Height: 2080}
	for _, state := range []string{"maximized", "fullscreen", "minimized"} {
		n.maximized, n.fullscreen, n.minimized = state == "maximized", state == "fullscreen", state == "minimized"
		st := captureWindowState(saved, n)
		if st.WindowsBounds != saved.WindowsBounds || st.X != saved.X || st.Y != saved.Y || st.Width != saved.Width || st.Height != saved.Height {
			t.Fatalf("%s overwrote normal geometry: %+v", state, st)
		}
		if st.Maximized != n.maximized || st.FullScreen != n.fullscreen {
			t.Fatalf("%s flags = %+v", state, st)
		}
	}
}
