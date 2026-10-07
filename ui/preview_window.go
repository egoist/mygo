//go:build !mygo_noinspector

package ui

import (
	"math"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/raster"
	"github.com/egoist/mygo/internal/scene"
)

// previewHost fits a scene rendered at the simulated scale into an actual
// native surface. The raster renderer is the same one Tester and captures
// use; the window's existing renderer presents the resulting image. Thus
// this works even when the simulated DPI differs from the host's GPU
// framebuffer size (particularly a GtkGLArea).
type previewHost struct {
	host
	rt        *engine
	pixels    raster.Renderer
	image     *scene.Image
	presented scene.Scene
	fit       previewFit
	access    platform.AccessTree
	nodes     []platform.AccessNode
}

type previewFit struct{ x, y, scale float64 }

func (h *previewHost) size() (float32, float32, float32) {
	c := h.rt.insp.preview.config
	return float32(c.Width), float32(c.Height), c.Scale
}

func fitPreview(w, h, vw, vh float64) previewFit {
	if w <= 0 || h <= 0 || vw <= 0 || vh <= 0 {
		return previewFit{scale: 1}
	}
	s := math.Min(w/vw, h/vh)
	return previewFit{x: (w - vw*s) / 2, y: (h - vh*s) / 2, scale: s}
}

func (f previewFit) rect(r platform.RectF) platform.RectF {
	return platform.RectF{X: f.x + r.X*f.scale, Y: f.y + r.Y*f.scale, W: r.W * f.scale, H: r.H * f.scale}
}

func (p *Preview) attachWindow(window *windowHost) {
	h := &previewHost{host: window, rt: window.rt, fit: previewFit{scale: 1}}
	window.rt.host = h
	p.attachEngine(window.rt)
	prepare := window.rt.insp.preview.beforeFrame
	window.rt.insp.preview.beforeFrame = func() {
		prepare()
		if !window.rt.insp.preview.closed {
			h.syncFit()
		}
	}
	window.rt.insp.preview.onClose = append(window.rt.insp.preview.onClose, func() {
		h.pixels.Release()
		h.image = nil
		h.presented = scene.Scene{}
		h.nodes, h.access.Nodes = nil, nil
	})
	window.conn.Event = func(ev platform.SurfaceEvent) bool {
		switch ev.Kind {
		case platform.PointerMove, platform.PointerDown, platform.PointerUp, platform.PointerScroll,
			platform.FileDragOver, platform.FileDrop:
			ev.X, ev.Y = (ev.X-h.fit.x)/h.fit.scale, (ev.Y-h.fit.y)/h.fit.scale
			ev.DX, ev.DY = ev.DX/h.fit.scale, ev.DY/h.fit.scale
		}
		return window.event(ev)
	}
	// Capture the configured viewport at its scale, including the inspector
	// when open, rather than the image fitted into the actual host window.
	window.conn.Capture = func() (int, int, []byte) {
		window.rt.runFrame()
		m := &h.pixels.Image
		return m.W, m.H, m.RGBA()
	}
}

func (h *previewHost) present(s *scene.Scene) {
	h.pixels.Render(s)
	w, ht, scale := h.host.size()
	if w <= 0 || ht <= 0 {
		return
	}
	c := h.rt.insp.preview.config
	h.syncFit()
	m := &h.pixels.Image
	// A scene image's dimensions are immutable: GPU texture caches key by
	// its ID. A new viewport or DPI therefore needs a new image identity.
	if h.image == nil || h.image.W != m.W || h.image.H != m.H {
		h.image = scene.NewImageRGBA(m.W, m.H, nil)
	}
	img := h.image
	img.W, img.H = m.W, m.H
	if cap(img.Pix) < 4*m.W*m.H {
		img.Pix = make([]byte, 4*m.W*m.H)
	} else {
		img.Pix = img.Pix[:4*m.W*m.H]
	}
	// Convert the raster's BGRA to the scene image's RGBA, reusing storage.
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = m.Pix[i+2], m.Pix[i+1], m.Pix[i], m.Pix[i+3]
	}
	img.Changed()
	h.presented.Reset(int(math.Ceil(float64(w*scale))), int(math.Ceil(float64(ht*scale))), scene.Color{R: 72, G: 72, B: 78, A: 255})
	h.presented.Scale = scale
	r := h.fit.rect(platform.RectF{W: float64(c.Width), H: float64(c.Height)})
	h.presented.Ops = append(h.presented.Ops, scene.Op{Kind: scene.OpImage, Image: img,
		Rect: scene.Rect{X: float32(r.X) * scale, Y: float32(r.Y) * scale, W: float32(r.W) * scale, H: float32(r.H) * scale},
		Src:  scene.Rect{W: float32(m.W), H: float32(m.H)}})
	h.host.present(&h.presented)
}

// syncFit runs before layout so a stationary pointer follows a viewport
// that moves or changes size. Events between frames use the last fit shown.
func (h *previewHost) syncFit() {
	w, ht, _ := h.host.size()
	if w <= 0 || ht <= 0 {
		return
	}
	c := h.rt.insp.preview.config
	fit := fitPreview(float64(w), float64(ht), float64(c.Width), float64(c.Height))
	if fit == h.fit {
		return
	}
	h.rt.redraw = false
	if h.rt.pointerIn {
		x, y := h.fit.x+float64(h.rt.pointerX)*h.fit.scale, h.fit.y+float64(h.rt.pointerY)*h.fit.scale
		h.rt.pointerX, h.rt.pointerY = float32((x-fit.x)/fit.scale), float32((y-fit.y)/fit.scale)
	}
	if h.rt.ime.state.Active {
		t := h.rt.ime.state
		t.Caret = fit.rect(t.Caret)
		h.host.setTextInput(t)
	}
	h.fit = fit
}

func (h *previewHost) setTextInput(t platform.TextInputState) {
	t.Caret = h.fit.rect(t.Caret)
	h.host.setTextInput(t)
}

func (h *previewHost) updateAccessibility(t *platform.AccessTree) {
	h.access = *t
	h.nodes = append(h.nodes[:0], t.Nodes...)
	for i := range h.nodes {
		h.nodes[i].Bounds = h.fit.rect(h.nodes[i].Bounds)
	}
	h.access.Nodes = h.nodes
	h.host.updateAccessibility(&h.access)
}

func (h *previewHost) popupMenu(m *platform.Menu, x, y float32, chosen func(int)) {
	f := h.fit
	h.host.popupMenu(m, float32(f.x)+x*float32(f.scale), float32(f.y)+y*float32(f.scale), chosen)
}

// The simulated viewport has no native title bar controls inside it.
func (h *previewHost) titleBar() TitleBar { return TitleBar{} }
