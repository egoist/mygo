package ui

import (
	"log"
	"slices"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/surface"
)

// NativeHostView is a platform view an app made for its content to show, as
// *mygo.NativeView, of mygo.NewNativeView.
type NativeHostView = surface.NativeView

// NativeView shows a native view, made with mygo.NewNativeView, in the
// element's content box. A native view has no size of its own: the element
// stretches across its container (AlignSelf(Stretch)), and takes the room
// along it with Grow or a size:
//
//	ui.Row(c).Fill().Children(func() {
//		sidebar(c)
//		ui.NativeView(c, terminal).Grow(1)
//	})
//
// The view shows while a frame builds its element and hides when one does
// not, keeping its state: a view switching tabs builds the native view of
// the tab shown. The element paints a hole through the window's content
// (the native view is under it, a view of the platform's, drawn by the
// system), cut by the scroll containers and clips around it, so that what
// paints after it shows over the view: its children, popovers, menus,
// dialogs, tooltips and toasts. The view takes the pointer in the part of
// its box the content paints nothing over: where the content paints over
// it, that content is what takes the pointer, as it shows. It takes the
// keyboard once clicked, focused or tabbed into: Tab stops at the element.
//
// The platform draws the view itself: its radius, shadow, opacity and
// transform are the view's, and MyGo shows only the part of it within the
// element's content box, cut by the clips around it.
//
// A view the app destroyed while the content still builds it is a plain box
// from the frame on: it keeps no hole, shows no view and stops taking Tab,
// which is what mygo.NativeView.Destroy promises.
func coreNativeView(c *context, v NativeHostView) *node {
	e := c.newElement(kindBox)
	e.self = Stretch
	if nativeViewGone(v) {
		return e
	}
	// The window's native runtime, installed here and nowhere else: a
	// content that builds no native view links none of this file, hooks
	// included (AGENTS.md: apps pay only for what they use).
	installNativeHost(c.rt)
	e.nativeView = v
	// Tab stops at it to give the view the keyboard, which shows its own
	// focus.
	e.flags |= flagFocusable | flagOwnRing
	return e
}

// nativeViewGone reports whether the app destroyed a native view while the
// content still builds its element. mygo's own NativeView answers
// IsDestroyed; a view of another implementation, which does not, is never
// gone to us.
func nativeViewGone(v NativeHostView) bool {
	if v == nil {
		return true
	}
	gone, ok := v.(interface{ IsDestroyed() bool })
	return ok && gone.IsDestroyed()
}

// nativeViewSurfaceHost is a native view that reports the surface taking its
// views (mygo.NativeView.SurfaceNativeViewHost). It is the opt-in accessor
// the content uses to find where its views go, together with the surface's
// own platform.NativeViewSurface for a view of another implementation.
type nativeViewSurfaceHost interface {
	SurfaceNativeViewHost(conn *surface.Conn) platform.NativeViewSurface
}

// nativeObserver is what a test host sees of a window's native views, which
// a window is not: headless records where they were placed, the view Tab
// went into, and the keyboard the content took back.
// The frames and the view are handed over as any: the types are this file's
// (dead in an app that shows no native view), and a host that keeps what it
// is given in always-linked state would keep them with it.
type nativeObserver interface {
	nativePlaced(frames any)
	nativeTabbed(v any, back bool)
	nativeTookBack()
}

// placedNativeView is where a frame shows a native view, for the surface.
type placedNativeView struct {
	v           NativeHostView
	frame, clip platform.RectF
	covers      []platform.RectF
	order       int
}

// nativePlace is where the frame being built shows a native view: hit is
// the index of its element's own hit, order where it was painted among the
// frame's views, and inert that it shows but takes no pointer.
type nativePlace struct {
	v     NativeHostView
	frame Rect
	clip  Rect
	hit   int
	order int
	inert bool
}

// nativeHostState is where the native views of one window's content live:
// the value behind engine.native. coreNativeView installs it the first time
// a frame builds a native view, so that a content showing none reaches none
// of it: it holds the frame's placements, gives them to the window's
// surface, and keeps the keyboard the views take.
type nativeHostState struct {
	rt *engine
	// host is the window host (nil when the runtime draws in memory) and
	// conn its connection, where the views report themselves.
	host *windowHost
	conn *surface.Conn
	// places are where the frame being built shows native views, out is
	// frameViews' scratch, frames and views what the last placement made of
	// them (views aligned with placed), and shown whether it showed any.
	places []nativePlace
	out    []placedNativeView
	frames []placedNativeView
	placed []platform.NativeViewPlacement
	views  []NativeHostView
	// showing says whether the last placement showed any view.
	showing bool
	// prov is the surface the last views that report one named
	// (nativeViewSurfaceHost), kept for the frames that show none: it is
	// what hears the empty placement, and gives the keyboard back, once
	// the content hides the last view. nil until a frame shows a view that
	// reports one.
	prov platform.NativeViewSurface
	// cur is the native view that has the keyboard, whose element the
	// content's focus follows, so that Tab moves on from there and the
	// content takes the keyboard back when the view goes; tabOut is the
	// view Tab left the keyboard from, which does not take it back as the
	// focus moves on (tabOutView).
	cur    NativeHostView
	tabOut NativeHostView
	// dups are the views a frame showed twice, as any, kept here rather
	// than on the engine: the apps that show no native view link neither
	// this state nor the metadata of its views (duplicate). A slice, not a
	// map: the report happens once per view.
	dups []any
}

// installNativeHost installs the native runtime of a window, once, as its
// content builds its first native view: the content's connection reports
// its views to it, and carries the focus request of mygo.NativeView.Focus
// to it. A window whose content builds no native view names none of this
// code.
//
// It is a function of the engine, not a method, so that the method set of
// every engine — which the always-linked runtime types keep — carries no
// name of the native views: an app showing none pays for the pointer to the
// runtime and nothing else.
func installNativeHost(rt *engine) *nativeHostState {
	if s, ok := rt.native.(*nativeHostState); ok {
		return s
	}
	s := &nativeHostState{rt: rt}
	if h, ok := rt.host.(*windowHost); ok {
		s.host, s.conn = h, h.conn
		conn := h.conn
		// The hooks live on the connection's opaque native view state,
		// which the window's own side made with the first view its app
		// made (mygo/nativeview.go). They take the view as any, and narrow
		// it here: the payload of a caller that is not a native view of a
		// content is ignored.
		b := surface.NativeViewState(conn, nil)
		b.RequestFocus = func(view any, back bool) {
			if v, ok := view.(NativeHostView); ok && v != nil {
				s.focusExplicit(conn, v, back)
			}
		}
		b.ReportFocus = func(v any) { s.reportedFocus(v) }
		b.ReportTabOut = func(v any, back bool) { s.reportedTabOut(v, back) }
		b.ReportPress = func(v any, x, y float64) { s.pressLocal(v, float32(x), float32(y)) }
	}
	rt.native = s
	return s
}

// nativeHostOf returns the window's native runtime, nil while no frame has
// built a native view. The frame's paths reach it through engine.native,
// the opaque interface; the tests and the Tester reach its state through
// this. A function of the engine, not a method: the engine is in every app
// that links ui, and a method of it would keep this name in every binary,
// while only tests call it.
func nativeHostOf(rt *engine) *nativeHostState {
	s, _ := rt.native.(*nativeHostState)
	return s
}

// note records where the frame shows the native view v, laid out within
// clip by the content, unless none of it shows, and nothing at all for a
// payload that is no native view: the frame's paths hand it what the element
// was given, as any. Called while committing, before the element's own hit.
func (s *nativeHostState) note(v any, frame, clip Rect, invisible bool) {
	nv, ok := v.(NativeHostView)
	if !ok || nv == nil {
		return
	}
	s.places = append(s.places, nativePlace{v: nv, frame: frame, clip: clip, hit: len(s.rt.hits), order: s.rt.viewOrder, inert: invisible})
	s.rt.viewOrder++
}

// place gives the surface where the frame shows the window's native views,
// hiding the ones it does not show.
func (s *nativeHostState) place() {
	views := s.frameViews()
	s.places = s.places[:0]
	if len(views) == 0 && !s.showing {
		return
	}
	s.showing = len(views) > 0
	s.placeViews(views)
}

// frameViews returns where the frame shows its native views, in the order
// it paints them, each with the boxes of what is painted over it and takes
// the pointer: the hits after its own. A view built twice, as by an exit
// transition, shows where it takes the pointer.
func (s *nativeHostState) frameViews() []placedNativeView {
	out := s.out[:0]
	for i := range s.places {
		p := &s.places[i]
		dup := slices.IndexFunc(s.places[:i], func(o nativePlace) bool { return o.v == p.v })
		if dup >= 0 && (p.inert || !s.places[dup].inert) {
			if !p.inert && !s.places[dup].inert {
				// One native view in two elements at once: one of the two
				// would silently lose.
				s.duplicate(p.v)
			}
			continue
		}
		if dup >= 0 {
			// The inert one went first: this one takes its place.
			out = slices.DeleteFunc(out, func(o placedNativeView) bool { return o.v == p.v })
		}
		own := func(st *state) bool { return st.nativeView == p.v }
		out = append(out, placedNativeView{v: p.v, frame: rectF(p.frame), clip: rectF(p.clip), covers: covers(s.rt.hits, p.clip, p.hit, p.inert, own), order: p.order})
	}
	s.out = out
	return out
}

// placeViews gives the surface where the window's native views show, once
// each: a call with the placements the surface already has writes nothing.
func (s *nativeHostState) placeViews(views []placedNativeView) {
	list := make([]platform.NativeViewPlacement, 0, len(views))
	hosts := make([]NativeHostView, 0, len(views))
	frames := make([]placedNativeView, 0, len(views))
	for _, v := range views {
		if n := v.v.SurfaceNativeView(s.conn); n != nil {
			list = append(list, platform.NativeViewPlacement{View: n, Frame: v.frame, Clip: v.clip, Covers: v.covers, Order: v.order})
			hosts = append(hosts, v.v)
			frames = append(frames, v)
		}
	}
	s.frames, s.views = frames, hosts
	if o, ok := s.rt.host.(nativeObserver); ok {
		o.nativePlaced(frames)
	}
	if slices.EqualFunc(list, s.placed, sameNativePlacement) {
		return
	}
	s.placed = list
	if p := s.provider(); p != nil {
		p.PlaceNativeViews(list)
	}
}

// sameNativePlacement reports whether two placements are the same, their
// orders included: only the orders changing places them again.
func sameNativePlacement(a, b platform.NativeViewPlacement) bool {
	return a.View == b.View && a.Frame == b.Frame && a.Clip == b.Clip && a.Order == b.Order && slices.Equal(a.Covers, b.Covers)
}

// provider returns the surface that takes the window's native views: the
// one the views themselves report (mygo.NativeView.SurfaceNativeViewHost),
// which only the window that made them has and only while it is live, or
// the window surface's own platform.NativeViewSurface, which a view of
// another implementation relies on. A frame that shows no view reports
// neither: the surface the last views named still takes its empty
// placement, and the keyboard the hidden view gave back, so that hiding
// every view reaches the surface that showed them (prov).
func (s *nativeHostState) provider() platform.NativeViewSurface {
	if s.host != nil && s.host.detached {
		// The window let the content go: its surface takes nothing more.
		return nil
	}
	if p := s.reportedProvider(); p != nil {
		s.prov = p
		return p
	}
	if len(s.views) == 0 && s.prov != nil {
		// The frame shows no view at all: the surface that showed the last
		// one hears the empty placement, and gives the keyboard back.
		return s.prov
	}
	if s.conn != nil && s.conn.Surface != nil {
		if p, ok := s.conn.Surface.(platform.NativeViewSurface); ok {
			return p
		}
	}
	return nil
}

// reportedProvider returns the surface the frame's own shown views report
// (nativeViewSurfaceHost), nil when they report none.
func (s *nativeHostState) reportedProvider() platform.NativeViewSurface {
	for _, v := range s.views {
		if h, ok := v.(nativeViewSurfaceHost); ok {
			if p := h.SurfaceNativeViewHost(s.conn); p != nil {
				return p
			}
		}
	}
	return nil
}

// shown reports whether the surface shows v where the frame placed it:
// false for a view it no longer takes, as a destroyed one.
func (s *nativeHostState) shown(v NativeHostView) bool {
	return slices.Contains(s.views, v) && v.SurfaceNativeView(s.conn) != nil
}

// focusExplicit is mygo.NativeView.Focus through the connection's focus
// request hook: the frame must show the view, whose toolkit then takes the
// keyboard, and the window must still be the content's: a request kept from
// before the detach of the window reaches nothing.
func (s *nativeHostState) focusExplicit(conn *surface.Conn, view NativeHostView, back bool) {
	if s.host != nil && s.host.detached || !slices.Contains(s.views, view) {
		return
	}
	s.focus(view)
	if n := view.SurfaceNativeView(conn); n != nil {
		n.Focus(back)
	}
}

// reportedFocus notes a view that reported the keyboard through the
// window's own callback (the connection's native view state,
// mygo.NativeViewHost.Focused). The report is asynchronous, and a view the
// content just hid may still make it: one the last frame does not show
// moves nothing, as its platform events already do (event). A view the
// frame shows reaches focus. The payload arrives as any (the state's hook
// takes it so), and one that is no native view moves nothing.
func (s *nativeHostState) reportedFocus(v any) {
	nv, ok := v.(NativeHostView)
	if !ok || nv == nil || !s.shown(nv) {
		return
	}
	s.focus(nv)
}

// reportedTabOut moves the focus on from the view of a Tab reported through
// the window's own callback (the connection's native view state,
// mygo.NativeViewHost.TabOut): a view the last frame does not show leaves
// the content's focus where it is, however valid the view still is. A
// payload that is no native view leaves it there too.
func (s *nativeHostState) reportedTabOut(v any, back bool) {
	nv, ok := v.(NativeHostView)
	if !ok || nv == nil || !s.shown(nv) {
		return
	}
	s.tabOutView(nv, back)
}

// focus notes that the view of v took the keyboard, as a click on one of
// its controls does: the focus moves to the view's element, so that Tab
// moves on from there, and the view shows its own focus.
func (s *nativeHostState) focus(v NativeHostView) {
	if nativeViewGone(v) {
		// A view that is gone takes no keyboard.
		return
	}
	s.cur = v
	for _, id := range s.rt.focusOrder {
		if st := s.rt.states[id]; st != nil && st.nativeView == v {
			s.rt.focused = id
			s.rt.focusVisible = false
			break
		}
	}
	s.rt.requestFrame()
}

// press handles a press that went to the native view of v at x, y, in DIPs
// relative to the content: the elements under the view hover, and the focus
// leaves the content, as the view has the keyboard, but nothing is pressed;
// popovers see a press outside them (PressedOutside).
func (s *nativeHostState) press(v NativeHostView, x, y float32, button int) {
	if nativeViewGone(v) {
		// The view is gone: the press of a view that is still being taken
		// away moves nothing.
		return
	}
	rt := s.rt
	// The view took the pointer, and with it the keyboard, which the app
	// reports as well (Focused): kept here so that the content takes it
	// back when the view goes.
	s.cur = v
	chain := rt.hitChain(x, y)
	rt.pointerX, rt.pointerY, rt.pointerIn = x, y, true
	rt.setHover(chain)
	if len(chain) > 0 {
		rt.downs = append(rt.downs, chain[0])
	} else {
		rt.downs = append(rt.downs, 0)
	}
	// The press took the keyboard from the content's element, which gives
	// it up, unless that element is the one showing this very view: the
	// view reported the keyboard before the press arrived (Focused first),
	// and clearing the focus here would hand the keyboard straight back to
	// the content at the end of the frame (release), stealing it from the
	// view the press went to.
	if button == 0 && rt.focused != 0 {
		if st := rt.states[rt.focused]; st == nil || st.nativeView != v {
			rt.focused = 0
			rt.focusVisible = false
		}
	}
	rt.requestFrame()
}

// pressLocal notes a pointer press the app's view reported, at x, y in DIPs
// relative to the view itself: the content moves the point to its own space
// with where the view shows, device pixels and all, and takes no press of a
// view its frame does not show. The payload arrives as any (surface.Conn is
// always linked), and one that is no native view takes no press.
func (s *nativeHostState) pressLocal(v any, x, y float32) {
	nv, ok := v.(NativeHostView)
	if !ok || nv == nil {
		return
	}
	if s.host != nil && s.host.detached || !s.shown(nv) {
		return
	}
	i := slices.Index(s.views, nv)
	if i < 0 || i >= len(s.placed) {
		return
	}
	f := s.placed[i].Frame
	s.press(nv, float32(f.X)+x, float32(f.Y)+y, 0)
}

// moved follows the content's focus as moveFocus moves it: the view takes
// the keyboard, or the content takes it back from the view that had it.
func (s *nativeHostState) moved(st *state, back bool) {
	// The element's view is held as any (ui/element.go): narrow it, so
	// that an element showing something else moves the focus back.
	var v NativeHostView
	if st != nil {
		v, _ = st.nativeView.(NativeHostView)
	}
	if v != nil && v != s.tabOut {
		// The view takes the keyboard, and Tab leaving it moves on from
		// here (tabOutView).
		s.cur = v
		s.tabInto(v, back)
		return
	}
	s.takeBack()
}

// tabInto gives the view of v the keyboard as Tab does, from its last
// control when back.
func (s *nativeHostState) tabInto(v NativeHostView, back bool) {
	if o, ok := s.rt.host.(nativeObserver); ok {
		o.nativeTabbed(v, back)
	}
	if n := v.SurfaceNativeView(s.conn); n != nil {
		n.Focus(back)
	}
}

// takeBack gives the content the keyboard back from the native view that
// had it, keeping the element the content's focus moved to.
func (s *nativeHostState) takeBack() {
	if s.cur == nil {
		return
	}
	s.cur = nil
	s.takeBackFocus()
}

// takeBackFocus gives the content the keyboard back: the surface takes it,
// and a test host notes it.
func (s *nativeHostState) takeBackFocus() {
	if p := s.provider(); p != nil {
		p.TakeFocus()
	}
	if o, ok := s.rt.host.(nativeObserver); ok {
		o.nativeTookBack()
	}
}

// tabOutView moves the focus on from the element showing v, whose view left
// the keyboard as Tab left its last control, or its first when back: the
// content has the keyboard again, whichever element takes it next.
func (s *nativeHostState) tabOutView(v NativeHostView, back bool) {
	s.cur = nil
	s.takeBackFocus()
	for _, id := range s.rt.focusOrder {
		if st := s.rt.states[id]; st != nil && st.nativeView == v {
			s.rt.focused = id
			break
		}
	}
	// A frame whose only other element is the view's own does not give the
	// view the keyboard back as it moves the focus on: it just left it.
	s.tabOut = v
	s.rt.moveFocus(back)
	s.tabOut = nil
	s.rt.requestFrame()
}

// dropFocus gives up the keyboard of a native view without asking the
// content for it, as a press of a web view's page does.
func (s *nativeHostState) dropFocus() {
	s.cur = nil
}

// release takes the keyboard back from a native view that no longer shows,
// that the surface no longer takes, or that the content's focus left for an
// element of its own. Called once a frame, once the frame's elements are
// known.
func (s *nativeHostState) release() {
	if s.cur == nil {
		return
	}
	st := s.rt.states[s.rt.focused]
	// A view that still shows keeps the keyboard, and so does the element
	// showing it, or the page of a web view the focus moved to.
	if s.shown(s.cur) && st != nil && (st.nativeView == s.cur || st.webView != nil) {
		return
	}
	s.cur = nil
	if st == nil || st.nativeView == nil && st.webView == nil && st.flags&flagFocusable == 0 {
		// The content's focus was left on nothing that can take the
		// keyboard: it has none as the view gives it back.
		s.rt.focused = 0
	}
	s.takeBackFocus()
}

// duplicate reports a frame showing one native view in two elements at
// once, which the first placement takes: the view is one view of the
// platform's, so one of the two placements would silently lose, and the
// view would show where the other element is not. An exit transition keeps
// one of the two inert, which no view takes the pointer in, and reports
// nothing. A Tester panics (engine.strict), so that tests fail where the
// view was placed twice; apps log it once, and the inspector lists it.
func (s *nativeHostState) duplicate(v NativeHostView) {
	msg := "ui: two elements show the same native view at once, which shows in the first of them: hide the other one"
	if at := callSite(); at != "" {
		msg += " (" + at + ")"
	}
	if s.rt.strict {
		panic(msg)
	}
	if slices.Contains(s.dups, any(v)) {
		return
	}
	s.dups = append(s.dups, v)
	log.Print(msg)
	s.rt.warn(msg)
}
