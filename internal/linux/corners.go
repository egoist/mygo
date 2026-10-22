//go:build linux && (amd64 || arm64)

package linux

import (
	"math"
	"sync"

	"github.com/ebitengine/purego"
)

// Hidden title bars have no GTK decorations to round their content. Clip
// the whole window, including its native controls, rather than asking an
// app to round its page or view. Explicitly transparent and frameless
// windows keep the shape the app draws.
var (
	gtkWidgetInputShapeCombineRegion                                       func(widget, region ptr)
	cornersOnce                                                            sync.Once
	gtkContainerPropagateDraw                                              func(container, child, cr ptr)
	gtkRenderBackground                                                    func(context, cr ptr, x, y, width, height float64)
	gdkScreenIsComposited                                                  func(screen ptr) bool
	gdkWindowSetOpaqueRegion                                               func(window, region ptr)
	cairoClip, cairoNewPath, cairoClosePath                                func(cr ptr)
	cairoArc                                                               func(cr ptr, x, y, radius, angle1, angle2 float64)
	cbRoundedDraw, cbCornersAllocated, cbCornersRealized, cbCornersChanged ptr
)

func loadCorners() {
	cornersOnce.Do(func() {
		mustBind(libGTK, &gtkContainerPropagateDraw, "gtk_container_propagate_draw")
		mustBind(libGTK, &gtkRenderBackground, "gtk_render_background")
		mustBind(libGTK, &gtkWidgetQueueDraw, "gtk_widget_queue_draw")
		mustBind(libGDK, &gdkScreenIsComposited, "gdk_screen_is_composited")
		mustBind(libGDK, &gdkWindowSetOpaqueRegion, "gdk_window_set_opaque_region")
		mustBind(libCairo, &cairoClip, "cairo_clip")
		mustBind(libCairo, &cairoNewPath, "cairo_new_path")
		mustBind(libCairo, &cairoClosePath, "cairo_close_path")
		mustBind(libCairo, &cairoArc, "cairo_arc")
		mustBind(libCairo, &cairoClipExtents, "cairo_clip_extents")
		mustBind(libGTK, &gtkWidgetInputShapeCombineRegion, "gtk_widget_input_shape_combine_region")
		mustBind(libCairo, &cairoRegionCreateRectangle, "cairo_region_create_rectangle")
		mustBind(libCairo, &cairoRegionSubtractRectangle, "cairo_region_subtract_rectangle")
		mustBind(libCairo, &cairoRegionDestroy, "cairo_region_destroy")
		cbCornersAllocated = purego.NewCallback(func(widget, allocation, data ptr) {
			if w := theBackend.window(data); w != nil {
				w.updateCornerInput()
			}
		})
		cbCornersRealized = purego.NewCallback(func(widget, data ptr) {
			if w := theBackend.window(data); w != nil {
				w.updateCornerInput()
			}
		})
		cbCornersChanged = purego.NewCallback(func(widget, data ptr) {
			if w := theBackend.window(data); w != nil {
				w.updateCornerInput()
				gtkWidgetQueueDraw(w.win)
			}
		})
		cbRoundedDraw = purego.NewCallback(func(widget, cr, data ptr) bool {
			w := theBackend.window(data)
			if w == nil {
				return false
			}
			var a gdkRectangle
			gtkWidgetGetAllocation(widget, &a)
			width, height := float64(a.Width), float64(a.Height)
			if r := w.cornerRadius(width, height); r > 0 {
				if w.drawCornerGL(cr, width, height, r) {
					return true
				}
				var x0, y0, x1, y1 float64
				cairoClipExtents(cr, &x0, &y0, &x1, &y1)
				// Interior damage keeps GTK's rectangular clip, including
				// its direct OpenGL compositing path.
				if (x0 < r || x1 > width-r) && (y0 < r || y1 > height-r) {
					roundedPath(cr, width, height, r)
					cairoClip(cr)
				}
			}
			gtkRenderBackground(gtkWidgetGetStyleContext(widget), cr, 0, 0, width, height)
			gtkContainerPropagateDraw(widget, w.box, cr)
			return true
		})
	})
}

func (w *window) initCorners() {
	if !w.hiddenTitleBar() || w.opts.Transparent || w.opts.Panel || w.opts.Flyout != nil || w.b.compositorDecorates {
		return
	}
	loadCorners()
	screen := gtkWidgetGetScreen(w.win)
	visual := gdkScreenGetRGBAVisual(screen)
	if visual == 0 {
		return
	}
	gtkWidgetSetVisual(w.win, visual)
	// Keep GTK from declaring the whole window opaque: updateCornerInput
	// declares everything but the corner squares instead.
	gtkWidgetSetAppPaintable(w.win, true)
	w.roundCorners = true
	connect(w.win, "draw", cbRoundedDraw, ptr(w.id))
	connect(w.win, "size-allocate", cbCornersAllocated, ptr(w.id))
	connect(w.win, "realize", cbCornersRealized, ptr(w.id))
	connect(w.win, "map", cbCornersRealized, ptr(w.id))
	connect(w.win, "composited-changed", cbCornersChanged, ptr(w.id))
}

// Input follows the visible outline. An invisible corner must not start
// a resize drag or consume a click meant for a window below.
func (w *window) updateCornerInput() {
	if !w.roundCorners {
		return
	}
	win := gtkWidgetGetWindow(w.win)
	if win == 0 {
		return
	}
	var a gdkRectangle
	gtkWidgetGetAllocation(w.win, &a)
	r := w.cornerRadius(float64(a.Width), float64(a.Height))
	// Only the corner squares can contain transparency. Keep the rest
	// opaque so compositors can skip what is behind the window.
	opaque := cairoRegionCreateRectangle(&gdkRectangle{Width: a.Width, Height: a.Height})
	corner := int32(math.Ceil(r))
	if corner > 0 {
		for _, x := range []int32{0, a.Width - corner} {
			for _, y := range []int32{0, a.Height - corner} {
				cairoRegionSubtractRectangle(opaque, &gdkRectangle{X: x, Y: y, Width: corner, Height: corner})
			}
		}
	}
	gdkWindowSetOpaqueRegion(win, opaque)
	cairoRegionDestroy(opaque)
	if r == 0 {
		gtkWidgetInputShapeCombineRegion(w.win, 0)
		return
	}
	region := cairoRegionCreateRectangle(&gdkRectangle{Width: a.Width, Height: a.Height})
	for y := int32(0); float64(y) < r; y++ {
		dy := r - float64(y) - 0.5
		inset := int32(math.Round(r - math.Sqrt(max(0, r*r-dy*dy))))
		if inset == 0 {
			continue
		}
		for _, row := range []int32{y, a.Height - 1 - y} {
			cairoRegionSubtractRectangle(region, &gdkRectangle{Y: row, Width: inset, Height: 1})
			cairoRegionSubtractRectangle(region, &gdkRectangle{X: a.Width - inset, Y: row, Width: inset, Height: 1})
		}
	}
	// Store it on the widget too: after the realize signal GTK reapplies
	// the widget's input shape and would discard a GdkWindow-only shape.
	gtkWidgetInputShapeCombineRegion(w.win, region)
	cairoRegionDestroy(region)
}

func (w *window) cornerRadius(width, height float64) float64 {
	if !w.roundCorners || w.state&(stateMaximized|stateFullscreen|stateAnyTiled) != 0 ||
		!gdkScreenIsComposited(gtkWidgetGetScreen(w.win)) {
		return 0
	}
	return max(0, min(12, width/2, height/2))
}

func roundedPath(cr ptr, width, height, radius float64) {
	cairoNewPath(cr)
	cairoArc(cr, width-radius, radius, radius, -math.Pi/2, 0)
	cairoArc(cr, width-radius, height-radius, radius, 0, math.Pi/2)
	cairoArc(cr, radius, height-radius, radius, math.Pi/2, math.Pi)
	cairoArc(cr, radius, radius, radius, math.Pi, 3*math.Pi/2)
	cairoClosePath(cr)
}
