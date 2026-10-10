# Native content

A Go-only app with image, PDF and video panes. The image pane works without
extra libraries and starts with a generated pattern. PDF uses the included
two-page document; install PDFium to enable it. Video uses libmpv and starts
paused when a `-video` source is supplied.

```sh
go run ./examples/content-native -image photo.jpg \
  -pdfium /absolute/path/libpdfium.dylib \
  -mpv /absolute/path/libmpv.2.dylib -video movie.mp4
```

On Linux use `.so` paths, on Windows `.dll`. The `-pdf` flag replaces the
sample document. `MYGO_PDFIUM_LIBRARY` and `MYGO_MPV_LIBRARY` provide defaults
for the library flags. PDF search runs off the UI thread and cancels the
previous search when another starts. Close disposes every component.

See [image viewing](../../docs/plugins/imageview.md), [PDF](../../docs/plugins/pdf.md)
and [video](../../docs/plugins/video.md) for APIs and packaging. For a packaged
app, put libraries and all their dependencies under your platform's
`resources/` directory as described there; there are no npm dependencies.
