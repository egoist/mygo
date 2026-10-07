//go:build linux && (amd64 || arm64)

package e2e

import (
	"bytes"
	"compress/zlib"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/linux"
	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/ui"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestNativePrintOperationPDF(t *testing.T) {
	w := newWindow(t, mygo.WindowOptions{Width: 200, Height: 160, Content: ui.View(func(c *ui.Context) {})})
	pages := ui.PrintPages(func(ui.PrintLayout) []func(*ui.Context) {
		return []func(*ui.Context){
			func(c *ui.Context) { ui.Box(c).Fill().Background(ui.RGB(255, 0, 0)); ui.Text(c, "Printed first page") },
			func(c *ui.Context) { ui.Box(c).Fill().Background(ui.RGB(0, 0, 255)); ui.Text(c, "Printed second page") },
		}
	})
	path := filepath.Join(t.TempDir(), "native.pdf")
	var err error
	mygo.RunOnMain(func() {
		job := &platform.PrintJob{Title: "GTK native pages", Width: 216, Height: 288}
		job.Pages, err = pages.RenderPrintPages(platform.PrintLayout{Width: 216, Height: 288, Scale: 1})
		if err == nil {
			err = linux.TestPrintNativePDF(w.NativeHandle(), job, path)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	pdf, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Cairo may pack page dictionaries into compressed object streams.
	// Inspect those too; counting only uncompressed objects misses pages.
	objects := bytes.Clone(pdf)
	for _, index := range regexp.MustCompile(`(?m)^stream\r?$`).FindAllIndex(pdf, -1) {
		start := index[1] + 1
		end := bytes.Index(pdf[start:], []byte("\nendstream"))
		if end < 0 {
			continue
		}
		z, e := zlib.NewReader(bytes.NewReader(pdf[start : start+end]))
		if e != nil {
			continue
		}
		inflated, e := io.ReadAll(z)
		z.Close()
		if e == nil {
			objects = append(objects, inflated...)
		}
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) || len(regexp.MustCompile(`/Type\s*/Page\b`).FindAll(objects, -1)) != 2 {
		t.Fatal("GTK did not draw two native pages")
	}
	if !bytes.Contains(pdf, []byte("/Subtype /Image")) {
		t.Fatal("GTK print callback produced no raster page")
	}
}
