// Package camera shows what a phone's camera sees in a window's native UI, and
// takes photos and reads the QR codes it sees: AVCaptureSession on iOS, CameraX
// on Android (with ZXing for the codes). For a photo in the system's own camera
// screen, use package imagepicker: it needs no permission.
//
//	cam, err := camera.New(win, camera.Options{})
//	...
//	ui.WebView(c, cam).Fill() // in the view; what it draws after shows over the preview
//	path, err := cam.TakePhoto(ctx)
//
// To scan a QR code, as an app imports a link or a key shown on another screen:
//
//	cam, err := camera.New(win, camera.Options{OnCode: func(code camera.Code) {
//		link = code.Text
//		win.Update(func() {})
//	}})
//
// The preview shows under the content, as a web view does. On desktops and in
// browsers New reports errors.ErrUnsupported.
package camera

import (
	"context"
	"encoding/json"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/surface"
)

const pluginID = "camera"

// Options are which camera shows, and what it reads.
type Options struct {
	// Front chooses the camera on the screen's side.
	Front bool
	// OnCode, when set, is called on the main thread with each QR code that
	// comes into the camera's view: once while it stays in view, as several
	// codes in view are told once each. A code read again after it was out of
	// view for a second is told again.
	OnCode func(Code)
}

// Code is a QR code the camera saw.
type Code struct {
	// Text is what the code holds.
	Text string
}

// Camera is a preview of a camera, which takes photos. Show it with ui.WebView.
type Camera struct {
	view *mygo.PluginView
	off  func()
}

// New asks the user for the camera, the first time, and makes its preview in a
// window showing native UI.
func New(w *mygo.Window, o Options) (*Camera, error) {
	v, err := mygo.NewPluginView(w, "camera", map[string]any{"front": o.Front, "codes": o.OnCode != nil})
	if err != nil {
		return nil, err
	}
	c := &Camera{view: v}
	if fn := o.OnCode; fn != nil {
		c.off = mygo.Native(pluginID).On("code", func(data []byte) {
			var e struct {
				ID   int64  `json:"id"`
				Text string `json:"text"`
			}
			if json.Unmarshal(data, &e) == nil && e.ID == int64(v.ID) {
				fn(Code{Text: e.Text})
			}
		})
	}
	return c, nil
}

// SurfaceWebView gives package ui the view to show.
func (c *Camera) SurfaceWebView(conn *surface.Conn) platform.WebView {
	return c.view.SurfaceWebView(conn)
}

// TakePhoto takes a photo and returns the path of its JPEG in the app's
// temporary directory.
func (c *Camera) TakePhoto(ctx context.Context) (string, error) {
	var r struct {
		Path string `json:"path"`
	}
	err := mygo.Native(pluginID).Call(ctx, "takePhoto", map[string]any{"id": c.view.ID}, &r)
	return r.Path, err
}

// Destroy stops the camera and takes its preview away.
func (c *Camera) Destroy() {
	if c.off != nil {
		c.off()
	}
	go mygo.Native(pluginID).Call(context.Background(), "dispose", map[string]any{"id": c.view.ID}, nil)
	c.view.Destroy()
}
