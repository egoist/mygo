//go:build linux && (amd64 || arm64)

package e2e

import (
	"context"
	_ "embed"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/linux"
	"github.com/egoist/mygo/internal/platform"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

//go:embed atspi_collection.py
var atspiCollectionProbe string

func externalCollection(t *testing.T, label string, rows, columns, row, column int) {
	t.Helper()
	if os.Getenv("MYGO_ATSPI_E2E") == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", "-c", atspiCollectionProbe, label, strconv.Itoa(rows), strconv.Itoa(columns), strconv.Itoa(row), strconv.Itoa(column))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("external AT-SPI: %v\n%s", err, output)
	} else {
		t.Log(string(output))
	}
}

func collectionProbe(w *mygo.Window, label string, row, column int) (out platform.AccessCollectionProbe, ok bool) {
	mygo.RunOnMain(func() { out, ok = linux.TestAccessibilityCollection(w.NativeHandle(), label, row, column) })
	return
}

func collectionPerform(w *mygo.Window, label string, row, column int, action string) (ok bool) {
	mygo.RunOnMain(func() { ok = linux.TestAccessibilityCollectionPerform(w.NativeHandle(), label, row, column, action) })
	return
}

func collectionLifetime(w *mygo.Window, label string, row, column int) (ok bool) {
	mygo.RunOnMain(func() {
		ok = linux.TestAccessibilityCollectionLifetime(w.NativeHandle(), label, row, column, w.Destroy)
	})
	return
}
