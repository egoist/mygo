//go:build linux && (amd64 || arm64)

package linux

import (
	"errors"
	"math"
	"sync"

	"github.com/ebitengine/purego"
	"github.com/egoist/mygo/internal/platform"
)

// GtkLayout supplies a real clipping GdkWindow, with the full control in
// its scrollable bin window. GtkOverlay's get-child-position gives it an
// exact allocation without letting a control's minimum size resize MyGo.
type nativeView struct {
	s          *surface
	h          platform.NativeViewHandler
	clip, view ptr
	p          platform.NativeViewPlacement
	closed     bool
	focusing   bool
}

var (
	nativeViewOnce                                                sync.Once
	hostedViews                                                   = map[ptr]*nativeView{}
	hostOverlays                                                  = map[ptr]*surface{}
	gtkLayoutNew                                                  func(h, v ptr) ptr
	gtkLayoutPut                                                  func(layout, child ptr, x, y int32)
	gtkLayoutSetSize                                              func(layout ptr, w, h uint32)
	gtkScrollableGetHAdjustment, gtkScrollableGetVAdjustment      func(w ptr) ptr
	gtkAdjustmentConfigure                                        func(a ptr, value, lower, upper, step, page, size float64)
	gtkWidgetGetParent                                            func(w ptr) ptr
	gtkWidgetIsAncestor                                           func(w, ancestor ptr) bool
	gtkWidgetChildFocus                                           func(w ptr, direction int32) bool
	gtkWindowGetFocus                                             func(w ptr) ptr
	gtkWidgetQueueResize                                          func(w ptr)
	gObjectIsFloating                                             func(w ptr) bool
	gtkOverlayGetType, gtkContainerAccessibleGetType              func() uintptr
	cbNativePosition, cbNativeFocus, cbNativeKey, cbNativeDestroy ptr
	nativeOverlayType                                             uintptr
)

var gtkBindingsActivate func(w ptr, key, modifiers uint32) bool

func loadNativeViews() {
	nativeViewOnce.Do(func() {
		for _, b := range []struct {
			fn   any
			name string
		}{
			{&gtkLayoutNew, "gtk_layout_new"}, {&gtkLayoutPut, "gtk_layout_put"}, {&gtkLayoutSetSize, "gtk_layout_set_size"},
			{&gtkScrollableGetHAdjustment, "gtk_scrollable_get_hadjustment"}, {&gtkScrollableGetVAdjustment, "gtk_scrollable_get_vadjustment"},
			{&gtkAdjustmentConfigure, "gtk_adjustment_configure"}, {&gtkWidgetGetParent, "gtk_widget_get_parent"},
			{&gtkWidgetIsAncestor, "gtk_widget_is_ancestor"}, {&gtkWidgetChildFocus, "gtk_widget_child_focus"},
			{&gtkWindowGetFocus, "gtk_window_get_focus"}, {&gtkWidgetQueueResize, "gtk_widget_queue_resize"},
			{&gtkOverlayGetType, "gtk_overlay_get_type"}, {&gtkContainerAccessibleGetType, "gtk_container_accessible_get_type"},
		} {
			mustBind(libGTK, b.fn, b.name)
		}
		mustBind(libGObject, &gObjectIsFloating, "g_object_is_floating")
		mustBind(libGTK, &gtkBindingsActivate, "gtk_bindings_activate")
		cbNativePosition = purego.NewCallback(func(overlay, child ptr, a *gdkRectangle, data ptr) bool {
			if n := hostedViews[child]; n != nil && !n.closed {
				*a = nativeAllocation(n.p.Clip)
				return true
			}
			return false
		})
		cbNativeFocus = purego.NewCallback(func(win, widget, data ptr) {
			if w := theBackend.window(data); w != nil && w.surface != nil {
				for _, n := range w.surface.nativeViews {
					if !n.closed && n.p.Visible && (widget == n.view || gtkWidgetIsAncestor(widget, n.clip)) {
						n.focusing = true
						n.h.Focused()
						n.focusing = false
						break
					}
				}
			}
		})
		cbNativeKey = purego.NewCallback(func(win, event, data ptr) bool {
			w := theBackend.window(data)
			if w == nil || w.surface == nil || keyvalKey(field[uint32](event, 28)) != platform.KeyTab {
				return false
			}
			mods := gdkMods(field[uint32](event, 24))
			if mods&^platform.ModShift != 0 {
				return false
			}
			for _, n := range w.surface.nativeViews {
				if !n.closed && n.p.Visible && !n.p.ManagesTab && n.hasFocus() {
					n.h.Traverse(mods&platform.ModShift != 0)
					return true
				}
			}
			return false
		})
		cbNativeDestroy = purego.NewCallback(func(widget, data ptr) {
			if n := hostedViews[widget]; n != nil {
				n.Close()
			}
		})
		registerNativeOverlay()
	})
}

// The physical overlay exposes only the MyGo surface to ATK. Native
// controls are grafted under their logical HostView nodes, avoiding two
// copies of the same subtree in the accessible tree.
func registerNativeOverlay() {
	if rootType == 0 {
		return
	} // the surface's ATK bridge is unavailable
	register := func(parent uintptr, name string, init ptr) uintptr {
		var q gTypeQueryInfo
		gTypeQuery(parent, &q)
		return gTypeRegisterStaticSimple(parent, cs(name), q.classSize, init, q.instanceSize, 0, 0)
	}
	children := purego.NewCallback(func(obj ptr) int32 { return 1 })
	child := purego.NewCallback(func(obj ptr, i int32) ptr {
		if s := hostOverlays[gtkAccessibleGetWidget(obj)]; s != nil && i == 0 {
			return gObjectRef(gtkWidgetGetAccessible(s.area))
		}
		return 0
	})
	accessibleInit := purego.NewCallback(func(class, data ptr) { *slot(class, atkGetNChildren), *slot(class, atkRefChild) = children, child })
	accessible := register(gtkContainerAccessibleGetType(), "MyGoHostOverlayAccessible", accessibleInit)
	widgetInit := purego.NewCallback(func(class, data ptr) { gtkWidgetClassSetAccessibleType(class, accessible) })
	nativeOverlayType = register(gtkOverlayGetType(), "MyGoHostOverlay", widgetInit)
}

func (s *surface) createNativeOverlay() {
	loadNativeViews()
	if nativeOverlayType != 0 {
		s.hostOverlay = gObjectNew(nativeOverlayType, 0)
	} else {
		s.hostOverlay = gtkOverlayNew()
	}
	hostOverlays[s.hostOverlay] = s
	gtkContainerAdd(s.hostOverlay, s.area)
	connect(s.hostOverlay, "get-child-position", cbNativePosition, ptr(s.w.id))
	connect(s.w.win, "set-focus", cbNativeFocus, ptr(s.w.id))
	connect(s.w.win, "key-press-event", cbNativeKey, ptr(s.w.id))
}

func (b *Backend) NewNativeView(s platform.Surface, h platform.NativeViewHandler) (platform.NativeViewHost, error) {
	area, ok := s.(*surface)
	if !ok || area.w.closed {
		return nil, platform.ErrUnsupported
	}
	n := &nativeView{s: area, h: h, clip: gtkLayoutNew(0, 0)}
	gObjectRefSink(n.clip)
	hostedViews[n.clip] = n
	gtkWidgetSetNoShowAll(n.clip, true)
	gtkOverlayAddOverlay(area.hostOverlay, n.clip)
	connect(n.clip, "destroy", cbNativeDestroy, n.clip)
	area.nativeViews = append(area.nativeViews, n)
	return n, nil
}

func (n *nativeView) Parent() uintptr { return n.clip }
func (n *nativeView) Attach(v uintptr) error {
	if n.closed || n.view != 0 || v == 0 || gtkWidgetGetParent(v) != 0 {
		return errors.New("a new parentless GTK 3 widget is required")
	}
	if gObjectIsFloating(v) {
		gObjectRefSink(v)
	}
	n.view = v
	gtkLayoutPut(n.clip, v, 0, 0)
	gtkWidgetShowAll(v)
	return nil
}

func nativeAllocation(r platform.RectF) gdkRectangle {
	x, y := int32(math.Round(r.X)), int32(math.Round(r.Y))
	return gdkRectangle{X: x, Y: y, Width: max(1, int32(math.Round(r.X+r.W))-x), Height: max(1, int32(math.Round(r.Y+r.H))-y)}
}

func (n *nativeView) Place(p platform.NativeViewPlacement) {
	if n.closed {
		return
	}
	n.p = p
	if !p.Visible || !p.Enabled {
		n.Blur()
	}
	gtkWidgetSetSensitive(n.clip, p.Enabled)
	if !p.Visible {
		gtkWidgetHide(n.clip)
		return
	}
	bounds, clip := nativeAllocation(p.Bounds), nativeAllocation(p.Clip)
	gtkWidgetSetSizeRequest(n.view, bounds.Width, bounds.Height)
	gtkLayoutSetSize(n.clip, uint32(bounds.Width), uint32(bounds.Height))
	gtkAdjustmentConfigure(gtkScrollableGetHAdjustment(n.clip), float64(clip.X-bounds.X), 0, float64(bounds.Width), 1, 10, float64(clip.Width))
	gtkAdjustmentConfigure(gtkScrollableGetVAdjustment(n.clip), float64(clip.Y-bounds.Y), 0, float64(bounds.Height), 1, 10, float64(clip.Height))
	gtkWidgetShow(n.clip)
	gtkWidgetQueueResize(n.s.hostOverlay)
}

func (n *nativeView) hasFocus() bool {
	f := gtkWindowGetFocus(n.s.w.win)
	return f != 0 && (f == n.view || gtkWidgetIsAncestor(f, n.clip))
}
func (n *nativeView) Focus(backward bool) {
	if n.closed || n.focusing || !n.p.Visible || !n.p.Enabled || n.hasFocus() {
		return
	}
	direction := int32(0)
	if backward {
		direction = 1
	}
	if !gtkWidgetChildFocus(n.view, direction) {
		gtkWidgetGrabFocus(n.view)
	}
}
func (n *nativeView) Blur() {
	if !n.s.w.closed && n.hasFocus() {
		gtkWidgetGrabFocus(n.s.area)
	}
}
func (n *nativeView) Close() {
	if n.closed {
		return
	}
	n.closed = true
	n.h.Closing()
	n.Blur()
	if n.view != 0 {
		gtkWidgetDestroy(n.view)
		gObjectUnref(n.view)
	}
	delete(hostedViews, n.clip)
	gtkWidgetDestroy(n.clip)
	gObjectUnref(n.clip)
	for i, v := range n.s.nativeViews {
		if v == n {
			n.s.nativeViews = append(n.s.nativeViews[:i:i], n.s.nativeViews[i+1:]...)
			break
		}
	}
}

func (n *nativeView) Command(command string) bool {
	if n.closed || !n.p.Enabled || !n.hasFocus() {
		return false
	}
	key := map[string]uint32{"copy": 'c', "cut": 'x', "paste": 'v', "selectAll": 'a', "undo": 'z', "redo": 'y', "delete": 0xffff}[command]
	if key == 0 {
		return false
	}
	var mods uint32 = 1 << 2
	if command == "delete" {
		mods = 0
	}
	return gtkBindingsActivate(gtkWindowGetFocus(n.s.w.win), key, mods)
}

func (s *surface) closeNativeViews() {
	for len(s.nativeViews) > 0 {
		i := len(s.nativeViews) - 1
		n := s.nativeViews[i]
		s.nativeViews = s.nativeViews[:i]
		n.Close()
	}
}
