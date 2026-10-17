//go:build windows && (amd64 || arm64)

package mygo

import (
	"github.com/egoist/mygo/internal/platform"
	win "github.com/egoist/mygo/internal/windows"
)

// nativeViewSurfaceFor returns the surface s shows native views through on
// Windows: this backend's opt-in provider, which the core reaches from a
// native view alone, so that an app that makes none carries neither its
// hosting code nor what it names (AGENTS.md: apps pay only for what they
// use). A surface of another backend, or one of a custom implementation,
// falls back to the optional interface (platform.NativeViewSurface), which
// the fake surface of the core's own tests implements; one that does neither
// hosts no native view.
func nativeViewSurfaceFor(s platform.Surface) platform.NativeViewSurface {
	if ns, ok := win.NativeViewSurface(s); ok {
		return ns
	}
	return nativeViewSurfaceFallback(s)
}
