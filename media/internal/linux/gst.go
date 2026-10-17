//go:build linux && (amd64 || arm64)

// Package linux is the GStreamer plumbing the media package reads Linux
// files with: loaded at run time, without cgo, and out of every app that
// does not import media.
//
// GStreamer is Linux's own media stack. The system webview already uses it
// for video, and it is what a distribution ships decoders for, so media asks
// the machine what it has instead of promising a codec: a machine without the
// plugin a file needs says so here, when it is asked, and not with an error
// at build time.
//
// Nothing GStreamer does needs a callback for the way media reads. An appsink
// is silent until it is pulled from, so decoding runs on the goroutine that
// asks for the next frame, exactly as the media package promises. The one
// signal connected, a demuxer's new pad, is a purego callback made once and
// routed by its user data.
//
// Ownership: a constructor hands back an element the caller owns, and
// [Element.Release] drops it. [Element.Add] hands that reference to a bin
// instead, so a child that has been added must not be released again.
package linux

import (
	"errors"
	"fmt"
	"structs"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

// A State is where an element is: nothing at all, ready to be told what to
// do, paused with the first frame decoded, or playing.
type State int32

const (
	StateVoidPending State = 0
	StateNull        State = 1
	StateReady       State = 2
	StatePaused      State = 3
	StatePlaying     State = 4
)

// A StateChange is how a state change went.
type StateChange int32

const (
	StateChangeFailure   StateChange = 0
	StateChangeSuccess   StateChange = 1
	StateChangeAsync     StateChange = 2
	StateChangeNoPreroll StateChange = 3
)

// The bus messages media waits for, as a mask for [Bus.Pop].
const (
	MessageEOS          int32 = 1 << 0
	MessageError        int32 = 1 << 1
	MessageStateChanged int32 = 1 << 6
	MessageAsyncDone    int32 = 1 << 21
)

// GStreamer's own numbers, as its headers define them.
const (
	formatTime    int32  = 3 // GST_FORMAT_TIME
	seekFlush     int32  = 1 // GST_SEEK_FLAG_FLUSH
	parseFatal    int32  = 1 // GST_PARSE_FLAG_FATAL_ERRORS
	mapRead       int32  = 1 // GST_MAP_READ
	uriSrc        int32  = 2 // GST_URI_SRC
	clockTimeNone uint64 = ^uint64(0)
)

// GStreamer's error domains and codes, as gsterror.h numbers them; the
// domains are compared as the quarks they are looked up as.
const (
	coreErrorMissingPlugin  int32 = 12
	coreErrorNotImplemented int32 = 3

	streamErrorTypeNotFound int32 = 4
	streamErrorDecode       int32 = 7
	streamErrorDemux        int32 = 9
	streamErrorFormat       int32 = 11

	resourceErrorNotFound int32 = 3
)

// Where GStreamer 1.x keeps the fields its C API only names in macros. The
// series keeps its ABI, which is what makes reading them directly sound: on
// 64-bit, GstMiniObject is 64 bytes, GstBuffer 112 and GstMessage 120, so a
// buffer's timestamps and a message's type are at these offsets. The media
// tests read real buffers and messages through them.
const (
	offsetMessageType  = 64
	offsetBufferPTS    = 72
	offsetBufferDTS    = 80
	offsetBufferLength = 88
)

// A mapInfo is a GstMapInfo: what gst_buffer_map fills in and what
// gst_buffer_unmap has to see unchanged.
type mapInfo struct {
	_        structs.HostLayout
	memory   uintptr
	flags    int32
	data     unsafe.Pointer
	size     uintptr
	maxsize  uintptr
	reserved [8]uintptr
}

// A gError is a GLib GError: where it came from, what it was, and a message
// to free.
type gError struct {
	_       structs.HostLayout
	domain  uint32
	code    int32
	message *byte
}

// The layouts are the C headers' own: a field added or moved here would be
// a silent misread of a struct GStreamer writes.
var (
	_ [unsafe.Sizeof(mapInfo{}) - 104]byte
	_ [104 - unsafe.Sizeof(mapInfo{})]byte
	_ [unsafe.Sizeof(gError{}) - 16]byte
	_ [16 - unsafe.Sizeof(gError{})]byte
)

// The libraries every machine that can play media has: GStreamer's core,
// the appsink helper from its plugin base set, and the GLib and GObject
// underneath them.
var libraries = []string{
	"libgstreamer-1.0.so.0",
	"libgstapp-1.0.so.0",
	"libglib-2.0.so.0",
	"libgobject-2.0.so.0",
}

var (
	loadOnce sync.Once
	loadErr  error
	libs     []uintptr

	// The error domains, as quarks: looked up rather than assumed.
	quarkCore     uint32
	quarkStream   uint32
	quarkResource uint32
)

// The functions this package binds. A function whose arguments include a Go
// pointer takes that pointer rather than an address as an integer, so that
// the garbage collector keeps what it points at alive across the call.
var (
	// libgstreamer-1.0
	gstInit                       func(argc *int32, argv **byte)
	gstElementFactoryMake         func(factory, name *byte) uintptr
	gstElementFactoryFind         func(name *byte) uintptr
	gstElementMakeFromURI         func(kind int32, uri, name *byte, err **gError) uintptr
	gstPipelineNew                func(name *byte) uintptr
	gstParseLaunchFull            func(description *byte, context uintptr, flags int32, err **gError) uintptr
	gstParseBinFromDescription    func(description *byte, ghost int32, err **gError) uintptr
	gstBinAdd                     func(bin, element uintptr) int32
	gstBinGetByName               func(bin uintptr, name *byte) uintptr
	gstElementLink                func(a, b uintptr) int32
	gstElementSetState            func(element uintptr, state int32) int32
	gstElementQueryDuration       func(element uintptr, format int32, dur *int64) int32
	gstElementSeekSimple          func(element uintptr, format, flags int32, position int64) int32
	gstElementGetBus              func(element uintptr) uintptr
	gstBusTimedPopFiltered        func(bus uintptr, timeout uint64, kinds int32) uintptr
	gstMessageParseError          func(message uintptr, err **gError, debug **byte)
	gstSampleGetBuffer            func(sample uintptr) uintptr
	gstSampleGetCaps              func(sample uintptr) uintptr
	gstBufferMap                  func(buffer uintptr, info *mapInfo, flags int32) int32
	gstBufferUnmap                func(buffer uintptr, info *mapInfo)
	gstCapsGetSize                func(caps uintptr) uint32
	gstCapsGetStructure           func(caps uintptr, index uint32) uintptr
	gstCapsToString               func(caps uintptr) *byte
	gstStructureGetName           func(structure uintptr) *byte
	gstStructureGetInt            func(structure uintptr, field *byte, value *int32) int32
	gstStructureGetUint           func(structure uintptr, field *byte, value *uint32) int32
	gstStructureGetFraction       func(structure uintptr, field *byte, numerator, denominator *int32) int32
	gstStructureGetString         func(structure uintptr, field *byte) *byte
	gstPadGetCurrentCaps          func(pad uintptr) uintptr
	gstPadLink                    func(source, sink uintptr) int32
	gstElementGetStaticPad        func(element uintptr, name *byte) uintptr
	gstElementSyncStateWithParent func(element uintptr) int32
	gstObjectGetName              func(object uintptr) *byte
	gstObjectUnref                func(object uintptr)
	gstMiniObjectRef              func(object uintptr) uintptr
	gstMiniObjectUnref            func(object uintptr)
	gstFilenameToURI              func(location *byte, err **gError) *byte
	gstDeviceMonitorNew           func() uintptr
	gstDeviceMonitorAddFilter     func(monitor uintptr, classes *byte, caps uintptr) int32
	gstDeviceMonitorStart         func(monitor uintptr) int32
	gstDeviceMonitorGetDevices    func(monitor uintptr) uintptr
	gstDeviceMonitorStop          func(monitor uintptr)
	gstDeviceGetDisplayName       func(device uintptr) *byte
	gstDeviceGetProperties        func(device uintptr) uintptr
	gstStructureFree              func(structure uintptr)

	// libgstapp-1.0
	gstAppSinkTryPullSample func(sink uintptr, timeout uint64) uintptr
	gstAppSinkIsEOS         func(sink uintptr) int32
	gstAppSinkSetDrop       func(sink uintptr, drop int32)
	gstAppSinkSetMaxBuffers func(sink uintptr, max int32)

	// libglib-2.0 and libgobject-2.0
	gFree              func(p unsafe.Pointer)
	gErrorFree         func(err *gError)
	gObjectRefSink     func(object uintptr) uintptr
	gObjectSetString   func(object uintptr, name, value *byte, end uintptr)
	gObjectSetObject   func(object uintptr, name *byte, value uintptr, end uintptr)
	gObjectSetInt      func(object uintptr, name *byte, value int32, end uintptr)
	gSignalConnectData func(instance uintptr, signal *byte, handler, data, destroy uintptr, flags int32) uint64
	gQuarkFromStatic   func(name *byte) uint32
	gListLength        func(list uintptr) uint32
	gListNthData       func(list uintptr, n uint32) uintptr
	gListFree          func(list uintptr)
)

// Load loads GStreamer, once. It returns an error when the machine has no
// GStreamer 1.x runtime at all, which is what media reports as a feature
// this machine does not have.
func Load() error {
	loadOnce.Do(open)
	return loadErr
}

func open() {
	for _, name := range libraries {
		lib, err := purego.Dlopen(name, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			loadErr = fmt.Errorf("gstreamer: %s: %w", name, err)
			return
		}
		libs = append(libs, lib)
	}
	core, app, glib, gobject := libs[0], libs[1], libs[2], libs[3]
	sym := func(lib uintptr, name string, fn any) {
		if loadErr != nil {
			return
		}
		var addr uintptr
		if addr, loadErr = purego.Dlsym(lib, name); loadErr != nil {
			loadErr = fmt.Errorf("gstreamer: %s: %w", name, loadErr)
			return
		}
		purego.RegisterFunc(fn, addr)
	}

	sym(core, "gst_init", &gstInit)
	sym(core, "gst_element_factory_make", &gstElementFactoryMake)
	sym(core, "gst_element_factory_find", &gstElementFactoryFind)
	sym(core, "gst_element_make_from_uri", &gstElementMakeFromURI)
	sym(core, "gst_pipeline_new", &gstPipelineNew)
	sym(core, "gst_parse_launch_full", &gstParseLaunchFull)
	sym(core, "gst_parse_bin_from_description", &gstParseBinFromDescription)
	sym(core, "gst_bin_add", &gstBinAdd)
	sym(core, "gst_bin_get_by_name", &gstBinGetByName)
	sym(core, "gst_element_link", &gstElementLink)
	sym(core, "gst_element_set_state", &gstElementSetState)
	sym(core, "gst_element_query_duration", &gstElementQueryDuration)
	sym(core, "gst_element_seek_simple", &gstElementSeekSimple)
	sym(core, "gst_element_get_bus", &gstElementGetBus)
	sym(core, "gst_bus_timed_pop_filtered", &gstBusTimedPopFiltered)
	sym(core, "gst_message_parse_error", &gstMessageParseError)
	sym(core, "gst_sample_get_buffer", &gstSampleGetBuffer)
	sym(core, "gst_sample_get_caps", &gstSampleGetCaps)
	sym(core, "gst_buffer_map", &gstBufferMap)
	sym(core, "gst_buffer_unmap", &gstBufferUnmap)
	sym(core, "gst_caps_get_size", &gstCapsGetSize)
	sym(core, "gst_caps_get_structure", &gstCapsGetStructure)
	sym(core, "gst_caps_to_string", &gstCapsToString)
	sym(core, "gst_structure_get_name", &gstStructureGetName)
	sym(core, "gst_structure_get_int", &gstStructureGetInt)
	sym(core, "gst_structure_get_uint", &gstStructureGetUint)
	sym(core, "gst_structure_get_fraction", &gstStructureGetFraction)
	sym(core, "gst_structure_get_string", &gstStructureGetString)
	sym(core, "gst_pad_get_current_caps", &gstPadGetCurrentCaps)
	sym(core, "gst_pad_link", &gstPadLink)
	sym(core, "gst_element_get_static_pad", &gstElementGetStaticPad)
	sym(core, "gst_element_sync_state_with_parent", &gstElementSyncStateWithParent)
	sym(core, "gst_object_get_name", &gstObjectGetName)
	sym(core, "gst_object_unref", &gstObjectUnref)
	sym(core, "gst_mini_object_ref", &gstMiniObjectRef)
	sym(core, "gst_mini_object_unref", &gstMiniObjectUnref)
	sym(core, "gst_filename_to_uri", &gstFilenameToURI)
	sym(core, "gst_device_monitor_new", &gstDeviceMonitorNew)
	sym(core, "gst_device_monitor_add_filter", &gstDeviceMonitorAddFilter)
	sym(core, "gst_device_monitor_start", &gstDeviceMonitorStart)
	sym(core, "gst_device_monitor_get_devices", &gstDeviceMonitorGetDevices)
	sym(core, "gst_device_monitor_stop", &gstDeviceMonitorStop)
	sym(core, "gst_device_get_display_name", &gstDeviceGetDisplayName)
	sym(core, "gst_device_get_properties", &gstDeviceGetProperties)
	sym(core, "gst_structure_free", &gstStructureFree)

	sym(app, "gst_app_sink_try_pull_sample", &gstAppSinkTryPullSample)
	sym(app, "gst_app_sink_is_eos", &gstAppSinkIsEOS)
	sym(app, "gst_app_sink_set_drop", &gstAppSinkSetDrop)
	sym(app, "gst_app_sink_set_max_buffers", &gstAppSinkSetMaxBuffers)

	sym(gobject, "g_object_ref_sink", &gObjectRefSink)
	sym(gobject, "g_object_set", &gObjectSetString)
	sym(gobject, "g_object_set", &gObjectSetObject)
	sym(gobject, "g_object_set", &gObjectSetInt)
	sym(gobject, "g_signal_connect_data", &gSignalConnectData)
	sym(glib, "g_free", &gFree)
	sym(glib, "g_error_free", &gErrorFree)
	sym(glib, "g_quark_from_static_string", &gQuarkFromStatic)
	sym(glib, "g_list_length", &gListLength)
	sym(glib, "g_list_nth_data", &gListNthData)
	sym(glib, "g_list_free", &gListFree)
	if loadErr != nil {
		return
	}

	// gst_init sets up the plugin registry every factory comes from. It
	// may be called more than once and takes no arguments here; from now
	// on, the machine's own plugin cache decides what exists.
	gstInit(nil, nil)
	quarkCore = withCStringRet("gst-core-error-quark", func(name *byte) uint32 { return gQuarkFromStatic(name) })
	quarkStream = withCStringRet("gst-stream-error-quark", func(name *byte) uint32 { return gQuarkFromStatic(name) })
	quarkResource = withCStringRet("gst-resource-error-quark", func(name *byte) uint32 { return gQuarkFromStatic(name) })
}

// ErrMissing is the error a machine without an element hands back: a plugin
// its distribution did not install.
var ErrMissing = errors.New("gstreamer: the plugin is not installed")

// A MissingError names the element a machine does not have, which is what a
// file needs and this distribution does not ship.
type MissingError struct{ Element string }

func (e *MissingError) Error() string { return "gstreamer: no element named " + e.Element }
func (e *MissingError) Unwrap() error { return ErrMissing }

// goString copies a string GStreamer owns. It copies rather than aliases:
// what it points at is freed, or is the pipeline's memory and reused, as soon
// as the call that handed it over is done.
func goString(s *byte) string {
	if s == nil {
		return ""
	}
	n := 0
	for p := unsafe.Pointer(s); *(*byte)(unsafe.Add(p, n)) != 0; n++ {
	}
	return string(unsafe.Slice(s, n))
}

// native turns an address GStreamer handed back into a pointer again, which
// is what reading a field out of its structs takes.
func native(addr uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&addr)) }

// withCString calls fn with a temporary C string: what it points at lives
// until fn returns, so GStreamer must not keep it.
func withCString(s string, fn func(*byte)) {
	b := append([]byte(s), 0)
	fn(&b[0])
}

// withCStringRet is withCString for a call whose closure returns something,
// which Go cannot infer on its own.
func withCStringRet[T any](s string, fn func(*byte) T) T {
	b := append([]byte(s), 0)
	return fn(&b[0])
}

// newError turns a GError, which is freed, into a Go error.
func newError(what string, err *gError) error {
	if err == nil {
		return errors.New("gstreamer: " + what)
	}
	message := goString(err.message)
	gErrorFree(err)
	if message == "" {
		return errors.New("gstreamer: " + what)
	}
	return fmt.Errorf("gstreamer: %s: %s", what, message)
}

// An Element is a GStreamer element, a bin or a pipeline, and owns one
// reference to it until Release.
type Element struct{ ptr uintptr }

// Pointer is the element's address, for a call this package does not wrap.
func (e Element) Pointer() uintptr { return e.ptr }

// Valid reports whether there is an element at all.
func (e Element) Valid() bool { return e.ptr != 0 }

// Release drops this reference to the element. An element handed to a bin
// by Add is not released again.
func (e Element) Release() {
	if e.ptr != 0 {
		gstObjectUnref(e.ptr)
	}
}

// Set sets a property from a Go value: a string, an element, or a whole
// number.
func (e Element) Set(name string, value any) error {
	return withCStringRet(name, func(property *byte) error {
		switch v := value.(type) {
		case string:
			withCString(v, func(text *byte) { gObjectSetString(e.ptr, property, text, 0) })
			return nil
		case Element:
			if !v.Valid() {
				return fmt.Errorf("gstreamer: setting %s to nothing", name)
			}
			gObjectSetObject(e.ptr, property, v.ptr, 0)
			return nil
		case int:
			gObjectSetInt(e.ptr, property, int32(v), 0)
			return nil
		}
		return fmt.Errorf("gstreamer: cannot set %s to %T", name, value)
	})
}

// Add puts a child in a bin or pipeline, which then owns it: the reference
// the caller held goes to the bin, and the child must not be released.
func (e Element) Add(child Element) error {
	if !e.Valid() || !child.Valid() {
		return errors.New("gstreamer: adding nothing to a bin")
	}
	if gstBinAdd(e.ptr, child.ptr) == 0 {
		return errors.New("gstreamer: a bin refused an element")
	}
	gstObjectUnref(child.ptr)
	return nil
}

// ByName returns the named descendant of a bin or pipeline, which the
// caller releases, or an invalid element when there is none.
func (e Element) ByName(name string) Element {
	var ptr uintptr
	withCString(name, func(want *byte) { ptr = gstBinGetByName(e.ptr, want) })
	return Element{ptr: ptr}
}

// Link connects two elements, both of which stay owned by the caller.
func Link(a, b Element) error {
	if !a.Valid() || !b.Valid() || gstElementLink(a.ptr, b.ptr) == 0 {
		return errors.New("gstreamer: two elements would not link")
	}
	return nil
}

// SetState moves the element to a state, and says how that went.
func (e Element) SetState(state State) StateChange {
	return StateChange(gstElementSetState(e.ptr, int32(state)))
}

// Duration returns how long the element's stream is, when it knows.
func (e Element) Duration() (time.Duration, bool) {
	var ns int64
	if gstElementQueryDuration(e.ptr, formatTime, &ns) == 0 || ns < 0 {
		return 0, false
	}
	return time.Duration(ns), true
}

// Seek moves the element's stream to t. A seek flushes what is already
// buffered, and is asked for the time itself rather than the key frame at or
// before it: a stream read from a key frame before the time asked for would
// hand out pictures the caller did not ask for.
func (e Element) Seek(t time.Duration) error {
	if t < 0 {
		t = 0
	}
	if gstElementSeekSimple(e.ptr, formatTime, seekFlush, int64(t)) == 0 {
		return errors.New("gstreamer: the seek was refused")
	}
	return nil
}

// Bus returns the element's bus, which the caller releases, or nil for an
// element that has none.
func (e Element) Bus() *Bus {
	ptr := gstElementGetBus(e.ptr)
	if ptr == 0 {
		return nil
	}
	return &Bus{ptr: ptr}
}

// Find reports whether the machine's GStreamer has an element of this name,
// which is how media answers what it can decode.
func Find(name string) bool {
	if Load() != nil {
		return false
	}
	found := false
	withCString(name, func(want *byte) {
		if factory := gstElementFactoryFind(want); factory != 0 {
			found = true
			gstObjectUnref(factory)
		}
	})
	return found
}

// NewElement makes an element from an installed factory, or reports the
// element the machine does not have.
func NewElement(factory string) (Element, error) {
	if err := Load(); err != nil {
		return Element{}, err
	}
	var ptr uintptr
	withCString(factory, func(name *byte) {
		if ptr = gstElementFactoryMake(name, nil); ptr != 0 {
			// A factory hands out a floating reference: sink it, so
			// that the caller owns one and Add can hand it on.
			gObjectRefSink(ptr)
		}
	})
	if ptr == 0 {
		return Element{}, &MissingError{Element: factory}
	}
	return Element{ptr: ptr}, nil
}

// NewBin builds a bin from a description, as gst-launch writes one, with
// its unlinked ends ghosted so that the bin plugs into a pipeline.
func NewBin(description string) (Element, error) {
	if err := Load(); err != nil {
		return Element{}, err
	}
	var (
		ptr uintptr
		err error
	)
	withCString(description, func(text *byte) {
		var gerr *gError
		if ptr = gstParseBinFromDescription(text, 1, &gerr); ptr == 0 {
			err = newError("building "+description, gerr)
			return
		}
		gObjectRefSink(ptr)
	})
	return Element{ptr: ptr}, err
}

// NewPipeline makes a pipeline, the container an element needs to be given
// a clock and a state.
func NewPipeline() (Element, error) {
	if err := Load(); err != nil {
		return Element{}, err
	}
	ptr := gstPipelineNew(nil)
	if ptr == 0 {
		return Element{}, errors.New("gstreamer: no pipeline")
	}
	gObjectRefSink(ptr)
	return Element{ptr: ptr}, nil
}

// NewPipelineFrom builds a whole pipeline from a description, as gst-launch
// writes one.
func NewPipelineFrom(description string) (Element, error) {
	if err := Load(); err != nil {
		return Element{}, err
	}
	var (
		ptr uintptr
		err error
	)
	withCString(description, func(text *byte) {
		var gerr *gError
		if ptr = gstParseLaunchFull(text, 0, parseFatal, &gerr); ptr == 0 {
			err = newError("building "+description, gerr)
			return
		}
		gObjectRefSink(ptr)
	})
	return Element{ptr: ptr}, err
}

// MakeFromURI makes the source element for a URI, which is what reads a
// file: the machine's own choice of source, so that http and rtsp work the
// same way as a path does.
func MakeFromURI(uri string) (Element, error) {
	if err := Load(); err != nil {
		return Element{}, err
	}
	var (
		ptr uintptr
		err error
	)
	withCString(uri, func(text *byte) {
		var gerr *gError
		if ptr = gstElementMakeFromURI(uriSrc, text, nil, &gerr); ptr == 0 {
			err = newError("no source for "+uri, gerr)
			return
		}
		gObjectRefSink(ptr)
	})
	return Element{ptr: ptr}, err
}

// FileURI turns a path into a file URI, resolving it against the working
// directory, and leaves a URI alone.
func FileURI(target string) (string, error) {
	if err := Load(); err != nil {
		return "", err
	}
	var (
		out string
		err error
	)
	withCString(target, func(text *byte) {
		var gerr *gError
		uri := gstFilenameToURI(text, &gerr)
		if uri == nil {
			err = newError("reading "+target, gerr)
			return
		}
		out = goString(uri)
		gFree(unsafe.Pointer(uri))
	})
	return out, err
}

// An AppSink is the end of a pipeline that media pulls frames from, rather
// than one that hands them over as they arrive.
type AppSink struct{ Element }

// PullSample takes the next buffer within the timeout, or nil when there
// was none: the end of a stream, an error, and a timeout all look like
// that, which is what IsEOS and the bus tell apart.
func (s AppSink) PullSample(timeout time.Duration) *Sample {
	ptr := gstAppSinkTryPullSample(s.ptr, uint64(timeout))
	if ptr == 0 {
		return nil
	}
	return &Sample{ptr: ptr}
}

// IsEOS reports whether the stream has ended.
func (s AppSink) IsEOS() bool { return gstAppSinkIsEOS(s.ptr) != 0 }

// SetDrop makes the sink throw away what it has instead of making the
// pipeline wait for a reader: what a live picture wants, where a late frame
// is worth less than the next one.
func (s AppSink) SetDrop(drop bool) {
	value := int32(0)
	if drop {
		value = 1
	}
	gstAppSinkSetDrop(s.ptr, value)
}

// SetMaxBuffers is how many buffers the sink holds before it either drops
// them or makes the pipeline wait.
func (s AppSink) SetMaxBuffers(n int) { gstAppSinkSetMaxBuffers(s.ptr, int32(n)) }

// A Sample is one buffer of data and the caps that describe it, as an
// appsink hands it out. The caller releases it.
type Sample struct{ ptr uintptr }

// Release drops the sample.
func (s *Sample) Release() {
	if s.ptr != 0 {
		gstMiniObjectUnref(s.ptr)
		s.ptr = 0
	}
}

// Caps returns what the sample's bytes are; the caller releases them.
func (s *Sample) Caps() Caps {
	ptr := gstSampleGetCaps(s.ptr)
	if ptr == 0 {
		return Caps{}
	}
	// The sample owns its caps: take a reference of our own.
	return Caps{ptr: gstMiniObjectRef(ptr)}
}

// Time returns when the sample is due on the media's own timeline.
func (s *Sample) Time() (time.Duration, bool) { return s.clock(offsetBufferPTS) }

// Duration returns how long the sample lasts, when it says.
func (s *Sample) Duration() (time.Duration, bool) { return s.clock(offsetBufferLength) }

func (s *Sample) clock(offset uintptr) (time.Duration, bool) {
	ptr := gstSampleGetBuffer(s.ptr)
	if ptr == 0 {
		return 0, false
	}
	ns := *(*uint64)(unsafe.Add(native(ptr), offset))
	if ns == clockTimeNone {
		return 0, false
	}
	return time.Duration(ns), true
}

// A Mapped is a sample's bytes, readable until Unmap.
type Mapped struct {
	// Data is what the buffer holds: for raw video, whole rows.
	Data []byte

	info mapInfo
	buf  uintptr
}

// Map maps the sample's buffer for reading.
func (s *Sample) Map() (*Mapped, error) {
	buf := gstSampleGetBuffer(s.ptr)
	if buf == 0 {
		return nil, errors.New("gstreamer: a sample with no buffer")
	}
	m := &Mapped{buf: buf}
	if gstBufferMap(buf, &m.info, mapRead) == 0 {
		return nil, errors.New("gstreamer: a buffer that could not be read")
	}
	m.Data = unsafe.Slice((*byte)(m.info.data), m.info.size)
	return m, nil
}

// Unmap unmaps the buffer, after which Data must not be read.
func (m *Mapped) Unmap() {
	if m.buf == 0 {
		return
	}
	gstBufferUnmap(m.buf, &m.info)
	m.buf = 0
	m.Data = nil
}

// Caps are the formats a pad or a sample accepts. The caller releases them.
type Caps struct{ ptr uintptr }

// Release drops the reference to the caps.
func (c Caps) Release() {
	if c.ptr != 0 {
		gstMiniObjectUnref(c.ptr)
	}
}

// Valid reports whether there are caps at all: a pad that has not
// negotiated a format has none, and neither has a sample that does not say.
func (c Caps) Valid() bool { return c.ptr != 0 }

// Count is how many formats the caps allow; a negotiated stream has one.
func (c Caps) Count() int {
	if c.ptr == 0 {
		return 0
	}
	return int(gstCapsGetSize(c.ptr))
}

// Structure returns one of the formats, borrowed from the caps: it lives as
// long as they do.
func (c Caps) Structure(i int) Structure {
	if c.ptr == 0 || i < 0 || i >= c.Count() {
		return Structure{}
	}
	return Structure{ptr: gstCapsGetStructure(c.ptr, uint32(i))}
}

// String is the caps in GStreamer's own notation, as gst-launch prints
// them, which is what an error about a format should quote.
func (c Caps) String() string {
	if c.ptr == 0 {
		return ""
	}
	text := gstCapsToString(c.ptr)
	if text == nil {
		return ""
	}
	defer gFree(unsafe.Pointer(text))
	return goString(text)
}

// A Structure is one format in a set of caps, borrowed from them.
type Structure struct{ ptr uintptr }

// Valid reports whether there is a format at all.
func (s Structure) Valid() bool { return s.ptr != 0 }

// Name is the format's family, such as video/x-raw or audio/mpeg.
func (s Structure) Name() string {
	if s.ptr == 0 {
		return ""
	}
	return goString(gstStructureGetName(s.ptr))
}

// Int reads a whole-number field, such as width or rate.
func (s Structure) Int(field string) (int, bool) {
	var value int32
	found := false
	withCString(field, func(name *byte) { found = gstStructureGetInt(s.ptr, name, &value) != 0 })
	return int(value), found
}

// Uint reads an unsigned whole-number field.
func (s Structure) Uint(field string) (uint, bool) {
	var value uint32
	found := false
	withCString(field, func(name *byte) { found = gstStructureGetUint(s.ptr, name, &value) != 0 })
	return uint(value), found
}

// Fraction reads a field that is a ratio, such as a frame rate.
func (s Structure) Fraction(field string) (int, int, bool) {
	var numerator, denominator int32
	found := false
	withCString(field, func(name *byte) {
		found = gstStructureGetFraction(s.ptr, name, &numerator, &denominator) != 0
	})
	return int(numerator), int(denominator), found
}

// String reads a field that names something, such as a pixel format.
func (s Structure) String(field string) (string, bool) {
	var value string
	found := false
	withCString(field, func(name *byte) {
		if text := gstStructureGetString(s.ptr, name); text != nil {
			value, found = goString(text), true
		}
	})
	return value, found
}

// A Bus is where a pipeline posts what happened to it. The caller releases
// it.
type Bus struct{ ptr uintptr }

// Release drops the reference to the bus.
func (b *Bus) Release() {
	if b != nil && b.ptr != 0 {
		gstObjectUnref(b.ptr)
		b.ptr = 0
	}
}

// Pop takes the next message of the given kinds within the timeout, or nil
// when there was none. A negative timeout waits as long as it takes.
func (b *Bus) Pop(kinds int32, timeout time.Duration) *Message {
	if b == nil || b.ptr == 0 {
		return nil
	}
	wait := clockTimeNone
	if timeout >= 0 {
		wait = uint64(timeout)
	}
	ptr := gstBusTimedPopFiltered(b.ptr, wait, kinds)
	if ptr == 0 {
		return nil
	}
	return &Message{ptr: ptr, Type: int32(*(*int32)(unsafe.Add(native(ptr), offsetMessageType)))}
}

// A Message is one thing that happened to a pipeline.
type Message struct {
	ptr  uintptr
	Type int32
}

// Release drops the message.
func (m *Message) Release() {
	if m.ptr != 0 {
		gstMiniObjectUnref(m.ptr)
		m.ptr = 0
	}
}

// A PipelineError is what GStreamer said went wrong, with the domain and
// code that say whether the machine or the file is at fault.
type PipelineError struct {
	// Message is GStreamer's own words.
	Message string
	// Domain and Code are its error domain and code.
	Domain uint32
	Code   int32
}

func (e *PipelineError) Error() string { return "gstreamer: " + e.Message }

// Missing reports whether the machine lacks the plugin that would read or
// decode this: a file this machine cannot play, not a broken one.
func (e *PipelineError) Missing() bool {
	return e.Domain == quarkCore && e.Code == coreErrorMissingPlugin
}

// Unsupported reports whether the file is one this machine's GStreamer
// cannot read or decode, rather than one that is broken.
func (e *PipelineError) Unsupported() bool {
	switch e.Domain {
	case quarkCore:
		return e.Code == coreErrorNotImplemented
	case quarkStream:
		switch e.Code {
		case streamErrorTypeNotFound, streamErrorDecode, streamErrorDemux, streamErrorFormat:
			return true
		}
	}
	return false
}

// NotFound reports whether the location itself is not there.
func (e *PipelineError) NotFound() bool {
	return e.Domain == quarkResource && e.Code == resourceErrorNotFound
}

// PipelineError turns an ERROR message into what it said, or nil for a
// message that is not an error.
func (m *Message) PipelineError() *PipelineError {
	if m.Type != MessageError {
		return nil
	}
	var (
		err   *gError
		debug *byte
	)
	gstMessageParseError(m.ptr, &err, &debug)
	if err == nil {
		return &PipelineError{Message: "the pipeline failed"}
	}
	out := &PipelineError{Message: goString(err.message), Domain: err.domain, Code: err.code}
	gErrorFree(err)
	return out
}

// A Stream is one stream of a file, as its demuxer describes it: the pad it
// appears on, in stream order, and the caps it carries.
type Stream struct {
	// Name is the demuxer's name for the stream, such as src_0.
	Name string
	// Caps describe the stream; the caller releases them.
	Caps Caps
}

// A probe collects the pads a demuxer adds while a file is opened, and the
// caps each carries. It drains them into sinks of its own, because a demuxer
// whose new pad nobody reads stops the pipeline at the first one, and then
// the streams behind it are never seen.
type probe struct {
	id   uintptr
	pipe uintptr
	mu   sync.Mutex
	pads []uintptr
	seen time.Time
}

var (
	probeSeq atomic.Uintptr
	probes   sync.Map // probe id -> *probe
)

// add takes a reference to a pad a demuxer has just made, so that its caps
// can be read once the pipeline has settled.
func (p *probe) add(pad uintptr) {
	if pad == 0 {
		return
	}
	gObjectRefSink(pad)
	p.mu.Lock()
	p.pads = append(p.pads, pad)
	p.seen = time.Now()
	p.mu.Unlock()
}

// drain links a demuxer's new pad to a sink that throws away what arrives,
// and takes that sink to the pipeline's state so that the stream runs.
func (p *probe) drain(pad uintptr) {
	if p.pipe == 0 {
		return
	}
	sink, err := NewElement("fakesink")
	if err != nil {
		return
	}
	sink.Set("sync", 0)  // do not wait for the clock
	sink.Set("async", 0) // and do not hold up going to a state
	pipe := Element{ptr: p.pipe}
	if err := pipe.Add(sink); err != nil {
		sink.Release()
		return
	}
	target := withCStringRet("sink", func(name *byte) uintptr {
		return gstElementGetStaticPad(sink.ptr, name)
	})
	if target == 0 || gstPadLink(pad, target) != 0 { // GST_PAD_LINK_OK
		return
	}
	gstElementSyncStateWithParent(sink.ptr)
}

// ready reports whether the demuxer has stopped adding pads: it has made at
// least one, and none for a while.
func (p *probe) ready(quiet time.Duration) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.pads) > 0 && time.Since(p.seen) >= quiet
}

// streams reads what the collected pads carry and lets them go.
func (p *probe) streams() []Stream {
	p.mu.Lock()
	pads := append([]uintptr(nil), p.pads...)
	p.pads = nil
	p.mu.Unlock()
	out := make([]Stream, 0, len(pads))
	for _, pad := range pads {
		stream := Stream{Name: goString(gstObjectGetName(pad))}
		if caps := gstPadGetCurrentCaps(pad); caps != 0 {
			stream.Caps = Caps{ptr: caps}
		}
		out = append(out, stream)
		gstObjectUnref(pad)
	}
	return out
}

var (
	padAddedOnce sync.Once
	padAddedFn   uintptr
)

// padAdded is the one callback this package makes. Purego callbacks are
// never freed, so it is made once, for every probe, and the probe it belongs
// to is found from the user data the signal carries.
func padAdded() uintptr {
	padAddedOnce.Do(func() {
		padAddedFn = purego.NewCallback(func(element, pad, data uintptr) {
			found, ok := probes.Load(data)
			if !ok {
				return
			}
			p := found.(*probe)
			p.add(pad)
			p.drain(pad)
		})
	})
	return padAddedFn
}

// Describe opens a file just far enough to see the streams in it, and
// returns them in the demuxer's order, having decoded nothing: a parsebin
// parses the container and hands out one pad per stream.
//
// The last stream's caps are what says whether a file has picture or sound,
// and which codec each is, which is how media answers Tracks before it
// decodes anything.
func Describe(uri string, timeout time.Duration) ([]Stream, error) {
	if err := Load(); err != nil {
		return nil, err
	}
	source, err := MakeFromURI(uri)
	if err != nil {
		return nil, err
	}
	pipe, err := NewPipeline()
	if err != nil {
		source.Release()
		return nil, err
	}
	defer pipe.Release()
	parse, err := NewElement("parsebin")
	if err != nil {
		source.Release()
		return nil, err
	}
	// From here the pipeline owns them, and Add has consumed the reference
	// each constructor handed over.
	if err := pipe.Add(source); err != nil {
		source.Release()
		parse.Release()
		return nil, err
	}
	if err := pipe.Add(parse); err != nil {
		parse.Release()
		return nil, err
	}
	if err := Link(source, parse); err != nil {
		return nil, err
	}

	p := &probe{id: probeSeq.Add(1), pipe: pipe.ptr}
	probes.Store(p.id, p)
	defer probes.Delete(p.id)
	withCString("pad-added", func(signal *byte) {
		gSignalConnectData(parse.ptr, signal, padAdded(), p.id, 0, 0)
	})

	// A source element with no sink of its own is a pipeline with nothing
	// to preroll, so a state change is enough to make the demuxer parse
	// the header and hand out its pads.
	pipe.SetState(StatePaused)
	defer pipe.SetState(StateNull)

	bus := pipe.Bus()
	defer bus.Release()

	// The demuxer has said everything it has when it stops making pads: a
	// file's streams all come out of its header, so a moment without a new
	// one is the end of them. Waiting for the whole file instead would read
	// all of it to learn what is in it.
	const quiet = 150 * time.Millisecond
	deadline := time.Now().Add(timeout)
	for !p.ready(quiet) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	streams := p.streams()
	if len(streams) == 0 {
		// Nothing came out of the container, so what the pipeline said
		// is the answer: a format it cannot read, or a plugin the
		// machine does not have.
		if bus != nil {
			if message := bus.Pop(MessageError, 0); message != nil {
				err := message.PipelineError()
				message.Release()
				if err != nil {
					return nil, err
				}
			}
		}
		return nil, fmt.Errorf("gstreamer: no streams in %s", uri)
	}
	return streams, nil
}
