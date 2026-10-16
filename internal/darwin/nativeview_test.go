//go:build darwin

package darwin

import (
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

// TestNativeViewClipFrames checks where a native view goes: the clip view
// in the window's content view, which is not flipped, and the app's view
// at its offset in the clip view, which is. The app's view keeps its size
// as the content scrolls the clip over its frame, which is what keeps a
// text view from re-laying-out its contents.
func TestNativeViewClipFrames(t *testing.T) {
	const height = 100
	tests := []struct {
		name  string
		clip  platform.RectF
		frame platform.RectF
		want  NSRect
		child NSRect
	}{
		{
			name:  "placed",
			clip:  platform.RectF{X: 10, Y: 20, W: 30, H: 40},
			frame: platform.RectF{X: 10, Y: 20, W: 30, H: 40},
			want:  NSRect{Origin: NSPoint{X: 10, Y: 40}, Size: NSSize{Width: 30, Height: 40}},
			child: NSRect{Origin: NSPoint{X: 0, Y: 0}, Size: NSSize{Width: 30, Height: 40}},
		},
		{
			name:  "scrolled over a taller frame",
			clip:  platform.RectF{X: 0, Y: 50, W: 100, H: 20},
			frame: platform.RectF{X: 0, Y: 40, W: 100, H: 60},
			want:  NSRect{Origin: NSPoint{X: 0, Y: 30}, Size: NSSize{Width: 100, Height: 20}},
			child: NSRect{Origin: NSPoint{X: 0, Y: -10}, Size: NSSize{Width: 100, Height: 60}},
		},
		{
			name:  "narrower than its frame",
			clip:  platform.RectF{X: 5, Y: 5, W: 20, H: 20},
			frame: platform.RectF{X: 0, Y: 0, W: 40, H: 40},
			want:  NSRect{Origin: NSPoint{X: 5, Y: 75}, Size: NSSize{Width: 20, Height: 20}},
			child: NSRect{Origin: NSPoint{X: -5, Y: -5}, Size: NSSize{Width: 40, Height: 40}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clipFrame(tt.clip, height); got != tt.want {
				t.Errorf("clipFrame(%+v) = %+v, want %+v", tt.clip, got, tt.want)
			}
			if got := childFrame(tt.frame, tt.clip); got != tt.child {
				t.Errorf("childFrame(%+v, %+v) = %+v, want %+v", tt.frame, tt.clip, got, tt.child)
			}
			// The view is not resized as the clip moves over its frame.
			scrolled := tt.clip
			scrolled.Y -= 10
			if got := childFrame(tt.frame, scrolled).Size; got != tt.child.Size {
				t.Errorf("scrolling resized the view: %+v, want %+v", got, tt.child.Size)
			}
		})
	}
}

// TestNativeViewMixedOrder checks that the clip views of the web views and
// the native views of a frame are stacked in the order the content painted
// them, from one counter across both kinds, whatever the order of the two
// lists.
func TestNativeViewMixedOrder(t *testing.T) {
	webA, webB := &window{clip: 11}, &window{clip: 12}
	nativeA, nativeB := &nativeView{clip: 21}, &nativeView{clip: 22}
	placedWeb := []platform.WebViewPlacement{
		{WebView: platform.WebView(webA), Order: 1},
		{WebView: platform.WebView(webB), Order: 3},
	}
	nativePlaced := []platform.NativeViewPlacement{
		{View: platform.NativeView(nativeA), Order: 0},
		{View: platform.NativeView(nativeB), Order: 2},
	}
	got := shownClips(placedWeb, nativePlaced, []*window{webA, webB}, []placedNative{nativeA, nativeB})
	want := []id{21, 11, 22, 12}
	if len(got) != len(want) {
		t.Fatalf("shownClips = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("shownClips = %v, want %v", got, want)
		}
	}
	// The views not shown are left out.
	got = shownClips(placedWeb, nativePlaced, []*window{webA}, []placedNative{nativeB})
	want = []id{11, 22}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("shownClips with hidden views = %v, want %v", got, want)
	}
	// Web views alone keep the order the content painted them in.
	got = shownClips(placedWeb, nil, []*window{webA, webB}, nil)
	want = []id{11, 12}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("shownClips of web views = %v, want %v", got, want)
	}
	if got := shownClips(nil, nil, nil, nil); len(got) != 0 {
		t.Fatalf("shownClips of nothing = %v, want none", got)
	}
}

// TestNativeViewTopView checks what takes the pointer: the view the content
// painted last among the web views and the native views whose placement
// takes the pointer there, so that a press over what the content painted
// over a native view is the content's, and a press over the native view is
// the view's. It is the extension's answer, which the surface routes
// through once it shows a native view (surface.go).
func TestNativeViewTopView(t *testing.T) {
	box := platform.RectF{X: 0, Y: 0, W: 100, H: 100}
	web := &window{clip: 11}
	native := &nativeView{clip: 21}
	s := &surface{}
	s.placed = []platform.WebViewPlacement{{WebView: platform.WebView(web), Clip: box, Order: 1}}
	p := s.placement()
	p.placed = []platform.NativeViewPlacement{{View: platform.NativeView(native), Clip: box, Order: 2}}

	if w, n := p.top(50, 50); w != nil || n != native {
		t.Fatalf("top = %v, %v, want the native view", w, n)
	}
	// The surface takes the pointer where the extension says a view takes
	// it, which is what its cursor and its hit testing ask: the hooks the
	// extension installed (pointerTaken, webview.go).
	if s.hitNative == nil || s.placeNative == nil {
		t.Fatal("the extension installed no hooks")
	}
	if w, taken := surfacePointerAt(s, 50, 50); w != nil || !taken {
		t.Fatalf("the pointer over the native view = %v, %v, want no web view, taken", w, taken)
	}
	if !s.pointerTaken(50, 50) {
		t.Fatal("the surface did not leave the pointer to the native view")
	}
	// A popup painted over the native view takes the pointer instead.
	popup := platform.RectF{X: 40, Y: 40, W: 20, H: 20}
	p.placed[0].Covers = []platform.RectF{popup}
	if w, n := p.top(50, 50); w != nil || n != nil {
		t.Fatalf("top over a cover = %v, %v, want neither", w, n)
	}
	if w, taken := surfacePointerAt(s, 50, 50); w != nil || taken {
		t.Fatalf("the pointer over a cover = %v, %v, want no view, not taken", w, taken)
	}
	if s.pointerTaken(50, 50) {
		t.Fatal("the surface took the pointer over a cover")
	}
	if w, n := p.top(10, 10); w != nil || n != native {
		t.Fatalf("top beside a cover = %v, %v, want the native view", w, n)
	}
	// Painted after the native view, the web view takes the pointer.
	s.placed[0].Order = 3
	if w, n := p.top(10, 10); w != web || n != nil {
		t.Fatalf("top with the web view last = %v, %v, want the web view", w, n)
	}
	if w, taken := surfacePointerAt(s, 10, 10); w != web || !taken {
		t.Fatalf("the pointer with the web view last = %v, %v, want the web view, taken", w, taken)
	}
	// A view that is gone takes nothing.
	native.closed = true
	if w, n := p.top(10, 10); w != web || n != nil {
		t.Fatalf("top with a destroyed native view = %v, %v, want the web view", w, n)
	}
	web.closed = true
	if w, n := p.top(10, 10); w != nil || n != nil {
		t.Fatalf("top with everything destroyed = %v, %v, want neither", w, n)
	}
	// Outside every placement the content takes the pointer.
	native.closed, web.closed = false, false
	if w, n := p.top(200, 200); w != nil || n != nil {
		t.Fatalf("top outside = %v, %v, want neither", w, n)
	}
	// The extension is what tells the surface that it shows a view at all,
	// which it holds from the hooks it installed, and it takes the pointer
	// through them alone: with no native view placed it takes nothing.
	if s.extOf() != p || !surfaceShowsViews(s) {
		t.Fatalf("extOf = %p, showsViews = %v, want the extension and true", s.extOf(), surfaceShowsViews(s))
	}
	p.placed = nil
	if w, taken := surfacePointerAt(s, 50, 50); w != web || !taken {
		t.Fatalf("the pointer with no native view placed = %v, %v, want the web view, taken", w, taken)
	}
}

// fakeNativeAdapter is a native view of an app: what the core keeps of what
// a factory made, and counts the Destroy calls MyGo makes of it.
type fakeNativeAdapter struct {
	handle   uintptr
	focuses  []bool
	destroys int
}

func (a *fakeNativeAdapter) NativeHandle() uintptr { return a.handle }
func (a *fakeNativeAdapter) Focus(back bool)       { a.focuses = append(a.focuses, back) }
func (a *fakeNativeAdapter) Destroy()              { a.destroys++ }

// TestNativeViewDestroyOnce checks that destroying a native view destroys
// the app's own view exactly once, takes it out of the extension that holds
// it and the content that shows it, and that destroying it again, or after
// the window closed, does nothing: the view is what the extension holds by
// hand, and a second teardown would release it twice. It also checks that
// closing the window destroys what it still holds, exactly once.
func TestNativeViewDestroyOnce(t *testing.T) {
	load()
	host := &window{}
	s := &surface{}
	gone, kept := &fakeNativeAdapter{}, &fakeNativeAdapter{}
	v := &nativeView{host: host, surface: s, adapter: gone}
	other := &nativeView{host: host, surface: s, adapter: kept}
	p := s.placement()
	p.views = []placedNative{v, other}
	p.placed = []platform.NativeViewPlacement{
		{View: platform.NativeView(v)},
		{View: platform.NativeView(other)},
	}

	v.Destroy()
	v.Destroy() // reentrant, or the app destroying it twice

	if gone.destroys != 1 {
		t.Fatalf("the app's Destroy ran %d times, want once", gone.destroys)
	}
	if kept.destroys != 0 {
		t.Fatalf("the other view's Destroy ran %d times, want none", kept.destroys)
	}
	if !v.closed || v.clip != 0 || v.handle != 0 {
		t.Fatalf("destroyed view = closed %v, clip %d, handle %#x, want closed with none", v.closed, v.clip, v.handle)
	}
	if len(p.views) != 1 || p.views[0] != other {
		t.Fatalf("the extension holds %d native views, want only the other one", len(p.views))
	}
	if len(p.placed) != 1 || p.placed[0].View != platform.NativeView(other) {
		t.Fatalf("the content holds %d placements, want only the other one", len(p.placed))
	}
	// The window's teardown destroys what is left, through the hook the
	// extension installed (NewNativeView, window.go).
	p.closeViews()
	if kept.destroys != 1 {
		t.Fatalf("caught at the window's close, the other view's Destroy ran %d times, want once", kept.destroys)
	}
	if len(p.views) != 0 {
		t.Fatalf("the extension holds %d native views after the close, want none", len(p.views))
	}
}

// otherSurface is a surface of another backend, for the opt-in provider
// check: it satisfies platform.Surface through the embedded interface, and
// implements no native-view hosting of its own.
type otherSurface struct{ platform.Surface }

// TestNativeViewSurfaceIsOptIn checks the backend's cross-agent contract:
// NativeViewSurface hands out the native-view provider for this backend's
// own surface, reports none for any other, and the surface itself does not
// implement platform.NativeViewSurface. Were it to, every app of native UI
// would carry the native-view hosting code through the interface
// assertions of the core and the ui, whether it makes a native view or not
// (AGENTS.md: apps pay only for what they use).
func TestNativeViewSurfaceIsOptIn(t *testing.T) {
	s := &surface{}
	ns, ok := NativeViewSurface(s)
	if !ok || ns == nil {
		t.Fatalf("NativeViewSurface(this surface) = %v, %v, want its provider", ns, ok)
	}
	p, ok := ns.(*nativeViewSurface)
	if !ok || p.s != s {
		t.Fatalf("the provider = %#v, want the wrapper over this surface", ns)
	}
	if _, ok := NativeViewSurface(otherSurface{}); ok {
		t.Fatal("NativeViewSurface(another backend's surface) reported a provider")
	}
	if ns, ok := NativeViewSurface(nil); ok || ns != nil {
		t.Fatalf("NativeViewSurface(nil) = %v, %v, want none", ns, ok)
	}
	// The whole point of the provider: the surface is not the interface,
	// so a runtime assertion on it finds nothing to root.
	var surface platform.Surface = s
	if ns, ok := surface.(platform.NativeViewSurface); ok {
		t.Fatalf("the backend's surface implements platform.NativeViewSurface (%T): the native-view code is always linked again", ns)
	}
}

// TestResponderBelongs checks the decision firstResponderIn makes of what
// it learns of the first responder: a control in the view holds the
// keyboard through its ancestry, and a text field the window edits in the
// field editor holds it through the field editor's delegate. Ancestry alone
// is not proof for the field editor, which the window shares.
func TestResponderBelongs(t *testing.T) {
	tests := []struct {
		name      string
		inView    bool
		fieldEdit bool
		want      bool
	}{
		{"the view, or a control in it", true, false, true},
		{"the field editor of a control in the view", false, true, true},
		{"neither", false, false, false},
		{"both, as while the field editor is in the field", true, true, true},
	}
	for _, tt := range tests {
		if got := responderBelongs(tt.inView, tt.fieldEdit); got != tt.want {
			t.Errorf("%s: responderBelongs(%v, %v) = %v, want %v", tt.name, tt.inView, tt.fieldEdit, got, tt.want)
		}
	}
}
