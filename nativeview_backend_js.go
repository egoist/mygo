//go:build js

package mygo

import "github.com/egoist/mygo/internal/platform"

// nativeViewSurfaceFor returns the surface s shows native views through in
// the browser: there are none. The browser's native view is the element the
// content paints, so the web backend defines no provider and this file names
// none of its code: a connection falls back to the optional interface
// (platform.NativeViewSurface), which a custom surface an app's tests show
// native views with may implement, and one that does not reports the
// unsupported error (errors.Is(err, errors.ErrUnsupported)).
func nativeViewSurfaceFor(s platform.Surface) platform.NativeViewSurface {
	return nativeViewSurfaceFallback(s)
}
