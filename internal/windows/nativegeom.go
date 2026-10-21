//go:build windows || nativegeom

package windows

import (
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/egoist/mygo/internal/platform"
)

// The geometry of native view hosting (nativeview.go) is arithmetic on whole
// pixels and has nothing of Windows in it, and the decision that refuses a
// handle the surface cannot host (nativeChildError) is a judgement on facts
// the platform found, and has nothing of Windows in it either. The nativegeom
// tag builds this file alone, with its test, on a host that is not Windows, so
// that both are tested where a Windows runtime is not available:
//
//	GOOS=darwin go test -tags nativegeom ./internal/windows/
//
// A build of the package for Windows and for a host without the tag takes the
// file exactly as a Windows build does.

// pixelRect is a rectangle in whole pixels of a surface: what the clip window
// of a native view is put at, where the app's control goes in it, and the boxes
// its cover window takes.
type pixelRect struct {
	Left, Top, Right, Bottom int32
}

// pxRect converts a placement rectangle, in DIPs, to whole pixels of the
// surface, rounding as the web views' placement does (webview_embed.go,
// placeEmbedded), so that a native view and a web view of one frame line up.
func pxRect(r platform.RectF, dpi int) pixelRect {
	px := func(v float64) int32 { return int32(math.Round(v * float64(dpi) / 96)) }
	return pixelRect{px(r.X), px(r.Y), px(r.X + r.W), px(r.Y + r.H)}
}

// childAt is where the app's control goes in the clip window of its view: its
// frame, moved so that the clip's origin is the clip window's origin. The two
// are in the same pixels, so the size of the control is its frame's: a clip
// that shrinks or moves as the content scrolls moves the control, and the
// control keeps the size the app gave it, which keeps what it draws (the text,
// caret and selection of an edit control) where the app put it.
//
// Clip is inside Frame (platform.NativeViewPlacement), so the control covers
// the whole clip window, and the clip window itself draws nothing.
func childAt(clip, frame pixelRect) pixelRect {
	return pixelRect{
		frame.Left - clip.Left,
		frame.Top - clip.Top,
		frame.Right - clip.Left,
		frame.Bottom - clip.Top,
	}
}

// sameSize reports whether two pixel rectangles are as wide and as tall as one
// another, wherever they are: a control moved by a scroll keeps its size, and
// must not be laid out again.
func sameSize(a, b pixelRect) bool {
	return a.Right-a.Left == b.Right-b.Left && a.Bottom-a.Top == b.Bottom-b.Top
}

// nativeChild is the handle an app's factory returned, as the platform found it
// and as the surface judges it: what checkNativeChild (nativeview.go) gathers
// from Windows, and what nativeChildError decides on. The decision is here,
// with the facts it is made of, so that it is tested where a Windows runtime is
// not (nativegeom_test.go).
type nativeChild struct {
	// handle is what the view the factory returned answered
	// (NativeView.NativeHandle), and clip the window the factory was given
	// (NativeViewHost.Parent).
	handle, clip uintptr
	// isWindow is whether handle is a window at all (IsWindow), mine whether
	// it is a window of mygo's own (the surface's, or the window's), and
	// inClip whether it is in the clip window's tree at all (IsChild, which
	// says yes to any descendant).
	isWindow, mine, inClip bool
	// parent is the window handle is directly in (GetParent): the clip window
	// for the control the factory made in the window it was given.
	parent uintptr
	// process is the process handle belongs to and currentProcess this
	// process; thread is the thread that made handle and currentThread this
	// thread.
	process, currentProcess uint32
	thread, currentThread   uintptr
}

// nativeChildError reports a handle the surface cannot host, and nil when it
// can. The control the factory returned must be a live window of this process
// and this thread, made in the window the factory was given, and the clip
// window must be the window the handle is *directly* in: the content places
// the control at its frame's offset from the clip window (childAt, placeNative)
// and Windows places a child window at its offset from its immediate parent, so
// a control nested under an intermediate window -- a grandchild, which IsChild
// alone accepts -- would paint and take the pointer at that window's offset
// rather than where the content shows the view. Nothing is reparented here:
// a window of another parent's, another thread's, another process's, or one of
// mygo's own is refused, never taken.
func nativeChildError(c nativeChild) error {
	switch {
	case c.handle == 0:
		return errors.New("mygo: windows: the native view has no window")
	case !c.isWindow:
		return fmt.Errorf("mygo: windows: the handle %#x of the native view is not a window", c.handle)
	case c.mine:
		return fmt.Errorf("mygo: windows: the handle %#x of the native view is a window of mygo's", c.handle)
	}
	if c.process != c.currentProcess || c.thread != c.currentThread {
		return fmt.Errorf("mygo: windows: the window %#x of the native view is not this thread's", c.handle)
	}
	if !c.inClip {
		return fmt.Errorf("mygo: windows: the native view %#x is not in the window its factory was given; mygo does not take a window from another parent", c.handle)
	}
	if c.parent != c.clip {
		return fmt.Errorf("mygo: windows: the native view %#x is not directly in the window its factory was given (%#x): it is in %#x; mygo places the control at its offset from the window it was given, and does not take a window from another parent", c.handle, c.clip, c.parent)
	}
	return nil
}

// empty reports whether a pixel rectangle has nothing in it.
func (r pixelRect) empty() bool {
	return r.Right <= r.Left || r.Bottom <= r.Top
}

// stackPlan is one call that stacks the clip window of a native view: the view
// to place (its index in the views given) and the view it goes directly below,
// or none for the top of the stack.
type stackPlan struct {
	View  int
	Below int
}

// topOfStack is the Below of a stackPlan that puts its window above every
// window of the stack: hWndInsertAfter none.
const topOfStack = -1

// stackNativePlan returns the calls that put the clip windows of the views
// given in the content's paint order, last painted on top. The orders are the
// content's (platform.NativeViewPlacement.Order), the same counter the web
// views' orders come from.
//
// Windows puts a window *below* the window named by hWndInsertAfter, so the
// plan walks the paint order downwards: the last painted view first, at the top
// of the stack, and each next one directly below the one before it. A chain
// that walked the order upwards would stack every window upside down, which is
// what a plan is compared against in tests. Ties keep the order the views were
// given in, and every window is placed once, however little the stack changed.
func stackNativePlan(orders []int) []stackPlan {
	top := make([]int, len(orders))
	for i := range top {
		top[i] = i
	}
	slices.SortStableFunc(top, func(a, b int) int { return orders[b] - orders[a] })
	plan := make([]stackPlan, 0, len(top))
	for at, view := range top {
		below := topOfStack
		if at > 0 {
			below = top[at-1]
		}
		plan = append(plan, stackPlan{View: view, Below: below})
	}
	return plan
}

// coverStep is what the native view of one placement takes, in the boxes of
// the content's pixels: the boxes its own control takes the pointer in, and
// the boxes it leaves to the content. The control's boxes are the view's clip
// without the boxes painted over it, which is not one rectangle: an overlap
// splits it.
type coverStep struct {
	Control []pixelRect
	Covers  []pixelRect
}

// coverSteps returns what each view takes, in the order the content painted the
// views: a later view is over an earlier one, so at its own place it decides
// what an earlier view took. Everything outside every clip belongs to no view.
func coverSteps(placements []platform.NativeViewPlacement, dpi int) []coverStep {
	ordered := slices.Clone(placements)
	slices.SortStableFunc(ordered, func(a, b platform.NativeViewPlacement) int { return a.Order - b.Order })
	steps := make([]coverStep, 0, len(ordered))
	for _, p := range ordered {
		clip := pxRect(p.Clip, dpi)
		if clip.empty() {
			continue
		}
		var covers []pixelRect
		for _, c := range p.Covers {
			if r := pxRect(c, dpi); !r.empty() {
				covers = append(covers, r)
			}
		}
		if len(covers) == 0 {
			steps = append(steps, coverStep{Control: []pixelRect{clip}})
			continue
		}
		steps = append(steps, coverStep{Control: subtractRects(clip, covers), Covers: covers})
	}
	return steps
}

// clipHoles returns the boxes of a native view's clip window a web view shows
// through: the boxes of the view's clip where a web view the content painted
// after the view (a greater Order, both from the content's paint order) shows
// its own clip. They are in the clip window's pixels, whose origin is the
// clip's, so they are what the window's region leaves out.
//
// A web view painted before the view is under it, and one painted at the same
// order was painted with it: neither shows through. The boxes the content
// painted over the view (NativeViewPlacement.Covers) are not web views and cut
// nothing: the pixels of the app's control stay under them.
func clipHoles(clip pixelRect, order int, webs []platform.WebViewPlacement, dpi int) []pixelRect {
	if clip.empty() {
		return nil
	}
	var holes []pixelRect
	for _, w := range webs {
		if w.Order <= order {
			continue
		}
		if box := intersectRect(clip, pxRect(w.Clip, dpi)); !box.empty() {
			holes = append(holes, pixelRect{
				box.Left - clip.Left,
				box.Top - clip.Top,
				box.Right - clip.Left,
				box.Bottom - clip.Top,
			})
		}
	}
	return holes
}

// clipRegion returns the boxes a clip window shows of its clip: the window's
// whole area without the boxes a web view shows through (clipHoles), in the
// window's pixels. It is what the window's region is made of: with no hole it
// is the whole window, and when a web view covers the clip whole it is empty,
// so the app's control shows nothing there (and takes no pointer).
func clipRegion(clip pixelRect, holes []pixelRect) []pixelRect {
	local := pixelRect{0, 0, clip.Right - clip.Left, clip.Bottom - clip.Top}
	if local.empty() {
		return nil
	}
	if len(holes) == 0 {
		return []pixelRect{local}
	}
	return subtractRects(local, holes)
}

// subtractRects returns r without the rectangles of minus.
func subtractRects(r pixelRect, minus []pixelRect) []pixelRect {
	out := []pixelRect{r}
	for _, m := range minus {
		if len(out) == 0 {
			return nil
		}
		next := make([]pixelRect, 0, len(out)+3)
		for _, x := range out {
			next = append(next, subtractRect(x, m)...)
		}
		out = next
	}
	return out
}

// subtractRect returns r without minus: what is left of r, as up to four
// rectangles (above, below, left and right of the hole).
func subtractRect(r, minus pixelRect) []pixelRect {
	hole := intersectRect(r, minus)
	if hole.empty() {
		return []pixelRect{r}
	}
	var out []pixelRect
	if hole.Top > r.Top {
		out = append(out, pixelRect{r.Left, r.Top, r.Right, hole.Top})
	}
	if hole.Bottom < r.Bottom {
		out = append(out, pixelRect{r.Left, hole.Bottom, r.Right, r.Bottom})
	}
	if hole.Left > r.Left {
		out = append(out, pixelRect{r.Left, hole.Top, hole.Left, hole.Bottom})
	}
	if hole.Right < r.Right {
		out = append(out, pixelRect{hole.Right, hole.Top, r.Right, hole.Bottom})
	}
	return out
}

// intersectRect returns the part of two pixel rectangles that is in both.
func intersectRect(a, b pixelRect) pixelRect {
	return pixelRect{
		max(a.Left, b.Left),
		max(a.Top, b.Top),
		min(a.Right, b.Right),
		min(a.Bottom, b.Bottom),
	}
}
