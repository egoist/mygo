//go:build linux && (amd64 || arm64)

package linux

import (
	"errors"
	"github.com/egoist/mygo/internal/platform"
)

// TestPrintNativePDF runs GtkPrintOperation's export action through the same
// cairo page callback as the native print dialog, main thread only.
func TestPrintNativePDF(handle uintptr, job *platform.PrintJob, path string) error {
	for _, w := range theBackend.windows {
		if w.win == handle {
			var err error
			theBackend.runNativePrint(w, job, 3, path, func(e error) { err = e })
			return err
		}
	}
	return errors.New("mygo: no native print test window")
}
