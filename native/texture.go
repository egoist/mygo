package native

import "github.com/egoist/mygo/internal/texture"

// Texture is an image on the GPU that a module's implementation draws
// into, and that native UI shows with no copy through the CPU: it is a
// ui.ImageSource, as a *ui.Bitmap is, so ui.Image shows it, and it can be
// covered, clipped, scaled, rotated and faded like any image.
//
// On macOS, pass an IOSurfaceRef to Present (in Swift, NativeTexture.present
// accepts an IOSurface or IOSurface-backed CVPixelBuffer). On Windows, pass a
// Direct3D 11 BGRA8 NT shared handle guarded by an IDXGIKeyedMutex (key 0) to
// PresentDXGI, which the D3D11 renderer opens via OpenSharedResource1 with no
// CPU copy. Both wake windows showing the texture without calling Invalidate.
type Texture = texture.Texture

// NewTexture makes a texture, which shows nothing until the implementation
// presents a frame.
func NewTexture() *Texture { return texture.New() }
