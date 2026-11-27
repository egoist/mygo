//go:build !windows

package texture

import "github.com/egoist/mygo/internal/scene"

// SharedTexture identifies a Windows Direct3D 11 shared BGRA8 texture.
// On non-Windows platforms, Share returns nil and PresentDXGI is a no-op.
type SharedTexture struct{}

// Share is a no-op on non-Windows platforms and returns nil.
func Share(uintptr) *SharedTexture { return nil }

// ID returns 0 on non-Windows platforms.
func (*SharedTexture) ID() uint64 { return 0 }

type dxgiState struct{}

// PresentDXGI is a no-op on non-Windows platforms.
func (*Texture) PresentDXGI(*SharedTexture, int, int, func(dst []byte) []byte) {}

func (*state) clearDXGILocked() {}

func (*state) isDXGILocked() bool { return false }

func (*state) dxgiFrame(any, func()) (*scene.Image, bool) { return nil, false }
