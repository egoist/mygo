# Image viewer

`github.com/egoist/mygo/plugins/imageview` is an optional Go-only component
for [native UI](../ui/README.md). It uses MyGo's existing bitmap renderer,
including its downsampled image levels. It requires no native codec library,
webview, npm package or runtime Bun.

```go
viewer := imageview.New()
if err := viewer.Open("photo.jpg"); err != nil { return err }
win := mygo.NewWindow(mygo.WindowOptions{
    Content: ui.View(func(c *ui.Context) { imageview.View(c, viewer).Fill() }),
})
win.OnClosed(viewer.Close)
```

`SetImage(image.Image)` copies an application's image; `Load([]byte)` decodes
PNG, JPEG, GIF's first frame, WebP or BMP. JPEG EXIF orientation is respected.
Images are limited to 64 million pixels. Large loads can run in a goroutine;
completion invalidates the view. A later load or close supersedes an older
load, and a loading error clears the previous image instead of leaving stale
content. `State` exposes size, transform, loading, error and closed state.

The component supports:

- Drag or scroll to pan. Pan clamps at the edges, centering axes that fit.
- Ctrl/Command + scroll to zoom around the pointer. `+` and `−` zoom too.
- `0` or `SetFit(FitContain)` fits the whole image and follows resizing;
  `FitWidth` fits the width; `1` or `FitActual` shows one image pixel per DIP.
- `R` or `Rotate(1)` turns clockwise by 90 degrees. Negative turns rotate
  backward. Repeated turns use the original pixels, preserving quality.
- `SetZoom` multiplies the selected fit scale, bounded to 1/64 through 64.
  `Pan` moves in DIPs, and the arrow keys pan by 40 DIPs.

`Transform` is also a standalone value for document viewports: `Rect` gives
the displayed content bounds, `ZoomAt` preserves the content under a point,
and `Clamp` keeps content reachable. Protect a shared transform yourself;
`Viewer` methods already synchronize their state.

A viewer shows in one view at a time. Explicitly `Close` it when its owner
closes; closing is idempotent and releases image/cached bitmap references.
Rendering, input and clipboard operations remain on MyGo's UI thread.
The component uses normal MyGo clipping, focus and image accessibility labels.
It supports sRGB static images; animated GIF/WebP, ICC color management,
HDR, arbitrary-angle rotation and tiled huge-image decoding are not exposed.

Run `go run ./examples/content-native` for an app that works with no extra
installation, or supply `-image /path/photo.jpg`.
