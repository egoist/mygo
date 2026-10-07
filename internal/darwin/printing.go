//go:build darwin

package darwin

import (
	"errors"
	"sync"

	"github.com/ebitengine/purego"
	"github.com/egoist/mygo/internal/platform"
)

// System PDFKit owns the print view and pagination, so no per-page Objective-C
// callbacks or struct-return trampolines are needed. It loads only on Print;
// exporting a native PDF uses the portable encoder without PDFKit.
var loadPrintPDFKit = sync.OnceValue(func() error {
	_, err := purego.Dlopen("/System/Library/Frameworks/PDFKit.framework/PDFKit", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	return err
})

func (b *Backend) PrintContent(parent platform.Window, job *platform.PrintJob, done func(error)) {
	w, ok := parent.(*window)
	if !ok || w.closed {
		done(errDestroyed)
		return
	}
	if err := loadPrintPDFKit(); err != nil {
		done(err)
		return
	}
	withPool(func() {
		info := autorelease(send(send(class("NSPrintInfo"), "sharedPrintInfo"), "copy"))
		msgSetSize(info, sel("setPaperSize:"), NSSize{job.Width, job.Height})
		send(info, "setJobDisposition:", uintptr(appKitString("NSPrintSpoolJob")))
		dictionary := send(info, "dictionary")
		for key, value := range map[string]int{"NSPrintAllPages": 1, "NSPrintFirstPage": 1, "NSPrintLastPage": len(job.Pages)} {
			number := send(class("NSNumber"), "numberWithInteger:", uintptr(value))
			send(dictionary, "setObject:forKey:", uintptr(number), uintptr(appKitString(key)))
		}
		for _, margin := range []string{"setTopMargin:", "setRightMargin:", "setBottomMargin:", "setLeftMargin:"} {
			msgSetFloat(info, sel(margin), 0)
		}
		// Show the panel separately so cancellation is distinguishable from
		// a failure to spool. Its modal loop continues dispatching UI events.
		panel := send(class("NSPrintPanel"), "printPanel")
		if sendInt(panel, "runModalWithPrintInfo:", uintptr(info)) != 1 {
			done(platform.ErrPrintCanceled)
			return
		}
		if w.closed {
			done(errDestroyed)
			return
		}
		doc := send(send(class("PDFDocument"), "alloc"), "initWithData:", uintptr(nsData(job.PDF)))
		if doc == 0 {
			done(errors.New("mygo: PDFKit could not read printable pages"))
			return
		}
		// kPDFPrintPageScaleToFit: changing printer paper never changes the
		// document's page breaks. PDFKit handles ranges, copies and rotation.
		op := retain(send(doc, "printOperationForPrintInfo:scalingMode:autoRotate:", uintptr(info), 1, 1))
		if op == 0 {
			release(doc)
			done(errors.New("mygo: could not create print operation"))
			return
		}
		send(op, "setShowsPrintPanel:", 0)
		send(op, "setShowsProgressPanel:", 1)
		if respondsTo(op, "setJobTitle:") {
			send(op, "setJobTitle:", uintptr(nsString(job.Title)))
		}
		key := uintptr(len(printJobs) + 1)
		for printJobs[key] != nil {
			key++
		}
		j := &printJob{w: w, cb: func(_ []byte, err error) { done(err) }}
		j.done = func(success bool) {
			var err error
			if !success {
				disposition := goString(send(send(op, "printInfo"), "jobDisposition"))
				if disposition == goString(appKitString("NSPrintCancelJob")) {
					err = platform.ErrPrintCanceled
				} else {
					err = errors.New("mygo: native print operation failed")
				}
			}
			release(op)
			release(doc)
			// Closing the window already answered the request.
			if j.cb == nil {
				return
			}
			j.cb(nil, err)
		}
		printJobs[key] = j
		// Reuse the app's one startup callback and window-destruction cleanup.
		send(op, "runOperationModalForWindow:delegate:didRunSelector:contextInfo:", uintptr(w.win), uintptr(b.delegate),
			uintptr(sel("mygoPrintOperationDidRun:success:contextInfo:")), key)
	})
}
