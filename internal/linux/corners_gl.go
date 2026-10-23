//go:build linux && (amd64 || arm64)

package linux

import (
	"log"
	"math"
	"sync"

	"github.com/ebitengine/purego"
)

var (
	cornerGLOnce            sync.Once
	gtkGLAreaMakeCurrent    func(area ptr)
	gdkCairoDrawFromGL      func(cr, window ptr, source, kind, scale, x, y, width, height int32)
	getColorAttachment      func(target, attachment, parameter uint32, value *int32)
	cairoSave, cairoRestore func(cr ptr)
	cairoRectangle          func(cr ptr, x, y, width, height float64)
	cairoTranslate          func(cr ptr, x, y float64)
)

func loadCornerGL() {
	cornerGLOnce.Do(func() {
		mustBind(libGTK, &gtkGLAreaMakeCurrent, "gtk_gl_area_make_current")
		mustBind(libGDK, &gdkCairoDrawFromGL, "gdk_cairo_draw_from_gl")
		mustBind(libCairo, &cairoSave, "cairo_save")
		mustBind(libCairo, &cairoRestore, "cairo_restore")
		mustBind(libCairo, &cairoRectangle, "cairo_rectangle")
		mustBind(libCairo, &cairoTranslate, "cairo_translate")
		// libepoxy exports pointers to dispatch stubs, as package gl uses.
		if lib, err := open("libepoxy.so.0"); err == nil {
			if p, err := purego.Dlsym(lib, "epoxy_glGetFramebufferAttachmentParameteriv"); err == nil {
				purego.RegisterFunc(&getColorAttachment, field[ptr](p, 0))
			}
		}
	})
}

// drawCornerGL keeps GTK's direct GPU copies for native UI. A curved Cairo
// clip makes gdk_cairo_draw_from_gl read its entire source back to the CPU;
// split the source so that only the four small corner squares take that
// path. The rest keeps a rectangular clip and never leaves the GPU.
// More complex widget trees keep GTK's usual drawing and clipping.
func (w *window) drawCornerGL(cr ptr, width, height, radius float64) bool {
	s := w.surface
	if s == nil || s.cornerSplitOff || !s.drawsGL() || s.layers != 0 || w.menubar != 0 && gtkWidgetGetVisible(w.menubar) {
		return false
	}
	var a gdkRectangle
	gtkWidgetGetAllocation(s.area, &a)
	if a.X != 0 || a.Y != 0 || float64(a.Width) != width || float64(a.Height) != height {
		return false
	}
	loadCornerGL()
	if getColorAttachment == nil {
		return false
	}
	scale := max(gtkWidgetGetScaleFactor(s.area), 1)
	r := math.Ceil(radius) // split on device-aligned boundaries
	if width < 2*r || height < 2*r {
		return false
	}
	before := s.cornerFrame.serial
	// Let GtkGLArea allocate its buffers, emit resize/render, and draw
	// once, through GTK's own handler. Its render callback records the
	// current attachment; neither IDs nor sizes survive a new allocation.
	cairoSave(cr)
	cairoRectangle(cr, r, 0, width-2*r, height)
	cairoRectangle(cr, 0, r, r, height-2*r)
	cairoRectangle(cr, width-r, r, r, height-2*r)
	cairoClip(cr)
	gtkContainerPropagateDraw(w.win, w.box, cr)
	cairoRestore(cr)
	f := s.cornerFrame
	if f.source == 0 || f.width != a.Width*scale || f.height != a.Height*scale {
		if f.serial != before {
			// An actual render could not supply the area's attachment.
			// Do not draw twice on every later frame trying it again.
			s.cornerSplitOff = true
			log.Print("mygo: rounded window using GTK clipping: no matching GL attachment")
		}
		// Damage confined to a corner can skip GtkGLArea's draw. On the
		// first frame of a new size there is no matching buffer yet: the
		// ordinary clipped draw will render it before copying it.
		return false
	}
	var x0, y0, x1, y1 float64
	cairoClipExtents(cr, &x0, &y0, &x1, &y1)
	draw := func(x, y float64) {
		if x+r <= x0 || x >= x1 || y+r <= y0 || y >= y1 {
			return
		}
		cairoSave(cr)
		cairoRectangle(cr, x, y, r, r)
		cairoClip(cr)
		roundedPath(cr, width, height, radius)
		cairoClip(cr)
		cairoTranslate(cr, x, y)
		gdkCairoDrawFromGL(cr, gtkWidgetGetWindow(s.area), f.source, f.kind, scale,
			int32(x)*scale, int32(height-y-r)*scale, int32(r)*scale, int32(r)*scale)
		cairoRestore(cr)
	}
	for _, x := range []float64{0, width - r} {
		for _, y := range []float64{0, height - r} {
			draw(x, y)
		}
	}
	// GDK switches to its compositing context; match GtkGLArea's draw
	// handler by returning with the area's own context current.
	gtkGLAreaMakeCurrent(s.area)
	if w.controls != nil {
		cairoSave(cr)
		// The interior pass already drew the buttons there. Only paint
		// their corners now, so translucent hover states blend once.
		for _, x := range []float64{0, width - r} {
			for _, y := range []float64{0, height - r} {
				cairoRectangle(cr, x, y, r, r)
			}
		}
		cairoClip(cr)
		roundedPath(cr, width, height, radius)
		cairoClip(cr)
		for _, bar := range w.controls.bars {
			if bar != 0 && gtkWidgetGetVisible(bar) {
				gtkContainerPropagateDraw(w.controls.overlay, bar, cr)
			}
		}
		cairoRestore(cr)
	}
	return true
}

type cornerFrame struct {
	source, kind, width, height int32
	serial                      uint64
}

func (s *surface) recordCornerFrame() {
	if !s.w.roundCorners || getColorAttachment == nil {
		return
	}
	s.cornerFrame = cornerFrame{serial: s.cornerFrame.serial + 1}
	f := &s.cornerFrame
	const framebuffer, colorAttachment0 = 0x8d40, 0x8ce0
	getColorAttachment(framebuffer, colorAttachment0, 0x8cd0, &f.kind)
	getColorAttachment(framebuffer, colorAttachment0, 0x8cd1, &f.source)
	if f.kind != 0x1702 && f.kind != 0x8d41 { // GL_TEXTURE, GL_RENDERBUFFER
		f.source = 0
	}
	scale := max(gtkWidgetGetScaleFactor(s.area), 1)
	f.width = gtkWidgetGetAllocatedWidth(s.area) * scale
	f.height = gtkWidgetGetAllocatedHeight(s.area) * scale
}
