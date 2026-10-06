//go:build windows && (amd64 || arm64)

package windows

import (
	"errors"
	"fmt"
	"math"
	"runtime"
	"unsafe"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/printdoc"
)

var (
	procPrintDlgExW         = systemDLL("comdlg32.dll").NewProc("PrintDlgExW")
	procPrintStartDoc       = gdi32.NewProc("StartDocW")
	procPrintEndDoc         = gdi32.NewProc("EndDoc")
	procPrintAbortDoc       = gdi32.NewProc("AbortDoc")
	procPrintStartPage      = gdi32.NewProc("StartPage")
	procPrintEndPage        = gdi32.NewProc("EndPage")
	procPrintGetDeviceCaps  = gdi32.NewProc("GetDeviceCaps")
	procPrintStretchDIBits  = gdi32.NewProc("StretchDIBits")
	procPrintSetStretchMode = gdi32.NewProc("SetStretchBltMode")
	procPrintSetBrushOrigin = gdi32.NewProc("SetBrushOrgEx")
)

type printPageRange struct{ From, To uint32 }

// PRINTDLGEXW from commdlg.h; all supported architectures use this 64-bit
// layout. No callbacks are needed: the system dispatches its modal loop.
type printDialogEx struct {
	Size                                            uint32
	Owner, DevMode, DevNames, DC                    uintptr
	Flags, Flags2, Exclusion, RangeCount, MaxRanges uint32
	Ranges                                          *printPageRange
	MinPage, MaxPage, Copies                        uint32
	Instance, Template, Callback                    uintptr
	PropertyPages                                   uint32
	PropertyPageHandles                             uintptr
	StartPage, ResultAction                         uint32
}

type printDocInfo struct {
	Size                   int32
	Name, Output, DataType *uint16
	Type                   uint32
}

func (b *Backend) PrintContent(parent platform.Window, job *platform.PrintJob, done func(error)) {
	w, ok := parent.(*window)
	if !ok || w.closed {
		done(errors.New("mygo: window has been destroyed"))
		return
	}
	ranges := [32]printPageRange{}
	d := printDialogEx{Owner: w.hwnd, Flags: 0x100 | 0x4 | 0x800000 | 0x40000 | 0x100000 | 0x80000, // RETURNDC, NOSELECTION, NOCURRENTPAGE, USEDEVMODECOPIESANDCOLLATE, HIDEPRINTTOFILE, DISABLEPRINTTOFILE
		Exclusion: 1, MaxRanges: uint32(len(ranges)), Ranges: &ranges[0], MinPage: 1, MaxPage: uint32(len(job.Pages)), Copies: 1, StartPage: ^uint32(0)}
	d.Size = uint32(unsafe.Sizeof(d))
	hr, _, _ := procPrintDlgExW.Call(uintptr(unsafe.Pointer(&d)))
	defer func() {
		if d.DC != 0 {
			procDeleteDC.Call(d.DC)
		}
		if d.DevMode != 0 {
			procGlobalFree.Call(d.DevMode)
		}
		if d.DevNames != 0 {
			procGlobalFree.Call(d.DevNames)
		}
	}()
	if failed(hr) {
		done(fmt.Errorf("mygo: print dialog: HRESULT %#x", uint32(hr)))
		return
	}
	if d.ResultAction != 1 {
		done(platform.ErrPrintCanceled)
		return
	}
	if w.closed {
		done(errors.New("mygo: window has been destroyed"))
		return
	}
	if d.DC == 0 {
		done(errors.New("mygo: printer returned no device context"))
		return
	}
	// With USEDEVMODECOPIESANDCOLLATE the driver handles copies/collation.
	var pages []int
	for page := range job.Pages {
		selected := d.Flags&0x2 == 0 // PD_PAGENUMS
		for _, r := range ranges[:min(int(d.RangeCount), len(ranges))] {
			if uint32(page+1) >= r.From && uint32(page+1) <= r.To {
				selected = true
			}
		}
		if selected {
			pages = append(pages, page)
		}
	}
	if len(pages) == 0 {
		done(errors.New("mygo: no print pages selected"))
		return
	}
	info := printDocInfo{Name: u16(job.Title)}
	info.Size = int32(unsafe.Sizeof(info))
	started, _, startErr := procPrintStartDoc.Call(d.DC, uintptr(unsafe.Pointer(&info)))
	if int32(started) <= 0 {
		done(fmt.Errorf("mygo: starting print job: %v", startErr))
		return
	}
	finished := false
	defer func() {
		if !finished {
			procPrintAbortDoc.Call(d.DC)
		}
	}()
	aw, _, _ := procPrintGetDeviceCaps.Call(d.DC, 8)  // HORZRES: printable pixels
	ah, _, _ := procPrintGetDeviceCaps.Call(d.DC, 10) // VERTRES
	dx, _, _ := procPrintGetDeviceCaps.Call(d.DC, 88) // LOGPIXELSX
	dy, _, _ := procPrintGetDeviceCaps.Call(d.DC, 90) // LOGPIXELSY
	if aw == 0 || ah == 0 || dx == 0 || dy == 0 {
		done(errors.New("mygo: printer has no printable area"))
		return
	}
	// Some printers have different horizontal/vertical DPI. Fit in physical
	// points first, then convert each axis back to its own device pixels.
	sx, sy := float64(dx)/72, float64(dy)/72
	x, y, width, height := printdoc.Fit(job.Width, job.Height, float64(aw)/sx, float64(ah)/sy)
	x, y, width, height = x*sx, y*sy, width*sx, height*sy
	procPrintSetStretchMode.Call(d.DC, 4) // HALFTONE
	procPrintSetBrushOrigin.Call(d.DC, 0, 0, 0)
	for _, page := range pages {
		started, _, e := procPrintStartPage.Call(d.DC)
		if int32(started) <= 0 {
			done(fmt.Errorf("mygo: starting print page: %v", e))
			return
		}
		img := job.Pages[page]
		pix := printdoc.BGRA(img)
		bi := bitmapInfoHeader{Width: int32(img.Rect.Dx()), Height: -int32(img.Rect.Dy()), Planes: 1, BitCount: 32}
		bi.Size = uint32(unsafe.Sizeof(bi))
		drawn, _, e := procPrintStretchDIBits.Call(d.DC, uintptr(math.Round(x)), uintptr(math.Round(y)), uintptr(math.Round(width)), uintptr(math.Round(height)),
			0, 0, uintptr(img.Rect.Dx()), uintptr(img.Rect.Dy()), uintptr(unsafe.Pointer(&pix[0])), uintptr(unsafe.Pointer(&bi)), 0, 0x00cc0020)
		runtime.KeepAlive(pix)
		if int32(drawn) <= 0 {
			done(fmt.Errorf("mygo: drawing print page: %v", e))
			return
		}
		ended, _, e := procPrintEndPage.Call(d.DC)
		if int32(ended) <= 0 {
			done(fmt.Errorf("mygo: ending print page: %v", e))
			return
		}
	}
	ended, _, endErr := procPrintEndDoc.Call(d.DC)
	if int32(ended) <= 0 {
		done(fmt.Errorf("mygo: finishing print job: %v", endErr))
		return
	}
	finished = true
	done(nil)
}
