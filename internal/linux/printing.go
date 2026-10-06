//go:build linux && (amd64 || arm64)

package linux

import (
	"errors"
	"runtime"
	"sync"

	"github.com/ebitengine/purego"
	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/printdoc"
)

var (
	nativePrintOnce                                   sync.Once
	gtkPrintOperationNew                              func() ptr
	gtkPrintOperationSetNPages                        func(ptr, int32)
	gtkPrintOperationSetUnit                          func(ptr, int32)
	gtkPrintOperationSetJobName                       func(ptr, *byte)
	gtkPrintOperationSetDefaultPageSetup              func(ptr, ptr)
	gtkPrintOperationSetExportFilename                func(ptr, *byte)
	gtkPrintOperationRun                              func(ptr, int32, ptr, *ptr) int32
	gtkPrintContextGetCairoContext                    func(ptr) ptr
	gtkPrintContextGetWidth, gtkPrintContextGetHeight func(ptr) float64
	printCairoSave, printCairoRestore                 func(ptr)
	printCairoTranslate, printCairoScale              func(ptr, float64, float64)
	cbNativePrintDraw                                 ptr
	nativePrintJobs                                   = map[ptr]*nativePrintJob{}
)

type nativePrintJob struct {
	*platform.PrintJob
	// PDF/print cairo surfaces may retain image data past draw-page. Keep
	// every borrowed buffer alive until the whole operation has finished.
	pixels [][]byte
}

func loadNativePrint() {
	nativePrintOnce.Do(func() {
		loadSurface() // cairo image bindings; no WebKit is loaded
		mustBind(libGTK, &gtkPrintOperationNew, "gtk_print_operation_new")
		mustBind(libGTK, &gtkPrintOperationSetNPages, "gtk_print_operation_set_n_pages")
		mustBind(libGTK, &gtkPrintOperationSetUnit, "gtk_print_operation_set_unit")
		mustBind(libGTK, &gtkPrintOperationSetJobName, "gtk_print_operation_set_job_name")
		mustBind(libGTK, &gtkPrintOperationSetDefaultPageSetup, "gtk_print_operation_set_default_page_setup")
		mustBind(libGTK, &gtkPrintOperationSetExportFilename, "gtk_print_operation_set_export_filename")
		mustBind(libGTK, &gtkPrintOperationRun, "gtk_print_operation_run")
		mustBind(libGTK, &gtkPrintContextGetCairoContext, "gtk_print_context_get_cairo_context")
		mustBind(libGTK, &gtkPrintContextGetWidth, "gtk_print_context_get_width")
		mustBind(libGTK, &gtkPrintContextGetHeight, "gtk_print_context_get_height")
		mustBind(libCairo, &printCairoSave, "cairo_save")
		mustBind(libCairo, &printCairoRestore, "cairo_restore")
		mustBind(libCairo, &printCairoTranslate, "cairo_translate")
		mustBind(libCairo, &printCairoScale, "cairo_scale")

	})
}

// Called once by initCallbacks at startup, before any print operation.
func initNativePrintCallbacks() {
	// One callback for all operations, routed by GtkPrintOperation.
	cbNativePrintDraw = purego.NewCallback(func(op, context ptr, page int32, data ptr) {
		job := nativePrintJobs[op]
		if job == nil || page < 0 || int(page) >= len(job.Pages) {
			return
		}
		img := job.Pages[page]
		pix := printdoc.BGRA(img)
		job.pixels = append(job.pixels, pix)
		cr := gtkPrintContextGetCairoContext(context)
		x, y, w, h := printdoc.Fit(job.Width, job.Height, gtkPrintContextGetWidth(context), gtkPrintContextGetHeight(context))
		surface := cairoImageSurfaceForData(&pix[0], 0, int32(img.Rect.Dx()), int32(img.Rect.Dy()), int32(img.Rect.Dx()*4))
		printCairoSave(cr)
		printCairoTranslate(cr, x, y)
		printCairoScale(cr, w/float64(img.Rect.Dx()), h/float64(img.Rect.Dy()))
		cairoSetSourceSurface(cr, surface, 0, 0)
		cairoPaint(cr)
		printCairoRestore(cr)
		cairoSurfaceDestroy(surface)
		runtime.KeepAlive(pix)
	})
}

func (b *Backend) PrintContent(parent platform.Window, job *platform.PrintJob, done func(error)) {
	b.runNativePrint(parent, job, 0, "", done)
}

// GTK_ACTION_EXPORT exercises the same page callback in GUI tests without
// depending on a physical printer. Production calls PRINT_DIALOG (0).
func (b *Backend) runNativePrint(parent platform.Window, job *platform.PrintJob, action int32, exportPath string, done func(error)) {
	w, ok := parent.(*window)
	if !ok || w.closed {
		done(errors.New("mygo: window has been destroyed"))
		return
	}
	loadNativePrint()
	op := gtkPrintOperationNew()
	defer gObjectUnref(op)
	nativePrintJobs[op] = &nativePrintJob{PrintJob: job}
	defer delete(nativePrintJobs, op)
	gtkPrintOperationSetNPages(op, int32(len(job.Pages)))
	gtkPrintOperationSetUnit(op, 1) // GTK_UNIT_POINTS
	gtkPrintOperationSetJobName(op, cs(job.Title))
	if exportPath != "" {
		gtkPrintOperationSetExportFilename(op, cs(exportPath))
	}
	setup := gtkPageSetupNew()
	defer gObjectUnref(setup)
	paper := gtkPaperSizeNewCustom(cs("mygo"), cs("Document"), job.Width, job.Height, 1)
	gtkPageSetupSetPaperSize(setup, paper)
	gtkPaperSizeFree(paper)
	gtkPrintOperationSetDefaultPageSetup(op, setup)
	connect(op, "draw-page", cbNativePrintDraw, 0)
	// The default is synchronous: GTK owns a nested loop for the dialog,
	// then emits draw-page for the selected ranges/copies. No polling.
	var gerr ptr
	result := gtkPrintOperationRun(op, action, w.win, &gerr)
	err := gErr(gerr)
	if err == nil {
		switch {
		case w.closed:
			err = errors.New("mygo: window has been destroyed")
		case result == 2:
			err = platform.ErrPrintCanceled
		case result != 1:
			err = errors.New("mygo: native print operation failed")
		}
	}
	done(err)
}
