//go:build darwin

package darwin

import "errors"

// TestDocumentState reads AppKit's native document indicator, main thread only.
func TestDocumentState(win uintptr) (path string, edited bool) {
	withPool(func() {
		path = goString(send(id(win), "representedFilename"))
		edited = sendBool(id(win), "isDocumentEdited")
	})
	return
}

// TestPrintablePDFPages checks the same system PDF reader used to print,
// without needing a physical printer. Main thread only.
func TestPrintablePDFPages(pdf []byte) (pages int, err error) {
	if err = loadPrintPDFKit(); err != nil {
		return
	}
	withPool(func() {
		doc := send(send(class("PDFDocument"), "alloc"), "initWithData:", uintptr(nsData(pdf)))
		if doc == 0 {
			err = errors.New("mygo: PDFKit rejected printable PDF")
			return
		}
		defer release(doc)
		pages = sendInt(doc, "pageCount")
	})
	return
}

// TestCancelNativePrintDialog dismisses an app-modal panel as Cancel. Only
// used by the GUI test while its native print panel is running.
func TestCancelNativePrintDialog() bool {
	if send(theBackend.app, "modalWindow") == 0 {
		return false
	}
	send(theBackend.app, "stopModalWithCode:", 0)
	return true
}

// TestCancelDocumentClose chooses the third button (Cancel) in the dirty
// document's Save / Discard / Cancel sheet. Main thread only.
func TestCancelDocumentClose(win uintptr) bool {
	sheet := send(id(win), "attachedSheet")
	if sheet == 0 {
		return false
	}
	send(id(win), "endSheet:returnCode:", uintptr(sheet), nsAlertFirstButtonReturn+2)
	return true
}
