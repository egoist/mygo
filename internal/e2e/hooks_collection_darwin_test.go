//go:build darwin

package e2e

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/darwin"
	"github.com/egoist/mygo/internal/platform"
	"testing"
)

func externalCollection(*testing.T, string, int, int, int, int) {}

func collectionProbe(w *mygo.Window, label string, row, column int) (out platform.AccessCollectionProbe, ok bool) {
	mygo.RunOnMain(func() { out, ok = darwin.TestAccessibilityCollection(w.NativeHandle(), label, row, column) })
	return
}

func collectionPerform(w *mygo.Window, label string, row, column int, action string) (ok bool) {
	mygo.RunOnMain(func() { ok = darwin.TestAccessibilityCollectionPerform(w.NativeHandle(), label, row, column, action) })
	return
}

func collectionLifetime(w *mygo.Window, label string, row, column int) (ok bool) {
	mygo.RunOnMain(func() {
		ok = darwin.TestAccessibilityCollectionLifetime(w.NativeHandle(), label, row, column, w.Destroy)
	})
	return
}
