package mygo

import (
	"bytes"
	"errors"
	"image/color"
	"math"
	"regexp"
	"testing"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/unsupported"
	"github.com/egoist/mygo/ui"
)

func TestNativePrintPagesAndPDF(t *testing.T) {
	w, fw := testWindow(t, WindowOptions{Title: "Report", Width: 1300, Height: 600, Content: ui.View(func(c *ui.Context) { ui.Text(c, "editor") })})
	var size ui.PrintLayout
	printable := ui.PrintPages(func(l ui.PrintLayout) []func(*ui.Context) {
		if !isMainThread() {
			t.Error("pagination is not on main")
		}
		size = l
		return []func(*ui.Context){
			func(c *ui.Context) { ui.Box(c).Fill().Background(ui.RGB(255, 0, 0)) },
			func(c *ui.Context) { ui.Box(c).Fill().Background(ui.RGB(0, 0, 255)) },
		}
	})
	opts := PrintOptions{PageSize: PageSize{2, 3}, Margins: &Margins{Top: .25, Right: .25, Bottom: .25, Left: .25}, DPI: 72}
	if err := w.Print(printable, opts); err != nil {
		t.Fatal(err)
	}
	job := onMainValue(func() *platform.PrintJob { return fb.PrintJob })
	if onMainValue(func() platform.Window { return fb.PrintParent }) != fw {
		t.Fatal("print dialog has wrong parent")
	}
	if job.Title != "Report" || job.Width != 144 || job.Height != 216 || len(job.Pages) != 2 {
		t.Fatalf("job: %+v", job)
	}
	if size.Width != 108 || size.Height != 180 {
		t.Fatal("pagination used window size", size)
	}
	for i, img := range job.Pages {
		if img.Rect.Dx() != 144 || img.Rect.Dy() != 216 {
			t.Fatal(img.Rect)
		}
		if p := img.RGBAAt(2, 2); p != (color.RGBA{255, 255, 255, 255}) {
			t.Fatal("page margin is not white", p)
		}
		want := color.RGBA{255, 0, 0, 255}
		if i == 1 {
			want = color.RGBA{0, 0, 255, 255}
		}
		if p := img.RGBAAt(24, 24); p != want {
			t.Fatalf("page %d pixels: %+v", i, p)
		}
	}
	pdf, err := w.PrintToPDF(printable, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pdf, job.PDF) {
		t.Fatal("printing and export use different page output")
	}
	if n := len(regexp.MustCompile(`/Type /Page\b`).FindAll(pdf, -1)); n != 2 {
		t.Fatalf("PDF page count: %d", n)
	}
	if !bytes.Contains(pdf, []byte("/MediaBox [0 0 144.000000 216.000000]")) {
		t.Fatal("wrong physical PDF page size")
	}
	// Display geometry has no influence on the paginated output.
	w.SetContentSize(300, 400)
	again, err := w.PrintToPDF(printable, opts)
	if err != nil || !bytes.Equal(pdf, again) {
		t.Fatal("window resize changed printed pages", err)
	}
}

func TestNativePrintFailuresAndDefaults(t *testing.T) {
	w, _ := testWindow(t, WindowOptions{Content: ui.View(func(c *ui.Context) {})})
	view := ui.PrintView(func(c *ui.Context) { ui.Text(c, "Native PDF").FontSize(12) })
	layout, err := printLayout(PrintOptions{})
	if err != nil || layout.Width != 612 || layout.Height != 792 || layout.Top != 28.8 || layout.Scale != 2 {
		t.Fatalf("defaults: %+v %v", layout, err)
	}
	landscape, err := printLayout(PrintOptions{PageSize: PageA4, Landscape: true, DPI: 72, Margins: &Margins{}})
	if err != nil || landscape.Width != PageA4.Height*72 || landscape.Height != PageA4.Width*72 || landscape.Top != 0 {
		t.Fatalf("landscape: %+v %v", landscape, err)
	}
	for _, o := range []PrintOptions{
		{PageSize: PageSize{-1, 2}}, {PageSize: PageSize{2, 0}}, {PageSize: PageSize{math.NaN(), 2}},
		{Margins: &Margins{Left: -1}}, {Margins: &Margins{Top: math.Inf(1)}}, {Margins: &Margins{Left: 9}}, {DPI: 71}, {DPI: 601}, {PageSize: PageSize{1000, 1000}},
	} {
		if _, err := w.PrintToPDF(view, o); err == nil {
			t.Fatalf("accepted invalid print options: %+v", o)
		}
	}
	if _, err := w.PrintToPDF(nil, PrintOptions{}); err == nil {
		t.Fatal("nil printable accepted")
	}
	if _, err := w.PrintToPDF(ui.PrintPages(func(ui.PrintLayout) []func(*ui.Context) { return nil }), PrintOptions{}); err == nil {
		t.Fatal("empty page list accepted")
	}
	t.Cleanup(func() { onMain(func() { fb.PrintError = nil; fb.PrintJob = nil; fb.PrintParent = nil }) })
	onMain(func() { fb.PrintError = ErrPrintCanceled })
	if err := w.Print(view, PrintOptions{}); !errors.Is(err, ErrPrintCanceled) {
		t.Fatal(err)
	}
	fail := errors.New("printer offline")
	onMain(func() { fb.PrintError = fail })
	if err := w.Print(view, PrintOptions{}); !errors.Is(err, fail) {
		t.Fatal(err)
	}
	w.Destroy()
	if _, err := w.PrintToPDF(view, PrintOptions{}); !errors.Is(err, errDestroyed) {
		t.Fatal("destroyed window export:", err)
	}
	if err := w.Print(view, PrintOptions{}); !errors.Is(err, errDestroyed) {
		t.Fatal("destroyed window print:", err)
	}
	called := 0
	unsupported.New().PrintContent(nil, nil, func(err error) {
		called++
		if !errors.Is(err, platform.ErrUnsupported) {
			t.Error(err)
		}
	})
	if called != 1 {
		t.Fatal("unsupported print callback count", called)
	}
}

func TestWebviewPrintContractUnchanged(t *testing.T) {
	w, fw := testWindow(t, WindowOptions{})
	w.Page().Print()
	pdf, err := w.Page().PrintToPDF(PDFOptions{PageSize: PageA4, Landscape: true, Background: true})
	if err != nil || !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Fatal(err)
	}
	if p := onMainValue(func() platform.PDFOptions { return fw.PDF }); !p.Landscape || !p.Background || p.PageWidth != PageA4.Width || p.PageHeight != PageA4.Height {
		t.Fatal(p)
	}
}
