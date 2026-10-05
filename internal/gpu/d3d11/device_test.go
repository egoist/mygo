//go:build windows

package d3d11

import (
	"testing"
	"unsafe"
)

// TestNewDevice checks that NewDevice gives a device and a context, and
// that the device is a DXGI device, which DirectComposition needs.
func TestNewDevice(t *testing.T) {
	device, context, software, err := NewDevice()
	if err != nil {
		t.Fatal(err)
	}
	defer free(&device)
	defer free(&context)
	if device == 0 || context == 0 {
		t.Fatalf("device %#x, context %#x", device, context)
	}
	var dxgi uintptr
	if hr := call(device, 0, uintptr(unsafe.Pointer(&iidIDXGIDevice)), uintptr(unsafe.Pointer(&dxgi))); failed(hr) {
		t.Fatalf("the device is no DXGI device: %#x", uint32(hr))
	}
	free(&dxgi)
	t.Logf("software (WARP): %v", software)
}
