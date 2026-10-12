package pdf

import (
	"errors"
	"fmt"
	"github.com/egoist/mygo/plugins/internal/contentlib"
	"runtime"
	"sync"
	"unsafe"
)

// PDFium is not thread safe. This lock serializes ALL calls, including calls
// on different documents. Decoding/rendering never touches a platform view.
var engineMu sync.Mutex
var engines = map[string]*engine{}
var enginesByHandle = map[uintptr]*engine{}

type engine struct {
	library              uintptr
	init                 func()
	load                 func(unsafe.Pointer, uint64, string) uintptr
	lastError            func() uint32
	closeDocument        func(uintptr)
	countPages           func(uintptr) int32
	permissions          func(uintptr) uint32
	loadPage             func(uintptr, int32) uintptr
	closePage            func(uintptr)
	width, height        func(uintptr) float64
	bitmapCreate         func(int32, int32, int32, unsafe.Pointer, int32) uintptr
	bitmapFill           func(uintptr, int32, int32, int32, int32, uint32)
	bitmapDestroy        func(uintptr)
	render               func(uintptr, uintptr, int32, int32, int32, int32, int32, int32)
	loadText             func(uintptr) uintptr
	closeText            func(uintptr)
	countChars           func(uintptr) int32
	unicode              func(uintptr, int32) uint32
	charBox              func(uintptr, int32, *float64, *float64, *float64, *float64) int32
	findStart            func(uintptr, *uint16, uint32, int32) uintptr
	findNext             func(uintptr) int32
	findIndex, findCount func(uintptr) int32
	findClose            func(uintptr)
	pageToDevice         func(uintptr, int32, int32, int32, int32, int32, float64, float64, *int32, *int32) int32
	text, search         bool
}

func loadEngine(path string) (*engine, error) {
	engineMu.Lock()
	defer engineMu.Unlock()
	if e := engines[path]; e != nil {
		return e, nil
	}
	names := []string{"libpdfium.so"}
	switch runtime.GOOS {
	case "darwin":
		names = []string{"libpdfium.dylib", "/opt/homebrew/lib/libpdfium.dylib", "/usr/local/lib/libpdfium.dylib"}
	case "windows":
		names = []string{"pdfium.dll"}
	}
	h, err := contentlib.Open(path, names...)
	if err != nil {
		return nil, fmt.Errorf("pdf: %w", err)
	}
	if e := enginesByHandle[h]; e != nil {
		contentlib.Release(h)
		engines[path] = e
		return e, nil
	}
	e := &engine{library: h}
	required := []struct {
		name string
		fn   any
	}{
		{"FPDF_InitLibrary", &e.init}, {"FPDF_LoadMemDocument64", &e.load}, {"FPDF_GetLastError", &e.lastError}, {"FPDF_CloseDocument", &e.closeDocument}, {"FPDF_GetPageCount", &e.countPages}, {"FPDF_GetDocPermissions", &e.permissions}, {"FPDF_LoadPage", &e.loadPage}, {"FPDF_ClosePage", &e.closePage}, {"FPDF_GetPageWidth", &e.width}, {"FPDF_GetPageHeight", &e.height}, {"FPDFBitmap_CreateEx", &e.bitmapCreate}, {"FPDFBitmap_FillRect", &e.bitmapFill}, {"FPDFBitmap_Destroy", &e.bitmapDestroy}, {"FPDF_RenderPageBitmap", &e.render},
	}
	for _, s := range required {
		if err = contentlib.Bind(h, s.name, s.fn); err != nil {
			contentlib.Release(h)
			return nil, fmt.Errorf("pdf: %w", err)
		}
	}
	e.text = true
	for _, s := range []struct {
		name string
		fn   any
	}{{"FPDFText_LoadPage", &e.loadText}, {"FPDFText_ClosePage", &e.closeText}, {"FPDFText_CountChars", &e.countChars}, {"FPDFText_GetUnicode", &e.unicode}, {"FPDFText_GetCharBox", &e.charBox}, {"FPDF_PageToDevice", &e.pageToDevice}} {
		if !contentlib.Optional(h, s.name, s.fn) {
			e.text = false
		}
	}
	e.search = e.text
	for _, s := range []struct {
		name string
		fn   any
	}{{"FPDFText_FindStart", &e.findStart}, {"FPDFText_FindNext", &e.findNext}, {"FPDFText_GetSchResultIndex", &e.findIndex}, {"FPDFText_GetSchCount", &e.findCount}, {"FPDFText_FindClose", &e.findClose}} {
		if !contentlib.Optional(h, s.name, s.fn) {
			e.search = false
		}
	}
	e.init()
	enginesByHandle[h] = e
	engines[path] = e
	return e, nil // process lifetime; documents/pages are separately released
}
func engineError(code uint32) error {
	names := map[uint32]string{1: "unknown failure", 2: "file could not be read", 3: "invalid or damaged PDF", 4: "password required or incorrect", 5: "unsupported encryption", 6: "invalid page"}
	name := names[code]
	if name == "" {
		name = "unknown failure"
	}
	return fmt.Errorf("pdf: %s (PDFium error %d)", name, code)
}

var ErrClosed = errors.New("pdf: document or viewer closed")
