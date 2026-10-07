//go:build darwin

package e2e

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/darwin"
)

func nativeDocumentState(w *mygo.Window) (path string, edited, supported bool) {
	mygo.RunOnMain(func() { path, edited = darwin.TestDocumentState(w.NativeHandle()) })
	return path, edited, true
}
func nativePDFPages(pdf []byte) (count int, err error, supported bool) {
	mygo.RunOnMain(func() { count, err = darwin.TestPrintablePDFPages(pdf) })
	return count, err, true
}
func cancelNativePrintDialog() (canceled, supported bool) {
	mygo.RunOnMain(func() { canceled = darwin.TestCancelNativePrintDialog() })
	return canceled, true
}
func cancelDocumentSheet(w *mygo.Window) bool {
	canceled := false
	mygo.RunOnMain(func() { canceled = darwin.TestCancelDocumentClose(w.NativeHandle()) })
	return canceled
}
