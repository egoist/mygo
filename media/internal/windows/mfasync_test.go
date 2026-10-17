//go:build windows && (amd64 || arm64)

package windows

import (
	"context"
	"testing"
	"time"
	"unsafe"
)

// releaseTestCallback gives back the one reference a test owns, taking the
// object out of the registry. A callback method that Windows calls after that
// must be a no-op, which the release tests check.
func releaseTestCallback(c *AsyncCallback) {
	if lookupCallback(c.addr()) != nil {
		callbackRelease(c.addr())
	}
}

// TestAsyncCallbackVtableIsMadeOnce is the vtable every callback shares, made
// once for the process: syscall.NewCallback allocates a callback entry and
// Windows keeps the pointers for the process's life.
func TestAsyncCallbackVtableIsMadeOnce(t *testing.T) {
	a := NewAsyncCallback(nil)
	defer releaseTestCallback(a)
	b := NewAsyncCallback(nil)
	defer releaseTestCallback(b)
	if a.vtbl == 0 || a.vtbl != b.vtbl {
		t.Fatalf("callbacks have vtables %#x and %#x; want one", a.vtbl, b.vtbl)
	}
	if got := len(vtable); got != 6 {
		t.Fatalf("the vtable has %d methods; want QueryInterface, AddRef, Release, OnReadSample, OnFlush and OnEvent", got)
	}
}

// TestAsyncCallbackQueryInterface is the interface Windows asks for when it
// takes the callback from the reader's attributes, and the one it asks for in
// a failed query.
func TestAsyncCallbackQueryInterface(t *testing.T) {
	cb := NewAsyncCallback(nil)
	defer releaseTestCallback(cb)
	addr := cb.addr()
	asked := func(iid GUID) (hr, out uintptr) {
		t.Helper()
		var answer uintptr
		hr = callbackQueryInterface(addr, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&answer)))
		return hr, answer
	}

	if hr, out := asked(iidSourceReaderCallback); hr != sOK || out != addr {
		t.Fatalf("QueryInterface(IMFSourceReaderCallback) = %#x, %#x; want %#x, %#x", hr, out, sOK, addr)
	}
	defer callbackRelease(addr) // QueryInterface owns a reference.
	if got := cb.refs.Load(); got != 2 {
		t.Fatalf("the interface was not referenced: refs = %d; want 2", got)
	}
	if hr, out := asked(iidUnknown); hr != sOK || out != addr {
		t.Fatalf("QueryInterface(IUnknown) = %#x, %#x; want %#x, %#x", hr, out, sOK, addr)
	}
	defer callbackRelease(addr)
	if got := cb.refs.Load(); got != 3 {
		t.Fatalf("refs = %d after two interfaces; want 3", got)
	}

	// An interface the callback does not implement hands back nothing: the
	// caller is left without a pointer it can mistake for an answer.
	hr, out := asked(guid("11111111-2222-3333-4444-555555555555"))
	if hr != eNoInterface {
		t.Fatalf("QueryInterface(unknown) = %#x; want %#x", hr, eNoInterface)
	}
	if out != 0 {
		t.Fatalf("a failed QueryInterface left %#x in the answer; want 0", out)
	}
	if got := cb.refs.Load(); got != 3 {
		t.Fatalf("a failed QueryInterface changed the references: refs = %d; want 3", got)
	}
}

// TestAsyncCallbackReleaseRemovesRegistry is the lifecycle the reader relies
// on: the object answers Windows while the last reference is held, and a call
// that arrives after it was let go does nothing instead of reaching freed
// memory.
func TestAsyncCallbackReleaseRemovesRegistry(t *testing.T) {
	cb := NewAsyncCallback(nil)
	addr := cb.addr()
	if lookupCallback(addr) == nil {
		t.Fatal("a new callback is not in the registry")
	}
	if n := callbackAddRef(addr); n != 2 {
		t.Fatalf("adding the second reference returned %d; want 2", n)
	}
	if n := callbackRelease(addr); n != 1 {
		t.Fatalf("the first of two references released returned %d; want 1", n)
	}
	if lookupCallback(addr) == nil {
		t.Fatal("the callback left the registry with a reference left")
	}
	if n := callbackRelease(addr); n != 0 {
		t.Fatalf("the last release returned %d; want 0", n)
	}
	if lookupCallback(addr) != nil {
		t.Fatal("the callback is still in the registry after its last release")
	}
	if n := callbackRelease(addr); n != 0 {
		t.Fatalf("releasing a callback that is gone returned %d; want 0", n)
	}
	if hr := callbackOnReadSample(addr, 0, 0, 0, 0, 0); hr != sOK {
		t.Fatalf("OnReadSample on a released callback = %#x; want %#x", hr, sOK)
	}
}

// TestAsyncCallbackOnReadSample is what the callback makes of a read: every
// read is reported, including the ones with no sample, which say that the
// stream ended or that the read failed.
func TestAsyncCallbackOnReadSample(t *testing.T) {
	var got []ReadResult
	cb := NewAsyncCallback(func(r ReadResult) { got = append(got, r) })
	defer releaseTestCallback(cb)
	addr := cb.addr()

	if hr := callbackOnReadSample(addr, 0, 1, FlagEndOfStream, 42, 0); hr != sOK {
		t.Fatalf("OnReadSample = %#x; want %#x", hr, sOK)
	}
	if len(got) != 1 {
		t.Fatalf("the read was not reported: %d results", len(got))
	}
	if r := got[0]; r.HR != 0 || r.Stream != 1 || r.Flags != FlagEndOfStream || r.Time != 42*100*time.Nanosecond || !r.Sample.Empty() {
		t.Fatalf("the read was reported as %+v", r)
	}

	// A gap in a live stream has no sample either: the flags and the time
	// say what the gap was, and the reader reads again rather than taking
	// it for the end.
	callbackOnReadSample(addr, 0, 1, FlagStreamTick, 77, 0)
	if len(got) != 2 || got[1].HR != 0 || got[1].Stream != 1 || got[1].Flags != FlagStreamTick || got[1].Time != 77*100*time.Nanosecond {
		t.Fatalf("the gap was reported as %+v", got)
	}
	if !got[1].Sample.Empty() {
		t.Fatal("a gap hands over a sample")
	}

	// A failed read has no sample either, and must still reach the reader:
	// the read's failure is what ends the stream.
	const failed = 0x80004005
	callbackOnReadSample(addr, failed, 2, 0, 100, 0)
	if len(got) != 3 || got[2].HR != failed || got[2].Stream != 2 || got[2].Time != 100*100*time.Nanosecond {
		t.Fatalf("the failed read was reported as %+v", got)
	}
	if !got[2].Sample.Empty() {
		t.Fatal("a read with no sample hands over one")
	}
}

// TestAsyncCallbackOnFlush is the confirmation a flush waits for, and the
// forgetting of one that arrived while nobody waited.
func TestAsyncCallbackOnFlush(t *testing.T) {
	cb := NewAsyncCallback(nil)
	defer releaseTestCallback(cb)
	addr := cb.addr()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := cb.WaitFlush(ctx); err != context.Canceled {
		t.Fatalf("WaitFlush with a canceled context = %v; want %v", err, context.Canceled)
	}
	if hr := callbackOnFlush(addr, 3); hr != sOK {
		t.Fatalf("OnFlush = %#x; want %#x", hr, sOK)
	}
	if err := cb.WaitFlush(context.Background()); err != nil {
		t.Fatalf("WaitFlush = %v; want nil", err)
	}
	// The confirmation is used once: the next flush waits for its own.
	if err := cb.WaitFlush(ctx); err != context.Canceled {
		t.Fatalf("a second WaitFlush = %v; want %v", err, context.Canceled)
	}
	// One that arrives while nobody waits is forgotten, not taken for the
	// next flush's.
	callbackOnFlush(addr, 3)
	cb.dropFlush()
	if err := cb.WaitFlush(ctx); err != context.Canceled {
		t.Fatalf("WaitFlush after dropFlush = %v; want %v", err, context.Canceled)
	}
}

// TestReadSampleAsyncRefusesSynchronousReader is the guard that keeps a
// synchronous reader from being read as if it reported reads: that call would
// wait in the platform, which is the wait asynchronous reading is for.
func TestReadSampleAsyncRefusesSynchronousReader(t *testing.T) {
	r := SourceReader{}
	if err := r.ReadSampleAsync(0); err != ErrNotAsync {
		t.Fatalf("ReadSampleAsync on a synchronous reader = %v; want %v", err, ErrNotAsync)
	}
	if err := r.FlushAsync(context.Background(), 0); err != ErrNotAsync {
		t.Fatalf("FlushAsync on a synchronous reader = %v; want %v", err, ErrNotAsync)
	}
}
