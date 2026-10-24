//go:build windows && (amd64 || arm64)

package windows

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

// ErrNotAsync is a read asked of a reader that reads synchronously, whose
// ReadSample waits: it is refused rather than made to wait for a sample these
// calls would never see.
var ErrNotAsync = errors.New("media: the reader reads samples synchronously")

// A ReadResult is one read an asynchronous reader completed: what the platform
// says of it, and the sample it read. Sample is the receiver's to release:
// this package takes its own reference to it before handing it over, since the
// platform lends it to the callback only.
type ReadResult struct {
	// HR is the HRESULT of the read. A failure here is the read's; the
	// callback itself returns S_OK.
	HR uint32
	// Stream is the stream the read was for.
	Stream uint32
	// Flags is what the read says of the sample, such as
	// [FlagEndOfStream].
	Flags uint32
	// Time is when the sample belongs, from the start of the media.
	Time time.Duration
	// Sample is the sample, which the receiver releases.
	Sample Sample
}

// An AsyncCallback is the IMFSourceReaderCallback an asynchronous reader
// reports its reads to. Windows holds COM references to it and calls it on
// its own threads, so it is an object with a vtable of its own and a
// reference count rather than a Go value a caller owns: NewAsyncCallback
// returns one reference, the reader takes another, and the release of the last
// one takes the object out of the registry the vtable methods find it through.
type AsyncCallback struct {
	// vtbl must stay the first word: Windows reads this object as a pointer
	// to its vtable.
	vtbl     uintptr
	refs     atomic.Int32
	flush    chan uint32
	onSample func(ReadResult)
}

// The callback objects Windows may call, by the address it was handed: the
// vtable methods are called with that address and nothing else to tell the
// objects apart, as COM works with one implementation for many objects.
var (
	callbacksMu sync.Mutex
	callbacks   = map[uintptr]*AsyncCallback{}
)

// NewAsyncCallback makes a reader callback that reports every completed read
// to onSample, on the thread Media Foundation completed it on, and that says
// when a flush is done. The caller owns one reference and hands it to the
// reader that uses it, which releases it when the reader is released.
//
// onSample is called on Media Foundation's thread and must not wait: it is
// where a sample is handed to the goroutine that reads.
func NewAsyncCallback(onSample func(ReadResult)) *AsyncCallback {
	c := &AsyncCallback{
		vtbl:     callbackVtable(),
		flush:    make(chan uint32, 1),
		onSample: onSample,
	}
	c.refs.Store(1)
	callbacksMu.Lock()
	callbacks[c.addr()] = c
	callbacksMu.Unlock()
	return c
}

// addr is the address Windows calls the callback at.
func (c *AsyncCallback) addr() uintptr { return uintptr(unsafe.Pointer(c)) }

// addRef takes a reference to the callback.
func (c *AsyncCallback) addRef() { c.refs.Add(1) }

// release gives up one reference; the last one takes the object out of the
// registry, after which Windows must not call it again.
func (c *AsyncCallback) release() {
	if c.refs.Add(-1) > 0 {
		return
	}
	callbacksMu.Lock()
	delete(callbacks, c.addr())
	callbacksMu.Unlock()
}

// WaitFlush waits for the platform to confirm the reader's flush. In
// asynchronous mode Flush returns at once and calls OnFlush when the read it
// took back is over, so a reader can only be moved or let go after it.
func (c *AsyncCallback) WaitFlush(ctx context.Context) error {
	if c == nil {
		return nil
	}
	select {
	case <-c.flush:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// dropFlush forgets a confirmation that arrived and that nobody waited for, so
// that the next WaitFlush waits for its own flush and not for that one.
func (c *AsyncCallback) dropFlush() {
	select {
	case <-c.flush:
	default:
	}
}

// The callback's vtable, made once for the process: every callback object
// points at it, and the callbacks in it stay reachable for the process's life,
// as syscall.NewCallback's pointers must.
var (
	vtableOnce sync.Once
	vtable     []uintptr
)

func callbackVtable() uintptr {
	vtableOnce.Do(func() {
		vtable = []uintptr{
			syscall.NewCallback(callbackQueryInterface),
			syscall.NewCallback(callbackAddRef),
			syscall.NewCallback(callbackRelease),
			syscall.NewCallback(callbackOnReadSample),
			syscall.NewCallback(callbackOnFlush),
			syscall.NewCallback(callbackOnEvent),
		}
	})
	return uintptr(unsafe.Pointer(unsafe.SliceData(vtable)))
}

// lookupCallback is the object whose address Windows calls, or nil.
func lookupCallback(this uintptr) *AsyncCallback {
	callbacksMu.Lock()
	c := callbacks[this]
	callbacksMu.Unlock()
	return c
}

// The vtable's methods. Windows writes them in the order the interface is
// declared: IUnknown's three, then OnReadSample, OnFlush and OnEvent.

func callbackQueryInterface(this, iid, out uintptr) uintptr {
	if out != 0 {
		// A QueryInterface that fails hands back no interface: clear what
		// the caller had to write the answer into.
		*(*uintptr)(native(out)) = 0
	}
	c := lookupCallback(this)
	if c == nil || iid == 0 || out == 0 {
		return eNoInterface
	}
	switch *(*GUID)(native(iid)) {
	case iidSourceReaderCallback, iidUnknown:
		c.addRef()
		*(*uintptr)(native(out)) = this
		return sOK
	}
	return eNoInterface
}

func callbackAddRef(this uintptr) uintptr {
	c := lookupCallback(this)
	if c == nil {
		return 1
	}
	return uintptr(uint32(c.refs.Add(1)))
}

func callbackRelease(this uintptr) uintptr {
	c := lookupCallback(this)
	if c == nil {
		return 0
	}
	if n := c.refs.Add(-1); n > 0 {
		return uintptr(uint32(n))
	}
	callbacksMu.Lock()
	delete(callbacks, this)
	callbacksMu.Unlock()
	return 0
}

func callbackOnReadSample(this, status, stream, flags, timestamp, sample uintptr) uintptr {
	c := lookupCallback(this)
	if c == nil || c.onSample == nil {
		return sOK
	}
	// A read with no sample still says something the reader must hear:
	// the end of the stream, or a failure of the read.
	got := Sample{}
	if sample != 0 {
		// The platform lends the sample to this call: the receiver gets
		// its own reference, which it releases after using it.
		addRef(sample)
		got = Sample{p: sample}
	}
	c.onSample(ReadResult{
		HR:     uint32(status),
		Stream: uint32(stream),
		Flags:  uint32(flags),
		Time:   time.Duration(int64(timestamp)) * 100,
		Sample: got,
	})
	return sOK
}

func callbackOnFlush(this, stream uintptr) uintptr {
	if c := lookupCallback(this); c != nil {
		select {
		case c.flush <- uint32(stream):
		default:
		}
	}
	return sOK
}

func callbackOnEvent(this, stream, event uintptr) uintptr { return sOK }

// ReadSampleAsync starts a read of one stream, which the platform reports to
// the callback instead of returning: reading asynchronously is asking for the
// next sample and coming back for it, so a caller that must not wait for a
// camera can stop waiting.
func (r SourceReader) ReadSampleAsync(stream int) error {
	if r.cb == nil {
		return ErrNotAsync
	}
	hr := comCall(r.p, slotReadSample, uintptr(uint32(stream)), 0, 0, 0, 0, 0)
	if failed(hr) {
		return hresultError("IMFSourceReader::ReadSample", hr)
	}
	return nil
}

// FlushAsync takes back the sample request the reader may have in flight and
// waits for the platform to confirm it, which is what lets the reader be moved
// or let go: SetCurrentPosition refuses to move a reader with a request
// pending, and a reader released with one leaves a callback pointing at
// memory that is gone.
func (r SourceReader) FlushAsync(ctx context.Context, stream int) error {
	if r.cb == nil {
		return ErrNotAsync
	}
	r.cb.dropFlush()
	if err := r.Flush(stream); err != nil {
		return err
	}
	return r.cb.WaitFlush(ctx)
}
