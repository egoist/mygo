package mobile

import (
	"github.com/egoist/mygo/internal/mobile/wire"
	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/transfer"
)

// surface is the drawing area of a window of native UI.
type surface struct {
	w *window
}

func newSurface(w *window) *surface { return &surface{w: w} }

func (s *surface) resized(size, insets bool) {}
func (s *surface) closed()                   {}
func (s *surface) themeChanged()             {}
func (s *surface) idle()                     {}
func (s *surface) back() bool                { return false }

// handleSurface handles the events of surfaces, and reports whether op
// was one.
func (b *Backend) handleSurface(op uint64, r *wire.Reader, w *wire.Writer) bool { return false }

var _ = platform.Insets{}

func (s *surface) Native() platform.SurfaceNative { return platform.SurfaceNative{} }
func (s *surface) Size() (float64, float64, float64) {
	return s.w.width, s.w.height, s.w.scale
}
func (s *surface) RequestFrame()                             {}
func (s *surface) RefreshRate() float64                      { return 60 }
func (s *surface) PresentPixels([]byte, int, int, int)       {}
func (s *surface) SetCursor(platform.Cursor)                 {}
func (s *surface) SetTextInput(platform.TextInputState)      {}
func (s *surface) UpdateAccessibility(*platform.AccessTree)  {}
func (s *surface) StartDataDrag(platform.DragRequest)        {}
func (s *surface) CancelDataDrag()                           {}
func (s *surface) SetDropFormats([]transfer.Format)          {}
func (s *surface) PlaceWebViews([]platform.WebViewPlacement) {}
func (s *surface) NewWebView(*platform.WindowOptions, platform.WindowHandler) (platform.WebView, error) {
	return nil, platform.ErrUnsupported
}
