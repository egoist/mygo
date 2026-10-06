# PDF viewer

`github.com/egoist/mygo/plugins/pdf` is an optional Go-only component for
[native UI](../ui/README.md), backed by the native [PDFium C API](https://pdfium.googlesource.com/pdfium/+/refs/heads/main/public/fpdfview.h).
It uses purego, works with `CGO_ENABLED=0`, and needs no npm package or Bun.

```go
doc, err := pdf.Open("report.pdf", pdf.Options{Library: "/path/to/libpdfium.dylib"})
if err != nil { return err }
viewer, err := pdf.NewViewer(doc)
if err != nil { doc.Close(); return err }
win := mygo.NewWindow(mygo.WindowOptions{
    Content: ui.View(func(c *ui.Context) { pdf.View(c, viewer).Fill() }),
})
win.OnClosed(func() { viewer.Close() })
// After App.Run returns: wait for viewer.Done(), then doc.Close().
```

`Load([]byte, Options)` copies a document into pinned storage that remains
valid until native disposal. `Password` opens encrypted PDFs when supported.
Files are limited to 256 MiB and 100,000 pages. `Pages()` returns zero-based
page sizes in PDF points (1/72 inch). `Render(page, width, height, rotation)`
returns RGBA pixels; rotation is in clockwise quarter turns. Render requests
are bounded to 8192 per axis and 32 million pixels.

`View` includes previous/next page, zoom, fit and fit-width controls. Page
Up/Down navigate, `+`/`−` zoom, `0` fits, and Ctrl/Command + wheel zooms around
the pointer. Drag selects characters when allowed; Alt+drag and the wheel
pan. Ctrl/Command+C and the Edit menu's Copy role copy selected text.
`SetPage`, `SetZoom` and `SetFit` provide the same controls programmatically.
Zoom multiplies the selected fit scale. Navigation clears old selections;
rendering uses one worker and a coalesced request queue, so rapid changes
cannot accumulate rendering goroutines. Raster resolution follows viewport
and zoom, at up to two pixels per DIP within the size limit.

`Document.Capabilities()` reports loaded native symbols and document copy
permissions. Text selection/extraction requires PDFium's text API and the
copy permission. `PageText` returns characters and bounds mapped through
PDFium's page-to-device conversion, including crop boxes and inherent page
rotation. `SelectedText` returns the viewer's current selection.

`Search(ctx, query, matchCase)` uses [PDFium Unicode text search](https://pdfium.googlesource.com/pdfium/+/refs/heads/main/public/fpdf_text.h),
returns page/character ranges, checks cancellation between pages/matches,
and limits results to 10,000. `Viewer.Find` selects the first match, and
`NextMatch(1)` / `NextMatch(-1)` wrap through matches, change pages and reveal
the active match at the current zoom. Run
search on a goroutine for large documents. The example supplies a search
field and cancels an earlier query when another starts. Image-only PDFs
have no text to search or select; OCR is not supplied.

All public methods are goroutine-safe. PDFium is process-wide serialized,
including across documents. Metadata/capability reads never wait for rendering.
Worker decoding and search do not touch a
platform view; all layout, rendering and input use MyGo's existing UI thread
and renderer. `Viewer.Close` never waits for its worker on the UI thread;
`Done` signals disposal. The viewer owns its worker and page bitmap, while
the caller owns `Document.Close`. Closing a document during work is safe:
subsequent operations return `ErrClosed`. PDFium's loaded library and
initialization stay for the process lifetime; pages, text pages, search
handles, bitmaps and documents are released deterministically.

## Installation and packaging

Supply a 64-bit PDFium build exporting the public C API for the app's
OS/architecture, including its dependent libraries and license notices.
[PDFium build instructions](https://pdfium.googlesource.com/pdfium/+/refs/heads/main/README.md)
and [prebuilt distribution source](https://github.com/bblanchon/pdfium-binaries)
are starting points. Builds without V8 are sufficient; this viewer never
executes JavaScript, submits forms or follows links automatically.

| Target | Native library | Additional requirement |
| --- | --- | --- |
| macOS amd64/arm64 | `libpdfium.dylib` | matching architecture, all dylib dependencies |
| Linux amd64/arm64 | `libpdfium.so` | matching libc/distribution and dependent libraries |
| Windows amd64/arm64 | `pdfium.dll` | matching architecture, dependency DLLs beside it |
| Other targets | unavailable | compiles, loading returns `errors.ErrUnsupported` |

Set `Options.Library` to an explicit path, preferably
`filepath.Join(mygo.App.Path(mygo.PathResources), "libpdfium.dylib")` in a
packaged app. An explicit path is tried alone. With no path, the loader tries
the app's packaged files, then system names (and Homebrew locations on macOS).
Windows requires packaged DLLs or an explicit path and does not search the
current working directory. Missing libraries/symbols produce a usable Go
error. Missing optional text symbols disable the corresponding capabilities.

Use existing [resource packaging](../configuration.md) to ship the engine:

```text
resources/
  darwin-arm64/libpdfium.dylib
  darwin-amd64/libpdfium.dylib
  linux-arm64/libpdfium.so
  linux-amd64/libpdfium.so
  windows-amd64/pdfium.dll
  windows-arm64/pdfium.dll
```

Only supply the targets you build. Include all dependencies and configure
relative dylib install names / ELF rpaths so the engine finds them in the
bundle; dependency DLLs should be beside the engine. MyGo signs packaged
native code with the app's signing settings. Universal macOS builds need
matching arm64/amd64 files. This plugin has no automatic binary download or
pinned `mygo-plugin.json`: the app explicitly chooses and packages PDFium.

This release supplies single-page viewing, native text search and selection.
It does not expose continuous page layout, interactive forms, annotations
editing, printing, tagged-PDF accessibility or OCR. PDF page pixels carry a
focusable label; the toolbar and search field are standard accessible MyGo
controls. Text ranges are not exposed as a complete PDF accessibility tree.
Native-view hosting is independent shared infrastructure; this renderer
needs no hosted NSView/GtkWidget/HWND and has no dependency on
[native-view hosting PR #121](https://github.com/egoist/mygo/pull/121).

Use `examples/content-native -pdfium /path/to/library` to view the bundled
sample, or add `-pdf report.pdf`. Set `MYGO_PDFIUM_LIBRARY` to run native
integration tests with `CGO_ENABLED=0 go test ./plugins/pdf`.
