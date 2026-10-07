//go:build windows && (amd64 || arm64)

package e2e

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/windows"
	"testing"
)

func externalCollection(*testing.T, string, int, int, int, int) {}

func collectionProbe(w *mygo.Window, label string, row, column int) (out platform.AccessCollectionProbe, ok bool) {
	mygo.RunOnMain(func() { out, ok = windows.TestAccessibilityCollection(w.NativeHandle(), label, row, column) })
	return
}

func collectionPerform(w *mygo.Window, label string, row, column int, action string) (ok bool) {
	mygo.RunOnMain(func() { ok = windows.TestAccessibilityCollectionPerform(w.NativeHandle(), label, row, column, action) })
	return
}

func collectionLifetime(w *mygo.Window, label string, row, column int) (ok bool) {
	mygo.RunOnMain(func() {
		ok = windows.TestAccessibilityCollectionLifetime(w.NativeHandle(), label, row, column, w.Destroy)
	})
	return
}
