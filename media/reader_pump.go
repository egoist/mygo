package media

import (
	"context"
	"errors"
	"sync"
)

// The errors a reader pump reports about itself, rather than about media.
var (
	// errPumpClosed is a wait on a pump that was closed: the reader behind
	// it was let go, and what was in flight is not coming back.
	errPumpClosed = errors.New("media: the reader is closed")
	// errPumpRestarted is a wait on a pump that was reset under it: the
	// reader was flushed and moved, so the read it was waiting for no
	// longer counts and a new one has to be started.
	errPumpRestarted = errors.New("media: the reader was flushed and moved")
)

// A readerPump carries the samples of a reader that reports them instead of
// returning them, from the thread the platform calls back on to the goroutine
// that takes them.
//
// The platform may read one sample at a time: a read is started with read,
// and its result is handed to deliver by the platform's callback. A caller
// that gives up waiting leaves the read in flight, and its result is kept for
// the next caller, so a caller that runs out of context does not throw a
// sample away. reset brings the pump back to having read nothing — the
// reader was flushed — and close ends it. Both release a result nobody is
// waiting for, through release, exactly once: a sample is never lost and
// never given up twice.
//
// More than one caller may wait on a pump at once, and each has its own
// context: the player pulls pictures while another caller may pull one too,
// and a caller that gives up must not take the others' wait with it. A change
// every waiter must see — a result, a reset, a close — therefore closes and
// replaces the channel they all wait on, rather than handing a token to one of
// them.
//
// A pump starts no goroutine of its own: the platform's callback thread
// delivers, and the waiting goroutine waits.
type readerPump[T any] struct {
	mu       sync.Mutex    // the state below
	starting sync.Mutex    // held while a read is started, so reset and close cannot split it
	wake     chan struct{} // closed and replaced by every change a wait waits for
	waiters  int           // callers waiting for a result: every change wakes all of them
	closed   bool
	inflight bool // a read was started and has not reported
	ready    bool // a result arrived and nobody has taken it
	gen      uint64
	value    T
	release  func(T)
}

// newReaderPump is a pump whose dropped results go to release.
func newReaderPump[T any](release func(T)) *readerPump[T] {
	return &readerPump[T]{
		wake:    make(chan struct{}),
		release: release,
	}
}

// broadcast wakes every caller waiting on the pump. It is called with mu held,
// by each change a wait looks at, so that no waiter is left sleeping on a
// state that already moved.
func (p *readerPump[T]) broadcast() {
	close(p.wake)
	p.wake = make(chan struct{})
}

// read starts a read with start unless one is already in flight or its result
// is waiting to be taken, in which case it does nothing: the platform reads
// one sample at a time.
func (p *readerPump[T]) read(start func() error) error {
	p.starting.Lock()
	defer p.starting.Unlock()
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return errPumpClosed
	}
	if p.ready || p.inflight {
		p.mu.Unlock()
		return nil
	}
	p.inflight = true
	p.mu.Unlock()
	// A start that fails has read nothing: the next call starts again, and a
	// waiter already waiting for this read is woken to hear that nothing is
	// coming.
	if err := start(); err != nil {
		p.mu.Lock()
		p.inflight = false
		p.broadcast()
		p.mu.Unlock()
		return err
	}
	return nil
}

// wait is the result of the read read started, waiting for it until ctx is
// done. A read that is still in flight when ctx is done stays in flight: its
// result waits for the next wait, not another read.
func (p *readerPump[T]) wait(ctx context.Context) (T, error) {
	var zero T
	p.mu.Lock()
	gen := p.gen
	p.waiters++
	defer func() {
		p.mu.Lock()
		p.waiters--
		p.mu.Unlock()
	}()
	for {
		switch {
		case p.closed:
			p.mu.Unlock()
			return zero, errPumpClosed
		case p.gen != gen:
			p.mu.Unlock()
			return zero, errPumpRestarted
		case p.ready:
			v := p.value
			p.ready = false
			p.value = zero
			p.mu.Unlock()
			return v, nil
		case !p.inflight:
			// Nothing is coming: the read that was in flight was taken
			// back by a reset before the wait started.
			p.mu.Unlock()
			return zero, errPumpRestarted
		}
		wake := p.wake
		p.mu.Unlock()
		select {
		case <-wake:
		case <-ctx.Done():
			return zero, ctx.Err()
		}
		p.mu.Lock()
	}
}

// deliver hands a read's result over to wait. It is called by the platform's
// callback and must not wait for anything: it never blocks on the caller.
// A result nobody can take — the pump is closed, or no read is in flight
// because it was reset — is released here.
func (p *readerPump[T]) deliver(v T) {
	p.mu.Lock()
	if p.closed || !p.inflight {
		p.mu.Unlock()
		if p.release != nil {
			p.release(v)
		}
		return
	}
	p.inflight = false
	p.ready = true
	p.value = v
	p.broadcast()
	p.mu.Unlock()
}

// hasRead reports whether a read the platform has not answered is in flight,
// which is when a flush has something to take back: a reader with no request
// pending needs no flush, and waiting for one would wait on nothing.
func (p *readerPump[T]) hasRead() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.inflight
}

// reset forgets the read in flight and drops a result nobody has taken, which
// is what a flush needs: the reader was moved, and what it read before is not
// what the caller asked for. A wait in progress is told to start again.
//
// The caller flushes the reader before it starts a new read — the lock that
// guards the reader is held across both — so a result the platform reports for
// the read reset took back arrives with no read in flight and is released,
// never handed to a later request.
func (p *readerPump[T]) reset() {
	p.starting.Lock()
	p.mu.Lock()
	p.gen++
	p.inflight = false
	var dropped T
	drop := p.ready
	if drop {
		dropped = p.value
		var zero T
		p.value = zero
		p.ready = false
	}
	p.broadcast()
	p.mu.Unlock()
	p.starting.Unlock()
	if drop && p.release != nil {
		p.release(dropped)
	}
}

// close ends the pump: every wait in progress is told the reader is closed,
// and a result nobody has taken is released. A result the platform delivers
// after that is released as it arrives.
func (p *readerPump[T]) close() {
	p.starting.Lock()
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		p.starting.Unlock()
		return
	}
	p.closed = true
	p.inflight = false
	var dropped T
	drop := p.ready
	if drop {
		dropped = p.value
		var zero T
		p.value = zero
		p.ready = false
	}
	p.broadcast()
	p.mu.Unlock()
	p.starting.Unlock()
	if drop && p.release != nil {
		p.release(dropped)
	}
}
