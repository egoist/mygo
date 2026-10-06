package platform

import (
	"errors"
	"image"
)

// ErrPrintCanceled reports that the user dismissed a native print dialog.
var ErrPrintCanceled = errors.New("mygo: printing canceled")

// PrintLayout is already validated/defaulted by the core. Lengths are points
// (72 per inch); Scale is pixels per point. Pages include their margins.
type PrintLayout struct {
	Width, Height            float64
	Top, Right, Bottom, Left float64
	Scale                    float64
}

// MaxPrintPixels bounds the memory of a job (256 MiB of RGBA, before PDF
// compression), and MaxPrintPages bounds native page-range integers.
const MaxPrintPixels = 64 * 1024 * 1024
const MaxPrintPages = 1000

// PrintJob is immutable until done is called. PDF is the same paginated
// raster document as Pages; PDFKit uses it for native pagination on macOS.
// Other backends draw Pages directly, without starting a webview.
type PrintJob struct {
	Title         string
	Width, Height float64       // oriented page dimensions in points
	Pages         []*image.RGBA // top-down, premultiplied sRGB
	PDF           []byte
}
