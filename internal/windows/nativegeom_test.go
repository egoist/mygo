//go:build windows || nativegeom

package windows

import (
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

// The geometry of native hosting is pure Go: the pixel rectangles of a
// placement, and where the app's control goes in its clip window. There is no
// Windows runtime here to show a window, so these are the parts of
// nativeview.go that are tested at all.

func TestPXRectRounding(t *testing.T) {
	// A placement is in DIPs and the surface in device pixels of its DPI, as
	// for the web views (webview_embed.go, placeEmbedded).
	for _, tc := range []struct {
		dpi  int
		in   platform.RectF
		want pixelRect
	}{
		{96, platform.RectF{X: 0, Y: 0, W: 100, H: 50}, pixelRect{0, 0, 100, 50}},
		{96, platform.RectF{X: 1.5, Y: 2.5, W: 10, H: 10}, pixelRect{2, 3, 12, 13}}, // halves round away from zero, as placeEmbedded does
		{144, platform.RectF{X: 0, Y: 0, W: 100, H: 50}, pixelRect{0, 0, 150, 75}},
		{144, platform.RectF{X: 0.5, Y: 0.5, W: 1, H: 1}, pixelRect{1, 1, 2, 2}},
		{192, platform.RectF{X: 10, Y: 20, W: 30, H: 40}, pixelRect{20, 40, 80, 120}},
	} {
		if got := pxRect(tc.in, tc.dpi); got != tc.want {
			t.Errorf("pxRect(%+v, %d) = %+v, want %+v", tc.in, tc.dpi, got, tc.want)
		}
		if got, want := pxRect(tc.in, tc.dpi), pxRectSameWay(tc.in, tc.dpi); got != want {
			t.Errorf("pxRect(%+v, %d) = %+v does not line up with a web view's placement %+v", tc.in, tc.dpi, got, want)
		}
	}
}

// pxRectSameWay is the rounding placeEmbedded uses, kept apart on purpose: a
// native view and a web view of one frame must land on the same pixels, or a
// clip drawn over them shows a seam.
func pxRectSameWay(r platform.RectF, dpi int) pixelRect {
	px := func(v float64) int32 { return int32(math.Round(v * float64(dpi) / 96)) }
	return pixelRect{px(r.X), px(r.Y), px(r.X + r.W), px(r.Y + r.H)}
}

func TestRectEmpty(t *testing.T) {
	for _, tc := range []struct {
		r    pixelRect
		want bool
	}{
		{pixelRect{0, 0, 10, 10}, false},
		{pixelRect{0, 0, 0, 10}, true},
		{pixelRect{0, 0, 10, 0}, true},
		// A clip that went past a frame, or a collapsed one: nothing shows.
		{pixelRect{10, 10, 0, 10}, true},
		{pixelRect{10, 10, 10, 0}, true},
	} {
		if got := tc.r.empty(); got != tc.want {
			t.Errorf("%+v.empty() = %v, want %v", tc.r, got, tc.want)
		}
	}
}

func TestChildAtCoversTheClip(t *testing.T) {
	// Clip is inside Frame (platform.NativeViewPlacement), so the app's
	// control covers the whole clip window wherever the clip sits in the
	// frame: the clip window itself draws nothing.
	for _, tc := range []struct{ clip, frame pixelRect }{
		{pixelRect{0, 0, 100, 100}, pixelRect{0, 0, 100, 100}},
		{pixelRect{0, 30, 100, 100}, pixelRect{0, 0, 100, 150}}, // scrolled: 20 pixels of the frame above the clip
		{pixelRect{0, 0, 100, 30}, pixelRect{0, 0, 100, 150}},   // scrolled: the rest below
		{pixelRect{20, 20, 80, 80}, pixelRect{0, 0, 100, 100}},  // padded frame
		{pixelRect{5, 5, 15, 25}, pixelRect{-10, -10, 30, 40}},  // the frame starts outside its parent
	} {
		child := childAt(tc.clip, tc.frame)
		if child.Left > 0 || child.Top > 0 ||
			child.Right-child.Left < tc.clip.Right-tc.clip.Left ||
			child.Bottom-child.Top < tc.clip.Bottom-tc.clip.Top {
			t.Errorf("childAt(%+v, %+v) = %+v does not cover the clip window", tc.clip, tc.frame, child)
		}
		if !sameSize(child, tc.frame) {
			t.Errorf("childAt(%+v, %+v) = %+v is not the frame's size %+v", tc.clip, tc.frame, child, tc.frame)
		}
	}
}

func TestChildAtScrollKeepsTheSize(t *testing.T) {
	// A frame of 100x200 in a clip that the content scrolls: the control is
	// moved, never resized, so what it draws keeps its own layout (a text
	// view its lines, its caret and its selection).
	frame := pixelRect{0, 0, 100, 200}
	first := childAt(pixelRect{0, 0, 100, 100}, frame)
	for _, top := range []int32{10, 99, 100, 120} {
		next := childAt(pixelRect{0, top, 100, top + 100}, frame)
		if next == first {
			continue
		}
		if !sameSize(next, first) {
			t.Errorf("the clip at %d resized the control: %+v then %+v", top, first, next)
		}
		if next.Top != first.Top-top {
			t.Errorf("the clip at %d put the control at %d, want %d", top, next.Top, first.Top-top)
		}
	}
	if sameSize(pixelRect{0, 0, 100, 200}, pixelRect{0, 0, 100, 201}) {
		t.Error("sameSize misses a height of one pixel")
	}
}

func TestSubtractRectSplits(t *testing.T) {
	// What the app's control takes is the clip without the popup over it: a
	// hole in a rectangle leaves up to four rectangles around it.
	for _, tc := range []struct {
		r, minus pixelRect
		want     int
	}{
		{pixelRect{0, 0, 100, 100}, pixelRect{20, 20, 60, 60}, 4},
		{pixelRect{0, 0, 100, 100}, pixelRect{-10, -10, 110, 50}, 1},  // the hole takes the top
		{pixelRect{0, 0, 100, 100}, pixelRect{-10, -10, 110, 110}, 0}, // the hole takes all
		{pixelRect{0, 0, 100, 100}, pixelRect{200, 200, 300, 300}, 1}, // nothing taken
		{pixelRect{0, 0, 100, 100}, pixelRect{0, 0, 20, 20}, 2},       // a corner
	} {
		got := subtractRect(tc.r, tc.minus)
		if len(got) != tc.want {
			t.Errorf("subtractRect(%+v, %+v) = %+v, want %d rectangles", tc.r, tc.minus, got, tc.want)
			continue
		}
		if got := area(got); got != area([]pixelRect{tc.r})-area([]pixelRect{intersectRect(tc.r, tc.minus)}) {
			t.Errorf("subtractRect(%+v, %+v) = %+v, area %d", tc.r, tc.minus, got, got)
		}
		for _, x := range got {
			if x.Left < tc.r.Left || x.Top < tc.r.Top || x.Right > tc.r.Right || x.Bottom > tc.r.Bottom {
				t.Errorf("subtractRect(%+v, %+v) left a rectangle outside it: %+v", tc.r, tc.minus, x)
			}
		}
	}
}

func TestCoverStepsTileTheClip(t *testing.T) {
	// Every pixel of a clip is either the app's control's or the content's:
	// the two together are the clip, and never overlap.
	placements := []platform.NativeViewPlacement{
		{Order: 0, Clip: platform.RectF{W: 100, H: 100}, Covers: []platform.RectF{{X: 20, Y: 20, W: 40, H: 40}}},
		{Order: 1, Clip: platform.RectF{X: 50, Y: 50, W: 100, H: 100}, Covers: []platform.RectF{{X: 110, Y: 110, W: 30, H: 30}}},
		{Order: 2, Clip: platform.RectF{X: 10, Y: 10, W: 0, H: 10}}, // nothing shows
	}
	steps := coverSteps(placements, 96)
	if len(steps) != 2 {
		t.Fatalf("coverSteps gave %d steps, want 2", len(steps))
	}
	for i, tc := range []struct {
		clip   pixelRect
		covers []platform.RectF
	}{
		{pixelRect{0, 0, 100, 100}, placements[0].Covers},
		{pixelRect{50, 50, 150, 150}, placements[1].Covers},
	} {
		got := area(steps[i].Control)
		for _, c := range tc.covers {
			got += area([]pixelRect{pxRect(c, 96)})
		}
		if got != area([]pixelRect{tc.clip}) {
			t.Errorf("step %d covers %d pixels of a clip of %d: %+v", i, got, area([]pixelRect{tc.clip}), steps[i])
		}
		for _, c := range steps[i].Control {
			for _, cv := range steps[i].Covers {
				if !intersectRect(c, cv).empty() {
					t.Errorf("step %d has the control %+v over its own cover %+v", i, c, cv)
				}
			}
		}
	}
}

func TestCoverStepsOrderDecides(t *testing.T) {
	// A later view is over an earlier one: at its own place, it takes the
	// pointer from what an earlier view took, and the boxes the content
	// painted over it are the content's.
	steps := coverSteps([]platform.NativeViewPlacement{
		{Order: 0, Clip: platform.RectF{W: 100, H: 100}, Covers: []platform.RectF{{X: 20, Y: 20, W: 40, H: 40}}},
		{Order: 1, Clip: platform.RectF{X: 50, Y: 50, W: 100, H: 100}, Covers: []platform.RectF{{X: 110, Y: 110, W: 30, H: 30}}},
	}, 96)
	// takeAt is the rule the cover window's region is built from: each step in
	// turn, its control first, its covers after, and nothing outside a clip.
	takeAt := func(x, y int32) (string, int) {
		kind, at := "", -1
		for i, st := range steps {
			if containsAny(st.Control, x, y) {
				kind, at = "control", i
				continue
			}
			if containsAny(st.Covers, x, y) {
				kind, at = "content", i
			}
		}
		return kind, at
	}
	for _, tc := range []struct {
		x, y int32
		kind string
		at   int
	}{
		{10, 10, "control", 0}, // the first view's control
		{30, 30, "content", 0}, // its popup
		{55, 55, "control", 1}, // the second view is over the popup there
		{70, 70, "control", 1},
		{130, 130, "content", 1}, // the second view's popup
		{95, 95, "control", 1},
		{200, 200, "", -1}, // outside every clip
	} {
		kind, at := takeAt(tc.x, tc.y)
		if kind != tc.kind || at != tc.at {
			t.Errorf("at (%d, %d) %q of view %d, want %q of view %d", tc.x, tc.y, kind, at, tc.kind, tc.at)
		}
	}
}

func TestStackNativePlanLastPaintedOnTop(t *testing.T) {
	// What the plan is for: a clip window the content painted later must show
	// over an earlier one where they overlap. Windows places a window in
	// hWndInsertAfter *below* the window that handle names, so a plan that
	// walks the paint order upwards stacks it upside down.
	placements := []platform.NativeViewPlacement{
		{Order: 3, Clip: platform.RectF{W: 100, H: 100}},               // last painted
		{Order: 1, Clip: platform.RectF{X: 50, Y: 50, W: 100, H: 100}}, // first painted
		{Order: 2, Clip: platform.RectF{X: 20, Y: 20, W: 100, H: 100}},
	}
	orders := make([]int, len(placements))
	for i, p := range placements {
		orders[i] = p.Order
	}
	plan := stackNativePlan(orders)
	if len(plan) != len(placements) {
		t.Fatalf("stackNativePlan(%v) gave %d calls, want %d", orders, len(plan), len(placements))
	}
	z := replayStack(plan)
	if len(z) != len(placements) {
		t.Fatalf("replaying %+v stacked %d windows, want %d", plan, len(z), len(placements))
	}
	seen := map[int]int{}
	for _, view := range z {
		seen[view]++
	}
	for view := range placements {
		if seen[view] != 1 {
			t.Fatalf("replaying %+v stacks the view %d %d times: %v", plan, view, seen[view], z)
		}
	}
	// z is the z-order, top first: whatever overlaps, the later painted window
	// is the higher one.
	at := func(i int) int { return slices.Index(z, i) }
	for i := range placements {
		for j := range placements {
			if i == j || placements[i].Order == placements[j].Order {
				continue
			}
			overlap := !intersectRect(pxRect(placements[i].Clip, 96), pxRect(placements[j].Clip, 96)).empty()
			if !overlap {
				continue
			}
			if placements[i].Order > placements[j].Order && at(i) > at(j) {
				t.Errorf("order %d is below order %d where their clips overlap: plan %+v, z-order %v", placements[i].Order, placements[j].Order, plan, z)
			}
		}
	}
	if got, want := at(0), 0; got != want {
		t.Errorf("the last painted view is at the depth %d, want the top (%d): plan %+v, z-order %v", got, want, plan, z)
	}
	if got, want := at(1), 2; got != want {
		t.Errorf("the first painted view is at the depth %d, want %d: plan %+v, z-order %v", got, want, plan, z)
	}
}

func TestStackNativePlanKeepsTiesAndSingleViews(t *testing.T) {
	// Equal orders keep the order the views were given in (the content's paint
	// order, for whatever reason it repeats one), and a plan stacks each
	// window once.
	plan := stackNativePlan([]int{1, 1, 0})
	if want := []stackPlan{{View: 0, Below: -1}, {View: 1, Below: 0}, {View: 2, Below: 1}}; !slices.Equal(plan, want) {
		t.Errorf("stackNativePlan([1 1 0]) = %+v, want %+v", plan, want)
	}
	if got := replayStack(plan); !slices.Equal(got, []int{0, 1, 2}) {
		t.Errorf("stackNativePlan([1 1 0]) stacks %v, want [0 1 2]", got)
	}
	if want := []stackPlan{{View: 0, Below: -1}}; !slices.Equal(stackNativePlan([]int{9}), want) {
		t.Errorf("stackNativePlan([9]) = %+v, want %+v", stackNativePlan([]int{9}), want)
	}
	if got := stackNativePlan(nil); len(got) != 0 {
		t.Errorf("stackNativePlan(nil) = %+v, want no call", got)
	}
}

func TestStackNativePlanNoticesAReorder(t *testing.T) {
	// Two views of one frame, the same geometry, painted either way. The plan
	// is what the surface remembers its stack by, so the same orders must give
	// the same plan (which is what leaves a window alone when nothing changed)
	// and a swap of the orders a different stack.
	first := stackNativePlan([]int{0, 1})
	if again := stackNativePlan([]int{0, 1}); !slices.Equal(first, again) {
		t.Errorf("the same paint order gave %+v and %+v", first, again)
	}
	if got, want := replayStack(first), replayStack(stackNativePlan([]int{1, 0})); slices.Equal(got, want) {
		t.Errorf("swapping which view was painted first left the stack %v", got)
	}
}

func TestClipHolesOnlyLaterWebViews(t *testing.T) {
	// A web view the content painted after a native view shows over it: the
	// clip window leaves its boxes out. A web view painted before it, or
	// elsewhere, or with no clip at all, does not.
	clip := pixelRect{0, 0, 100, 100}
	webs := []platform.WebViewPlacement{
		{Order: 1, Clip: platform.RectF{X: 10, Y: 10, W: 20, H: 20}},   // under the native view
		{Order: 3, Clip: platform.RectF{X: 20, Y: 20, W: 40, H: 40}},   // over it
		{Order: 3, Clip: platform.RectF{X: 200, Y: 200, W: 10, H: 10}}, // over it, elsewhere
		{Order: 3, Clip: platform.RectF{X: 10, Y: 10, W: 0, H: 20}},    // over it, hidden
	}
	holes := clipHoles(clip, 2, webs, 96)
	if want := []pixelRect{{20, 20, 60, 60}}; !slices.Equal(holes, want) {
		t.Fatalf("clipHoles for the web view painted after = %+v, want %+v", holes, want)
	}
	if holes := clipHoles(clip, 9, webs, 96); len(holes) != 0 {
		t.Errorf("a native view painted after every web view left %+v out, want nothing", holes)
	}
	// The same order is not after: the content painted them together, and the
	// app's control keeps its pixels.
	if holes := clipHoles(clip, 3, webs, 96); len(holes) != 0 {
		t.Errorf("a web view of the same order cut the native view: %+v", holes)
	}
	if holes := clipHoles(clip, 2, nil, 96); len(holes) != 0 {
		t.Errorf("no web view at all cut the native view: %+v", holes)
	}
}

func TestClipHolesBothOrders(t *testing.T) {
	// The same two boxes, once with the web view painted after the native view
	// and once before it: only the later one cuts.
	clip := pixelRect{0, 0, 50, 50}
	web := platform.RectF{X: 10, Y: 10, W: 30, H: 30}
	after := clipHoles(clip, 4, []platform.WebViewPlacement{{Order: 5, Clip: web}}, 96)
	if want := []pixelRect{{10, 10, 40, 40}}; !slices.Equal(after, want) {
		t.Errorf("the web view painted after the native view cut %+v, want %+v", after, want)
	}
	if before := clipHoles(clip, 5, []platform.WebViewPlacement{{Order: 4, Clip: web}}, 96); len(before) != 0 {
		t.Errorf("the web view painted before the native view cut %+v, want nothing", before)
	}
}

func TestClipHolesAreInTheClipWindow(t *testing.T) {
	// A clip window is as big as its clip and sits at the clip's own origin:
	// the region of the cut is in the window's pixels, never the surface's.
	clip := pixelRect{100, 50, 200, 150}
	webs := []platform.WebViewPlacement{{Order: 1, Clip: platform.RectF{X: 120, Y: 80, W: 40, H: 40}}}
	holes := clipHoles(clip, 0, webs, 96)
	if want := []pixelRect{{20, 30, 60, 70}}; !slices.Equal(holes, want) {
		t.Fatalf("clipHoles at an offset clip = %+v, want %+v in the clip window", holes, want)
	}
	// A web view whose clip hangs over the edge of the native clip is cut to
	// the part of it the native clip covers.
	webs = []platform.WebViewPlacement{{Order: 1, Clip: platform.RectF{X: 180, Y: 20, W: 100, H: 100}}}
	holes = clipHoles(clip, 0, webs, 96)
	if want := []pixelRect{{80, 0, 100, 70}}; !slices.Equal(holes, want) {
		t.Errorf("a web view over the edge cut %+v, want %+v", holes, want)
	}
}

func TestClipRegionTilesTheClip(t *testing.T) {
	// The region of a cut clip window plus the boxes left out are the whole
	// clip: the app's control shows in the region and the page in the boxes,
	// and neither overlaps the other.
	clip := pixelRect{0, 0, 100, 100}
	webs := []platform.WebViewPlacement{
		{Order: 7, Clip: platform.RectF{X: 60, Y: 0, W: 100, H: 40}},
		{Order: 6, Clip: platform.RectF{X: -10, Y: 70, W: 30, H: 60}},
	}
	holes := clipHoles(clip, 5, webs, 96)
	if len(holes) != 2 {
		t.Fatalf("clipHoles = %+v, want the two boxes of the two later web views", holes)
	}
	region := clipRegion(clip, holes)
	if got, want := area(region)+area(holes), area([]pixelRect{clip}); got != want {
		t.Errorf("the region %+v and the boxes %+v cover %d pixels of a clip of %d", region, holes, got, want)
	}
	for _, r := range region {
		if r.Left < 0 || r.Top < 0 || r.Right > clip.Right || r.Bottom > clip.Bottom {
			t.Errorf("the region %+v leaves the clip window %+v", region, clip)
		}
		for _, h := range holes {
			if !intersectRect(r, h).empty() {
				t.Errorf("the app's control %+v and the page %+v overlap", r, h)
			}
		}
	}
	// A whole clip without a later web view is one region, the whole window.
	if region := clipRegion(clip, nil); len(region) != 1 || region[0] != clip {
		t.Errorf("clipRegion(clip, no holes) = %+v, want the whole clip %+v", region, clip)
	}
	// A clip a later web view takes whole leaves the app's control nothing.
	taken := pixelRect{0, 0, 40, 40}
	if region := clipRegion(taken, clipHoles(taken, 0, []platform.WebViewPlacement{{Order: 1, Clip: platform.RectF{W: 100, H: 100}}}, 96)); len(region) != 0 {
		t.Errorf("a clip a later web view covers whole kept %+v, want nothing", region)
	}
}

func TestClipRegionReorderSameGeometry(t *testing.T) {
	// Two native views over the same pixels and one web view over them too:
	// which clip window is cut follows the paint order alone, so the same
	// geometry stacks both ways.
	clip := pixelRect{0, 0, 100, 100}
	web := []platform.WebViewPlacement{{Order: 1, Clip: platform.RectF{W: 100, H: 100}}}
	// The web view was painted between the two: over the first, under the
	// second.
	if region := clipRegion(clip, clipHoles(clip, 0, web, 96)); len(region) != 0 {
		t.Errorf("the native view painted before the web view kept %+v, want nothing", region)
	}
	if region := clipRegion(clip, clipHoles(clip, 2, web, 96)); !slices.Equal(region, []pixelRect{clip}) {
		t.Errorf("the native view painted after the web view kept %+v, want the whole clip", region)
	}
	// Swapping which one was painted first swaps which one is cut.
	if region := clipRegion(clip, clipHoles(clip, 2, web, 96)); !slices.Equal(region, []pixelRect{clip}) {
		t.Errorf("swapped: the native view painted last kept %+v, want the whole clip", region)
	}
	if region := clipRegion(clip, clipHoles(clip, 0, web, 96)); len(region) != 0 {
		t.Errorf("swapped: the native view painted first kept %+v, want nothing", region)
	}
}

func TestClipCoversKeepNativePixels(t *testing.T) {
	// A popup the content paints over a native view (NativeViewPlacement
	// .Covers) is not a web view: the clip window keeps those pixels, so what
	// the app's control drew under a translucent popup stays, and only the
	// transparent cover window takes the pointer there.
	clip := pixelRect{0, 0, 100, 100}
	holes := clipHoles(clip, 0, nil, 96)
	region := clipRegion(clip, holes)
	if len(holes) != 0 || !slices.Equal(region, []pixelRect{clip}) {
		t.Errorf("a popup over a native view cut the app's control: holes %+v, region %+v", holes, region)
	}
	// A web view that also has popups of its own is cut by its clip only:
	// covers never widen what the page shows.
	webs := []platform.WebViewPlacement{{
		Order:  1,
		Clip:   platform.RectF{X: 10, Y: 10, W: 20, H: 20},
		Covers: []platform.RectF{{X: 50, Y: 50, W: 40, H: 40}},
	}}
	if holes := clipHoles(clip, 0, webs, 96); !slices.Equal(holes, []pixelRect{{10, 10, 30, 30}}) {
		t.Errorf("a later web view's popups widened the cut: %+v", holes)
	}
}

// replayStack applies a plan the way SetWindowPos does: hWndInsertAfter puts
// the window directly below the window it names, and none puts it above every
// window of the stack. The result is the z-order, top first.
func replayStack(plan []stackPlan) []int {
	var z []int
	for _, step := range plan {
		at := 0
		if step.Below >= 0 {
			at = slices.Index(z, step.Below) + 1
		}
		z = slices.Insert(z, at, step.View)
	}
	return z
}

// TestNativeChildNeedsTheClipAsItsDirectParent checks the handle a native
// view's factory returned, which the surface hosts: a live window of this
// process and thread, made in the window the factory was given. IsChild alone
// accepts any descendant of it, and a control nested under an intermediate
// window is not where the content shows the view: Windows places a child
// window at its offset from its *immediate* parent, while the content places
// the control at its offset from the clip window (childAt), so a grandchild
// would paint and take the pointer elsewhere. The clip window must therefore
// be the window the returned root is directly in; what that root makes in
// itself is its own business, and nothing is reparented.
func TestNativeChildNeedsTheClipAsItsDirectParent(t *testing.T) {
	const (
		clip   = 0x1000 // the window the factory was given (NativeViewHost.Parent)
		parent = 0x2000 // a window of mygo's: the clip window is a child of it
	)
	// direct is what a factory that made its control in the window it was
	// given, in this process, on this thread, hands back.
	direct := nativeChild{
		handle: 0x3000, clip: clip, isWindow: true, inClip: true, parent: clip,
		process: 7, currentProcess: 7, thread: 11, currentThread: 11,
	}
	// The same handle with one fact of it changed.
	with := func(change func(*nativeChild)) nativeChild {
		c := direct
		change(&c)
		return c
	}
	tests := []struct {
		name string
		view nativeChild
		// hosts is whether the surface takes the handle, and mentions a word
		// the refusal must say when it does not.
		hosts    bool
		mentions string
	}{
		{name: "direct child accepted", view: direct, hosts: true},
		// IsChild says yes to every descendant; GetParent is the clip only
		// for the direct child.
		{name: "grandchild under an intermediate window", view: with(func(c *nativeChild) {
			c.parent = parent
		}), mentions: "directly"},
		{name: "a window of another parent", view: with(func(c *nativeChild) {
			c.inClip, c.parent = false, parent
		}), mentions: "another parent"},
		// The clip window itself: its parent is the surface's window, not
		// the clip.
		{name: "the clip window returned", view: with(func(c *nativeChild) {
			c.handle, c.inClip, c.parent = clip, false, parent
		}), mentions: "another parent"},
		{name: "the window's own window returned", view: with(func(c *nativeChild) {
			c.mine = true
		}), mentions: "mygo's"},
		{name: "no handle", view: with(func(c *nativeChild) { c.handle = 0 })},
		{name: "not a window", view: with(func(c *nativeChild) { c.isWindow = false })},
		{name: "another process", view: with(func(c *nativeChild) { c.process = 8 })},
		{name: "another thread", view: with(func(c *nativeChild) { c.thread = 12 })},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := nativeChildError(tt.view)
			if tt.hosts {
				if err != nil {
					t.Fatalf("nativeChildError(%+v) = %v, want the handle hosted", tt.view, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("nativeChildError(%+v) = nil, want a refusal", tt.view)
			}
			if tt.mentions != "" && !strings.Contains(err.Error(), tt.mentions) {
				t.Errorf("nativeChildError(%+v) = %q, want it to mention %q", tt.view, err, tt.mentions)
			}
		})
	}
}

func area(rects []pixelRect) int32 {
	var n int32
	for _, r := range rects {
		if !r.empty() {
			n += (r.Right - r.Left) * (r.Bottom - r.Top)
		}
	}
	return n
}

func containsAny(rects []pixelRect, x, y int32) bool {
	for _, r := range rects {
		if x >= r.Left && x < r.Right && y >= r.Top && y < r.Bottom {
			return true
		}
	}
	return false
}
