package gpusurface

import "fmt"

// The package's API is the same on every platform, where it does
// something or not: these compile only if each platform's files declare
// all of it, with the same signatures. go vet and go test -c for each
// GOOS check it.

var (
	_ func(Options) (*Surface, error)           = New
	_ func() int64                              = Now
	_ func(fw, fh, dw, dh int) (x, y, w, h int) = FitRect

	_ error = ErrUnsupported
	_ error = ErrFormat
	_ error = ErrHDRNeeds10Bit

	_ int          = MaxOverlayRects
	_ [4]Format    = [...]Format{FormatBGRA8, FormatNV12, FormatP010, FormatBGR10A2}
	_ fmt.Stringer = Format(0)

	_ = Options{Window: 0, Adapter: "", Timings: 0, Composite: false, Drawable10Bit: false, HDR10: false}
	_ = Timing{Submitted: 0, Encoded: 0, Presented: 0}
	_ = OverlayRect{X: 0, Y: 0, W: 0, H: 0, Radius: 0, Color: [4]float32{}, UV: [4]float32{}}
)

var _ interface {
	Present(uintptr) error
	PresentTagged(uintptr, int64) error
	PresentSlice(texture uintptr, slice, width, height int) error
	OnPresented(func(tag, submitted, presented int64))
	Timings() []Timing

	Resize(w, h int) error
	Size() (w, h int)
	Scale() float64
	ScreenSize() (w, h int)

	SetDirect(bool) error
	Direct() bool
	SetHDR10(bool) error
	HDR10() bool
	SetOverlay([]OverlayRect)
	SetOverlayAtlas(pix []byte, w, h int) error

	Device() uintptr
	Queue() uintptr
	Context() uintptr
	SwapChain() uintptr
	Tearing() bool
	AdapterName() string

	Close()
} = (*Surface)(nil)
