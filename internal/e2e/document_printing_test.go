package e2e

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func TestNativeDocumentLifecycle(t *testing.T) {
	w := newWindow(t, mygo.WindowOptions{Title: "Document", Width: 320, Height: 240, Content: ui.View(func(c *ui.Context) { ui.Text(c, "Document") })})
	m := mygo.NewDocuments(mygo.DocumentsOptions[string]{MemoryOnly: true, Read: func(p string) (string, error) { b, e := os.ReadFile(p); return string(b), e }, Write: func(p, s string) error { return os.WriteFile(p, []byte(s), 0o600) }})
	t.Cleanup(func() {
		w.Destroy()
		if err := m.Dispose(); err != nil {
			t.Error(err)
		}
	})
	d := m.New()
	if err := d.Attach(w); err != nil {
		t.Fatal(err)
	}
	d.SetValue("unsaved")
	if path, edited, supported := nativeDocumentState(w); supported && (path != "" || !edited) {
		t.Fatalf("untitled native marker: %q %v", path, edited)
	}
	p := filepath.Join(t.TempDir(), "document.txt")
	if err := d.SaveTo(p); err != nil {
		t.Fatal(err)
	}
	if path, edited, supported := nativeDocumentState(w); supported && (path != d.State().Path || edited) {
		t.Fatalf("saved native identity: %q %v", path, edited)
	}
	d.SetValue("more edits")
	// The real sheet must map a dismissal to Cancel, keeping the document.
	if _, _, supported := nativeDocumentState(w); supported {
		result := make(chan error, 1)
		go func() { result <- d.Close() }()
		until := time.Now().Add(5 * time.Second)
		ended := false
		for time.Now().Before(until) {
			if cancelDocumentSheet(w) {
				ended = true
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if !ended {
			t.Fatal("document close sheet did not open")
		}
		select {
		case err := <-result:
			if !errors.Is(err, mygo.ErrDocumentCanceled) {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("document close did not return")
		}
		if w.IsDestroyed() || !d.State().Dirty {
			t.Fatal("canceled close lost the document")
		}
	}
}

func TestNativeContentPrintToPDF(t *testing.T) {
	w := newWindow(t, mygo.WindowOptions{Width: 200, Height: 180, Content: ui.View(func(c *ui.Context) { ui.Text(c, "Editor window") })})
	if w.Page() != nil {
		t.Fatal("native printing window has a webview")
	}
	pages := ui.PrintPages(func(l ui.PrintLayout) []func(*ui.Context) {
		return []func(*ui.Context){func(c *ui.Context) { ui.Text(c, "First printable page").FontSize(14) }, func(c *ui.Context) { ui.Text(c, "Second printable page").FontSize(14) }}
	})
	for _, landscape := range []bool{false, true} {
		pdf, err := w.PrintToPDF(pages, mygo.PrintOptions{PageSize: mygo.PageA4, Landscape: landscape, DPI: 72})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.HasPrefix(pdf, []byte("%PDF-")) || len(regexp.MustCompile(`/Type /Page\b`).FindAll(pdf, -1)) != 2 {
			t.Fatal("invalid native pages")
		}
		if count, err, supported := nativePDFPages(pdf); supported && (err != nil || count != 2) {
			t.Fatalf("system PDF parser: %d %v", count, err)
		}
	}
}

func TestNativePrintDialogCancel(t *testing.T) {
	if _, supported := cancelNativePrintDialog(); !supported {
		t.Skip("native print dialog cancellation hook is available on macOS")
	}
	w := newWindow(t, mygo.WindowOptions{Title: "Native print cancellation", Width: 320, Height: 220, Content: ui.View(func(c *ui.Context) { ui.Text(c, "Print test") })})
	result := make(chan error, 1)
	go func() {
		result <- w.Print(ui.PrintView(func(c *ui.Context) { ui.Text(c, "Cancel without printing") }), mygo.PrintOptions{DPI: 72})
	}()
	until := time.Now().Add(8 * time.Second)
	canceled := false
	for time.Now().Before(until) {
		if ok, _ := cancelNativePrintDialog(); ok {
			canceled = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !canceled {
		t.Fatal("native print panel did not open")
	}
	select {
	case err := <-result:
		if !errors.Is(err, mygo.ErrPrintCanceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("print panel did not return")
	}
}
