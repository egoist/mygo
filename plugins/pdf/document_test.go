package pdf

import (
	"context"
	"errors"
	"github.com/egoist/mygo/ui"
	"os"
	"sync"
	"testing"
	"time"
	"unsafe"
)

func fakeDocument() (*Document, *int) {
	closed := new(int)
	var pixels unsafe.Pointer
	e := &engine{pageToDevice: func(_ uintptr, _, _, w, h, _ int32, x, y float64, dx, dy *int32) int32 {
		*dx = int32(x * float64(w) / 300)
		*dy = h - int32(y*float64(h)/400)
		return 1
	}, text: true, search: true, closeDocument: func(uintptr) { *closed++ }, loadPage: func(uintptr, int32) uintptr { return 2 }, closePage: func(uintptr) {}, width: func(uintptr) float64 { return 300 }, height: func(uintptr) float64 { return 400 }, bitmapCreate: func(w, h, f int32, p unsafe.Pointer, stride int32) uintptr { pixels = p; return 3 }, bitmapDestroy: func(uintptr) {}, bitmapFill: func(uintptr, int32, int32, int32, int32, uint32) {}, render: func(uintptr, uintptr, int32, int32, int32, int32, int32, int32) {
		*(*[4]byte)(pixels) = [4]byte{10, 20, 30, 255}
	}, loadText: func(uintptr) uintptr { return 4 }, closeText: func(uintptr) {}, countChars: func(uintptr) int32 { return 5 }, unicode: func(_ uintptr, i int32) uint32 { return uint32("Hello"[i]) }, charBox: func(_ uintptr, i int32, l, r, b, top *float64) int32 {
		*l = float64(i * 10)
		*r = *l + 10
		*b = 200
		*top = 220
		return 1
	}}
	return &Document{e: e, handle: 1, pages: []Page{{300, 400}, {400, 300}}, caps: Capabilities{Engine: "test", Render: true, Search: true, Selection: true}}, closed
}
func TestRenderValidationAndCleanup(t *testing.T) {
	d, closed := fakeDocument()
	out, err := d.Render(0, 20, 30, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := out.Pix[:4]; got[0] != 30 || got[1] != 20 || got[2] != 10 || got[3] != 255 {
		t.Fatal(got)
	}
	if _, err = d.Render(2, 20, 30, 0); err == nil {
		t.Fatal("out of bounds page")
	}
	if _, err = d.Render(0, 9000, 1, 0); err == nil {
		t.Fatal("excessive dimensions")
	}
	_ = d.Close()
	_ = d.Close()
	if *closed != 1 {
		t.Fatal(*closed)
	}
	if _, err = d.Render(0, 20, 30, 0); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}
func TestTextCapabilitiesAndCoordinates(t *testing.T) {
	d, _ := fakeDocument()
	defer d.Close()
	text, err := d.PageText(0)
	if err != nil {
		t.Fatal(err)
	}
	if text.String() != "Hello" || text.Characters[0].Bounds.Y != 180 {
		t.Fatal(text)
	}
	d.caps.Selection = false
	if _, err = d.PageText(0); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatal(err)
	}
	if text, err = d.pageTextForView(0); err != nil || text.String() != "Hello" {
		t.Fatal(text, err)
	}
}
func TestSearchCancellationAndCleanup(t *testing.T) {
	d, _ := fakeDocument()
	defer d.Close()
	closedSearch, closedText, closedPage := 0, 0, 0
	found := false
	d.e.closeText = func(uintptr) { closedText++ }
	d.e.closePage = func(uintptr) { closedPage++ }
	d.e.findStart = func(uintptr, *uint16, uint32, int32) uintptr { found = false; return 5 }
	d.e.findNext = func(uintptr) int32 {
		if found {
			return 0
		}
		found = true
		return 1
	}
	d.e.findIndex = func(uintptr) int32 { return 0 }
	d.e.findCount = func(uintptr) int32 { return 5 }
	d.e.findClose = func(uintptr) { closedSearch++ }
	matches, err := d.Search(context.Background(), "Hello", false)
	if err != nil || len(matches) != 2 {
		t.Fatal(matches, err)
	}
	if closedSearch != 2 || closedText != 2 || closedPage != 2 {
		t.Fatal("search leaked handles")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = d.Search(ctx, "Hello", false); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func waitViewer(t *testing.T, v *Viewer) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !v.State().Loading {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("viewer stuck loading")
}
func TestViewerNavigationSelectionAndClose(t *testing.T) {
	d, _ := fakeDocument()
	defer d.Close()
	v, err := NewViewer(d)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	tt := ui.NewTester(func(c *ui.Context) { View(c, v).Fill() }, 600, 480)
	waitViewer(t, v)
	tt.Frame()
	if err = tt.Click("Next page"); err != nil {
		t.Fatal(err)
	}
	waitViewer(t, v)
	if v.State().Page != 1 {
		t.Fatal(v.State())
	}
	if err = v.SetPage(99); err == nil {
		t.Fatal("invalid page accepted")
	}
	if err = v.SetZoom(0); err == nil {
		t.Fatal("invalid zoom accepted")
	}
	_ = v.SetPage(0)
	tt.Frame()
	waitViewer(t, v)
	tt.Frame()
	v.mu.Lock()
	v.state.SelectionStart = 0
	v.state.SelectionEnd = 5
	v.mu.Unlock()
	if err = tt.Click("PDF page"); err != nil {
		t.Fatal(err)
	}
	v.mu.Lock()
	v.state.SelectionStart = 0
	v.state.SelectionEnd = 5
	v.mu.Unlock()
	tt.Command("copy")
	if tt.Clipboard() != "Hello" {
		t.Fatal(tt.Clipboard())
	}
	v.Close()
	v.Close()
	select {
	case <-v.Done():
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}
	if v.bitmap != nil || v.text.Characters != nil {
		t.Fatal("retained page")
	}
}
func TestRenderingFailureClearsLoading(t *testing.T) {
	d, _ := fakeDocument()
	defer d.Close()
	d.e.bitmapCreate = func(int32, int32, int32, unsafe.Pointer, int32) uintptr { return 0 }
	v, _ := NewViewer(d)
	defer v.Close()
	_ = ui.NewTester(func(c *ui.Context) { View(c, v).Fill() }, 300, 400)
	waitViewer(t, v)
	if v.State().Err == nil {
		t.Fatal("missing render failure")
	}
}
func TestConcurrentRenderClose(t *testing.T) {
	d, closed := fakeDocument()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Go(func() {
			for j := 0; j < 20; j++ {
				_, _ = d.Render(0, 20, 20, 0)
			}
		})
	}
	_ = d.Close()
	wg.Wait()
	if *closed != 1 {
		t.Fatal(*closed)
	}
}
func TestMissingLibraryAndInvalidData(t *testing.T) {
	if _, err := Load(nil, Options{}); err == nil {
		t.Fatal("empty PDF loaded")
	}
	if _, err := Load([]byte("bad"), Options{Library: "/missing/mygo-pdfium"}); err == nil {
		t.Fatal("missing library loaded")
	}
}
func TestNativePDFium(t *testing.T) {
	path := os.Getenv("MYGO_PDFIUM_LIBRARY")
	if path == "" {
		t.Skip("set MYGO_PDFIUM_LIBRARY for native PDFium integration")
	}
	d, err := Open("testdata/sample.pdf", Options{Library: path})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if len(d.Pages()) != 2 || !d.Capabilities().Search || !d.Capabilities().Selection {
		t.Fatal(d.Pages(), d.Capabilities())
	}
	matches, err := d.Search(context.Background(), "hello", false)
	if err != nil || len(matches) != 2 {
		t.Fatal(matches, err)
	}
	text, err := d.PageText(0)
	if err != nil || text.String() != "Hello MyGo PDF viewer" {
		t.Fatal(text.String(), err)
	}
	img, err := d.Render(0, 600, 800, 0)
	if err != nil {
		t.Fatal(err)
	}
	dark := 0
	for i := 0; i < len(img.Pix); i += 4 {
		if img.Pix[i] < 100 {
			dark++
		}
	}
	if dark < 100 {
		t.Fatal("PDF text did not render")
	}
	if _, err = Load([]byte("bad"), Options{Library: path}); err == nil {
		t.Fatal("invalid PDF loaded")
	}
	v, _ := NewViewer(d)
	tt := ui.NewTester(func(c *ui.Context) { View(c, v).Fill() }, 500, 600)
	waitViewer(t, v)
	if v.State().Err != nil {
		t.Fatal(v.State().Err)
	}
	tt.Frame()
	v.mu.Lock()
	v.state.SelectionStart = 0
	v.state.SelectionEnd = 5
	v.mu.Unlock()
	tt.Frame()
	canvas, ok := tt.Find("PDF page")
	if !ok {
		t.Fatal("missing page canvas")
	}
	rect := v.State().Transform.Rect(300, 400, canvas)
	dark = 0
	for _, ch := range text.Characters[:5] {
		b := ch.Bounds
		x0, y0 := int(rect.X+b.X*rect.W/300), int(rect.Y+b.Y*rect.H/400)
		x1, y1 := int(rect.X+(b.X+b.W)*rect.W/300), int(rect.Y+(b.Y+b.H)*rect.H/400)
		for y := y0; y <= y1; y++ {
			for x := x0; x <= x1; x++ {
				c := tt.Image().RGBAAt(x, y)
				if c.R < 100 && c.B < 100 {
					dark++
				}
			}
		}
	}
	if dark < 10 {
		t.Fatal("selection highlight obscured PDF glyphs")
	}
	v.Close()
	<-v.Done()
}

func TestNativePDFiumCroppedRotatedBounds(t *testing.T) {
	library := os.Getenv("MYGO_PDFIUM_LIBRARY")
	if library == "" {
		t.Skip("MYGO_PDFIUM_LIBRARY required")
	}
	d, err := Open("testdata/cropped-rotated.pdf", Options{Library: library})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	pages := d.Pages()
	if pages[0].Width != 330 || pages[0].Height != 260 {
		t.Fatal(pages)
	}
	text, err := d.PageText(0)
	if err != nil {
		t.Fatal(err)
	}
	img, err := d.Render(0, 660, 520, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, ch := range text.Characters {
		if ch.Rune == ' ' || ch.Bounds.W <= 0 || ch.Bounds.H <= 0 {
			continue
		}
		box := ch.Bounds
		dark := false
		for y := max(0, int(box.Y*2)); y < min(img.Rect.Dy(), int((box.Y+box.H)*2)+1); y++ {
			for x := max(0, int(box.X*2)); x < min(img.Rect.Dx(), int((box.X+box.W)*2)+1); x++ {
				if img.RGBAAt(x, y).R < 100 {
					dark = true
				}
			}
		}
		if !dark {
			t.Fatalf("text bounds %v for %q did not cover rendered glyph", box, ch.Rune)
		}
	}
}
func TestViewerCloseWhileRendering(t *testing.T) {
	d, _ := fakeDocument()
	defer d.Close()
	entered, release := make(chan struct{}), make(chan struct{})
	d.e.render = func(uintptr, uintptr, int32, int32, int32, int32, int32, int32) { close(entered); <-release }
	v, _ := NewViewer(d)
	v.request(renderRequest{0, 20, 20})
	<-entered
	before := time.Now()
	v.Close()
	if time.Since(before) > 50*time.Millisecond {
		t.Fatal("Close waited for rendering")
	}
	select {
	case <-v.Done():
		t.Fatal("worker ended before render completed")
	default:
	}
	close(release)
	select {
	case <-v.Done():
	case <-time.After(time.Second):
		t.Fatal("worker leaked")
	}
}

func TestMetadataDoesNotWaitForRendering(t *testing.T) {
	d, _ := fakeDocument()
	defer d.Close()
	entered, release := make(chan struct{}), make(chan struct{})
	d.e.render = func(uintptr, uintptr, int32, int32, int32, int32, int32, int32) { close(entered); <-release }
	done := make(chan struct{})
	go func() { _, _ = d.Render(0, 20, 20, 0); close(done) }()
	<-entered
	read := make(chan bool, 1)
	go func() { read <- d.Capabilities().Render && len(d.Pages()) == 2 }()
	select {
	case valid := <-read:
		if !valid {
			t.Fatal("metadata unavailable")
		}
	case <-time.After(100 * time.Millisecond):
		close(release)
		<-done
		t.Fatal("metadata blocked UI on rendering")
	}
	close(release)
	<-done
}
func TestNavigationRejectsInFlightOldPage(t *testing.T) {
	d, _ := fakeDocument()
	defer d.Close()
	entered, release := make(chan struct{}), make(chan struct{})
	d.e.render = func(uintptr, uintptr, int32, int32, int32, int32, int32, int32) { close(entered); <-release }
	v, _ := NewViewer(d)
	defer v.Close()
	notified := make(chan struct{}, 4)
	v.mu.Lock()
	v.invalidate = func() { notified <- struct{}{} }
	v.mu.Unlock()
	v.request(renderRequest{0, 20, 20})
	<-entered
	_ = v.SetPage(1)
	<-notified
	close(release)
	select {
	case <-notified:
	case <-time.After(time.Second):
		t.Fatal("render stuck")
	}
	v.mu.Lock()
	bitmap := v.bitmap
	state := v.state
	v.mu.Unlock()
	if bitmap != nil || state.Page != 1 || !state.Loading {
		t.Fatal("old page published after navigation", state)
	}
	v.Close()
	<-v.Done()
}
func TestSearchMatchRevealedWhenZoomed(t *testing.T) {
	d, _ := fakeDocument()
	defer d.Close()
	v, _ := NewViewer(d)
	defer v.Close()
	_ = v.SetZoom(4)
	tt := ui.NewTester(func(c *ui.Context) { View(c, v).Fill() }, 500, 600)
	waitViewer(t, v)
	v.update(func() { v.matches = []Match{{Page: 0, Start: 0, Count: 5}}; v.chooseMatch(0) })
	tt.Frame()
	if v.State().Transform.PanX <= 0 {
		t.Fatal("zoomed match stayed outside viewport", v.State())
	}
}
