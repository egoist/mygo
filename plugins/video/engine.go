package video

import (
	"errors"
	"fmt"
	"github.com/egoist/mygo/plugins/internal/contentlib"
	"image"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"
)

type backend interface {
	command(uint64, []string) error
	next() (event, bool)
	render(int, int) (*image.RGBA, error)
	close()
}
type event struct {
	unavailable bool
	kind        int32
	reply       uint64
	err         error
	name        string
	number      float64
	flag        bool
	entry       int64
	reason      int32
}
type nativeEvent struct {
	kind, error int32
	reply       uint64
	data        unsafe.Pointer
}
type property struct {
	name   unsafe.Pointer
	format int32
	_      int32
	data   unsafe.Pointer
}
type endFile struct {
	reason, error int32
	entry         int64
}
type renderParam struct {
	kind int32
	_    int32
	data unsafe.Pointer
}
type engine struct {
	library      uintptr
	version      func() uint32
	create       func() uintptr
	option       func(uintptr, string, string) int32
	initialize   func(uintptr) int32
	destroy      func(uintptr)
	command      func(uintptr, uint64, unsafe.Pointer) int32
	wait         func(uintptr, float64) *nativeEvent
	observe      func(uintptr, uint64, string, int32) int32
	wake         func(uintptr, uintptr, uintptr)
	errorString  func(int32) unsafe.Pointer
	renderCreate func(*uintptr, uintptr, *renderParam) int32
	renderUpdate func(uintptr) uint64
	render       func(uintptr, *renderParam) int32
	renderWake   func(uintptr, uintptr, uintptr)
	renderFree   func(uintptr)
}

var libraryMu sync.Mutex
var libraries = map[string]*engine{}
var librariesByHandle = map[uintptr]*engine{}
var callbackOnce sync.Once
var callback uintptr
var callbackID atomic.Uint64
var routes sync.Map

// One callback per signature for the whole process. It routes by integer user
// data, and does nothing except wake Go workers; no native API or UI runs here.
func route(ch chan struct{}) uintptr {
	callbackOnce.Do(func() {
		callback = contentlib.Callback(func(id uintptr) {
			if c, ok := routes.Load(id); ok {
				select {
				case c.(chan struct{}) <- struct{}{}:
				default:
				}
			}
		})
	})
	id := uintptr(callbackID.Add(1))
	routes.Store(id, ch)
	return id
}
func signal(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}
func loadEngine(path string) (*engine, error) {
	libraryMu.Lock()
	defer libraryMu.Unlock()
	if e := libraries[path]; e != nil {
		return e, nil
	}
	names := []string{"libmpv.so.2", "libmpv.so.1"}
	switch runtime.GOOS {
	case "darwin":
		names = []string{"libmpv.2.dylib", "/opt/homebrew/lib/libmpv.2.dylib", "/usr/local/lib/libmpv.2.dylib"}
	case "windows":
		names = []string{"libmpv-2.dll", "mpv-2.dll"}
	}
	h, err := contentlib.Open(path, names...)
	if err != nil {
		return nil, fmt.Errorf("video: %w", err)
	}
	if e := librariesByHandle[h]; e != nil {
		contentlib.Release(h)
		libraries[path] = e
		return e, nil
	}
	e := &engine{library: h}
	for _, s := range []struct {
		name string
		fn   any
	}{{"mpv_client_api_version", &e.version}, {"mpv_create", &e.create}, {"mpv_set_option_string", &e.option}, {"mpv_initialize", &e.initialize}, {"mpv_destroy", &e.destroy}, {"mpv_command_async", &e.command}, {"mpv_wait_event", &e.wait}, {"mpv_observe_property", &e.observe}, {"mpv_set_wakeup_callback", &e.wake}, {"mpv_error_string", &e.errorString}, {"mpv_render_context_create", &e.renderCreate}, {"mpv_render_context_update", &e.renderUpdate}, {"mpv_render_context_render", &e.render}, {"mpv_render_context_set_update_callback", &e.renderWake}, {"mpv_render_context_free", &e.renderFree}} {
		if err = contentlib.Bind(h, s.name, s.fn); err != nil {
			contentlib.Release(h)
			return nil, err
		}
	}
	// API v2 adds stable playlist entry IDs used to reject replaced-file events.
	if e.version()>>16 != 2 {
		contentlib.Release(h)
		return nil, errors.New("video: libmpv client API v2 required")
	}
	libraries[path] = e
	librariesByHandle[h] = e
	return e, nil
}
func (e *engine) err(code int32) error {
	if code >= 0 {
		return nil
	}
	return fmt.Errorf("video: %s (%d)", contentlib.CString(e.errorString(code)), code)
}

type native struct {
	e                *engine
	handle, renderer uintptr
	wakeID, renderID uintptr
}

func newNative(opts Options, wake, renderWake chan struct{}) (backend, error) {
	e, err := loadEngine(opts.Library)
	if err != nil {
		return nil, err
	}
	n := &native{e: e, handle: e.create()}
	if n.handle == 0 {
		return nil, errors.New("video: mpv_create failed")
	}
	fail := func(err error) (backend, error) { n.close(); return nil, err }
	for _, o := range [][2]string{{"config", "no"}, {"load-scripts", "no"}, {"ytdl", "no"}, {"input-default-bindings", "no"}, {"input-terminal", "no"}, {"terminal", "no"}, {"vo", "libmpv"}, {"hwdec", "no"}, {"keep-open", "yes"}, {"idle", "yes"}, {"pause", "yes"}, {"osd-level", "0"}} {
		if err = e.err(e.option(n.handle, o[0], o[1])); err != nil {
			return fail(err)
		}
	}
	if opts.Silent {
		if err = e.err(e.option(n.handle, "ao", "null")); err != nil {
			return fail(err)
		}
	}
	if err = e.err(e.initialize(n.handle)); err != nil {
		return fail(err)
	}
	api := []byte("sw\x00")
	params := []renderParam{{kind: 1, data: unsafe.Pointer(&api[0])}, {}}
	if err = e.err(e.renderCreate(&n.renderer, n.handle, &params[0])); err != nil {
		return fail(fmt.Errorf("video: libmpv software render API unavailable: %w", err))
	}
	runtime.KeepAlive(api)
	for i, o := range []struct {
		name   string
		format int32
	}{{"pause", 3}, {"time-pos", 5}, {"duration", 5}, {"volume", 5}, {"mute", 3}, {"speed", 5}, {"seekable", 3}, {"eof-reached", 3}, {"paused-for-cache", 3}} {
		if err = e.err(e.observe(n.handle, uint64(i+1), o.name, o.format)); err != nil {
			return fail(err)
		}
	}
	n.wakeID = route(wake)
	n.renderID = route(renderWake)
	e.wake(n.handle, callback, n.wakeID)
	e.renderWake(n.renderer, callback, n.renderID)
	return n, nil
}
func (n *native) command(id uint64, args []string) error {
	storage := make([][]byte, len(args))
	pointers := make([]unsafe.Pointer, len(args)+1)
	for i, s := range args {
		storage[i] = append([]byte(s), 0)
		pointers[i] = unsafe.Pointer(&storage[i][0])
	}
	err := n.e.err(n.e.command(n.handle, id, unsafe.Pointer(&pointers[0])))
	runtime.KeepAlive(storage)
	runtime.KeepAlive(pointers)
	return err
}
func (n *native) next() (event, bool) {
	raw := n.e.wait(n.handle, 0)
	if raw == nil || raw.kind == 0 {
		return event{}, false
	}
	ev := event{kind: raw.kind, reply: raw.reply, err: n.e.err(raw.error)}
	switch raw.kind {
	case 6:
		if raw.data != nil {
			ev.entry = *(*int64)(raw.data)
		}
	case 7:
		if raw.data != nil {
			d := *(*endFile)(raw.data)
			ev.entry = d.entry
			ev.reason = d.reason
			ev.err = n.e.err(d.error)
		}
	case 22:
		if raw.data != nil {
			p := *(*property)(raw.data)
			ev.name = contentlib.CString(p.name)
			if p.data != nil {
				switch p.format {
				case 3:
					ev.flag = *(*int32)(p.data) != 0
				case 5:
					ev.number = *(*float64)(p.data)
				}
			} else {
				ev.unavailable = true
			}
		}
	}
	return ev, true
}
func (n *native) render(w, h int) (*image.RGBA, error) {
	n.e.renderUpdate(n.renderer)
	size := [2]int32{int32(w), int32(h)}
	stride := uintptr((w*4 + 63) &^ 63)
	buffer := make([]byte, int(stride)*h+63)
	offset := int((64 - uintptr(unsafe.Pointer(&buffer[0]))%64) % 64)
	pix := buffer[offset : offset+int(stride)*h]
	format := []byte("rgb0\x00")
	block := int32(0)
	params := []renderParam{{kind: 17, data: unsafe.Pointer(&size[0])}, {kind: 18, data: unsafe.Pointer(&format[0])}, {kind: 19, data: unsafe.Pointer(&stride)}, {kind: 20, data: unsafe.Pointer(&pix[0])}, {kind: 12, data: unsafe.Pointer(&block)}, {}}
	err := n.e.err(n.e.render(n.renderer, &params[0]))
	runtime.KeepAlive(buffer)
	runtime.KeepAlive(format)
	if err != nil {
		return nil, err
	}
	for y := 0; y < h; y++ {
		row := pix[y*int(stride):]
		for x := 0; x < w; x++ {
			row[x*4+3] = 255
		}
	}
	return &image.RGBA{Pix: pix, Stride: int(stride), Rect: image.Rect(0, 0, w, h)}, nil
}
func (n *native) close() {
	if n.handle != 0 {
		n.e.wake(n.handle, 0, 0)
	}
	routes.Delete(n.wakeID)
	routes.Delete(n.renderID)
	if n.renderer != 0 {
		n.e.renderWake(n.renderer, 0, 0)
		n.e.renderFree(n.renderer)
		n.renderer = 0
	}
	if n.handle != 0 {
		n.e.destroy(n.handle)
		n.handle = 0
	}
}
