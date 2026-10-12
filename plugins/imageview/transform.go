// Package imageview provides an optional image viewer for MyGo native UI.
package imageview

import (
	"github.com/egoist/mygo/ui"
	"math"
)

// FitMode determines how content fits its viewport.
type FitMode uint8

const (
	// FitContain fits the whole image. It follows viewport size changes.
	FitContain FitMode = iota
	FitWidth
	// FitActual shows one image pixel per DIP before Zoom.
	FitActual
)

// Transform is a viewport transform. Its zero value fits the whole image.
// Zoom multiplies the fit scale (zero means one). Pan is in DIPs from center.
// Rotation is clockwise in quarter turns. A Transform is a value; callers
// sharing one between goroutines must synchronize it.
type Transform struct {
	Fit        FitMode
	Zoom       float64
	PanX, PanY float64
	Rotation   int
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func (t Transform) zoom() float64 {
	if !finite(t.Zoom) || t.Zoom <= 0 {
		return 1
	}
	return max(1.0/64, min(64, t.Zoom))
}

// Rect returns the displayed content rectangle in viewport coordinates.
// width and height are the unrotated content size, in pixels or document DIPs.
func (t Transform) Rect(width, height float64, viewport ui.Rect) ui.Rect {
	if width <= 0 || height <= 0 || !finite(width+height) || viewport.W <= 0 || viewport.H <= 0 {
		return ui.Rect{}
	}
	if t.Rotation%2 != 0 {
		width, height = height, width
	}
	scale := 1.0
	switch t.Fit {
	case FitContain:
		scale = min(float64(viewport.W)/width, float64(viewport.H)/height)
	case FitWidth:
		scale = float64(viewport.W) / width
	}
	scale *= t.zoom()
	w, h := width*scale, height*scale
	x, y := t.PanX, t.PanY
	if !finite(x) {
		x = 0
	}
	if !finite(y) {
		y = 0
	}
	// Keep the image reachable, and center axes that fit.
	x = max(-max(0, (w-float64(viewport.W))/2), min(max(0, (w-float64(viewport.W))/2), x))
	y = max(-max(0, (h-float64(viewport.H))/2), min(max(0, (h-float64(viewport.H))/2), y))
	return ui.Rect{X: viewport.X + float32((float64(viewport.W)-w)/2+x), Y: viewport.Y + float32((float64(viewport.H)-h)/2+y), W: float32(w), H: float32(h)}
}

// ZoomAt zooms around a point in the viewport, preserving the image point
// under it until an edge is reached. Invalid factors leave the transform alone.
func (t *Transform) ZoomAt(factor, x, y, width, height float64, viewport ui.Rect) {
	if !finite(factor) || factor <= 0 {
		return
	}
	old := t.Rect(width, height, viewport)
	if old.W <= 0 || old.H <= 0 {
		return
	}
	t.Zoom = max(1.0/64, min(64, t.zoom()*factor))
	next := t.Rect(width, height, viewport)
	t.PanX += x - float64(next.X) - (x-float64(old.X))*float64(next.W/old.W)
	t.PanY += y - float64(next.Y) - (y-float64(old.Y))*float64(next.H/old.H)
	t.Clamp(width, height, viewport)
}

// Clamp keeps pan within the content's edges.
func (t *Transform) Clamp(width, height float64, viewport ui.Rect) {
	r := t.Rect(width, height, viewport)
	t.PanX = float64(r.X - viewport.X - (viewport.W-r.W)/2)
	t.PanY = float64(r.Y - viewport.Y - (viewport.H-r.H)/2)
}
