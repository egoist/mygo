//go:build windows && (amd64 || arm64)

// Package windows is media's own way into Windows' own media stack: Media
// Foundation, called through COM, without cgo.
//
// It is media's own rather than the framework's internal/windows, whose every
// method every Windows app links in: a program that does not import media
// carries none of this.
package windows

import (
	"errors"
	"fmt"
	"sync"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

// A GUID is a COM class or interface identifier.
type GUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

// String writes the GUID the way COM does.
func (g GUID) String() string {
	return fmt.Sprintf("%08x-%04x-%04x-%02x%02x-%02x%02x%02x%02x%02x%02x",
		g.Data1, g.Data2, g.Data3,
		g.Data4[0], g.Data4[1], g.Data4[2], g.Data4[3], g.Data4[4], g.Data4[5], g.Data4[6], g.Data4[7])
}

// guid parses "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx".
func guid(s string) GUID {
	hex := func(s string) uint64 {
		var v uint64
		for _, c := range s {
			v <<= 4
			switch {
			case c >= '0' && c <= '9':
				v |= uint64(c - '0')
			case c >= 'a' && c <= 'f':
				v |= uint64(c-'a') + 10
			case c >= 'A' && c <= 'F':
				v |= uint64(c-'A') + 10
			}
		}
		return v
	}
	g := GUID{Data1: uint32(hex(s[0:8])), Data2: uint16(hex(s[9:13])), Data3: uint16(hex(s[14:18]))}
	g.Data4[0], g.Data4[1] = byte(hex(s[19:21])), byte(hex(s[21:23]))
	for i := range 6 {
		g.Data4[2+i] = byte(hex(s[24+2*i : 26+2*i]))
	}
	return g
}

// native turns an address of memory Windows owns into a pointer.
func native(addr uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&addr)) }

// u16 is s as a NUL-terminated UTF-16 string, which must stay reachable for
// as long as the call it is passed to.
func u16(s string) *uint16 { return &utf16z(s)[0] }

// utf16z is u16 as a slice, NUL included.
func utf16z(s string) []uint16 {
	b := make([]uint16, 0, len(s)+1)
	for _, r := range s {
		if r == 0 {
			continue
		}
		if r >= 0x10000 {
			r1, r2 := utf16.EncodeRune(r)
			b = append(b, uint16(r1), uint16(r2))
			continue
		}
		b = append(b, uint16(r))
	}
	return append(b, 0)
}

// HRESULTs media tells apart. An error this package returns carries one, and
// errors.As recovers it as an *Error.
const (
	NotImplemented            uint32 = 0x80004001 // E_NOTIMPL: no Media Foundation at all
	AccessDenied              uint32 = 0x80070005
	FileNotFound              uint32 = 0x80070002
	PathNotFound              uint32 = 0x80070003
	NoMoreTypes               uint32 = 0xC00D36B9
	InvalidStreamNumber       uint32 = 0xC00D36B3
	UnsupportedByteStreamType uint32 = 0xC00D36C4
	UnsupportedScheme         uint32 = 0xC00D36C3
	InvalidMediaType          uint32 = 0xC00D36B4
	InvalidRequest            uint32 = 0xC00D36B2
	NotFound                  uint32 = 0xC00D36D5
	AttributeNotFound         uint32 = 0xC00D36E6
	EndOfStream               uint32 = 0xC00D3E84
	CodecNotFound             uint32 = 0xC00D5212
	ByteStreamNotSeekable     uint32 = 0xC00D36EE
)

// Other HRESULTs this package uses itself.
const (
	sOK          uintptr = 0x00000000
	sFalse       uintptr = 0x00000001
	eNoInterface uintptr = 0x80004002
)

// An Error is a failed Media Foundation or COM call, with the HRESULT
// Windows gave, so that a caller can tell one failure from another.
type Error struct {
	// What is the call that failed.
	What string
	// HR is the HRESULT, as the constants above spell it.
	HR uint32
}

func (e *Error) Error() string {
	return fmt.Sprintf("media: %s failed (HRESULT %#08x)", e.What, e.HR)
}

// failed reports whether hr is an error rather than a success code.
func failed(hr uintptr) bool { return int32(uint32(hr)) < 0 }

// hresultError is a failed call as an error.
func hresultError(what string, hr uintptr) error {
	return &Error{What: what, HR: uint32(hr)}
}

// vtable slots, as the interfaces are written: slot 0 is QueryInterface.
const (
	slotQueryInterface = 0
	slotAddRef         = 1
	slotRelease        = 2
)

// comCall calls method i of the COM interface obj. Pointers converted to
// uintptr in the call stay valid until it returns (go:uintptrescapes).
//
//go:uintptrescapes
func comCall(obj uintptr, i int, args ...uintptr) uintptr {
	vtbl := *(*uintptr)(native(obj))
	fn := *(*uintptr)(unsafe.Add(native(vtbl), i*int(unsafe.Sizeof(uintptr(0)))))
	var a [16]uintptr
	a[0] = obj
	n := copy(a[1:], args)
	r, _, _ := syscall.SyscallN(fn, a[:n+1]...)
	return r
}

func addRef(obj uintptr) {
	if obj != 0 {
		comCall(obj, slotAddRef)
	}
}

func release(obj uintptr) {
	if obj != 0 {
		comCall(obj, slotRelease)
	}
}

// ReleaseObject gives up an interface this package handed back as an address,
// which is what a caller of ActivateObject holds.
func ReleaseObject(obj uintptr) { release(obj) }

// IsAccessDenied reports whether err is Windows refusing a device to this app,
// which its privacy settings do.
func IsAccessDenied(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.HR == AccessDenied
}

// wstring copies a wide string Windows owns, up to its NUL.
func wstring(p *uint16) string {
	if p == nil {
		return ""
	}
	n := 0
	for q := unsafe.Pointer(p); *(*uint16)(unsafe.Add(q, n*2)) != 0; n++ {
	}
	return syscall.UTF16ToString(unsafe.Slice(p, n))
}

// queryInterface returns obj's implementation of iid, or 0.
func queryInterface(obj uintptr, iid *GUID) uintptr {
	if obj == 0 {
		return 0
	}
	var out uintptr
	if failed(comCall(obj, slotQueryInterface, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out)))) {
		return 0
	}
	return out
}

// The DLLs Media Foundation lives in. Windows N without the Media Feature
// Pack has none of them, which Find reports.
var (
	mfplat      = syscall.NewLazyDLL("mfplat.dll")
	mfreadwrite = syscall.NewLazyDLL("mfreadwrite.dll")
	// The device enumerator is exported by mf.dll rather than by mfplat.dll,
	// where the rest of this package's entry points are.
	mf       = syscall.NewLazyDLL("mf.dll")
	ole32    = syscall.NewLazyDLL("ole32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")

	procMFStartup                           = mfplat.NewProc("MFStartup")
	procMFCreateAttributes                  = mfplat.NewProc("MFCreateAttributes")
	procMFCreateMediaType                   = mfplat.NewProc("MFCreateMediaType")
	procMFCreateSample                      = mfplat.NewProc("MFCreateSample")
	procMFCreateMemoryBuffer                = mfplat.NewProc("MFCreateMemoryBuffer")
	procMFTEnumEx                           = mfplat.NewProc("MFTEnumEx")
	procMFCreateFile                        = mfplat.NewProc("MFCreateFile")
	procMFGetStrideForBitmapInfoHeader      = mfplat.NewProc("MFGetStrideForBitmapInfoHeader")
	procMFEnumDeviceSources                 = mf.NewProc("MFEnumDeviceSources")
	procMFCreateSourceReaderFromURL         = mfreadwrite.NewProc("MFCreateSourceReaderFromURL")
	procMFCreateSourceReaderFromByteStream  = mfreadwrite.NewProc("MFCreateSourceReaderFromByteStream")
	procMFCreateSourceReaderFromMediaSource = mfreadwrite.NewProc("MFCreateSourceReaderFromMediaSource")
	procCoInitializeEx                      = ole32.NewProc("CoInitializeEx")
	procCoTaskMemFree                       = ole32.NewProc("CoTaskMemFree")
	procShellExecuteW                       = shell32.NewProc("ShellExecuteW")
	procTlsAlloc                            = kernel32.NewProc("TlsAlloc")
	procTlsGetValue                         = kernel32.NewProc("TlsGetValue")
	procTlsSetValue                         = kernel32.NewProc("TlsSetValue")
)

// MF_VERSION is MF_SDK_VERSION 2 and MF_API_VERSION 0x70, the pairing this
// package was written against.
const mfVersion = 0x00020070

var (
	loadOnce sync.Once
	loadErr  error
)

// Load starts Media Foundation, once for the process, and says what went
// wrong when it cannot: Windows N without the Media Feature Pack has no
// Media Foundation, and says so rather than failing without a word.
func Load() error {
	loadOnce.Do(func() { loadErr = load() })
	return loadErr
}

func load() error {
	procs := []*syscall.LazyProc{
		procMFStartup, procMFCreateAttributes, procMFCreateMediaType,
		procMFCreateSample, procMFCreateMemoryBuffer, procMFTEnumEx,
		procMFCreateFile, procMFGetStrideForBitmapInfoHeader,
		procMFCreateSourceReaderFromURL, procMFCreateSourceReaderFromByteStream,
	}
	for _, p := range procs {
		if err := p.Find(); err != nil {
			return fmt.Errorf("Media Foundation is not available on this Windows (%v)", err)
		}
	}
	enter()
	// MFSTARTUP_FULL, whose every call Windows asks to be matched by
	// MFShutdown. Media is media: an app that plays a file may play
	// another an hour later, so the platform stays started for the
	// process's life rather than for a file's.
	if hr, _, _ := procMFStartup.Call(mfVersion, 0); failed(hr) {
		return hresultError("MFStartup", hr)
	}
	return nil
}

// Enter puts the calling thread in the multithreaded apartment, once per
// thread: Media Foundation's work queues are multithreaded, and Go moves work
// between threads as it likes. A thread-local slot says whether this thread
// has called CoInitializeEx already, so that the count is one per thread
// rather than one per call. It is called by every entry point that reaches
// Media Foundation, since the thread a call lands on is not the caller's to
// choose.
func Enter() { enter() }

// enter puts the calling thread in the multithreaded apartment, once per
// thread: Media Foundation's work queues are multithreaded, and Go moves work
// between threads as it likes. A thread-local slot says whether this thread
// has called CoInitializeEx already, so that the count is one per thread
// rather than one per call.
func enter() {
	tlsOnce.Do(func() {
		if i, _, _ := procTlsAlloc.Call(); i != tlsOutOfIndexes {
			tlsIndex, tlsOK = i, true
		}
	})
	if !tlsOK {
		return
	}
	if v, _, _ := procTlsGetValue.Call(tlsIndex); v != 0 {
		return
	}
	// COINIT_MULTITHREADED. A thread already in another apartment
	// (RPC_E_CHANGED_MODE) is left as it is: Media Foundation's objects
	// are free threaded, so its calls work from there too.
	procCoInitializeEx.Call(0, coinitMultithreaded)
	procTlsSetValue.Call(tlsIndex, 1)
}

const (
	tlsOutOfIndexes     = ^uintptr(0)
	coinitMultithreaded = 0x0
)

var (
	tlsOnce  sync.Once
	tlsIndex uintptr
	tlsOK    bool
)
