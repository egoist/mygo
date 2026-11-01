package main

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	user32   = syscall.NewLazyDLL("user32.dll")
	pdh      = syscall.NewLazyDLL("pdh.dll")
	d3d11    = syscall.NewLazyDLL("d3d11.dll")

	queryPerformanceCounter   = kernel32.NewProc("QueryPerformanceCounter")
	queryPerformanceFrequency = kernel32.NewProc("QueryPerformanceFrequency")
	queryThreadCycleTime      = kernel32.NewProc("QueryThreadCycleTime")
	queryProcessCycleTime     = kernel32.NewProc("QueryProcessCycleTime")
	getCurrentThread          = kernel32.NewProc("GetCurrentThread")
	getSystemTimes            = kernel32.NewProc("GetSystemTimes")
	getProcessMemoryInfo      = kernel32.NewProc("K32GetProcessMemoryInfo")

	setDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	enumWindows            = user32.NewProc("EnumWindows")
	getWindowThreadProcess = user32.NewProc("GetWindowThreadProcessId")
	isWindowVisible        = user32.NewProc("IsWindowVisible")
	getClientRect          = user32.NewProc("GetClientRect")
	clientToScreen         = user32.NewProc("ClientToScreen")
	getDpiForWindow        = user32.NewProc("GetDpiForWindow")
	setWindowPos           = user32.NewProc("SetWindowPos")
	postMessage            = user32.NewProc("PostMessageW")
	windowFromPoint        = user32.NewProc("WindowFromPoint")
	screenToClient         = user32.NewProc("ScreenToClient")

	pdhOpenQuery                = pdh.NewProc("PdhOpenQueryW")
	pdhAddEnglishCounter        = pdh.NewProc("PdhAddEnglishCounterW")
	pdhCollectQueryData         = pdh.NewProc("PdhCollectQueryData")
	pdhGetFormattedCounterArray = pdh.NewProc("PdhGetFormattedCounterArrayW")
	pdhCloseQuery               = pdh.NewProc("PdhCloseQuery")
	d3d11CreateDevice           = d3d11.NewProc("D3D11CreateDevice")
	windowProcCallback          = syscall.NewCallback(enumWindowsProc)
	foundWindows                []foundWindow
)

// qpc returns the performance counter, the clock of the duplication API's
// LastPresentTime.
func qpc() int64 {
	var t int64
	queryPerformanceCounter.Call(uintptr(unsafe.Pointer(&t)))
	return t
}

var qpcHz = func() float64 {
	var f int64
	queryPerformanceFrequency.Call(uintptr(unsafe.Pointer(&f)))
	return float64(f)
}()

// calibrateCycles returns how many cycles QueryThreadCycleTime counts a
// second, from spinning alone on a thread, keeping the fastest of a few tries
// as the others were interrupted.
func calibrateCycles() float64 {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	h, _, _ := getCurrentThread.Call()
	var best float64
	for range 5 {
		var c0, c1 uint64
		queryThreadCycleTime.Call(h, uintptr(unsafe.Pointer(&c0)))
		t0 := qpc()
		for float64(qpc()-t0)/qpcHz < 0.1 {
		}
		queryThreadCycleTime.Call(h, uintptr(unsafe.Pointer(&c1)))
		if hz := float64(c1-c0) / (float64(qpc()-t0) / qpcHz); hz > best {
			best = hz
		}
	}
	return best
}

// systemBusy returns how long all processors have been busy, in seconds.
func systemBusy() float64 {
	var idle, kernel, user syscall.Filetime
	getSystemTimes.Call(uintptr(unsafe.Pointer(&idle)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user)))
	// Kernel time counts idle time too.
	return float64(kernel.Nanoseconds()+user.Nanoseconds()-idle.Nanoseconds()) / 1e9
}

const processQueryLimited = 0x1000

func processCycles(pid int) (uint64, error) {
	h, err := syscall.OpenProcess(processQueryLimited, false, uint32(pid))
	if err != nil {
		return 0, err
	}
	defer syscall.CloseHandle(h)
	var c uint64
	if ok, _, err := queryProcessCycleTime.Call(uintptr(h), uintptr(unsafe.Pointer(&c))); ok == 0 {
		return 0, err
	}
	return c, nil
}

// processMemoryCounters is PROCESS_MEMORY_COUNTERS_EX2, of Windows 10 1809.
type processMemoryCounters struct {
	cb                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
	PrivateUsage               uintptr
	PrivateWorkingSetSize      uintptr
	SharedCommitUsage          uint64
}

func memoryOf(pid int) (processMemoryCounters, error) {
	var c processMemoryCounters
	h, err := syscall.OpenProcess(processQueryLimited, false, uint32(pid))
	if err != nil {
		return c, err
	}
	defer syscall.CloseHandle(h)
	c.cb = uint32(unsafe.Sizeof(c))
	if ok, _, err := getProcessMemoryInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&c)), uintptr(c.cb)); ok == 0 {
		return c, err
	}
	return c, nil
}

type foundWindow struct {
	hwnd uintptr
	pid  uint32
}

func enumWindowsProc(hwnd, _ uintptr) uintptr {
	var pid uint32
	getWindowThreadProcess.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if v, _, _ := isWindowVisible.Call(hwnd); v != 0 {
		foundWindows = append(foundWindows, foundWindow{hwnd, pid})
	}
	return 1
}

type rect struct{ left, top, right, bottom int32 }

// window is where a process's window shows, in pixels of the screen.
type window struct {
	hwnd       uintptr
	x, y, w, h int
	dpi        int
}

// point returns where x, y DIPs of the window's content are on the screen.
func (w window) point(x, y float64) (int, int) {
	return w.x + w.client(x), w.y + w.client(y)
}

// client returns v DIPs in pixels.
func (w window) client(v float64) int { return int(v * float64(w.dpi) / 96) }

// findWindow returns the visible window of pid with the most content.
func findWindow(pid int) (window, bool) {
	foundWindows = foundWindows[:0]
	enumWindows.Call(windowProcCallback, 0)
	var best window
	for _, f := range foundWindows {
		if f.pid != uint32(pid) {
			continue
		}
		var r rect
		getClientRect.Call(f.hwnd, uintptr(unsafe.Pointer(&r)))
		if int(r.right)*int(r.bottom) <= best.w*best.h {
			continue
		}
		var p [2]int32
		clientToScreen.Call(f.hwnd, uintptr(unsafe.Pointer(&p)))
		dpi, _, _ := getDpiForWindow.Call(f.hwnd)
		best = window{hwnd: f.hwnd, x: int(p[0]), y: int(p[1]), w: int(r.right), h: int(r.bottom), dpi: int(dpi)}
	}
	return best, best.hwnd != 0
}

// keepOnTop has nothing cover the window, as Windows stops showing frames of
// what shows nowhere, without taking the keyboard.
func keepOnTop(hwnd uintptr) {
	const topmost, noSize, noMove, noActivate = ^uintptr(0), 0x1, 0x2, 0x10
	setWindowPos.Call(hwnd, topmost, 0, 0, 0, 0, noSize|noMove|noActivate)
}

// postMouseMove sends the pointer's move to x, y of the screen to the message
// queue of the window there, as the system's input thread does: MyGo draws in
// a child window, GPUI in the top-level one.
func postMouseMove(x, y int) {
	const wmMouseMove = 0x0200
	pt := [2]int32{int32(x), int32(y)}
	target, _, _ := windowFromPoint.Call(uintptr(uint32(pt[0])) | uintptr(uint32(pt[1]))<<32)
	screenToClient.Call(target, uintptr(unsafe.Pointer(&pt)))
	postMessage.Call(target, wmMouseMove, 0, uintptr(uint16(pt[1]))<<16|uintptr(uint16(pt[0])))
}

// gpuTime reads how long the GPU's 3D engine has run a process's work
// (the GPU Engine\Running Time counter, as Task Manager does).
type gpuTime struct{ query, counter uintptr }

func newGPUTime(pid int) (*gpuTime, error) {
	g := &gpuTime{}
	if st, _, _ := pdhOpenQuery.Call(0, 0, uintptr(unsafe.Pointer(&g.query))); st != 0 {
		return nil, fmt.Errorf("PdhOpenQuery: %#x", st)
	}
	path, _ := syscall.UTF16PtrFromString(fmt.Sprintf(`\GPU Engine(pid_%d_*engtype_3D)\Running Time`, pid))
	if st, _, _ := pdhAddEnglishCounter.Call(g.query, uintptr(unsafe.Pointer(path)), 0, uintptr(unsafe.Pointer(&g.counter))); st != 0 {
		return nil, fmt.Errorf("PdhAddEnglishCounter: %#x", st)
	}
	return g, nil
}

func (g *gpuTime) close() { pdhCloseQuery.Call(g.query) }

// seconds returns the running time of the process's 3D engines.
func (g *gpuTime) seconds() (float64, error) {
	if st, _, _ := pdhCollectQueryData.Call(g.query); st != 0 {
		return 0, fmt.Errorf("PdhCollectQueryData: %#x", st)
	}
	const fmtLarge = 0x400
	var size, count uint32
	st, _, _ := pdhGetFormattedCounterArray.Call(g.counter, fmtLarge, uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&count)), 0)
	const moreData = 0x800007D2
	if st != moreData {
		return 0, fmt.Errorf("PdhGetFormattedCounterArray: %#x", st)
	}
	buf := make([]uint64, size/8+1)
	st, _, _ = pdhGetFormattedCounterArray.Call(g.counter, fmtLarge, uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&buf[0])))
	if st != 0 {
		return 0, fmt.Errorf("PdhGetFormattedCounterArray: %#x", st)
	}
	// PDH_FMT_COUNTERVALUE_ITEM_W: a name, a status, then the value, in 100 ns.
	var sum uint64
	for i := range count {
		sum += buf[int(i)*3+2]
	}
	return float64(sum) / 1e7, nil
}

// ptr turns an address from outside Go's heap into a pointer.
func ptr(v uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&v)) }

// call makes a COM call by vtable index. The pointers among args stay valid
// until it returns.
//
//go:uintptrescapes
func call(obj uintptr, i int, args ...uintptr) uintptr {
	vtbl := *(*uintptr)(ptr(obj))
	fn := *(*uintptr)(unsafe.Add(ptr(vtbl), i*int(unsafe.Sizeof(uintptr(0)))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{obj}, args...)...)
	return r
}

func release(obj uintptr) {
	if obj != 0 {
		call(obj, 2)
	}
}

func failed(hr uintptr) bool { return int32(uint32(hr)) < 0 }

type guid struct {
	a    uint32
	b, c uint16
	d    [8]byte
}

var (
	iidDXGIDevice   = guid{0x54ec77fa, 0x1377, 0x44e6, [8]byte{0x8c, 0x32, 0x88, 0xfd, 0x5f, 0x44, 0xc8, 0x4c}}
	iidDXGIOutput1  = guid{0x00cddea8, 0x939b, 0x4b83, [8]byte{0xa3, 0x40, 0xa6, 0x85, 0x22, 0x66, 0x66, 0xcc}}
	iidD3D11Texture = guid{0x6f15aaf2, 0xd208, 0x4e89, [8]byte{0x9a, 0xb4, 0x48, 0x95, 0x35, 0xd3, 0x4f, 0x9c}}
)

// frameInfo is DXGI_OUTDUPL_FRAME_INFO.
type frameInfo struct {
	LastPresentTime, LastMouseUpdateTime int64
	AccumulatedFrames                    uint32
	RectsCoalesced, ProtectedMaskedOut   int32
	PointerX, PointerY, PointerVisible   int32
	TotalMetadataBufferSize              uint32
	PointerShapeBufferSize               uint32
}

// outduplDesc is DXGI_OUTDUPL_DESC.
type outduplDesc struct {
	Width, Height                     uint32
	RefreshNum, RefreshDen            uint32
	Format, ScanlineOrdering, Scaling uint32
	Rotation                          uint32
	DesktopImageInSystemMemory        int32
}

// texture2DDesc is D3D11_TEXTURE2D_DESC.
type texture2DDesc struct {
	Width, Height, MipLevels, ArraySize         uint32
	Format, SampleCount, SampleQuality          uint32
	Usage, BindFlags, CPUAccessFlags, MiscFlags uint32
}

// duplicator sees what the display shows, through the Desktop Duplication
// API, which also tells when each image reached the display.
type duplicator struct {
	dev, ctx, dup uintptr
	staging       uintptr
	stW, stH      uint32
	hz            float64
	screenW       int
	screenH       int
}

func newDuplicator() (*duplicator, error) {
	d := &duplicator{}
	var level uint32
	const hardware, sdkVersion = 1, 7
	if hr, _, _ := d3d11CreateDevice.Call(0, hardware, 0, 0, 0, 0, sdkVersion, uintptr(unsafe.Pointer(&d.dev)), uintptr(unsafe.Pointer(&level)), uintptr(unsafe.Pointer(&d.ctx))); failed(hr) {
		return nil, fmt.Errorf("D3D11CreateDevice: %#x", uint32(hr))
	}
	var dxgiDev, adapter, output, output1 uintptr
	defer func() { release(dxgiDev); release(adapter); release(output); release(output1) }()
	if hr := call(d.dev, 0, uintptr(unsafe.Pointer(&iidDXGIDevice)), uintptr(unsafe.Pointer(&dxgiDev))); failed(hr) {
		return nil, fmt.Errorf("IDXGIDevice: %#x", uint32(hr))
	}
	if hr := call(dxgiDev, 7, uintptr(unsafe.Pointer(&adapter))); failed(hr) {
		return nil, fmt.Errorf("GetAdapter: %#x", uint32(hr))
	}
	if hr := call(adapter, 7, 0, uintptr(unsafe.Pointer(&output))); failed(hr) {
		return nil, fmt.Errorf("EnumOutputs: %#x", uint32(hr))
	}
	if hr := call(output, 0, uintptr(unsafe.Pointer(&iidDXGIOutput1)), uintptr(unsafe.Pointer(&output1))); failed(hr) {
		return nil, fmt.Errorf("IDXGIOutput1: %#x", uint32(hr))
	}
	if hr := call(output1, 22, d.dev, uintptr(unsafe.Pointer(&d.dup))); failed(hr) {
		return nil, fmt.Errorf("DuplicateOutput: %#x", uint32(hr))
	}
	var desc outduplDesc
	call(d.dup, 7, uintptr(unsafe.Pointer(&desc)))
	const bgra8 = 87
	if desc.Format != bgra8 {
		d.close()
		return nil, fmt.Errorf("the desktop's format is %d, not BGRA8", desc.Format)
	}
	d.hz = float64(desc.RefreshNum) / float64(desc.RefreshDen)
	d.screenW, d.screenH = int(desc.Width), int(desc.Height)
	return d, nil
}

func (d *duplicator) close() {
	release(d.staging)
	release(d.dup)
	release(d.ctx)
	release(d.dev)
}

var errNoFrame = errors.New("the display showed nothing new")

// next waits up to timeout for a new image of the desktop, and hands it to
// use with when it reached the display. It returns errNoFrame after timeout.
func (d *duplicator) next(timeout time.Duration, use func(info *frameInfo, image uintptr) error) error {
	var info frameInfo
	var res uintptr
	hr := call(d.dup, 8, uintptr(timeout.Milliseconds()), uintptr(unsafe.Pointer(&info)), uintptr(unsafe.Pointer(&res)))
	const waitTimeout = 0x887A0027
	if uint32(hr) == waitTimeout {
		return errNoFrame
	}
	if failed(hr) {
		return fmt.Errorf("AcquireNextFrame: %#x", uint32(hr))
	}
	defer release(res)
	defer call(d.dup, 14)
	return use(&info, res)
}

// read copies a region of the image and hands it to use as rows of BGRA.
func (d *duplicator) read(image uintptr, x, y, w, h int, use func(pix []byte, pitch int)) error {
	if d.staging == 0 || int(d.stW) < w || int(d.stH) < h {
		release(d.staging)
		d.staging = 0
		const bgra8, usageStaging, cpuRead = 87, 3, 0x20000
		desc := texture2DDesc{Width: uint32(w), Height: uint32(h), MipLevels: 1, ArraySize: 1, Format: bgra8, SampleCount: 1, Usage: usageStaging, CPUAccessFlags: cpuRead}
		if hr := call(d.dev, 5, uintptr(unsafe.Pointer(&desc)), 0, uintptr(unsafe.Pointer(&d.staging))); failed(hr) {
			return fmt.Errorf("CreateTexture2D: %#x", uint32(hr))
		}
		d.stW, d.stH = uint32(w), uint32(h)
	}
	var tex uintptr
	if hr := call(image, 0, uintptr(unsafe.Pointer(&iidD3D11Texture)), uintptr(unsafe.Pointer(&tex))); failed(hr) {
		return fmt.Errorf("the image's texture: %#x", uint32(hr))
	}
	defer release(tex)
	box := [6]uint32{uint32(x), uint32(y), 0, uint32(x + w), uint32(y + h), 1}
	call(d.ctx, 46, d.staging, 0, 0, 0, 0, tex, 0, uintptr(unsafe.Pointer(&box)))
	var mapped struct {
		pData      uintptr
		rowPitch   uint32
		depthPitch uint32
	}
	const mapRead = 1
	if hr := call(d.ctx, 14, d.staging, 0, mapRead, 0, uintptr(unsafe.Pointer(&mapped))); failed(hr) {
		return fmt.Errorf("Map: %#x", uint32(hr))
	}
	defer call(d.ctx, 15, d.staging, 0)
	use(unsafe.Slice((*byte)(ptr(mapped.pData)), int(mapped.rowPitch)*h), int(mapped.rowPitch))
	return nil
}
