//go:build windows && (amd64 || arm64)

package windows

import (
	"errors"
	"fmt"
	"sync"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

// ErrNoDesktop is what capturing a screen fails with in a session that has no
// desktop to copy: a service, or a remote shell that does not run on the
// logged-in desktop.
var ErrNoDesktop = errors.New("windows: this session has no desktop")

// This file reads the screen through GDI: Windows draws every window into the
// display's own surface, and BitBlt copies a display's pixels out of it. It is
// what the system has always offered an app that wants the screen as a
// picture, and unlike Desktop Duplication it also works in a remote session
// that has a desktop but no GPU output to duplicate.
//
// Windows' newer, change-driven way is DXGI Desktop Duplication, which needs a
// Direct3D device and hands over textures on the GPU; it is the upgrade path
// once there is a reason to pay for it.

var (
	user32 = syscall.NewLazyDLL("user32.dll")
	gdi32  = syscall.NewLazyDLL("gdi32.dll")

	enumDisplayMonitors = user32.NewProc("EnumDisplayMonitors")
	getMonitorInfoW     = user32.NewProc("GetMonitorInfoW")
	getDC               = user32.NewProc("GetDC")
	releaseDC           = user32.NewProc("ReleaseDC")
	createCompatibleDC  = gdi32.NewProc("CreateCompatibleDC")
	deleteDC            = gdi32.NewProc("DeleteDC")
	createDIBSection    = gdi32.NewProc("CreateDIBSection")
	selectObject        = gdi32.NewProc("SelectObject")
	deleteObject        = gdi32.NewProc("DeleteObject")
	bitBlt              = gdi32.NewProc("BitBlt")

	monitorEnumOnce sync.Once
	monitorEnumFn   uintptr
	monitorEnumMu   sync.Mutex
	monitorEnumDst  *[]Monitor
)

// A Monitor is a display Windows draws on.
type Monitor struct {
	// Name is what Windows calls the display: `\\.\DISPLAY1`.
	Name string
	// Left, Top, Right and Bottom are its rectangle in the screen's
	// coordinates, in pixels.
	Left, Top, Right, Bottom int
	// Primary is whether this is the display the system calls the main one.
	Primary bool
}

// Width and Height are the display's size in pixels.
func (m Monitor) Width() int  { return m.Right - m.Left }
func (m Monitor) Height() int { return m.Bottom - m.Top }

// Monitors lists the displays Windows draws on.
func Monitors() ([]Monitor, error) {
	monitorEnumOnce.Do(func() {
		monitorEnumFn = syscall.NewCallback(func(hmon, hdc, rect, data uintptr) uintptr {
			var info monitorInfoEx
			info.CbSize = uint32(unsafe.Sizeof(info))
			if r, _, _ := getMonitorInfoW.Call(hmon, uintptr(unsafe.Pointer(&info))); r == 0 {
				return 1 // keep going: one bad display does not hide the others
			}
			m := Monitor{
				Name:    utf16String(info.SzDevice[:]),
				Left:    int(info.RcMonitor.Left),
				Top:     int(info.RcMonitor.Top),
				Right:   int(info.RcMonitor.Right),
				Bottom:  int(info.RcMonitor.Bottom),
				Primary: info.DwFlags&monitorInfoPrimary != 0,
			}
			monitorEnumMu.Lock()
			*monitorEnumDst = append(*monitorEnumDst, m)
			monitorEnumMu.Unlock()
			return 1
		})
	})
	monitorEnumMu.Lock()
	out := []Monitor{}
	monitorEnumDst = &out
	monitorEnumMu.Unlock()
	defer func() {
		monitorEnumMu.Lock()
		monitorEnumDst = nil
		monitorEnumMu.Unlock()
	}()
	r, _, err := enumDisplayMonitors.Call(0, 0, monitorEnumFn, 0)
	if r == 0 {
		return nil, fmt.Errorf("windows: listing displays: %w", err)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("windows: the machine lists no displays")
	}
	return out, nil
}

// CaptureMonitor copies a monitor's picture into dst, which must hold four
// bytes for every pixel, in the order blue, green, red and alpha.
func CaptureMonitor(m Monitor, dst []byte) error {
	w, h := m.Width(), m.Height()
	if w <= 0 || h <= 0 {
		return fmt.Errorf("windows: the display %q is %dx%d", m.Name, w, h)
	}
	if len(dst) < w*h*4 {
		return fmt.Errorf("windows: a picture of %dx%d needs %d bytes, not %d", w, h, w*h*4, len(dst))
	}
	screen, _, err := getDC.Call(0)
	if screen == 0 {
		return fmt.Errorf("windows: opening the screen: %w", err)
	}
	defer releaseDC.Call(0, screen)
	mem, _, err := createCompatibleDC.Call(screen)
	if mem == 0 {
		return fmt.Errorf("windows: a memory surface for the screen: %w", err)
	}
	defer deleteDC.Call(mem)

	info := bitmapInfo{Header: bitmapInfoHeader{
		Size:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		Width:       int32(w),
		Height:      int32(-h), // negative: rows from the top, as Go wants them
		Planes:      1,
		BitCount:    32,
		Compression: biRGB,
	}}
	var bits unsafe.Pointer
	bitmap, _, err := createDIBSection.Call(screen, uintptr(unsafe.Pointer(&info)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bitmap == 0 {
		return fmt.Errorf("windows: a picture surface for the screen: %w", err)
	}
	defer deleteObject.Call(bitmap)
	if old, _, _ := selectObject.Call(mem, bitmap); old != 0 {
		defer selectObject.Call(mem, old)
	}
	if r, _, err := bitBlt.Call(mem, 0, 0, uintptr(w), uintptr(h), screen, uintptr(m.Left), uintptr(m.Top), srcCopy); r == 0 {
		// A session with no desktop hands out a screen it cannot copy, and
		// says the handle is invalid rather than that there is no desktop.
		if errno, ok := err.(syscall.Errno); ok && errno == 6 {
			return ErrNoDesktop
		}
		return fmt.Errorf("windows: copying the screen: %w", err)
	}
	copy(dst[:w*h*4], unsafe.Slice((*byte)(bits), w*h*4))
	return nil
}

// What GetMonitorInfoW's flags say.
const monitorInfoPrimary = 1

// What BitBlt's raster operation copies.
const srcCopy = 0x00cc0020

// What a bitmap's header says about its pixels.
const biRGB = 0

type rect struct {
	Left, Top, Right, Bottom int32
}

type monitorInfoEx struct {
	CbSize    uint32
	RcMonitor rect
	RcWork    rect
	DwFlags   uint32
	SzDevice  [32]uint16
}

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [1]uint32
}

// utf16String reads a NUL-terminated string Windows keeps.
func utf16String(b []uint16) string {
	n := 0
	for n < len(b) && b[n] != 0 {
		n++
	}
	return string(utf16.Decode(b[:n]))
}
