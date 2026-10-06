package ui

import (
	"image/color"
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

func TestPrintPagesHaveIndependentStateAndLayout(t *testing.T) {
	var sizes []PrintLayout
	view := func(c *Context) {
		w, h := c.Size()
		sizes = append(sizes, PrintLayout{w, h})
		if c.Theme().Dark || !c.Preferences().ReduceMotion {
			t.Error("printing did not isolate appearance/motion")
		}
		e := Box(c).Key("page-state")
		value := Local(e, "count", func() int { return 0 })
		*value++
		if *value != 1 {
			t.Error("page shares another page's view state")
		}
		e.Size(w+100, h+100).Background(RGB(0, 200, 0))
	}
	p := PrintPages(func(l PrintLayout) []func(*Context) {
		if l.Width != 80 || l.Height != 60 {
			t.Error(l)
		}
		return []func(*Context){view, view}
	})
	pages, err := p.RenderPrintPages(platform.PrintLayout{Width: 100, Height: 80, Top: 10, Right: 10, Bottom: 10, Left: 10, Scale: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 2 || len(sizes) != 2 || sizes[0] != (PrintLayout{80, 60}) {
		t.Fatal("page layout", sizes)
	}
	for _, img := range pages {
		if p := img.RGBAAt(21, 21); p != (color.RGBA{0, 200, 0, 255}) {
			t.Fatal("page body", p)
		}
		if p := img.RGBAAt(190, 150); p != (color.RGBA{255, 255, 255, 255}) {
			t.Fatal("overflow painted over margins", p)
		}
	}
}

func TestPrintRejectsInvalidViewsBeforeRendering(t *testing.T) {
	l := platform.PrintLayout{Width: 600, Height: 800, Scale: 2}
	for _, p := range []*PrintContent{nil, PrintPages(nil), PrintPages(func(PrintLayout) []func(*Context) { return nil }), PrintView(nil),
		PrintPages(func(PrintLayout) []func(*Context) { return make([]func(*Context), 1000) }),
	} {
		if _, err := p.RenderPrintPages(l); err == nil {
			t.Fatal("invalid print views accepted")
		}
	}
}
