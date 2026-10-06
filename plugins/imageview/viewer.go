package imageview

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/egoist/mygo/ui"
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"sync"
)

var ErrClosed = errors.New("imageview: viewer closed")

const maxPixels = 64 << 20

// State is a snapshot of the viewer. Err reports the last loading failure.
type State struct {
	Width, Height   int
	Transform       Transform
	Loading, Closed bool
	Err             error
}

// Viewer owns a decoded image and its viewport state. All its methods are
// goroutine-safe. Show it in one View at a time and Close it when its owner closes.
type Viewer struct {
	mu         sync.Mutex
	state      State
	source     *image.RGBA
	bitmap     *ui.Bitmap
	generation uint64
	invalidate func()
}

// New creates an empty viewer whose zero transform fits its content.
func New() *Viewer { return &Viewer{} }

// State returns a copy of the current loading and viewport state.
func (v *Viewer) State() State { v.mu.Lock(); defer v.mu.Unlock(); return v.state }
func (v *Viewer) change(fn func()) {
	v.mu.Lock()
	if !v.state.Closed {
		fn()
	}
	notify := v.invalidate
	v.mu.Unlock()
	if notify != nil {
		notify()
	}
}

// SetImage copies img, resetting rotation, zoom and pan. nil clears the image.
func (v *Viewer) SetImage(img image.Image) error {
	if img == nil {
		v.change(func() { v.generation++; v.source = nil; v.bitmap = nil; v.state = State{} })
		return nil
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 || w > maxPixels/h {
		return errors.New("imageview: image exceeds 64 million pixels or has an empty size")
	}
	copyImage := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(copyImage, copyImage.Bounds(), img, b.Min, draw.Src)
	bitmap := ui.NewBitmap(copyImage)
	v.mu.Lock()
	if v.state.Closed {
		v.mu.Unlock()
		return ErrClosed
	}
	v.generation++
	v.source = copyImage
	v.bitmap = bitmap
	v.state = State{Width: w, Height: h}
	notify := v.invalidate
	v.mu.Unlock()
	if notify != nil {
		notify()
	}
	return nil
}

// Load decodes PNG, JPEG, GIF (first frame), WebP or BMP. JPEG EXIF orientation
// is respected. Call from a goroutine for large files; Views update on completion.
// A newer load or Close supersedes an older in-flight load.
func (v *Viewer) Load(data []byte) error {
	v.mu.Lock()
	if v.state.Closed {
		v.mu.Unlock()
		return ErrClosed
	}
	v.generation++
	generation := v.generation
	v.state.Loading = true
	v.state.Err = nil
	notify := v.invalidate
	v.mu.Unlock()
	if notify != nil {
		notify()
	}
	var cfg image.Config
	var err error
	if len(data) > 256<<20 {
		err = errors.New("imageview: encoded image exceeds 256 MiB")
	} else {
		cfg, _, err = image.DecodeConfig(bytes.NewReader(data))
	}
	if err == nil && (cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxPixels/cfg.Height) {
		err = errors.New("imageview: image exceeds 64 million pixels")
	}
	var src image.Image
	if err == nil {
		src, _, err = image.Decode(bytes.NewReader(data))
	}
	var rgba *image.RGBA
	var bitmap *ui.Bitmap
	if err == nil {
		b := src.Bounds()
		rgba = image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(rgba, rgba.Bounds(), src, b.Min, draw.Src)
		rgba = orient(rgba, exifOrientation(data))
		bitmap = ui.NewBitmap(rgba)
	}
	v.mu.Lock()
	if v.state.Closed {
		v.mu.Unlock()
		return ErrClosed
	}
	if generation != v.generation {
		v.mu.Unlock()
		return nil
	}
	v.state = State{Err: err}
	v.source = rgba
	v.bitmap = bitmap
	if rgba != nil {
		v.state.Width = rgba.Bounds().Dx()
		v.state.Height = rgba.Bounds().Dy()
	}
	notify = v.invalidate
	v.mu.Unlock()
	if notify != nil {
		notify()
	}
	return err
}

// Open reads and decodes a local image, limited to 256 MiB of encoded data.
func (v *Viewer) Open(path string) error {
	f, err := os.Open(path)
	var data []byte
	if err == nil {
		data, err = io.ReadAll(io.LimitReader(f, (256<<20)+1))
		_ = f.Close()
	}
	if err != nil {
		v.change(func() {
			v.generation++
			v.state.Err = err
			v.state.Loading = false
			v.source = nil
			v.bitmap = nil
			v.state.Width = 0
			v.state.Height = 0
		})
		return err
	}
	return v.Load(data)
}

// SetFit chooses the fit mode and resets zoom/pan, preserving rotation.
func (v *Viewer) SetFit(fit FitMode) {
	v.change(func() { v.state.Transform = Transform{Fit: fit, Rotation: v.state.Transform.Rotation} })
}

// SetZoom multiplies the fit scale. Invalid values are ignored.
func (v *Viewer) SetZoom(zoom float64) {
	if finite(zoom) && zoom > 0 {
		v.change(func() { v.state.Transform.Zoom = max(1.0/64, min(64, zoom)) })
	}
}

// Pan moves the image by DIPs; a showing view clamps it at its edges.
func (v *Viewer) Pan(dx, dy float64) {
	if finite(dx) && finite(dy) {
		v.change(func() { v.state.Transform.PanX += dx; v.state.Transform.PanY += dy })
	}
}

// Rotate turns clockwise by quarterTurns. It retains the decoded source so
// repeated turns never lose quality. Rotated bitmaps are made only on a turn.
func (v *Viewer) Rotate(quarterTurns int) {
	v.change(func() {
		t := &v.state.Transform
		t.Rotation = ((t.Rotation+quarterTurns)%4 + 4) % 4
		t.PanX = 0
		t.PanY = 0
		if v.source != nil {
			v.bitmap = ui.NewBitmap(turn(v.source, t.Rotation))
		}
	})
}

// Close releases the image, cancels pending loads and detaches invalidation.
func (v *Viewer) Close() {
	v.mu.Lock()
	if v.state.Closed {
		v.mu.Unlock()
		return
	}
	v.generation++
	v.source = nil
	v.bitmap = nil
	v.state = State{Closed: true}
	notify := v.invalidate
	v.invalidate = nil
	v.mu.Unlock()
	if notify != nil {
		notify()
	}
}

// View shows the image. Drag pans; the wheel pans; Cmd/Ctrl+wheel zooms
// around the pointer. +/− zoom, 0 fits, 1 shows actual size, and R rotates.
func View(c *ui.Context, v *Viewer) *ui.Element {
	e := ui.Box(c).Focusable().Cursor(ui.CursorGrab).Clip().Label("Image viewer")
	v.mu.Lock()
	if !v.state.Closed {
		v.invalidate = c.Invalidate
		bounds := e.Bounds()
		if v.state.Width > 0 && bounds.W > 0 && bounds.H > 0 {
			v.state.Transform.Clamp(float64(v.state.Width), float64(v.state.Height), ui.Rect{W: bounds.W, H: bounds.H})
		}
	}
	v.mu.Unlock()
	type drag struct {
		active bool
		x, y   float32
	}
	d := ui.Local(e, "imageview-drag", func() drag { return drag{} })
	e.HandleInput(func(ev ui.InputEvent) bool {
		b := e.Bounds()
		vp := ui.Rect{W: b.W, H: b.H}
		switch ev.Kind {
		case ui.InputPointerDown:
			if ev.Button == 0 {
				d.active = true
				d.x = ev.X
				d.y = ev.Y
				return true
			}
		case ui.InputPointerUp:
			if d.active {
				d.active = false
				return true
			}
		case ui.InputPointerMove:
			if d.active {
				v.Pan(float64(ev.X-d.x), float64(ev.Y-d.y))
				d.x = ev.X
				d.y = ev.Y
				return true
			}
		case ui.InputScroll:
			if ev.Mods&(ui.Ctrl|ui.Super) != 0 {
				v.change(func() {
					v.state.Transform.ZoomAt(powZoom(ev.DY), float64(ev.X), float64(ev.Y), float64(v.state.Width), float64(v.state.Height), vp)
				})
			} else {
				v.Pan(-float64(ev.DX), -float64(ev.DY))
			}
			return true
		case ui.InputKeyDown:
			switch ev.Key {
			case ui.KeyEqual:
				v.SetZoom(v.State().Transform.zoom() * 1.25)
			case ui.KeyMinus:
				v.SetZoom(v.State().Transform.zoom() / 1.25)
			case ui.Key0:
				v.SetFit(FitContain)
			case ui.Key1:
				v.SetFit(FitActual)
			case ui.KeyR:
				v.Rotate(1)
			case ui.KeyLeft:
				v.Pan(40, 0)
			case ui.KeyRight:
				v.Pan(-40, 0)
			case ui.KeyUp:
				v.Pan(0, 40)
			case ui.KeyDown:
				v.Pan(0, -40)
			default:
				return false
			}
			return true
		}
		return false
	})
	theme := c.Theme()
	return e.Draw(func(p *ui.Painter, r ui.Rect) {
		v.mu.Lock()
		state, bitmap := v.state, v.bitmap
		v.mu.Unlock()
		if bitmap != nil {
			p.Image(bitmap, state.Transform.Rect(float64(state.Width), float64(state.Height), r), ui.FillBox)
		} else {
			msg := "No image"
			if state.Loading {
				msg = "Loading image…"
			}
			if state.Err != nil {
				msg = fmt.Sprintf("Image: %v", state.Err)
			}
			if state.Closed {
				msg = "Image viewer closed"
			}
			p.Text(r.X+12, r.Y+12, msg, 13, theme.TextMuted)
		}
	})
}
