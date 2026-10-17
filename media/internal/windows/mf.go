//go:build windows && (amd64 || arm64)

package windows

import (
	"errors"
	"fmt"
	"runtime"
	"time"
	"unsafe"
)

// Errors for what Media Foundation reports as a success with nothing in it.
var (
	// ErrNoBuffer is a sample with no memory for its media, as an empty
	// sample at the start of a track is.
	ErrNoBuffer = errors.New("media: a sample holding nothing")
	// ErrNoReader is a call on a reader that is not there.
	ErrNoReader = errors.New("media: no reader")
)

// vtable slots of the interfaces this package calls. Windows writes each
// interface's methods in one order and this is that order with the first
// method, IUnknown::QueryInterface, as slot 0, the way the pointer in the
// object is indexed. Release, for instance, is slot 2.
const (
	slotGetUINT32 = 7
	slotGetUINT64 = 8
	slotGetGUID   = 10

	slotGetAllocatedString = 13

	slotSetUINT32  = 21
	slotSetUINT64  = 22
	slotSetGUID    = 24
	slotSetUnknown = 27

	slotGetMajorType = 33

	slotGetSampleTime     = 35
	slotGetSampleDuration = 37
	slotGetBufferCount    = 39
	slotGetBufferByIndex  = 40

	slotBufferLock          = 3
	slotBufferUnlock        = 4
	slotBufferCurrentLength = 5

	slot2DLock   = 3
	slot2DUnlock = 4

	slotSetStreamSelection       = 4
	slotGetNativeMediaType       = 5
	slotGetCurrentMediaType      = 6
	slotSetCurrentMediaType      = 7
	slotSetCurrentPosition       = 8
	slotReadSample               = 9
	slotFlush                    = 10
	slotGetServiceForStream      = 11
	slotGetPresentationAttribute = 12

	slotShutdown = 12

	// IMFActivate's own methods, after the attribute methods it inherits.
	slotActivateObject = 33
	slotShutdownObject = 34
	slotDetachObject   = 35
)

// Stream indices Media Foundation takes instead of a stream's number.
const (
	// StreamAll is every stream.
	StreamAll = 0xfffffffe
	// StreamFirstVideo is the file's first picture track.
	StreamFirstVideo = 0xfffffffc
	// StreamFirstAudio is its first sound track.
	StreamFirstAudio = 0xfffffffd
	// streamMediaSource is the source behind the reader.
	streamMediaSource = 0xffffffff
)

// Flags a read reports.
const (
	// FlagError is a read that failed.
	FlagError = 0x1
	// FlagEndOfStream is the end of the stream that was read.
	FlagEndOfStream = 0x2
	// FlagCurrentMediaTypeChanged is a stream whose format changed, which
	// a live source may do.
	FlagCurrentMediaTypeChanged = 0x20
	// FlagStreamTick is a stream that has nothing at this time.
	FlagStreamTick = 0x100
)

// Bitmap formats Media Foundation names by Direct3D's numbers. Its 32-bit
// colour is blue, green, red and unused, in memory.
const (
	BitmapRGB24  uint32 = 20 // D3DFMT_R8G8B8
	BitmapARGB32 uint32 = 21 // D3DFMT_A8R8G8B8
	BitmapRGB32  uint32 = 22 // D3DFMT_X8R8G8B8
)

// fourCC is the FOURCC macro: four characters as the number a format is.
func fourCC(s string) uint32 {
	if len(s) != 4 {
		panic("media: a format is four bytes")
	}
	return uint32(s[0]) | uint32(s[1])<<8 | uint32(s[2])<<16 | uint32(s[3])<<24
}

// FourCC writes a format's four bytes as text, for saying what was not read.
func FourCC(v uint32) string {
	b := []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)}
	for i, c := range b {
		if c < 0x20 || c > 0x7e {
			b[i] = '.'
		}
	}
	return string(b)
}

// mediaFormat builds one of Media Foundation's media type identifiers the
// way DEFINE_MEDIATYPE_GUID does: the format's own number, then the tail
// every one of them shares.
func mediaFormat(format uint32) GUID {
	return GUID{
		Data1: format,
		Data2: 0x0000,
		Data3: 0x0010,
		Data4: [8]byte{0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71},
	}
}

// Major types, and the formats media reads and writes.
var (
	MajorVideo = mediaFormat(fourCC("vids"))
	MajorAudio = mediaFormat(fourCC("auds"))

	FormatRGB32  = mediaFormat(BitmapRGB32)
	FormatARGB32 = mediaFormat(BitmapARGB32)
	FormatRGB24  = mediaFormat(BitmapRGB24)
	FormatH264   = mediaFormat(fourCC("H264"))
	FormatHEVC   = mediaFormat(fourCC("HEVC"))
	FormatAV1    = mediaFormat(fourCC("AV01"))
	FormatVP80   = mediaFormat(fourCC("VP80"))
	FormatVP90   = mediaFormat(fourCC("VP90"))
	FormatMJPEG  = mediaFormat(fourCC("MJPG"))
	FormatPCM    = mediaFormat(1)
	FormatFloat  = mediaFormat(3)
	FormatAAC    = mediaFormat(0x1610)
	FormatRawAAC = mediaFormat(0x00FF)
	FormatMP3    = mediaFormat(0x0055)
	FormatFLAC   = mediaFormat(0xF1AC)
	FormatOpus   = mediaFormat(0x704F)
	FormatALAC   = mediaFormat(0x6C61)
)

// Media type attributes media reads and writes.
var (
	KeyMajorType              = guid("48eba18e-f8c9-4687-bf11-0a74c9f96a8f")
	KeySubtype                = guid("f7e34c9a-42e8-4714-b74b-cb29d72c35e5")
	KeyAllSamplesIndependent  = guid("c9173739-5e56-461c-b713-46fb995cb95f")
	KeyAudioNumChannels       = guid("37e48bf5-645e-4c5b-89de-ada9e29b696a")
	KeyAudioSamplesPerSecond  = guid("5faeeae7-0290-4c31-9e8a-c534f68d9dba")
	KeyAudioBlockAlignment    = guid("322de230-9eeb-43bd-ab7a-ff412251541d")
	KeyAudioBitsPerSample     = guid("f2deb57f-40fa-4764-aa33-ed4f2d1ff669")
	KeyAudioAvgBytesPerSecond = guid("1aab75c8-cfef-451c-ab95-ac034b8e1731")
	KeyFrameSize              = guid("1652c33d-d6b2-4012-b834-72030849a37d")
	KeyFrameRate              = guid("c459a2e8-3d2c-4e44-b132-fee5156c7bb0")
	KeyDefaultStride          = guid("644b4e48-1e02-4516-b0eb-c01ca9d49ac6")
	KeyAvgBitrate             = guid("20332624-fb0d-4d9e-bd0d-cbf6786c102e")
	KeyUserData               = guid("b6bc765f-4c3b-40a4-bd51-2535b66fe09d")
	KeyMPEGSequenceHeader     = guid("3c036de7-3ad0-4c9e-9216-ee6d6ac21cb3")

	// How the source reader is set up.
	KeyEnableAdvancedVideoProcessing = guid("0f81da2c-b537-4672-a8b2-a681b17307a3")
	KeyDisableConverters             = guid("98d5b065-1374-4847-8d5d-31520fee7156")
	// attrAsyncCallback makes a reader report its reads to an
	// IMFSourceReaderCallback instead of waiting for them, which is
	// MF_SOURCE_READER_ASYNC_CALLBACK.
	attrAsyncCallback = guid("1e3dbeac-bb43-4c35-b507-cd644464c965")

	// What a source says about itself.
	KeyDuration = guid("6c990d33-bb8e-477a-8598-0d5d96fcd88a")

	// Interfaces.
	IIDMediaSource          = guid("279a808d-aec7-40c8-9c6b-a6b492c78a66")
	iid2DBuffer             = guid("7dc9d5f9-9ed9-44ec-9bbf-0600bb589fbb")
	iidSourceReaderCallback = guid("deec8d99-fa1d-4d82-84c2-2c8969944867")
	iidUnknown              = guid("00000000-0000-0000-c000-000000000046")
	guidNull                = GUID{}
)

// What a capture device says about itself, and how the machine is asked for
// them. Windows writes an attribute's name as a GUID, the same one its
// property key has.
var (
	// AttrDeviceSourceType is what kind of device is being looked for.
	AttrDeviceSourceType = guid("c60ac5fe-252a-478f-a0ef-bc8fa5f7cad3")
	// AttrVideoCapture is the kind that takes pictures.
	AttrVideoCapture = guid("8ac3587a-4ae7-42d8-99e0-0a6013eef90f")
	// AttrFriendlyName is what a device calls itself.
	AttrFriendlyName = guid("60d0e559-52f8-4fa2-bbce-acdb34a8ec01")
	// AttrSymbolicLink is the device's own name, which stays the same
	// across launches.
	AttrSymbolicLink = guid("58f0aad8-22bf-4f8a-bb3d-d2c4978c6e2f")
)

// A propVariant is PROPVARIANT, which Media Foundation uses for the values
// that are not attributes: a seek's position, a file's length.
type propVariant struct {
	vt   uint16
	_    [6]byte
	data [16]byte
}

// The variant types this package uses.
const (
	vtI8  = 20
	vtUI8 = 21
)

// position is a PROPVARIANT holding a time in 100ns units, which is how
// Media Foundation counts.
func position(t time.Duration) propVariant {
	p := propVariant{vt: vtI8}
	*(*int64)(unsafe.Pointer(&p.data[0])) = int64(t / (100 * time.Nanosecond))
	return p
}

// duration reads a PROPVARIANT holding a time in 100ns units.
func (p *propVariant) duration() (time.Duration, error) {
	if p.vt != vtUI8 && p.vt != vtI8 {
		return 0, fmt.Errorf("media: a time of variant type %d", p.vt)
	}
	return time.Duration(*(*int64)(unsafe.Pointer(&p.data[0]))) * 100, nil
}

// An Attributes is IMFAttributes, the property bag that media types and
// settings both are.
type Attributes struct{ p uintptr }

// NewAttributes makes an empty property bag.
func NewAttributes() (Attributes, error) {
	var p uintptr
	if hr, _, _ := procMFCreateAttributes.Call(uintptr(unsafe.Pointer(&p)), 4); failed(hr) {
		return Attributes{}, hresultError("MFCreateAttributes", hr)
	}
	return Attributes{p: p}, nil
}

// Release gives up the bag.
func (a Attributes) Release() { release(a.p) }

// SetGUID sets key to value.
func (a Attributes) SetGUID(key, value *GUID) error {
	if hr := comCall(a.p, slotSetGUID, uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(value))); failed(hr) {
		return hresultError("IMFAttributes::SetGUID", hr)
	}
	return nil
}

// SetUINT32 sets key to value.
func (a Attributes) SetUINT64(key *GUID, v uint64) error {
	if hr := comCall(a.p, slotSetUINT64, uintptr(unsafe.Pointer(key)), uintptr(v)); failed(hr) {
		return hresultError("IMFAttributes::SetUINT64", hr)
	}
	return nil
}

func (a Attributes) SetUINT32(key *GUID, v uint32) error {
	if hr := comCall(a.p, slotSetUINT32, uintptr(unsafe.Pointer(key)), uintptr(v)); failed(hr) {
		return hresultError("IMFAttributes::SetUINT32", hr)
	}
	return nil
}

// SetUnknown sets key to the interface at v, which the bag holds a reference to
// as long as it lives.
func (a Attributes) SetUnknown(key *GUID, v uintptr) error {
	if hr := comCall(a.p, slotSetUnknown, uintptr(unsafe.Pointer(key)), v); failed(hr) {
		return hresultError("IMFAttributes::SetUnknown", hr)
	}
	return nil
}

// GetGUID returns key's value.
func (a Attributes) GetGUID(key *GUID) (GUID, error) {
	var g GUID
	if hr := comCall(a.p, slotGetGUID, uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(&g))); failed(hr) {
		return GUID{}, hresultError("IMFAttributes::GetGUID", hr)
	}
	return g, nil
}

// GetUINT32 returns key's value.
func (a Attributes) GetUINT32(key *GUID) (uint32, error) {
	var v uint32
	if hr := comCall(a.p, slotGetUINT32, uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(&v))); failed(hr) {
		return 0, hresultError("IMFAttributes::GetUINT32", hr)
	}
	return v, nil
}

// GetUINT64 returns key's value.
func (a Attributes) GetUINT64(key *GUID) (uint64, error) {
	var v uint64
	if hr := comCall(a.p, slotGetUINT64, uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(&v))); failed(hr) {
		return 0, hresultError("IMFAttributes::GetUINT64", hr)
	}
	return v, nil
}

// GetAllocatedString returns key's text. Windows allocates the text and this
// copies it before freeing it.
func (a Attributes) GetAllocatedString(key *GUID) (string, error) {
	var (
		text *uint16
		n    uint32
	)
	hr := comCall(a.p, slotGetAllocatedString,
		uintptr(unsafe.Pointer(key)),
		uintptr(unsafe.Pointer(&text)),
		uintptr(unsafe.Pointer(&n)))
	if failed(hr) {
		return "", hresultError("IMFAttributes::GetAllocatedString", hr)
	}
	if text == nil {
		return "", nil
	}
	defer procCoTaskMemFree.Call(uintptr(unsafe.Pointer(text)))
	return wstring(text), nil
}

// A MediaType is IMFMediaType: what a stream's samples hold.
type MediaType struct{ p uintptr }

// NewMediaType makes an empty media type.
func NewMediaType() (MediaType, error) {
	var p uintptr
	if hr, _, _ := procMFCreateMediaType.Call(uintptr(unsafe.Pointer(&p))); failed(hr) {
		return MediaType{}, hresultError("MFCreateMediaType", hr)
	}
	return MediaType{p: p}, nil
}

// attrs is the media type as the property bag it also is: IMFMediaType's
// first methods are IMFAttributes', so one set of calls serves both.
func (m MediaType) attrs() Attributes { return Attributes{p: m.p} }

// Release gives up the media type.
func (m MediaType) Release() { release(m.p) }

// SetGUID sets key to value.
func (m MediaType) SetGUID(key, value *GUID) error { return m.attrs().SetGUID(key, value) }

// SetUINT32 sets key to value.
func (m MediaType) SetUINT32(key *GUID, v uint32) error { return m.attrs().SetUINT32(key, v) }

func (m MediaType) SetUINT64(key *GUID, v uint64) error { return m.attrs().SetUINT64(key, v) }

// GetGUID returns key's value, or the HRESULT Windows gave.
func (m MediaType) GetGUID(key *GUID) (GUID, error) { return m.attrs().GetGUID(key) }

// GetUINT32 returns key's value, or the HRESULT Windows gave.
func (m MediaType) GetUINT32(key *GUID) (uint32, error) { return m.attrs().GetUINT32(key) }

// GetUINT64 returns key's value, or the HRESULT Windows gave.
func (m MediaType) GetUINT64(key *GUID) (uint64, error) { return m.attrs().GetUINT64(key) }

// MajorType is what the type carries: a picture, sound.
func (m MediaType) MajorType() (GUID, error) {
	var g GUID
	if hr := comCall(m.p, slotGetMajorType, uintptr(unsafe.Pointer(&g))); failed(hr) {
		return GUID{}, hresultError("IMFMediaType::GetMajorType", hr)
	}
	return g, nil
}

// Subtype is how it is encoded.
func (m MediaType) Subtype() (GUID, error) { return m.GetGUID(&KeySubtype) }

// FrameSize is the picture's size, when the type says.
func (m MediaType) FrameSize() (w, h int, ok bool) {
	v, err := m.GetUINT64(&KeyFrameSize)
	if err != nil {
		return 0, 0, false
	}
	// MFSetAttributeSize packs the width in the high half.
	return int(v >> 32), int(v & 0xffffffff), true
}

// DefaultStride is the distance between two rows of a bitmap as the platform
// stores it, when the type says: negative for a picture stored from its
// bottom row up.
func (m MediaType) DefaultStride() (int32, bool) {
	v, err := m.GetUINT32(&KeyDefaultStride)
	if err != nil {
		return 0, false
	}
	return int32(v), true
}

// SampleRate is the sound's rate, in samples a second.
func (m MediaType) SampleRate() (int, bool) {
	v, err := m.GetUINT32(&KeyAudioSamplesPerSecond)
	if err != nil {
		return 0, false
	}
	return int(v), true
}

// Channels is the sound's number of channels.
func (m MediaType) Channels() (int, bool) {
	v, err := m.GetUINT32(&KeyAudioNumChannels)
	if err != nil {
		return 0, false
	}
	return int(v), true
}

// BitsPerSample is the sound's bits a sample.
func (m MediaType) BitsPerSample() (int, bool) {
	v, err := m.GetUINT32(&KeyAudioBitsPerSample)
	if err != nil {
		return 0, false
	}
	return int(v), true
}

// StrideForBitmap is the stride Media Foundation stores a bitmap format
// with, in bytes, which is negative for its colour formats: they are stored
// from the bottom row up.
func StrideForBitmap(format uint32, width int) (int32, error) {
	var stride int32
	if hr, _, _ := procMFGetStrideForBitmapInfoHeader.Call(uintptr(format), uintptr(uint32(width)), uintptr(unsafe.Pointer(&stride))); failed(hr) {
		return 0, hresultError("MFGetStrideForBitmapInfoHeader", hr)
	}
	return stride, nil
}

// A SourceReader is IMFSourceReader: one stream of a file or a device,
// read a sample at a time. It is not safe to read from two goroutines at
// once, and one reader serves one stream.
//
// A reader made by one of the Async opens reads asynchronously: its reads are
// started with ReadSampleAsync and reported to cb, whose reference the reader
// holds and gives up with itself.
type SourceReader struct {
	p  uintptr
	cb *AsyncCallback
}

// OpenReaderFromPath opens a reader over the file at path, which is a
// Windows path rather than a URL: Media Foundation reads it through its own
// file byte stream, so that a drive letter is not taken for a URL scheme.
func OpenReaderFromPath(path string, video bool) (SourceReader, error) {
	attrs, err := readerAttrs(video, nil)
	if err != nil {
		return SourceReader{}, err
	}
	defer attrs.Release()
	var stream uintptr
	p := u16(path)
	hr, _, _ := procMFCreateFile.Call(
		mfAccessRead, mfOpenFailIfNotExist, mfFileFlagsNone,
		uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&stream)))
	runtime.KeepAlive(p)
	if failed(hr) {
		return SourceReader{}, hresultError("MFCreateFile", hr)
	}
	defer release(stream)
	var r uintptr
	hr, _, _ = procMFCreateSourceReaderFromByteStream.Call(stream, attrs.p, uintptr(unsafe.Pointer(&r)))
	if failed(hr) {
		return SourceReader{}, hresultError("MFCreateSourceReaderFromByteStream", hr)
	}
	return SourceReader{p: r}, nil
}

// OpenReaderFromURL opens a reader over a location rather than a path: what
// Media Foundation's own scheme handlers can read over the network.
func OpenReaderFromURL(url string, video bool) (SourceReader, error) {
	attrs, err := readerAttrs(video, nil)
	if err != nil {
		return SourceReader{}, err
	}
	defer attrs.Release()
	var r uintptr
	p := u16(url)
	hr, _, _ := procMFCreateSourceReaderFromURL.Call(uintptr(unsafe.Pointer(p)), attrs.p, uintptr(unsafe.Pointer(&r)))
	runtime.KeepAlive(p)
	if failed(hr) {
		return SourceReader{}, hresultError("MFCreateSourceReaderFromURL", hr)
	}
	return SourceReader{p: r}, nil
}

// OpenReaderFromPathAsync opens a reader over the file at path that reads
// asynchronously: it reports its samples to cb instead of making its caller
// wait for them. The reader takes over the caller's reference to cb, and gives
// it up with itself; a reader that cannot be made releases it here.
func OpenReaderFromPathAsync(path string, video bool, cb *AsyncCallback) (SourceReader, error) {
	attrs, err := readerAttrs(video, cb)
	if err != nil {
		cb.release()
		return SourceReader{}, err
	}
	defer attrs.Release()
	var stream uintptr
	p := u16(path)
	hr, _, _ := procMFCreateFile.Call(
		mfAccessRead, mfOpenFailIfNotExist, mfFileFlagsNone,
		uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&stream)))
	runtime.KeepAlive(p)
	if failed(hr) {
		cb.release()
		return SourceReader{}, hresultError("MFCreateFile", hr)
	}
	defer release(stream)
	var r uintptr
	hr, _, _ = procMFCreateSourceReaderFromByteStream.Call(stream, attrs.p, uintptr(unsafe.Pointer(&r)))
	if failed(hr) {
		cb.release()
		return SourceReader{}, hresultError("MFCreateSourceReaderFromByteStream", hr)
	}
	return SourceReader{p: r, cb: cb}, nil
}

// OpenReaderFromURLAsync opens an asynchronous reader over a location rather
// than a path, as OpenReaderFromURL does, reporting to cb.
func OpenReaderFromURLAsync(url string, video bool, cb *AsyncCallback) (SourceReader, error) {
	attrs, err := readerAttrs(video, cb)
	if err != nil {
		cb.release()
		return SourceReader{}, err
	}
	defer attrs.Release()
	var r uintptr
	p := u16(url)
	hr, _, _ := procMFCreateSourceReaderFromURL.Call(uintptr(unsafe.Pointer(p)), attrs.p, uintptr(unsafe.Pointer(&r)))
	runtime.KeepAlive(p)
	if failed(hr) {
		cb.release()
		return SourceReader{}, hresultError("MFCreateSourceReaderFromURL", hr)
	}
	return SourceReader{p: r, cb: cb}, nil
}

// File access modes, as Media Foundation names them.
const (
	mfAccessRead         = 1 // MF_ACCESSMODE_READ
	mfOpenFailIfNotExist = 0 // MF_OPENMODE_FAIL_IF_NOT_EXIST
	mfFileFlagsNone      = 0 // MF_FILEFLAGS_NONE
)

// A CaptureDevice is IMFActivate: a camera the machine offers, which is not
// started until it is activated.
type CaptureDevice struct{ p uintptr }

// Release gives up the device.
func (d CaptureDevice) Release() { release(d.p) }

// Name is what the device calls itself, which is what a person picks from.
func (d CaptureDevice) Name() (string, error) {
	return Attributes{p: d.p}.GetAllocatedString(&AttrFriendlyName)
}

// ID is the device's own name, which stays the same across launches.
func (d CaptureDevice) ID() (string, error) {
	return Attributes{p: d.p}.GetAllocatedString(&AttrSymbolicLink)
}

// Activate starts the device and returns its media source, which the caller
// releases.
func (d CaptureDevice) Activate() (uintptr, error) {
	var source uintptr
	hr := comCall(d.p, slotActivateObject, uintptr(unsafe.Pointer(&IIDMediaSource)), uintptr(unsafe.Pointer(&source)))
	if failed(hr) {
		return 0, hresultError("IMFActivate::ActivateObject", hr)
	}
	return source, nil
}

// ShutdownObject stops the device this device started.
func (d CaptureDevice) ShutdownObject() {
	if d.p != 0 {
		comCall(d.p, slotShutdownObject)
	}
}

// CaptureDevices lists the cameras the machine offers. It starts none of them
// and asks the person nothing.
func CaptureDevices() ([]CaptureDevice, error) {
	if err := Load(); err != nil {
		return nil, err
	}
	enter()
	attrs, err := NewAttributes()
	if err != nil {
		return nil, err
	}
	defer attrs.Release()
	if err := attrs.SetGUID(&AttrDeviceSourceType, &AttrVideoCapture); err != nil {
		return nil, err
	}
	var (
		devices uintptr
		count   uint32
	)
	hr, _, _ := procMFEnumDeviceSources.Call(attrs.p, uintptr(unsafe.Pointer(&devices)), uintptr(unsafe.Pointer(&count)))
	if failed(hr) {
		return nil, hresultError("MFEnumDeviceSources", hr)
	}
	if devices == 0 || count == 0 {
		return nil, nil
	}
	// Windows hands over the array and one reference per device in it:
	// the devices are taken here and the array is freed with it.
	defer procCoTaskMemFree.Call(devices)
	out := make([]CaptureDevice, 0, count)
	for _, p := range unsafe.Slice((*uintptr)(native(devices)), int(count)) {
		if p != 0 {
			out = append(out, CaptureDevice{p: p})
		}
	}
	return out, nil
}

// OpenSettings opens one of Windows' own settings pages, which is where a
// person's answer about a device can be changed.
func OpenSettings(uri string) error {
	verb := u16("open")
	target := u16(uri)
	r, _, err := procShellExecuteW.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(target)), 0, 0, swShownormal)
	runtime.KeepAlive(verb)
	runtime.KeepAlive(target)
	if r <= 32 {
		return fmt.Errorf("media: opening Settings: %v", err)
	}
	return nil
}

// swShownormal is SW_SHOWNORMAL, how ShellExecuteW is asked to show a window.
const swShownormal = 1

// OpenReaderFromSource reads a live source, such as a camera: the reader pulls
// samples as the device makes them.
func OpenReaderFromSource(source uintptr, video bool) (SourceReader, error) {
	if err := Load(); err != nil {
		return SourceReader{}, err
	}
	enter()
	attrs, err := readerAttrs(video, nil)
	if err != nil {
		return SourceReader{}, err
	}
	defer attrs.Release()
	var r uintptr
	hr, _, _ := procMFCreateSourceReaderFromMediaSource.Call(source, attrs.p, uintptr(unsafe.Pointer(&r)))
	if failed(hr) {
		return SourceReader{}, hresultError("MFCreateSourceReaderFromMediaSource", hr)
	}
	return SourceReader{p: r}, nil
}

// readerAttrs is how a reader is set up: converting left on, so that the
// platform turns a file's own encoding into the picture and sound asked of
// it, and the video processor that does the colour conversion in the reader
// rather than in a pass of its own.
func readerAttrs(video bool, cb *AsyncCallback) (Attributes, error) {
	attrs, err := NewAttributes()
	if err != nil {
		return Attributes{}, err
	}
	if cb != nil {
		if err := attrs.SetUnknown(&attrAsyncCallback, cb.addr()); err != nil {
			attrs.Release()
			return Attributes{}, err
		}
	}
	if err := attrs.SetUINT32(&KeyDisableConverters, 0); err != nil {
		attrs.Release()
		return Attributes{}, err
	}
	if video {
		if err := attrs.SetUINT32(&KeyEnableAdvancedVideoProcessing, 1); err != nil {
			attrs.Release()
			return Attributes{}, err
		}
	}
	return attrs, nil
}

// Release gives up the reader, and with it the callback it reports reads to:
// the reader takes back any request still pending, so no callback arrives
// after this.
func (r SourceReader) Release() {
	release(r.p)
	if r.cb != nil {
		r.cb.release()
		r.cb = nil
	}
}

// Valid reports whether there is a reader behind this value: the zero value
// is not one.
func (r SourceReader) Valid() bool { return r.p != 0 }

// SetStreamSelection turns one stream on or off, or every stream with
// [StreamAll].
func (r SourceReader) SetStreamSelection(stream int, on bool) error {
	v := uintptr(0)
	if on {
		v = 1
	}
	if hr := comCall(r.p, slotSetStreamSelection, uintptr(uint32(stream)), v); failed(hr) {
		return hresultError("IMFSourceReader::SetStreamSelection", hr)
	}
	return nil
}

// NativeMediaType is one of a stream's formats as the file has it, which is
// how a track's codec and size are read. Index 0 is the first.
func (r SourceReader) NativeMediaType(stream, index int) (MediaType, error) {
	var p uintptr
	if hr := comCall(r.p, slotGetNativeMediaType, uintptr(uint32(stream)), uintptr(uint32(index)), uintptr(unsafe.Pointer(&p))); failed(hr) {
		return MediaType{}, hresultError("IMFSourceReader::GetNativeMediaType", hr)
	}
	return MediaType{p: p}, nil
}

// CurrentMediaType is the format a stream is read as now.
func (r SourceReader) CurrentMediaType(stream int) (MediaType, error) {
	var p uintptr
	if hr := comCall(r.p, slotGetCurrentMediaType, uintptr(uint32(stream)), uintptr(unsafe.Pointer(&p))); failed(hr) {
		return MediaType{}, hresultError("IMFSourceReader::GetCurrentMediaType", hr)
	}
	return MediaType{p: p}, nil
}

// SetCurrentMediaType asks for a format, which the reader agrees to or
// refuses: the platform decodes and converts on its way to it.
func (r SourceReader) SetCurrentMediaType(stream int, mt MediaType) error {
	if hr := comCall(r.p, slotSetCurrentMediaType, uintptr(uint32(stream)), 0, mt.p); failed(hr) {
		return hresultError("IMFSourceReader::SetCurrentMediaType", hr)
	}
	return nil
}

// SetCurrentPosition moves the reader to t from the start of the media.
// Media Foundation does not promise to land exactly there: it goes to the
// key frame before it and reads on.
func (r SourceReader) SetCurrentPosition(t time.Duration) error {
	pv := position(t)
	if hr := comCall(r.p, slotSetCurrentPosition, uintptr(unsafe.Pointer(&guidNull)), uintptr(unsafe.Pointer(&pv))); failed(hr) {
		return hresultError("IMFSourceReader::SetCurrentPosition", hr)
	}
	return nil
}

// ReadSample reads the next sample of one stream, waiting for it: the flags
// say what it is, including [FlagEndOfStream]. The sample is nil when the
// stream has nothing at this time, and the caller releases it.
func (r SourceReader) ReadSample(stream int) (Sample, uint32, time.Duration, error) {
	var (
		actual uint32
		flags  uint32
		ts     int64
		sample uintptr
	)
	hr := comCall(r.p, slotReadSample,
		uintptr(uint32(stream)),
		0,
		uintptr(unsafe.Pointer(&actual)),
		uintptr(unsafe.Pointer(&flags)),
		uintptr(unsafe.Pointer(&ts)),
		uintptr(unsafe.Pointer(&sample)))
	if failed(hr) {
		return Sample{}, 0, 0, hresultError("IMFSourceReader::ReadSample", hr)
	}
	return Sample{p: sample}, flags, time.Duration(ts) * 100, nil
}

// Flush drops the samples the reader holds for a stream and brings back a read
// that is waiting for one: a camera that is being closed has no picture to
// send, and the read that waits for it would otherwise wait for ever.
func (r SourceReader) Flush(stream int) error {
	hr := comCall(r.p, slotFlush, uintptr(uint32(stream)))
	if failed(hr) {
		return hresultError("IMFSourceReader::Flush", hr)
	}
	return nil
}

// Duration is how long the whole source is, or 0 when it does not say.
func (r SourceReader) Duration() (time.Duration, error) {
	var pv propVariant
	if hr := comCall(r.p, slotGetPresentationAttribute, uintptr(uint32(streamMediaSource)), uintptr(unsafe.Pointer(&KeyDuration)), uintptr(unsafe.Pointer(&pv))); failed(hr) {
		return 0, hresultError("IMFSourceReader::GetPresentationAttribute", hr)
	}
	return pv.duration()
}

// Shutdown shuts the source behind the reader down, which is what closes
// the file: a media source that is not shut down keeps its own reference to
// the file for as long as the process runs.
func (r SourceReader) Shutdown() {
	var src uintptr
	hr := comCall(r.p, slotGetServiceForStream,
		uintptr(uint32(streamMediaSource)),
		uintptr(unsafe.Pointer(&guidNull)),
		uintptr(unsafe.Pointer(&IIDMediaSource)),
		uintptr(unsafe.Pointer(&src)))
	if failed(hr) || src == 0 {
		return
	}
	comCall(src, slotShutdown)
	release(src)
}

// A Sample is IMFSample: one timed piece of media.
type Sample struct{ p uintptr }

// Release gives up the sample.
func (s Sample) Release() { release(s.p) }

// Empty reports whether there is no sample at all, which a read says when
// the stream has nothing at this time.
func (s Sample) Empty() bool { return s.p == 0 }

// Time is when the sample belongs, from the start of the media.
func (s Sample) Time() (time.Duration, bool) {
	if s.p == 0 {
		return 0, false
	}
	var v int64
	if hr := comCall(s.p, slotGetSampleTime, uintptr(unsafe.Pointer(&v))); failed(hr) {
		return 0, false
	}
	return time.Duration(v) * 100, true
}

// Duration is how long the sample lasts, when it says.
func (s Sample) Duration() (time.Duration, bool) {
	if s.p == 0 {
		return 0, false
	}
	var v int64
	if hr := comCall(s.p, slotGetSampleDuration, uintptr(unsafe.Pointer(&v))); failed(hr) {
		return 0, false
	}
	return time.Duration(v) * 100, true
}

// Buffer is the sample's memory.
func (s Sample) Buffer() (Buffer, error) {
	if s.p == 0 {
		return Buffer{}, ErrNoBuffer
	}
	var count uint32
	if hr := comCall(s.p, slotGetBufferCount, uintptr(unsafe.Pointer(&count))); failed(hr) {
		return Buffer{}, hresultError("IMFSample::GetBufferCount", hr)
	}
	if count == 0 {
		return Buffer{}, ErrNoBuffer
	}
	var p uintptr
	if hr := comCall(s.p, slotGetBufferByIndex, 0, uintptr(unsafe.Pointer(&p))); failed(hr) {
		return Buffer{}, hresultError("IMFSample::GetBufferByIndex", hr)
	}
	if p == 0 {
		return Buffer{}, ErrNoBuffer
	}
	return Buffer{p: p}, nil
}

// A Buffer is IMFMediaBuffer: where a sample's bytes are.
type Buffer struct{ p uintptr }

// Release gives up the buffer.
func (b Buffer) Release() { release(b.p) }

// TwoD is the buffer as a picture with rows of its own, when it is one.
func (b Buffer) TwoD() (TwoD, bool) {
	if p := queryInterface(b.p, &iid2DBuffer); p != 0 {
		return TwoD{p: p}, true
	}
	return TwoD{}, false
}

// Lock is the buffer's bytes and how many of them there are.
func (b Buffer) Lock() (unsafe.Pointer, int, error) {
	var (
		p      unsafe.Pointer
		maxLen uint32
		curLen uint32
	)
	if hr := comCall(b.p, slotBufferLock, uintptr(unsafe.Pointer(&p)), uintptr(unsafe.Pointer(&maxLen)), uintptr(unsafe.Pointer(&curLen))); failed(hr) {
		return nil, 0, hresultError("IMFMediaBuffer::Lock", hr)
	}
	return p, int(curLen), nil
}

// LockWrite locks a buffer for writing and says how much room it has, which
// is the length it was made with, not the length it says it holds.
func (b Buffer) LockWrite() (unsafe.Pointer, int, error) {
	var (
		p      unsafe.Pointer
		maxLen uint32
		curLen uint32
	)
	if hr := comCall(b.p, slotBufferLock, uintptr(unsafe.Pointer(&p)), uintptr(unsafe.Pointer(&maxLen)), uintptr(unsafe.Pointer(&curLen))); failed(hr) {
		return nil, 0, hresultError("IMFMediaBuffer::Lock", hr)
	}
	return p, int(maxLen), nil
}

// Unlock lets the buffer be written again.
func (b Buffer) Unlock() { comCall(b.p, slotBufferUnlock) }

// A TwoD is IMF2DBuffer: a picture whose rows are a stride apart, which may
// be more than the row's pixels need, and which is stored from its bottom
// row up when the stride is negative.
type TwoD struct{ p uintptr }

// Release gives up the interface.
func (d TwoD) Release() { release(d.p) }

// Lock2D is the first byte of the picture's first row in memory, and the
// stride between rows.
func (d TwoD) Lock2D() (unsafe.Pointer, int32, error) {
	var (
		p     unsafe.Pointer
		pitch int32
	)
	if hr := comCall(d.p, slot2DLock, uintptr(unsafe.Pointer(&p)), uintptr(unsafe.Pointer(&pitch))); failed(hr) {
		return nil, 0, hresultError("IMF2DBuffer::Lock2D", hr)
	}
	return p, pitch, nil
}

// Unlock2D lets the picture be written again.
func (d TwoD) Unlock2D() { comCall(d.p, slot2DUnlock) }
