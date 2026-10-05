//go:build windows

package d3d11

import (
	"fmt"
	"unsafe"
)

// NewDevice creates a Direct3D 11 device and its immediate context as New
// does, on the GPU or, when there is none, on WARP, for code that draws
// without a Renderer, such as through DirectComposition. software reports
// WARP. The caller releases the device and the context.
func NewDevice() (device, context uintptr, software bool, err error) {
	if err := procD3D11CreateDevice.Find(); err != nil {
		return 0, 0, false, err
	}
	levels := []uint32{0xb000, 0xa100, 0xa000} // 11_0, 10_1, 10_0
	const bgraSupport = 0x20
	const hardware, warp = 1, 5
	var hr uintptr
	for _, driver := range []uintptr{hardware, warp} {
		var level uint32
		hr, _, _ = procD3D11CreateDevice.Call(0, driver, 0, bgraSupport,
			uintptr(unsafe.Pointer(&levels[0])), uintptr(len(levels)), 7,
			uintptr(unsafe.Pointer(&device)), uintptr(unsafe.Pointer(&level)), uintptr(unsafe.Pointer(&context)))
		if !failed(hr) {
			return device, context, driver == warp, nil
		}
	}
	return 0, 0, false, fmt.Errorf("d3d11: cannot create a device: %#x", uint32(hr))
}
