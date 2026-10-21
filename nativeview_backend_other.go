//go:build !darwin && !(linux && (amd64 || arm64)) && !(windows && (amd64 || arm64)) && !js

package mygo

import "github.com/egoist/mygo/internal/platform"

// nativeViewSurfaceFor returns the surface s shows native views through on
// the platforms whose backend has none: there are no native views to show.
// This file names no platform code at all, so an app on such a platform
// carries nothing for the feature: a connection falls back to the optional
// interface (platform.NativeViewSurface), which a custom surface an app's
// tests show native views with may implement, and one that does not reports
// the unsupported error (errors.Is(err, errors.ErrUnsupported)).
func nativeViewSurfaceFor(s platform.Surface) platform.NativeViewSurface {
	return nativeViewSurfaceFallback(s)
}
