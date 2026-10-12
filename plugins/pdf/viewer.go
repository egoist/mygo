package pdf

import (
	"context"
	"errors"
	"github.com/egoist/mygo/plugins/imageview"
	"github.com/egoist/mygo/ui"
	"math"
	"strings"
	"sync"
)

func validNumber(n float64) bool { return !math.IsNaN(n) && !math.IsInf(n, 0) }

// State snapshots the viewport and last render. Page is zero-based. Selection
// is a half-open PDFium character range on that page.
type State struct {
	Page, Pages                  int
	Transform                    imageview.Transform
	Loading, Closed              bool
	Err                          error
	SelectionStart, SelectionEnd int
	Match, Matches               int
}
type renderRequest struct{ page, w, h int }

// Viewer controls one Document. It owns its worker and cached page, but not the
// document: Close the viewer before closing its document. Methods are safe from
// any goroutine. A viewer shows in one View at a time.
type Viewer struct {
	reveal              bool
	mu                  sync.Mutex
	doc                 *Document
	pages               []Page
	state               State
	bitmap              *ui.Bitmap
	text                Text
	matches             []Match
	query               uint64
	requested, rendered renderRequest
	wake                chan struct{}
	stop                chan struct{}
	done                chan struct{}
	invalidate          func()
}

func NewViewer(d *Document) (*Viewer, error) {
	if d == nil || !d.Capabilities().Render {
		return nil, ErrClosed
	}
	v := &Viewer{doc: d, pages: d.Pages(), wake: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{})}
	v.state.Pages = len(v.pages)
	v.state.SelectionStart = -1
	v.state.SelectionEnd = -1
	v.state.Match = -1
	v.rendered.page = -1
	go v.run()
	return v, nil
}
func (v *Viewer) State() State { v.mu.Lock(); defer v.mu.Unlock(); return v.state }
func (v *Viewer) update(fn func()) {
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
func (v *Viewer) setPage(page int) {
	if page != v.state.Page {
		v.state.Page = page
		v.state.Transform.PanX = 0
		v.state.Transform.PanY = 0
		v.state.SelectionStart = -1
		v.state.SelectionEnd = -1
		v.bitmap = nil
		v.text = Text{}
		v.state.Loading = true
		v.state.Err = nil
		v.requested = renderRequest{page: -1}
	}
}
func (v *Viewer) SetPage(page int) error {
	v.mu.Lock()
	if v.state.Closed {
		v.mu.Unlock()
		return ErrClosed
	}
	if page < 0 || page >= len(v.pages) {
		v.mu.Unlock()
		return errors.New("pdf: page index out of range")
	}
	v.setPage(page)
	notify := v.invalidate
	v.mu.Unlock()
	if notify != nil {
		notify()
	}
	return nil
}

// SetZoom multiplies the selected fit scale; valid zooms are 1/64 through 64.
func (v *Viewer) SetZoom(zoom float64) error {
	if !validNumber(zoom) || zoom < 1.0/64 || zoom > 64 {
		return errors.New("pdf: zoom must be between 1/64 and 64")
	}
	if v.State().Closed {
		return ErrClosed
	}
	v.update(func() { v.state.Transform.Zoom = zoom })
	return nil
}
func (v *Viewer) SetFit(fit imageview.FitMode) {
	v.update(func() { v.state.Transform = imageview.Transform{Fit: fit} })
}

// Find searches the document and selects the first match. Cancellation or a
// later search prevents an earlier result from replacing newer results.
func (v *Viewer) Find(ctx context.Context, query string, matchCase bool) ([]Match, error) {
	v.mu.Lock()
	if v.state.Closed {
		v.mu.Unlock()
		return nil, ErrClosed
	}
	v.query++
	id := v.query
	v.mu.Unlock()
	matches, err := v.doc.Search(ctx, query, matchCase)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	v.update(func() {
		if id != v.query {
			return
		}
		v.matches = matches
		v.state.Matches = len(matches)
		v.state.Match = -1
		if len(matches) > 0 {
			v.chooseMatch(0)
		}
		v.state.Err = err
	})
	return matches, err
}
func (v *Viewer) chooseMatch(index int) {
	m := v.matches[index]
	v.setPage(m.Page)
	v.state.Match = index
	v.reveal = true
	v.state.SelectionStart = m.Start
	v.state.SelectionEnd = m.Start + m.Count
}

// NextMatch wraps through the search results, backward when delta is negative.
func (v *Viewer) NextMatch(delta int) {
	v.update(func() {
		if len(v.matches) > 0 {
			i := ((v.state.Match+delta)%len(v.matches) + len(v.matches)) % len(v.matches)
			v.chooseMatch(i)
		}
	})
}
func (v *Viewer) SelectedText() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.doc.caps.Selection {
		return ""
	}
	from, to := v.state.SelectionStart, v.state.SelectionEnd
	if from < 0 || to <= from || from >= len(v.text.Characters) {
		return ""
	}
	return (Text{v.text.Characters[from:min(to, len(v.text.Characters))]}).String()
}

// Close stops scheduling frames and releases the cached page. It never waits
// for the rendering worker on the UI thread. Done closes after that worker exits.
func (v *Viewer) Close() {
	v.mu.Lock()
	if v.state.Closed {
		v.mu.Unlock()
		return
	}
	v.state.Closed = true
	v.state.Loading = false
	v.bitmap = nil
	v.text = Text{}
	v.matches = nil
	v.query++
	close(v.stop)
	notify := v.invalidate
	v.invalidate = nil
	v.mu.Unlock()
	if notify != nil {
		notify()
	}
}
func (v *Viewer) Done() <-chan struct{} { return v.done }
func (v *Viewer) request(r renderRequest) {
	v.mu.Lock()
	if v.state.Closed || r == v.requested {
		v.mu.Unlock()
		return
	}
	v.requested = r
	v.state.Loading = true
	v.mu.Unlock()
	select {
	case v.wake <- struct{}{}:
	default:
	}
}
func (v *Viewer) run() {
	defer close(v.done)
	for {
		select {
		case <-v.stop:
			return
		case <-v.wake:
		}
		v.mu.Lock()
		r := v.requested
		closed := v.state.Closed
		v.mu.Unlock()
		if closed {
			return
		}
		if r.page < 0 {
			continue
		}
		img, err := v.doc.Render(r.page, r.w, r.h, 0)
		var text Text
		var bitmap *ui.Bitmap
		if err == nil {
			bitmap = ui.NewBitmap(img)
			if caps := v.doc.Capabilities(); caps.Selection || caps.Search {
				text, err = v.doc.pageTextForView(r.page)
			}
		}
		v.mu.Lock()
		if !v.state.Closed && r == v.requested && r.page == v.state.Page {
			v.bitmap = bitmap
			v.text = text
			v.rendered = r
			v.state.Loading = false
			v.state.Err = err
		}
		notify := v.invalidate
		v.mu.Unlock()
		if notify != nil {
			notify()
		}
	}
}

// View includes page and zoom controls plus a selectable page viewport. Drag
// selects text, Alt+drag pans. The wheel pans; Cmd/Ctrl+wheel zooms. Page Up/Down
// navigate, +/− zoom, 0 fits, Cmd/Ctrl+C and the Edit Copy role copy selection.
func View(c *ui.Context, v *Viewer) *ui.Element {
	v.mu.Lock()
	if !v.state.Closed {
		v.invalidate = c.Invalidate
	}
	state := v.state
	v.mu.Unlock()
	caps := v.doc.Capabilities()
	root := ui.Column(c).Gap(8)
	root.Children(func() {
		ui.Row(c).Gap(8).Children(func() {
			if ui.Button(c, "Previous page").Disabled(state.Closed || state.Page == 0).Clicked() {
				_ = v.SetPage(state.Page - 1)
			}
			ui.Textf(c, "%d / %d", state.Page+1, state.Pages)
			if ui.Button(c, "Next page").Disabled(state.Closed || state.Page+1 >= state.Pages).Clicked() {
				_ = v.SetPage(state.Page + 1)
			}
			if ui.Button(c, "−").Label("Zoom out").Disabled(state.Closed).Clicked() {
				_ = v.SetZoom(max(1.0/64, zoomOf(state.Transform)/1.25))
			}
			if ui.Button(c, "+").Label("Zoom in").Disabled(state.Closed).Clicked() {
				_ = v.SetZoom(min(64, zoomOf(state.Transform)*1.25))
			}
			if ui.Button(c, "Fit").Disabled(state.Closed).Clicked() {
				v.SetFit(imageview.FitContain)
			}
			if ui.Button(c, "Width").Disabled(state.Closed).Clicked() {
				v.SetFit(imageview.FitWidth)
			}
		})
		e := ui.Box(c).Grow(1).MinHeight(80).Focusable().Clip().Label("PDF page").Cursor(ui.CursorText)
		if !caps.Selection {
			e.Cursor(ui.CursorGrab)
		}
		bounds := e.Bounds()
		vp := ui.Rect{W: bounds.W, H: bounds.H}
		v.mu.Lock()
		v.revealSelection(vp)
		v.mu.Unlock()
		size := v.pages[state.Page]
		display := state.Transform.Rect(size.Width, size.Height, vp)
		// Rasterize off the UI thread. Resolution follows zoom and viewport, with
		// a bounded latest-request queue while the user resizes or zooms quickly.
		w, h := max(1, int(math.Ceil(float64(display.W)*2))), max(1, int(math.Ceil(float64(display.H)*2)))
		if vp.W == 0 {
			w, h = int(size.Width*2), int(size.Height*2)
		}
		shrink := max(1, max(float64(w)/8192, float64(h)/8192))
		shrink = max(shrink, math.Sqrt(float64(w)*float64(h)/(32<<20)))
		w = max(1, int(float64(w)/shrink))
		h = max(1, int(float64(h)/shrink))
		v.request(renderRequest{state.Page, w, h})
		type drag struct {
			active, pan bool
			x, y        float32
			start       int
		}
		d := ui.Local(e, "pdf-drag", func() drag { return drag{} })
		e.HandleInput(func(ev ui.InputEvent) bool {
			b := e.Bounds()
			viewport := ui.Rect{W: b.W, H: b.H}
			if (ev.Kind == ui.InputCommand && ev.Text == "copy") || (ev.Kind == ui.InputKeyDown && ev.Mods == ui.Cmd && ev.Key == ui.KeyC) {
				if caps.Selection {
					c.WriteClipboard(v.SelectedText())
					return true
				}
				return false
			}
			switch ev.Kind {
			case ui.InputKeyDown:
				s := v.State()
				switch ev.Key {
				case ui.KeyPageDown:
					_ = v.SetPage(min(s.Page+1, s.Pages-1))
				case ui.KeyPageUp:
					_ = v.SetPage(max(0, s.Page-1))
				case ui.KeyEqual:
					_ = v.SetZoom(min(64, zoomOf(s.Transform)*1.25))
				case ui.KeyMinus:
					_ = v.SetZoom(max(1.0/64, zoomOf(s.Transform)/1.25))
				case ui.Key0:
					v.SetFit(imageview.FitContain)
				default:
					return false
				}
				return true
			case ui.InputScroll:
				v.update(func() {
					t := &v.state.Transform
					if ev.Mods&(ui.Ctrl|ui.Super) != 0 {
						t.ZoomAt(math.Exp(-float64(ev.DY)/200), float64(ev.X), float64(ev.Y), size.Width, size.Height, viewport)
					} else {
						t.PanX -= float64(ev.DX)
						t.PanY -= float64(ev.DY)
						t.Clamp(size.Width, size.Height, viewport)
					}
				})
				return true
			case ui.InputPointerDown:
				if ev.Button != 0 {
					return false
				}
				d.active = true
				d.pan = ev.Mods&ui.Alt != 0 || !caps.Selection
				d.x = ev.X
				d.y = ev.Y
				if !d.pan {
					v.mu.Lock()
					d.start = v.characterAt(ev.X, ev.Y, viewport)
					v.state.SelectionStart = d.start
					v.state.SelectionEnd = d.start + 1
					v.mu.Unlock()
				}
				return true
			case ui.InputPointerMove:
				if !d.active {
					return false
				}
				v.update(func() {
					if d.pan {
						v.state.Transform.PanX += float64(ev.X - d.x)
						v.state.Transform.PanY += float64(ev.Y - d.y)
						v.state.Transform.Clamp(size.Width, size.Height, viewport)
					} else {
						i := v.characterAt(ev.X, ev.Y, viewport)
						if i >= 0 && d.start >= 0 {
							v.state.SelectionStart = min(i, d.start)
							v.state.SelectionEnd = max(i, d.start) + 1
						}
					}
				})
				d.x = ev.X
				d.y = ev.Y
				return true
			case ui.InputPointerUp:
				if d.active {
					d.active = false
					return true
				}
			}
			return false
		})
		theme := c.Theme()
		e.Draw(func(p *ui.Painter, r ui.Rect) {
			v.mu.Lock()
			s, bitmap, text := v.state, v.bitmap, v.text
			v.mu.Unlock()
			page := v.pages[s.Page]
			rect := s.Transform.Rect(page.Width, page.Height, r)
			if bitmap != nil {
				p.Image(bitmap, rect, ui.FillBox)
				for i := max(0, s.SelectionStart); i < min(s.SelectionEnd, len(text.Characters)); i++ {
					box := text.Characters[i].Bounds
					box = ui.Rect{X: rect.X + box.X*rect.W/float32(page.Width), Y: rect.Y + box.Y*rect.H/float32(page.Height), W: box.W * rect.W / float32(page.Width), H: box.H * rect.H / float32(page.Height)}
					p.Fill(box, theme.Selection, 0)
				}
			}
			if s.Err != nil {
				p.Text(r.X+12, r.Y+12, s.Err.Error(), 13, theme.Text)
			} else if s.Closed {
				p.Text(r.X+12, r.Y+12, "PDF viewer closed", 13, theme.TextMuted)
			} else if bitmap == nil {
				p.Text(r.X+12, r.Y+12, "Loading PDF page…", 13, theme.TextMuted)
			}
		})
	})
	return root
}
func zoomOf(t imageview.Transform) float64 {
	if t.Zoom == 0 {
		return 1
	}
	return t.Zoom
}
func (v *Viewer) characterAt(x, y float32, viewport ui.Rect) int {
	page := v.pages[v.state.Page]
	r := v.state.Transform.Rect(page.Width, page.Height, viewport)
	if r.W <= 0 {
		return -1
	}
	px, py := (x-r.X)*float32(page.Width)/r.W, (y-r.Y)*float32(page.Height)/r.H
	closest := -1
	distance := float32(math.Inf(1))
	for i, c := range v.text.Characters {
		b := c.Bounds
		if b.W <= 0 || b.H <= 0 || strings.TrimSpace(string(c.Rune)) == "" {
			continue
		}
		dx := max(b.X-px, max(0, px-b.X-b.W))
		dy := max(b.Y-py, max(0, py-b.Y-b.H))
		d := dx*dx + dy*dy
		if d < distance {
			distance = d
			closest = i
		}
	}
	return closest
}

// revealSelection runs during build, with the viewport from the previous frame.
// Waiting for text keeps a match on another page pending until its render loads.
func (v *Viewer) revealSelection(viewport ui.Rect) {
	if !v.reveal || v.state.Closed || viewport.W <= 0 || viewport.H <= 0 {
		return
	}
	from, to := v.state.SelectionStart, v.state.SelectionEnd
	if from < 0 || to <= from || to > len(v.text.Characters) {
		return
	}
	v.reveal = false
	x0, y0, x1, y1 := float32(math.Inf(1)), float32(math.Inf(1)), float32(math.Inf(-1)), float32(math.Inf(-1))
	for _, ch := range v.text.Characters[from:to] {
		b := ch.Bounds
		if b.W <= 0 || b.H <= 0 {
			continue
		}
		x0 = min(x0, b.X)
		y0 = min(y0, b.Y)
		x1 = max(x1, b.X+b.W)
		y1 = max(y1, b.Y+b.H)
	}
	if !validNumber(float64(x0 + y0 + x1 + y1)) {
		return
	}
	size := v.pages[v.state.Page]
	t := &v.state.Transform
	r := t.Rect(size.Width, size.Height, viewport)
	left, right := r.X+x0*r.W/float32(size.Width), r.X+x1*r.W/float32(size.Width)
	top, bottom := r.Y+y0*r.H/float32(size.Height), r.Y+y1*r.H/float32(size.Height)
	if left < viewport.X || right > viewport.X+viewport.W {
		t.PanX += float64(viewport.X + viewport.W/2 - (left+right)/2)
	}
	if top < viewport.Y || bottom > viewport.Y+viewport.H {
		t.PanY += float64(viewport.Y + viewport.H/2 - (top+bottom)/2)
	}
	t.Clamp(size.Width, size.Height, viewport)
}
