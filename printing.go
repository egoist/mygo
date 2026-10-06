package mygo

import (
	"errors"
	"image"
	"math"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/printdoc"
)

// ErrPrintCanceled means the native print dialog was canceled.
var ErrPrintCanceled = platform.ErrPrintCanceled

// PrintOptions configures native UI printing and PDF export. The zero value
// uses Letter paper, 0.4-inch margins and 144 DPI. Pages are fixed before the
// print dialog opens; changing printer paper fits each page to that paper,
// without repaginating. Backgrounds are always included. PDF output embeds
// lossless raster pages, so text is not selectable/searchable.
type PrintOptions struct {
	Title     string   // defaults to the window title
	PageSize  PageSize // inches, like PDFOptions
	Landscape bool
	Margins   *Margins // inches; nil means 0.4 inch; &Margins{} means none
	DPI       int      // 72–600; zero means 144
}

// Printable supplies native UI pages. Use ui.PrintView for a single page or
// ui.PrintPages to paginate explicitly. Only MyGo packages implement the
// rendering hook, which runs on the main thread in isolation from the window.
type Printable interface {
	RenderPrintPages(platform.PrintLayout) ([]*image.RGBA, error)
}

func printLayout(o PrintOptions) (platform.PrintLayout, error) {
	size := o.PageSize
	if size == (PageSize{}) {
		size = PageLetter
	}
	w, h := size.Width*72, size.Height*72
	if o.Landscape {
		w, h = h, w
	}
	m := Margins{0.4, 0.4, 0.4, 0.4}
	if o.Margins != nil {
		m = *o.Margins
	}
	dpi := o.DPI
	if dpi == 0 {
		dpi = 144
	}
	for _, length := range []float64{w, h, m.Top, m.Right, m.Bottom, m.Left} {
		if math.IsNaN(length) || math.IsInf(length, 0) || length < 0 {
			return platform.PrintLayout{}, errors.New("mygo: print dimensions must be finite and nonnegative")
		}
	}
	if w <= 0 || h <= 0 || (m.Left+m.Right)*72 >= w || (m.Top+m.Bottom)*72 >= h {
		return platform.PrintLayout{}, errors.New("mygo: print margins leave no content area")
	}
	if dpi < 72 || dpi > 600 {
		return platform.PrintLayout{}, errors.New("mygo: print DPI must be between 72 and 600")
	}
	scale := float64(dpi) / 72
	if w*scale > 16384 || h*scale > 16384 || math.Ceil(w*scale)*math.Ceil(h*scale) > platform.MaxPrintPixels {
		return platform.PrintLayout{}, errors.New("mygo: print page exceeds the pixel limit; reduce DPI or paper size")
	}
	return platform.PrintLayout{Width: w, Height: h, Top: m.Top * 72, Right: m.Right * 72, Bottom: m.Bottom * 72, Left: m.Left * 72, Scale: scale}, nil
}

func (w *Window) printJob(content Printable, opts PrintOptions) (*platform.PrintJob, error) {
	if content == nil {
		return nil, errors.New("mygo: nil printable content")
	}
	layout, err := printLayout(opts)
	if err != nil {
		return nil, err
	}
	job := &platform.PrintJob{Title: opts.Title, Width: layout.Width, Height: layout.Height}
	err = errLoopStopped
	onMain(func() {
		if w.native == nil {
			err = errDestroyed
			return
		}
		if job.Title == "" {
			job.Title = w.native.Title()
		}
		job.Pages, err = content.RenderPrintPages(layout)
	})
	if err != nil {
		return nil, err
	}
	// Compression is CPU work. A main-thread caller pumps the event loop
	// while it runs, just as asynchronous dialogs and page PDF export do.
	ch := make(chan error, 1)
	go func() {
		var err error
		job.PDF, err = printdoc.PDF(job)
		deliver(ch, err)
	}()
	if err := await(ch); err != nil {
		return nil, err
	}
	return job, nil
}

// Print opens the system print dialog for printable native content. It
// returns after submission, or ErrPrintCanceled when dismissed. Physical
// printer completion is outside the app's control. It starts no webview.
// Page.Print continues to print web content as before.
func (w *Window) Print(content Printable, opts PrintOptions) error {
	job, err := w.printJob(content, opts)
	if err != nil {
		return err
	}
	ch := make(chan error, 1)
	if !postMain(func() {
		if w.native == nil {
			deliver(ch, errDestroyed)
			return
		}
		backend().PrintContent(w.native, job, func(err error) { deliver(ch, err) })
	}) {
		return errLoopStopped
	}
	return await(ch)
}

// PrintToPDF exports fixed native UI pages without a printer or print dialog.
// The result is portable on macOS, Linux and Windows, regardless of the GPU.
func (w *Window) PrintToPDF(content Printable, opts PrintOptions) ([]byte, error) {
	job, err := w.printJob(content, opts)
	if err != nil {
		return nil, err
	}
	return job.PDF, nil
}
