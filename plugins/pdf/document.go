// Package pdf provides optional PDF viewing for MyGo native UI using PDFium.
// Supply a native PDFium library; the plugin neither downloads one at runtime
// nor needs Bun or cgo. See docs/plugins/pdf.md for installation and packaging.
package pdf

import (
	"context"
	"errors"
	"github.com/egoist/mygo/ui"
	"image"
	"io"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"unicode/utf16"
	"unsafe"
)

// Options selects the native library and an optional PDF password. Empty Library
// tries packaged PDFium followed by the system library. No library is required
// by applications that do not import/use this optional plugin.
type Options struct{ Library, Password string }

// Capabilities describes the loaded engine AND document permissions. Search
// needs searchable text; image-only PDFs return no matches. Selection respects
// the document's copy permission. This viewer does not run JavaScript or forms.
type Capabilities struct {
	Engine                    string
	Render, Search, Selection bool
}

// Page is the logical page size in PDF points (1/72 inch).
type Page struct{ Width, Height float64 }

// Document owns the bytes and PDFium document. Methods are goroutine-safe,
// including Close racing rendering or search. Explicitly Close documents.
type Document struct {
	closed atomic.Bool // nonblocking metadata queries; native pointers use engineMu
	e      *engine
	handle uintptr
	data   []byte
	pin    runtime.Pinner
	pages  []Page
	caps   Capabilities
}

// Open opens a local PDF, up to 256 MiB. Network access is left to the app.
func Open(path string, opts Options) (*Document, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > 256<<20 {
		return nil, errors.New("pdf: document exceeds 256 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(f, (256<<20)+1))
	if err != nil {
		return nil, err
	}
	return Load(data, opts)
}

// Load copies data so a caller may reuse its buffer. The buffer stays pinned
// until PDFium releases the document; no native pointer outlives its storage.
func Load(data []byte, opts Options) (*Document, error) {
	if len(data) == 0 || len(data) > 256<<20 {
		return nil, errors.New("pdf: empty document or document exceeds 256 MiB")
	}
	if strings.ContainsRune(opts.Password, 0) {
		return nil, errors.New("pdf: password contains NUL")
	}
	e, err := loadEngine(opts.Library)
	if err != nil {
		return nil, err
	}
	d := &Document{e: e, data: append([]byte(nil), data...)}
	d.pin.Pin(&d.data[0])
	engineMu.Lock()
	defer engineMu.Unlock()
	d.handle = e.load(unsafe.Pointer(&d.data[0]), uint64(len(d.data)), opts.Password)
	if d.handle == 0 {
		d.pin.Unpin()
		return nil, engineError(e.lastError())
	}
	fail := func(err error) (*Document, error) {
		e.closeDocument(d.handle)
		d.handle = 0
		d.pin.Unpin()
		return nil, err
	}
	n := e.countPages(d.handle)
	if n <= 0 || n > 100000 {
		return fail(errors.New("pdf: invalid page count"))
	}
	for i := int32(0); i < n; i++ {
		p := e.loadPage(d.handle, i)
		if p == 0 {
			return fail(engineError(e.lastError()))
		}
		size := Page{e.width(p), e.height(p)}
		e.closePage(p)
		if size.Width <= 0 || size.Height <= 0 || size.Width > 1e6 || size.Height > 1e6 || !validNumber(size.Width+size.Height) {
			return fail(errors.New("pdf: invalid page dimensions"))
		}
		d.pages = append(d.pages, size)
	}
	d.caps = Capabilities{Engine: "PDFium", Render: true, Search: e.search, Selection: e.text && e.permissions(d.handle)&16 != 0}
	return d, nil
}
func (d *Document) Capabilities() Capabilities {
	if d.closed.Load() {
		return Capabilities{Engine: "PDFium"}
	}
	return d.caps
}
func (d *Document) Pages() []Page {
	return append([]Page(nil), d.pages...)
}
func (d *Document) Close() error {
	d.closed.Store(true)
	engineMu.Lock()
	defer engineMu.Unlock()
	if d.handle != 0 {
		d.e.closeDocument(d.handle)
		d.handle = 0
		d.pin.Unpin()
		d.data = nil
	}
	return nil
}
func (d *Document) checkPage(page int) error {
	if d.handle == 0 || d.closed.Load() {
		return ErrClosed
	}
	if page < 0 || page >= len(d.pages) {
		return errors.New("pdf: page index out of range")
	}
	return nil
}

// Render rasterizes a zero-based page at the requested pixel size. Rotation is
// clockwise quarter turns. Annotations render; active content is never executed.
// Dimensions are bounded to 8192 per axis and 32 million pixels.
func (d *Document) Render(page, width, height, rotation int) (*image.RGBA, error) {
	if width <= 0 || height <= 0 || width > 8192 || height > 8192 || int64(width)*int64(height) > 32<<20 {
		return nil, errors.New("pdf: invalid or excessive render dimensions")
	}
	engineMu.Lock()
	defer engineMu.Unlock()
	if err := d.checkPage(page); err != nil {
		return nil, err
	}
	p := d.e.loadPage(d.handle, int32(page))
	if p == 0 {
		return nil, engineError(d.e.lastError())
	}
	defer d.e.closePage(p)
	out := image.NewRGBA(image.Rect(0, 0, width, height))
	var pin runtime.Pinner
	pin.Pin(&out.Pix[0])
	defer pin.Unpin()
	b := d.e.bitmapCreate(int32(width), int32(height), 4, unsafe.Pointer(&out.Pix[0]), int32(out.Stride))
	if b == 0 {
		return nil, errors.New("pdf: could not allocate bitmap")
	}
	defer d.e.bitmapDestroy(b)
	d.e.bitmapFill(b, 0, 0, int32(width), int32(height), 0xffffffff)
	d.e.render(b, p, 0, 0, int32(width), int32(height), int32((rotation%4+4)%4), 1) // FPDF_ANNOT
	for i := 0; i < len(out.Pix); i += 4 {
		out.Pix[i], out.Pix[i+2] = out.Pix[i+2], out.Pix[i]
	}
	return out, nil
}

// Character carries PDFium's character index, Unicode rune and top-left bounds
// in PDF points. Whitespace may have empty bounds.
type Character struct {
	Rune   rune
	Bounds ui.Rect
}
type Text struct{ Characters []Character }

func (t Text) String() string {
	var b strings.Builder
	for _, c := range t.Characters {
		b.WriteRune(c.Rune)
	}
	return b.String()
}
func (d *Document) text(page int) (Text, error) {
	p := d.e.loadPage(d.handle, int32(page))
	if p == 0 {
		return Text{}, engineError(d.e.lastError())
	}
	defer d.e.closePage(p)
	t := d.e.loadText(p)
	if t == 0 {
		return Text{}, errors.New("pdf: text page unavailable")
	}
	defer d.e.closeText(t)
	count := d.e.countChars(t)
	if count < 0 || count > 1<<20 {
		return Text{}, errors.New("pdf: invalid or excessive character count")
	}
	chars := make([]Character, count)
	for i := range chars {
		chars[i].Rune = rune(d.e.unicode(t, int32(i)))
		var l, r, b, top float64
		if d.e.charBox(t, int32(i), &l, &r, &b, &top) != 0 {
			// PDFium maps crop boxes, page rotation and origins to device coordinates.
			const scale = 100
			var x0, y0, x1, y1 int32
			pw, ph := int32(d.pages[page].Width*scale), int32(d.pages[page].Height*scale)
			if d.e.pageToDevice(p, 0, 0, pw, ph, 0, l, b, &x0, &y0) != 0 && d.e.pageToDevice(p, 0, 0, pw, ph, 0, r, top, &x1, &y1) != 0 {
				chars[i].Bounds = ui.Rect{X: float32(min(x0, x1)) / scale, Y: float32(min(y0, y1)) / scale, W: float32(abs(x1-x0)) / scale, H: float32(abs(y1-y0)) / scale}
			}
		}
	}
	return Text{chars}, nil
}

func (d *Document) pageTextForView(page int) (Text, error) {
	engineMu.Lock()
	defer engineMu.Unlock()
	if err := d.checkPage(page); err != nil {
		return Text{}, err
	}
	if !d.e.text {
		return Text{}, errors.ErrUnsupported
	}
	return d.text(page)
}

// PageText extracts character-level selectable text when permitted.
func (d *Document) PageText(page int) (Text, error) {
	engineMu.Lock()
	defer engineMu.Unlock()
	if err := d.checkPage(page); err != nil {
		return Text{}, err
	}
	if !d.caps.Selection {
		return Text{}, errors.ErrUnsupported
	}
	return d.text(page)
}

// Match identifies a search result by zero-based page and PDFium character range.
type Match struct{ Page, Start, Count int }

// Search uses PDFium's Unicode text search, with optional case sensitivity.
// It checks cancellation between pages and matches, and caps results at 10,000.
func (d *Document) Search(ctx context.Context, query string, matchCase bool) ([]Match, error) {
	if strings.ContainsRune(query, 0) {
		return nil, errors.New("pdf: query contains NUL")
	}
	if query == "" {
		return nil, nil
	}
	engineMu.Lock()
	defer engineMu.Unlock()
	if d.handle == 0 || d.closed.Load() {
		return nil, ErrClosed
	}
	if !d.caps.Search {
		return nil, errors.ErrUnsupported
	}
	encoded := append(utf16.Encode([]rune(query)), 0)
	var pin runtime.Pinner
	pin.Pin(&encoded[0])
	defer pin.Unpin()
	flags := uint32(0)
	if matchCase {
		flags = 1
	}
	var matches []Match
	for page := range d.pages {
		if d.closed.Load() {
			return nil, ErrClosed
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		err := func() error {
			p := d.e.loadPage(d.handle, int32(page))
			if p == 0 {
				return engineError(d.e.lastError())
			}
			defer d.e.closePage(p)
			t := d.e.loadText(p)
			if t == 0 {
				return errors.New("pdf: text page unavailable")
			}
			defer d.e.closeText(t)
			search := d.e.findStart(t, &encoded[0], flags, 0)
			if search == 0 {
				return errors.New("pdf: could not start search")
			}
			defer d.e.findClose(search)
			for d.e.findNext(search) != 0 {
				if err := ctx.Err(); err != nil {
					return err
				}
				matches = append(matches, Match{page, int(d.e.findIndex(search)), int(d.e.findCount(search))})
				if len(matches) >= 10000 {
					return errors.New("pdf: search exceeds 10,000 matches; narrow the query")
				}
			}
			return nil
		}()
		if err != nil {
			return matches, err
		}
	}
	runtime.KeepAlive(encoded)
	return matches, nil
}

func abs(n int32) int32 {
	if n < 0 {
		return -n
	}
	return n
}
