package native

import "github.com/egoist/mygo/internal/texture"

// Texture is an image on the GPU that a module's implementation draws
// into, and that native UI shows with no copy through the CPU: it is a
// ui.ImageSource, as a *ui.Bitmap is, so ui.Image shows it, and it can be
// covered, clipped, scaled, rotated and faded like any image. Pass it to a
// method of the module; in Swift it is a NativeTexture, whose present
// shows a frame (an IOSurface, or a CVPixelBuffer backed by one) in the
// next frame of the windows showing the texture, without the app calling
// Invalidate.
//
// On Windows, Go producers can wrap a Direct3D 11 BGRA8 NT shared handle
// (guarded by an IDXGIKeyedMutex on key 0) with ShareTexture and pass it to
// Texture.PresentDXGI from any goroutine, which opens the texture in the D3D11
// renderer with no CPU copy and wakes windows showing the texture.
type Texture = texture.Texture

// NewTexture makes a texture, which shows nothing until the implementation
// presents a frame.
func NewTexture() *Texture { return texture.New() }

// SharedTexture identifies a Windows Direct3D 11 shared BGRA8 texture
// (NT handle with an IDXGIKeyedMutex on key 0) with a process-wide unique ID
// for renderer caches.
type SharedTexture = texture.SharedTexture

// ShareTexture wraps a Windows Direct3D 11 BGRA8 NT shared handle with a
// process-wide unique ID shared across all producers in the app (including
// ui.Video). Returns nil if handle is 0 or on non-Windows platforms.
func ShareTexture(handle uintptr) *SharedTexture { return texture.Share(handle) }
