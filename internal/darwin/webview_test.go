//go:build darwin

package darwin

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

// TestShownClipsPaintOrder checks that the clip views of the views the
// content shows come out in the order it painted them, merged from the two
// placement lists: the web view wins a tie, entries the frame does not show
// (hidden, closed, destroyed or of another kind) take no place, and a view
// the content painted twice keeps both places. Repeated calls return the
// same list.
func TestShownClipsPaintOrder(t *testing.T) {
	webA, webB, webC := &window{clip: 11}, &window{clip: 12}, &window{clip: 13}
	nativeA, nativeB := &nativeView{clip: 21}, &nativeView{clip: 22}
	nativeB.closed = true // destroyed: the frame does not show it

	tests := []struct {
		name         string
		webPlaced    []platform.WebViewPlacement
		nativePlaced []platform.NativeViewPlacement
		web          []*window
		native       []placedNative
		want         []id
	}{
		{
			name: "interleaved",
			webPlaced: []platform.WebViewPlacement{
				{WebView: platform.WebView(webA), Order: 1},
				{WebView: platform.WebView(webB), Order: 3},
			},
			nativePlaced: []platform.NativeViewPlacement{
				{View: platform.NativeView(nativeA), Order: 0},
				{View: platform.NativeView(nativeB), Order: 2},
			},
			web:    []*window{webA, webB},
			native: []placedNative{nativeA, nativeB},
			want:   []id{21, 11, 22, 12},
		},
		{
			name:         "tie keeps the web view first",
			webPlaced:    []platform.WebViewPlacement{{WebView: platform.WebView(webA), Order: 2}},
			nativePlaced: []platform.NativeViewPlacement{{View: platform.NativeView(nativeA), Order: 2}},
			web:          []*window{webA},
			native:       []placedNative{nativeA},
			want:         []id{11, 21},
		},
		{
			name: "hidden, closed and missing entries are skipped",
			webPlaced: []platform.WebViewPlacement{
				{Order: 0}, // no view at all
				{WebView: platform.WebView(webA), Order: 1},
				{WebView: platform.WebView(webB), Order: 2}, // closed: not in web
				{WebView: platform.WebView(webC), Order: 4}, // hidden: not in web
			},
			nativePlaced: []platform.NativeViewPlacement{
				{Order: 0}, // no view at all
				{View: platform.NativeView(nativeA), Order: 3},
				{View: platform.NativeView(nativeB), Order: 5}, // destroyed: not in native
			},
			web:    []*window{webA},
			native: []placedNative{nativeA},
			want:   []id{11, 21},
		},
		{
			name: "a view painted twice keeps both places",
			webPlaced: []platform.WebViewPlacement{
				{WebView: platform.WebView(webA), Order: 0},
				{WebView: platform.WebView(webA), Order: 2},
			},
			nativePlaced: []platform.NativeViewPlacement{{View: platform.NativeView(nativeA), Order: 1}},
			web:          []*window{webA},
			native:       []placedNative{nativeA},
			want:         []id{11, 21, 11},
		},
		{
			name: "web views alone",
			webPlaced: []platform.WebViewPlacement{
				{WebView: platform.WebView(webA), Order: 0},
				{WebView: platform.WebView(webB), Order: 1},
			},
			web:  []*window{webA, webB},
			want: []id{11, 12},
		},
		{
			name:         "native views alone",
			nativePlaced: []platform.NativeViewPlacement{{View: platform.NativeView(nativeA), Order: 0}},
			native:       []placedNative{nativeA},
			want:         []id{21},
		},
		{
			name: "nothing",
		},
		{
			name:         "nothing shown",
			webPlaced:    []platform.WebViewPlacement{{WebView: platform.WebView(webA), Order: 0}},
			nativePlaced: []platform.NativeViewPlacement{{View: platform.NativeView(nativeA), Order: 1}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for range 3 {
				got := shownClips(tt.webPlaced, tt.nativePlaced, tt.web, tt.native)
				if !slices.Equal(got, tt.want) {
					t.Fatalf("shownClips = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

// TestShownClipsMatchesStableSort checks the merge against what it
// replaced, joining the two lists and sorting them stably by paint order,
// over every placement of three web views and three native views whose
// orders each count up (the content numbers the views of a frame by one
// counter, so each list is ordered by itself), with every subset of them
// shown.
func TestShownClipsMatchesStableSort(t *testing.T) {
	webViews := []*window{{clip: 11}, {clip: 12}, {clip: 13}}
	nativeViews := []*nativeView{{clip: 21}, {clip: 22}, {clip: 23}}
	for _, webOrders := range increasingOrders(3, 6) {
		for _, nativeOrders := range increasingOrders(3, 6) {
			webPlaced := make([]platform.WebViewPlacement, 0, len(webOrders))
			for i, order := range webOrders {
				webPlaced = append(webPlaced, platform.WebViewPlacement{WebView: platform.WebView(webViews[i]), Order: order})
			}
			nativePlaced := make([]platform.NativeViewPlacement, 0, len(nativeOrders))
			for i, order := range nativeOrders {
				nativePlaced = append(nativePlaced, platform.NativeViewPlacement{View: platform.NativeView(nativeViews[i]), Order: order})
			}
			for shown := 0; shown < 1<<3; shown++ {
				for shownNative := 0; shownNative < 1<<3; shownNative++ {
					var web []*window
					for i := range 3 {
						if shown&(1<<i) != 0 {
							web = append(web, webViews[i])
						}
					}
					var native []placedNative
					for i := range 3 {
						if shownNative&(1<<i) != 0 {
							native = append(native, nativeViews[i])
						}
					}
					got := shownClips(webPlaced, nativePlaced, web, native)
					want := joinedStableSort(webPlaced, nativePlaced, web, native)
					if !slices.Equal(got, want) {
						t.Fatalf("shownClips(%v, %v, %v, %v) = %v, want %v", webOrders, nativeOrders, shown, shownNative, got, want)
					}
				}
			}
		}
	}
}

// surfacePointerAt is the pointer test a caller of the surface makes
// (pointerTaken, webview.go): the extension's own where the surface made a
// native view, the web views' alone otherwise, which is every app that does
// not use native UI.
func surfacePointerAt(s *surface, x, y float64) (*window, bool) {
	if s.hitNative != nil {
		return s.hitNative(x, y)
	}
	v := s.webViewAt(x, y)
	return v, v != nil
}

// surfaceShowsViews is the placement test a caller of the surface makes
// (applyCursor and the hit test of the surface view, surface.go): the
// content showed a view under the surface, a web view of its own or a
// native view through the extension.
func surfaceShowsViews(s *surface) bool {
	return len(s.placed) > 0 || s.hitNative != nil
}

// TestWebViewAtFallsThroughCover checks hit testing on a surface that shows
// no native view, which is every app that does not use native UI: the
// placement the content painted last does not take the pointer where what
// it painted over covers it, and the search goes on to the placements under
// it, as the web-only backend always did. A surface that shows a native
// view routes through its extension instead, which takes the cover as the
// surface's answer (nativeview_test.go).
func TestWebViewAtFallsThroughCover(t *testing.T) {
	box := platform.RectF{X: 0, Y: 0, W: 100, H: 100}
	under, over := &window{clip: 11}, &window{clip: 12}
	s := &surface{}
	if s.extOf() != nil || s.placeNative != nil || s.hitNative != nil {
		t.Fatal("a surface with no native view carries an extension or its hooks")
	}
	s.placed = []platform.WebViewPlacement{
		{WebView: platform.WebView(under), Clip: box, Order: 1},
		{
			WebView: platform.WebView(over),
			Clip:    box,
			Order:   2,
			Covers:  []platform.RectF{{X: 40, Y: 40, W: 20, H: 20}},
		},
	}
	if got := s.webViewAt(50, 50); got != under {
		t.Fatalf("webViewAt over a cover = %v, want the view under it", got)
	}
	if got := s.webViewAt(10, 10); got != over {
		t.Fatalf("webViewAt beside a cover = %v, want the view painted last", got)
	}
	if w, taken := surfacePointerAt(s, 50, 50); w != under || !taken {
		t.Fatalf("the pointer over a cover = %v, %v, want the view under it, taken", w, taken)
	}
	// The surface's hit testing sends what is over the pointer, or
	// neither, which is the same answer the web views alone give it.
	if w, taken := surfacePointerAt(s, 200, 200); w != nil || taken {
		t.Fatalf("the pointer outside = %v, %v, want neither", w, taken)
	}
	if s.pointerTaken(200, 200) {
		t.Fatal("the surface took the pointer outside its web views")
	}
	// A closed view takes nothing, and the search goes on under it.
	over.closed = true
	if got := s.webViewAt(10, 10); got != under {
		t.Fatalf("webViewAt with the top view closed = %v, want the view under it", got)
	}
	under.closed = true
	if got := s.webViewAt(10, 10); got != nil {
		t.Fatalf("webViewAt with everything closed = %v, want none", got)
	}
}

// TestSurfaceWebOnlyPlacement checks the default path of the surface: a
// surface that shows no native view has no extension and no hooks, runs the
// web-only placement and hit-testing code, and only what the content placed
// as a web view tells it that it shows anything. The extension of a native
// view, once made, tells it through the two hooks it installs
// (nativeview.go).
func TestSurfaceWebOnlyPlacement(t *testing.T) {
	s := &surface{}
	if s.extOf() != nil {
		t.Fatal("a surface with no native view carries an extension")
	}
	if surfaceShowsViews(s) {
		t.Fatal("an empty surface reported placements")
	}
	s.placed = []platform.WebViewPlacement{{WebView: platform.WebView(&window{clip: 11})}}
	if !surfaceShowsViews(s) {
		t.Fatal("a surface with a web view reported no placements")
	}
	if w, taken := surfacePointerAt(s, 0, 0); w != nil || taken {
		t.Fatalf("the pointer outside = %v, %v, want neither", w, taken)
	}
	// The extension tells the surface that it shows a view even where the
	// content placed no web view for this frame, as it does between
	// frames, and which view takes the pointer.
	p := s.placement()
	if s.placeNative == nil || s.hitNative == nil {
		t.Fatal("the extension installed no hooks")
	}
	if !surfaceShowsViews(s) {
		t.Fatal("a surface with a native-view extension reported no placements")
	}
	box := platform.RectF{X: 0, Y: 0, W: 100, H: 100}
	p.placed = []platform.NativeViewPlacement{{
		View:  platform.NativeView(&nativeView{clip: 21}),
		Clip:  box,
		Order: 0,
	}}
	if w, taken := surfacePointerAt(s, 50, 50); w != nil || !taken {
		t.Fatalf("the pointer over the native view = %v, %v, want no web view, taken", w, taken)
	}
	if !s.pointerTaken(50, 50) {
		t.Fatal("the surface did not leave the pointer to the native view")
	}
	s.placed = nil
	if !surfaceShowsViews(s) {
		t.Fatal("a surface showing a native view reported no placements")
	}
	p.placed = nil
	if w, taken := surfacePointerAt(s, 50, 50); w != nil || taken {
		t.Fatalf("the pointer with no native view placed = %v, %v, want neither", w, taken)
	}
}

// increasingOrders returns every list of n strictly increasing orders below
// max.
func increasingOrders(n, max int) [][]int {
	var out [][]int
	var walk func(cur []int)
	walk = func(cur []int) {
		if len(cur) == n {
			out = append(out, slices.Clone(cur))
			return
		}
		start := 0
		if len(cur) > 0 {
			start = cur[len(cur)-1] + 1
		}
		for v := start; v < max; v++ {
			walk(append(slices.Clone(cur), v))
		}
	}
	walk(nil)
	return out
}

// joinedStableSort is what shownClips replaced: the clip views of the
// shown views, the two lists joined, sorted stably by paint order, so that
// a web view comes before a native view of the same order.
func joinedStableSort(webPlaced []platform.WebViewPlacement, nativePlaced []platform.NativeViewPlacement, web []*window, native []placedNative) []id {
	type entry struct {
		order int
		clip  id
	}
	joined := make([]entry, 0, len(webPlaced)+len(nativePlaced))
	for i := range webPlaced {
		if v, ok := webPlaced[i].WebView.(*window); ok && slices.Contains(web, v) {
			joined = append(joined, entry{webPlaced[i].Order, v.clip})
		}
	}
	for i := range nativePlaced {
		if v, ok := nativePlaced[i].View.(placedNative); ok && slices.Contains(native, v) {
			joined = append(joined, entry{nativePlaced[i].Order, v.clipID()})
		}
	}
	slices.SortStableFunc(joined, func(a, b entry) int { return a.order - b.order })
	ordered := make([]id, 0, len(joined))
	for _, e := range joined {
		ordered = append(ordered, e.clip)
	}
	return ordered
}
