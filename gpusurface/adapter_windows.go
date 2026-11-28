//go:build windows

package gpusurface

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"

	"github.com/egoist/mygo/internal/gpu/d3d11/device"
)

var (
	procCreateDXGIFactory1 = syscall.NewLazyDLL(device.SystemDir() + `\dxgi.dll`).NewProc("CreateDXGIFactory1")
	procD3D11CreateDevice  = syscall.NewLazyDLL(device.SystemDir() + `\d3d11.dll`).NewProc("D3D11CreateDevice")
	iidIDXGIFactory1       = guid{0x770aae78, 0xf26f, 0x4dba, [8]byte{0xa8, 0x29, 0x25, 0x3c, 0x83, 0xd1, 0xb3, 0x87}}
)

const (
	factory1EnumAdapters1 = 12
	adapter1GetDesc1      = 10
	dxgiErrorNotFound     = 0x887a0002
)

// adapterDesc1 is DXGI_ADAPTER_DESC1.
type adapterDesc1 struct {
	Description                                                     [128]uint16
	VendorID, DeviceID, SubSysID, Revision                          uint32
	DedicatedVideoMemory, DedicatedSystemMemory, SharedSystemMemory uintptr
	LUID                                                            uint64
	Flags                                                           uint32
}

// newDeviceOn creates a device on the first adapter whose description
// contains name, ignoring case.
func newDeviceOn(name string) (dev, ctx uintptr, err error) {
	if err := procCreateDXGIFactory1.Find(); err != nil {
		return 0, 0, err
	}
	var factory uintptr
	if hr, _, _ := procCreateDXGIFactory1.Call(uintptr(unsafe.Pointer(&iidIDXGIFactory1)), uintptr(unsafe.Pointer(&factory))); failed(hr) {
		return 0, 0, fmt.Errorf("gpusurface: no DXGI factory: %#x", uint32(hr))
	}
	defer free(&factory)
	var seen []string
	for i := uintptr(0); ; i++ {
		var adapter uintptr
		if hr := call(factory, factory1EnumAdapters1, i, uintptr(unsafe.Pointer(&adapter))); uint32(hr) == dxgiErrorNotFound || failed(hr) {
			break
		}
		var d adapterDesc1
		call(adapter, adapter1GetDesc1, uintptr(unsafe.Pointer(&d)))
		desc := syscall.UTF16ToString(d.Description[:])
		seen = append(seen, desc)
		if !strings.Contains(strings.ToLower(desc), strings.ToLower(name)) {
			free(&adapter)
			continue
		}
		levels := []uint32{0xb100, 0xb000} // 11_1, 11_0
		const driverUnknown, bgraSupport, sdkVersion = 0, 0x20, 7
		hr, _, _ := procD3D11CreateDevice.Call(adapter, driverUnknown, 0, bgraSupport,
			uintptr(unsafe.Pointer(&levels[0])), uintptr(len(levels)), sdkVersion,
			uintptr(unsafe.Pointer(&dev)), 0, uintptr(unsafe.Pointer(&ctx)))
		free(&adapter)
		if failed(hr) {
			return 0, 0, fmt.Errorf("gpusurface: cannot create a device on %s: %#x", desc, uint32(hr))
		}
		return dev, ctx, nil
	}
	return 0, 0, fmt.Errorf("gpusurface: no adapter matches %q (adapters: %s)", name, strings.Join(seen, "; "))
}

// AdapterName returns the description of the adapter the surface draws on.
func (s *Surface) AdapterName() string { return s.adapterName }
