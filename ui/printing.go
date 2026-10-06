package ui

import (
	"errors"
	"image"
	"image/color"
	"image/draw"
	"math"

	"github.com/egoist/mygo/internal/platform"
)

// PrintLayout is the printable content area in points (72 per inch). One
// ui layout unit is one point when printing. Window size, display scale,
// scroll position, inspector and system appearance do not affect pages.
type PrintLayout struct{ Width, Height float32 }

// PrintContent is a printable native document, for Window.Print/PrintToPDF.
// Each page has an isolated view state and uses the CPU renderer in light
// appearance with motion disabled. Page views should build static content;
// they can use the full native drawing API, including text, images and paths.
type PrintContent struct {
	paginate func(PrintLayout) []func(*Context)
}

// PrintView defines a single printable page. Content outside its area clips
// at the margins. Use PrintPages to paginate a larger document.
func PrintView(view func(*Context)) *PrintContent {
	return PrintPages(func(PrintLayout) []func(*Context) { return []func(*Context){view} })
}

// PrintPages calls paginate on the main thread with the page's available
// content size, then renders the returned views in order. The app chooses
// page breaks explicitly, for example by grouping rows or paragraphs that
// fit within Height. Returning no pages is an error. The pagination callback
// should capture a snapshot so all pages describe the same document revision.
func PrintPages(paginate func(PrintLayout) []func(*Context)) *PrintContent {
	return &PrintContent{paginate: paginate}
}

// RenderPrintPages is the rendering hook used by package mygo. Call through
// Window.Print or Window.PrintToPDF, which validate options and marshal to
// the main thread. No live window state or input is used while rendering.
func (p *PrintContent) RenderPrintPages(l platform.PrintLayout) ([]*image.RGBA, error) {
	if p == nil || p.paginate == nil {
		return nil, errors.New("ui: nil print pagination callback")
	}
	area := PrintLayout{float32(l.Width - l.Left - l.Right), float32(l.Height - l.Top - l.Bottom)}
	if area.Width <= 0 || area.Height <= 0 || l.Scale <= 0 {
		return nil, errors.New("ui: invalid print layout")
	}
	views := p.paginate(area)
	if len(views) == 0 || len(views) > platform.MaxPrintPages {
		return nil, errors.New("ui: printing requires between 1 and 1000 pages")
	}
	w, h := int(math.Ceil(l.Width*l.Scale)), int(math.Ceil(l.Height*l.Scale))
	if int64(w)*int64(h)*int64(len(views)) > platform.MaxPrintPixels {
		return nil, errors.New("ui: print job exceeds the pixel limit; reduce DPI or page count")
	}
	for _, view := range views {
		if view == nil {
			return nil, errors.New("ui: nil print page view")
		}
	}
	pages := make([]*image.RGBA, 0, len(views))
	for _, view := range views {
		host := &headless{w: area.Width, h: area.Height, scale: float32(l.Scale), prefs: platform.Preferences{ReduceMotion: true, TextScale: 1}}
		rt := newRuntime(view, host)
		rt.strict = true
		rt.runFrame()
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.Draw(img, img.Rect, &image.Uniform{color.White}, image.Point{}, draw.Src)
		src := host.image()
		at := image.Pt(int(math.Round(l.Left*l.Scale)), int(math.Round(l.Top*l.Scale)))
		draw.Draw(img, src.Rect.Add(at), src, src.Rect.Min, draw.Over)
		rt.close()
		host.img.Release()
		pages = append(pages, img)
	}
	return pages, nil
}
