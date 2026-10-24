//go:build darwin

// Package darwin is the Objective-C, CoreMedia, CoreVideo and AVFoundation
// plumbing the media package needs, without cgo: the same approach the
// framework's own internal/darwin takes, kept apart from it so that media
// stands on its own and an app that imports only media drags in only this.
//
// Everything here is loaded with dlopen on first use. Message sends are
// registered once each, with a type of their own, rather than built per
// call: a frame costs one or two sends and nothing else.
package darwin

import (
	"runtime"
	"structs"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

// CMTime is CoreMedia's time: a value over a timescale, with flags and an
// epoch.
type CMTime struct {
	_         structs.HostLayout
	Value     int64
	Timescale int32
	Flags     uint32
	Epoch     int64
}

// CMTimeRange is a start and a duration.
type CMTimeRange struct {
	_        structs.HostLayout
	Start    CMTime
	Duration CMTime
}

// CGSize is a width and a height.
type CGSize struct {
	_      structs.HostLayout
	Width  float64
	Height float64
}

// Pixel formats CoreVideo names by their four bytes, and the lock flag and
// format id of CoreAudio that go with them.
const (
	PixelFormat32ARGB = 0x41524742 // 'ARGB'
	PixelFormat32BGRA = 0x42475241 // 'BGRA'
	PixelFormat32RGBA = 0x52474241 // 'RGBA'

	// The formats a camera delivers, which capture outputs hand over as they
	// come: they do not convert for the app.
	PixelFormat2vuy = 0x32767579 // '2vuy', 4:2:2 packed
	PixelFormat420v = 0x34323076 // '420v', 4:2:0 bi-planar, video range
	PixelFormat420f = 0x34323066 // '420f', 4:2:0 bi-planar, full range

	LockReadOnly = 1

	// FormatLinearPCM is CoreAudio's kAudioFormatLinearPCM.
	FormatLinearPCM = 0x6c70636d // 'lpcm'
)

var (
	loadOnce sync.Once
	loadErr  error

	// libobjc and the frameworks, held for symbols looked up later.
	libs   []uintptr
	libObj uintptr

	classes sync.Map // name -> uintptr
	sels    sync.Map // name -> uintptr
	globals sync.Map // name -> uintptr

	sendID    func(id, sel uintptr) uintptr
	sendID1   func(id, sel, a uintptr) uintptr
	sendID2   func(id, sel, a, b uintptr) uintptr
	sendID3   func(id, sel, a, b, c uintptr) uintptr
	sendIDD   func(id, sel uintptr, d float64) uintptr
	sendVoid  func(id, sel uintptr)
	sendVoid1 func(id, sel, a uintptr)
	sendVoid2 func(id, sel, a, b uintptr)
	sendVoid3 func(id, sel, a, b, c uintptr)
	sendVoid4 func(id, sel, a, b, c, d uintptr)
	sendVoidT func(id, sel uintptr, r CMTimeRange)
	sendBool  func(id, sel uintptr) bool
	sendBool1 func(id, sel, a uintptr) bool
	sendInt   func(id, sel uintptr) int
	sendUint  func(id, sel uintptr) uintptr
	sendTime  func(id, sel uintptr) CMTime
	sendTimeR func(id, sel uintptr) CMTimeRange
	sendSize  func(id, sel uintptr) CGSize

	objectGetClass      func(id uintptr) uintptr
	objcRetain          func(id uintptr) uintptr
	objcRelease         func(id uintptr)
	objcAutorelease     func(id uintptr) uintptr
	objcPoolPush        func() uintptr
	objcPoolPop         func(p uintptr)
	objcGetClassByName  func(name *byte) uintptr
	objcRegisterSelName func(name *byte) uintptr

	// CoreFoundation
	cfRelease func(p uintptr)

	// CoreMedia
	cmSampleBufferPTS          func(sb uintptr) CMTime
	cmSampleBufferDuration     func(sb uintptr) CMTime
	cmSampleBufferImage        func(sb uintptr) uintptr
	cmSampleBufferSamples      func(sb uintptr) int
	cmSampleBufferData         func(sb uintptr) uintptr
	cmSampleBufferFormat       func(sb uintptr) uintptr
	cmBlockBufferLength        func(bb uintptr) int
	cmBlockBufferCopy          func(bb uintptr, at, n int, dst unsafe.Pointer) int32
	cmTimeGetSeconds           func(t CMTime) float64
	cmTimeRangeMake            func(start, duration CMTime) CMTimeRange
	cmTimeMakeWithSeconds      func(seconds float64, timescale int32) CMTime
	cmAudioFormatDescription   func(fd uintptr) uintptr
	cmVideoFormatDescriptionSz func(fd uintptr) CMVideoDimensions
	cmFormatDescriptionSubType func(fd uintptr) uint32

	// CoreVideo
	cvPixelBufferLock   func(pb uintptr, flags uint64) int32
	cvPixelBufferUnlock func(pb uintptr, flags uint64) int32
	cvPixelBufferBase   func(pb uintptr) unsafe.Pointer
	cvPixelBufferStride func(pb uintptr) int
	cvPixelBufferWidth  func(pb uintptr) int
	cvPixelBufferHeight func(pb uintptr) int
	cvPixelBufferFormat func(pb uintptr) uint32

	cvPixelBufferPlanes      func(pb uintptr) int
	cvPixelBufferPlaneBase   func(pb uintptr, plane int) unsafe.Pointer
	cvPixelBufferPlaneStride func(pb uintptr, plane int) int
	globalPixelFormatKey     uintptr
)

// CMVideoDimensions is a coded picture's size.
type CMVideoDimensions struct {
	_      structs.HostLayout
	Width  int32
	Height int32
}

// AudioStreamBasicDescription is CoreAudio's description of a sound's
// format.
type AudioStreamBasicDescription struct {
	_                structs.HostLayout
	SampleRate       float64
	FormatID         uint32
	FormatFlags      uint32
	BytesPerPacket   uint32
	FramesPerPacket  uint32
	BytesPerFrame    uint32
	ChannelsPerFrame uint32
	BitsPerChannel   uint32
	Reserved         uint32
}

// Load loads everything this package needs, once. It returns an error when
// a framework is missing, which a Mac always has.
func Load() error {
	loadOnce.Do(open)
	return loadErr
}

func open() {
	libObj, loadErr = purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
	if loadErr != nil {
		return
	}
	frameworks := []string{
		"/System/Library/Frameworks/Foundation.framework/Foundation",
		"/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation",
		"/System/Library/Frameworks/AVFoundation.framework/AVFoundation",
		"/System/Library/Frameworks/CoreMedia.framework/CoreMedia",
		"/System/Library/Frameworks/CoreVideo.framework/CoreVideo",
		"/System/Library/Frameworks/VideoToolbox.framework/VideoToolbox",
	}
	for _, path := range frameworks {
		lib, err := purego.Dlopen(path, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			loadErr = err
			return
		}
		libs = append(libs, lib)
	}

	msgSend, err := purego.Dlsym(libObj, "objc_msgSend")
	if err != nil {
		loadErr = err
		return
	}
	// A structure returned by value goes through objc_msgSend_stret on
	// amd64 when it is larger than the registers hold, and through
	// objc_msgSend everywhere else.
	msgSendStret := msgSend
	if runtime.GOARCH == "amd64" {
		if fn, err := purego.Dlsym(libObj, "objc_msgSend_stret"); err == nil {
			msgSendStret = fn
		}
	}
	bind := func(fn any, addr uintptr) {
		if loadErr == nil {
			purego.RegisterFunc(fn, addr)
		}
	}
	bind(&sendID, msgSend)
	bind(&sendID1, msgSend)
	bind(&sendID2, msgSend)
	bind(&sendID3, msgSend)
	bind(&sendIDD, msgSend)
	bind(&sendVoid, msgSend)
	bind(&sendVoid1, msgSend)
	bind(&sendVoid2, msgSend)
	bind(&sendVoid3, msgSend)
	bind(&sendVoid4, msgSend)
	bind(&sendVoidT, msgSend)
	bind(&sendBool, msgSend)
	bind(&sendBool1, msgSend)
	bind(&sendInt, msgSend)
	bind(&sendUint, msgSend)
	bind(&sendSize, msgSend)
	bind(&sendTime, msgSendStret)
	bind(&sendTimeR, msgSendStret)

	sym := func(lib uintptr, name string, fn any) {
		var addr uintptr
		if addr, loadErr = purego.Dlsym(lib, name); loadErr != nil {
			return
		}
		purego.RegisterFunc(fn, addr)
	}
	obj := libObj
	sym(obj, "object_getClass", &objectGetClass)
	sym(obj, "objc_retain", &objcRetain)
	sym(obj, "objc_release", &objcRelease)
	sym(obj, "objc_autorelease", &objcAutorelease)
	sym(obj, "objc_autoreleasePoolPush", &objcPoolPush)
	sym(obj, "objc_autoreleasePoolPop", &objcPoolPop)
	sym(obj, "objc_getClass", &objcGetClassByName)
	sym(obj, "sel_registerName", &objcRegisterSelName)
	coremedia, corevideo := libs[3], libs[4]
	sym(libs[1], "CFRelease", &cfRelease)
	sym(coremedia, "CMSampleBufferGetPresentationTimeStamp", &cmSampleBufferPTS)
	sym(coremedia, "CMSampleBufferGetDuration", &cmSampleBufferDuration)
	sym(coremedia, "CMSampleBufferGetImageBuffer", &cmSampleBufferImage)
	sym(coremedia, "CMSampleBufferGetNumSamples", &cmSampleBufferSamples)
	sym(coremedia, "CMSampleBufferGetDataBuffer", &cmSampleBufferData)
	sym(coremedia, "CMSampleBufferGetFormatDescription", &cmSampleBufferFormat)
	sym(coremedia, "CMBlockBufferGetDataLength", &cmBlockBufferLength)
	sym(coremedia, "CMBlockBufferCopyDataBytes", &cmBlockBufferCopy)
	sym(coremedia, "CMTimeGetSeconds", &cmTimeGetSeconds)
	sym(coremedia, "CMTimeRangeMake", &cmTimeRangeMake)
	sym(coremedia, "CMTimeMakeWithSeconds", &cmTimeMakeWithSeconds)
	sym(coremedia, "CMAudioFormatDescriptionGetStreamBasicDescription", &cmAudioFormatDescription)
	sym(coremedia, "CMVideoFormatDescriptionGetDimensions", &cmVideoFormatDescriptionSz)
	sym(coremedia, "CMFormatDescriptionGetMediaSubType", &cmFormatDescriptionSubType)
	sym(corevideo, "CVPixelBufferLockBaseAddress", &cvPixelBufferLock)
	sym(corevideo, "CVPixelBufferUnlockBaseAddress", &cvPixelBufferUnlock)
	sym(corevideo, "CVPixelBufferGetBaseAddress", &cvPixelBufferBase)
	sym(corevideo, "CVPixelBufferGetBytesPerRow", &cvPixelBufferStride)
	sym(corevideo, "CVPixelBufferGetWidth", &cvPixelBufferWidth)
	sym(corevideo, "CVPixelBufferGetHeight", &cvPixelBufferHeight)
	sym(corevideo, "CVPixelBufferGetPixelFormatType", &cvPixelBufferFormat)
	sym(corevideo, "CVPixelBufferGetPlaneCount", &cvPixelBufferPlanes)
	sym(corevideo, "CVPixelBufferGetBaseAddressOfPlane", &cvPixelBufferPlaneBase)
	sym(corevideo, "CVPixelBufferGetBytesPerRowOfPlane", &cvPixelBufferPlaneStride)
	if globalPixelFormatKey, loadErr = symbol(corevideo, "kCVPixelBufferPixelFormatTypeKey"); loadErr != nil {
		return
	}
	if loadErr = bindVideoToolbox(libs[5]); loadErr != nil {
		return
	}
}

// symbol returns the value of a global whose address a framework exports,
// such as a CFStringRef constant.
func symbol(lib uintptr, name string) (uintptr, error) {
	p, err := purego.Dlsym(lib, name)
	if err != nil {
		return 0, err
	}
	// The symbol names a variable; its value is what the variable holds.
	// The address of a Go value stands in for the address the symbol
	// gives, which a pointer conversion of a uintptr must not be.
	return **(**uintptr)(unsafe.Pointer(&p)), nil
}

// Class returns the class a name stands for, looked up once.
func Class(name string) uintptr {
	if v, ok := classes.Load(name); ok {
		return v.(uintptr)
	}
	if err := Load(); err != nil {
		return 0
	}
	c := objcGetClassByName(cstr(name))
	classes.Store(name, c)
	return c
}

// Sel returns the selector a name stands for, registered once.
func Sel(name string) uintptr {
	if v, ok := sels.Load(name); ok {
		return v.(uintptr)
	}
	if err := Load(); err != nil {
		return 0
	}
	s := objcRegisterSelName(cstr(name))
	sels.Store(name, s)
	return s
}

func cstr(s string) *byte {
	b := make([]byte, len(s)+1)
	copy(b, s)
	return &b[0]
}

// Global returns the value of a global symbol a framework exports, such as
// one of AVFoundation's key strings, looked up once.
//
// It panics when the symbol is not there, as the name is written in this
// package's own code rather than taken from anywhere: a name that resolves
// today and not tomorrow is a mistake to fix, not a condition to carry
// through the program. Use another way — asking a class whether it has a
// selector, or whether a class is there at all — for anything an older macOS
// might genuinely not have.
func Global(name string) uintptr {
	if v, ok := globals.Load(name); ok {
		return v.(uintptr)
	}
	if err := Load(); err != nil {
		panic("media: " + name + ": " + err.Error())
	}
	var value uintptr
	for _, lib := range libs {
		if p, err := purego.Dlsym(lib, name); err == nil {
			value = **(**uintptr)(unsafe.Pointer(&p))
			break
		}
	}
	if value == 0 {
		panic("media: macOS does not have " + name)
	}
	globals.Store(name, value)
	return value
}

// Alloc allocates an object of a class.
func Alloc(cls uintptr) uintptr { return sendID(cls, Sel("alloc")) }

// Auto releases an object at the end of the current pool.
func Auto(p uintptr) uintptr {
	if p == 0 {
		return 0
	}
	return objcAutorelease(p)
}

// Retain keeps an object alive, and returns it.
func Retain(p uintptr) uintptr {
	if p == 0 {
		return 0
	}
	return objcRetain(p)
}

// Release gives up a reference to an object.
func Release(p uintptr) {
	if p != 0 {
		objcRelease(p)
	}
}

// WithPool runs fn with an autorelease pool of its own, which collects what
// the calls inside it autorelease.
//
// A pool belongs to the thread that made it, so this holds the goroutine on
// one thread for as long as fn runs: without that, the goroutine could be
// moved between the push and the pop, and the pop would empty another
// thread's pool — which is a crash, not a leak.
func WithPool(fn func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	p := objcPoolPush()
	defer objcPoolPop(p)
	fn()
}

// Send is the plain message send: the message, and what it returns.
func SendID(id, sel uintptr) uintptr { return sendID(id, sel) }

// SendID1 sends a message with one object or integer argument.
func SendID1(id, sel, a uintptr) uintptr { return sendID1(id, sel, a) }

// SendID2 sends a message with two arguments.
func SendID2(id, sel, a, b uintptr) uintptr { return sendID2(id, sel, a, b) }

// SendID3 sends a message with three arguments.
func SendID3(id, sel, a, b, c uintptr) uintptr { return sendID3(id, sel, a, b, c) }

// SendVoid sends a message that returns nothing.
func SendVoid(id, sel uintptr) { sendVoid(id, sel) }

// SendVoid1 sends a message with one argument that returns nothing.
func SendVoid1(id, sel, a uintptr) { sendVoid1(id, sel, a) }

// SendVoid2 sends a message with two arguments that returns nothing.
func SendVoid2(id, sel, a, b uintptr) { sendVoid2(id, sel, a, b) }

// SendVoid3 sends a message with three arguments that returns nothing.
func SendVoid3(id, sel, a, b, c uintptr) { sendVoid3(id, sel, a, b, c) }

// SendVoid4 sends a message with four arguments that returns nothing.
func SendVoid4(id, sel, a, b, c, d uintptr) { sendVoid4(id, sel, a, b, c, d) }

// SendBool sends a message that returns a boolean.
func SendBool(id, sel uintptr) bool { return sendBool(id, sel) }

// SendBool1 sends a message with one argument that returns a boolean.
func SendBool1(id, sel, a uintptr) bool { return sendBool1(id, sel, a) }

// SendInt sends a message that returns an integer.
func SendInt(id, sel uintptr) int { return sendInt(id, sel) }

// SendUint sends a message that returns an unsigned integer.
func SendUint(id, sel uintptr) uintptr { return sendUint(id, sel) }

// SendTime sends a message that returns a time.
func SendTime(id, sel uintptr) CMTime { return sendTime(id, sel) }

// SendSize sends a message that returns a size.
func SendSize(id, sel uintptr) CGSize { return sendSize(id, sel) }

// SendTimeRangeOf sends a message that returns a time range.
func SendTimeRangeOf(id, sel uintptr) CMTimeRange { return sendTimeR(id, sel) }

// SendTimeRange sends a message with a time range, as starting an asset
// reader at a time takes.
func SendTimeRange(id, sel uintptr, r CMTimeRange) { sendVoidT(id, sel, r) }

// NSString makes a string object, autoreleased.
func NSString(s string) uintptr {
	if s == "" {
		return 0
	}
	b := append([]byte(s), 0)
	o := sendID3(Alloc(Class("NSString")), Sel("initWithBytes:length:encoding:"),
		uintptr(unsafe.Pointer(&b[0])), uintptr(len(s)), 4) // NSUTF8StringEncoding
	return Auto(o)
}

// NSNumberInt makes a number object from an integer.
func NSNumberInt(i int) uintptr {
	return sendID1(Class("NSNumber"), Sel("numberWithInt:"), uintptr(i))
}

// NSNumberDouble makes a number object from a double.
func NSNumberDouble(f float64) uintptr {
	return sendIDD(Class("NSNumber"), Sel("numberWithDouble:"), f)
}

// NSNumberBool makes a number object from a boolean.
func NSNumberBool(b bool) uintptr {
	n := uintptr(0)
	if b {
		n = 1
	}
	return sendID1(Class("NSNumber"), Sel("numberWithBool:"), n)
}

// NSNumberUint makes a number object from an unsigned integer.
func NSNumberUint(i uint32) uintptr {
	return sendID1(Class("NSNumber"), Sel("numberWithUnsignedInt:"), uintptr(i))
}

// MutableDict makes an empty dictionary, autoreleased.
func MutableDict() uintptr {
	return sendID(Class("NSMutableDictionary"), Sel("dictionary"))
}

// DictSet adds a value to a dictionary under a key.
func DictSet(dict, value, key uintptr) {
	sendVoid2(dict, Sel("setObject:forKey:"), value, key)
}

// FileURL makes a URL of a file path, autoreleased.
func FileURL(path string) uintptr {
	return sendID1(Class("NSURL"), Sel("fileURLWithPath:"), NSString(path))
}

// URL makes a URL of a string, autoreleased.
func URL(s string) uintptr {
	return sendID1(Class("NSURL"), Sel("URLWithString:"), NSString(s))
}

// ArrayCount returns how many elements an array has.
func ArrayCount(a uintptr) int { return sendInt(a, Sel("count")) }

// ArrayAt returns one element of an array, autoreleased.
func ArrayAt(a uintptr, i int) uintptr {
	return sendID1(a, Sel("objectAtIndex:"), uintptr(i))
}

// String returns an object as a Go string.
func String(p uintptr) string {
	if p == 0 {
		return ""
	}
	utf8 := sendID(p, Sel("UTF8String"))
	if utf8 == 0 {
		return ""
	}
	c := *(**byte)(unsafe.Pointer(&utf8))
	n := 0
	for *(*byte)(unsafe.Add(unsafe.Pointer(c), n)) != 0 {
		n++
	}
	return string(unsafe.Slice(c, n))
}

// ErrorString returns an error object's description, for an error of our
// own that says what the platform said.
func ErrorString(err uintptr) string {
	if err == 0 {
		return "unknown error"
	}
	return String(SendID(err, Sel("localizedDescription")))
}

// MediaSubType returns a format description's codec, as its four bytes.
func MediaSubType(fd uintptr) uint32 { return cmFormatDescriptionSubType(fd) }

// Time returns a CoreMedia time in Go's.
func Time(t CMTime) time.Duration { return seconds(cmTimeGetSeconds(t)) }

// SamplePTS returns when a sample belongs, in the media's own timeline.
func SamplePTS(sb uintptr) time.Duration { return seconds(cmTimeGetSeconds(cmSampleBufferPTS(sb))) }

// SampleDuration returns how long a sample lasts.
func SampleDuration(sb uintptr) time.Duration {
	return seconds(cmTimeGetSeconds(cmSampleBufferDuration(sb)))
}

// SampleImage returns a sample's picture, which the caller must not release.
func SampleImage(sb uintptr) uintptr { return cmSampleBufferImage(sb) }

// SampleSamples returns how many frames of sound a sample holds.
func SampleSamples(sb uintptr) int { return cmSampleBufferSamples(sb) }

// SampleData returns a sample's data, which the caller must not release.
func SampleData(sb uintptr) uintptr { return cmSampleBufferData(sb) }

// SampleFormat returns a sample's format description, which the caller must
// not release.
func SampleFormat(sb uintptr) uintptr { return cmSampleBufferFormat(sb) }

// SoundFormat returns the sound's own format, or nil.
func SoundFormat(fd uintptr) *AudioStreamBasicDescription {
	p := cmAudioFormatDescription(fd)
	if p == 0 {
		return nil
	}
	return *(**AudioStreamBasicDescription)(unsafe.Pointer(&p))
}

// VideoSize returns the coded size of a picture format, or zero.
func VideoSize(fd uintptr) (w, h int) {
	if fd == 0 {
		return 0, 0
	}
	d := cmVideoFormatDescriptionSz(fd)
	return int(d.Width), int(d.Height)
}

// BlockLength returns how many bytes of data a block holds.
func BlockLength(bb uintptr) int { return cmBlockBufferLength(bb) }

// BlockCopy copies a block's data into dst.
func BlockCopy(bb uintptr, n int, dst unsafe.Pointer) int32 {
	return cmBlockBufferCopy(bb, 0, n, dst)
}

// TimeRange makes a range of a start and a duration.
func TimeRange(start, duration time.Duration) CMTimeRange {
	return cmTimeRangeMake(timeOf(start), timeOf(duration))
}

// CFReleaseItem gives up a Core Foundation object.
func CFReleaseItem(p uintptr) {
	if p != 0 {
		cfRelease(p)
	}
}

// PixelFormatKey returns CoreVideo's key for a pixel buffer's format.
func PixelFormatKey() uintptr { return globalPixelFormatKey }

// PixelLock locks a pixel buffer's memory for reading.
func PixelLock(pb uintptr) int32 { return cvPixelBufferLock(pb, LockReadOnly) }

// PixelUnlock unlocks it.
func PixelUnlock(pb uintptr) { cvPixelBufferUnlock(pb, LockReadOnly) }

// PixelBase returns the first row of a locked pixel buffer.
func PixelBase(pb uintptr) unsafe.Pointer { return cvPixelBufferBase(pb) }

// PixelStride returns the distance between its rows.
func PixelStride(pb uintptr) int { return cvPixelBufferStride(pb) }

// PixelSize returns its size in pixels.
func PixelSize(pb uintptr) (w, h int) {
	return cvPixelBufferWidth(pb), cvPixelBufferHeight(pb)
}

// PixelFormat returns its format.
func PixelFormat(pb uintptr) uint32 { return cvPixelBufferFormat(pb) }

// PixelPlanes returns how many planes its pixels are kept in: one for a packed
// format, two for a planar one.
func PixelPlanes(pb uintptr) int { return cvPixelBufferPlanes(pb) }

// PixelPlaneBase returns the first row of one plane of a locked pixel buffer.
func PixelPlaneBase(pb uintptr, plane int) unsafe.Pointer { return cvPixelBufferPlaneBase(pb, plane) }

// PixelPlaneStride returns the distance between the rows of one of its planes.
func PixelPlaneStride(pb uintptr, plane int) int { return cvPixelBufferPlaneStride(pb, plane) }

func timeOf(d time.Duration) CMTime {
	return cmTimeMakeWithSeconds(d.Seconds(), 600)
}

func seconds(s float64) time.Duration {
	if s != s || s < 0 { // NaN, or a time CoreMedia cannot express
		return 0
	}
	return time.Duration(s * float64(time.Second))
}
