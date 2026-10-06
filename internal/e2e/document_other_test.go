//go:build !darwin

package e2e

import "github.com/egoist/mygo"

func cancelDocumentSheet(*mygo.Window) bool { return false }

func nativeDocumentState(*mygo.Window) (string, bool, bool) { return "", false, false }
func nativePDFPages([]byte) (int, error, bool)              { return 0, nil, false }
func cancelNativePrintDialog() (bool, bool)                 { return false, false }
