package media

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo/internal/wait"
)

// pumpResult is the sample a pump test hands around, standing in for a
// platform sample: it carries a number so a test can tell which read it is
// and is released by the pump when nobody takes it.
type pumpResult struct{ n int }

// pumpCounter counts what a pump released.
type pumpCounter struct{ n atomic.Int32 }

// newTestPump is a pump of pumpResult that counts the results it releases.
func newTestPump() (*readerPump[pumpResult], *pumpCounter) {
	c := &pumpCounter{}
	return newReaderPump(func(r pumpResult) { c.n.Add(1) }), c
}

// noRead is an issue function that says a read was started.
func noRead() error { return nil }

// readOnce reads a value and fails the test when the pump does not hand one
// over.
func readOnce(t *testing.T, p *readerPump[pumpResult], ctx context.Context) pumpResult {
	t.Helper()
	if err := p.read(noRead); err != nil {
		t.Fatalf("read: %v", err)
	}
	v, err := p.wait(ctx)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	return v
}

func TestReaderPumpDeliversResult(t *testing.T) {
	p, _ := newTestPump()
	if err := p.read(noRead); err != nil {
		t.Fatalf("read: %v", err)
	}
	p.deliver(pumpResult{n: 7})
	if v, err := p.wait(context.Background()); err != nil || v.n != 7 {
		t.Fatalf("wait = %v, %v; want 7, nil", v, err)
	}
}

func TestReaderPumpWaitCancelKeepsResult(t *testing.T) {
	p, _ := newTestPump()
	if err := p.read(noRead); err != nil {
		t.Fatalf("read: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.wait(ctx); err == nil {
		t.Fatal("wait with a canceled context returned a result")
	}
	// The read was not taken back: its sample waits for the next caller
	// instead of being read again or dropped.
	p.deliver(pumpResult{n: 3})
	v, err := p.wait(context.Background())
	if err != nil || v.n != 3 {
		t.Fatalf("second wait = %v, %v; want 3, nil", v, err)
	}
}

func TestReaderPumpResetDropsUnreadResult(t *testing.T) {
	p, released := newTestPump()
	if err := p.read(noRead); err != nil {
		t.Fatalf("read: %v", err)
	}
	p.deliver(pumpResult{n: 1})
	p.reset()
	if got := released.n.Load(); got != 1 {
		t.Fatalf("released %d results on reset; want 1", got)
	}
	// The next read is a new one: neither the dropped result nor the old
	// request counts.
	if err := p.read(noRead); err != nil {
		t.Fatalf("read after reset: %v", err)
	}
	p.deliver(pumpResult{n: 2})
	if v, err := p.wait(context.Background()); err != nil || v.n != 2 {
		t.Fatalf("wait after reset = %v, %v; want 2, nil", v, err)
	}
	if got := released.n.Load(); got != 1 {
		t.Fatalf("released %d results; want 1", got)
	}
}

func TestReaderPumpResetStopsWaiter(t *testing.T) {
	p, _ := newTestPump()
	if err := p.read(noRead); err != nil {
		t.Fatalf("read: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := p.wait(context.Background())
		done <- err
	}()
	p.reset()
	if err := <-done; err != errPumpRestarted {
		t.Fatalf("wait after reset = %v; want %v", err, errPumpRestarted)
	}
}

func TestReaderPumpCloseReleasesUnreadResult(t *testing.T) {
	p, released := newTestPump()
	if err := p.read(noRead); err != nil {
		t.Fatalf("read: %v", err)
	}
	p.deliver(pumpResult{n: 9})
	p.close()
	if got := released.n.Load(); got != 1 {
		t.Fatalf("released %d results on close; want 1", got)
	}
	if _, err := p.wait(context.Background()); err != errPumpClosed {
		t.Fatalf("wait after close = %v; want %v", err, errPumpClosed)
	}
}

func TestReaderPumpCloseStopsWaiter(t *testing.T) {
	p, _ := newTestPump()
	if err := p.read(noRead); err != nil {
		t.Fatalf("read: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := p.wait(context.Background())
		done <- err
	}()
	p.close()
	if err := <-done; err != errPumpClosed {
		t.Fatalf("wait after close = %v; want %v", err, errPumpClosed)
	}
}

func TestReaderPumpDeliverAfterCloseReleases(t *testing.T) {
	p, released := newTestPump()
	p.close()
	p.deliver(pumpResult{n: 1})
	if got := released.n.Load(); got != 1 {
		t.Fatalf("released %d results; want 1", got)
	}
	p.close()
	p.deliver(pumpResult{n: 2})
	if got := released.n.Load(); got != 2 {
		t.Fatalf("released %d results; want 2", got)
	}
	if _, err := p.wait(context.Background()); err != errPumpClosed {
		t.Fatalf("wait after close = %v; want %v", err, errPumpClosed)
	}
}

func TestReaderPumpReadOnceAtATime(t *testing.T) {
	p, _ := newTestPump()
	var started int32
	issue := func() error {
		started++
		return nil
	}
	if err := p.read(issue); err != nil {
		t.Fatalf("first read: %v", err)
	}
	if err := p.read(issue); err != nil {
		t.Fatalf("second read: %v", err)
	}
	if started != 1 {
		t.Fatalf("started %d reads while one was in flight; want 1", started)
	}
	p.deliver(pumpResult{n: 1})
	// A result waiting to be taken is not read for again either.
	if err := p.read(issue); err != nil {
		t.Fatalf("read with a result waiting: %v", err)
	}
	if started != 1 {
		t.Fatalf("started %d reads with a result waiting; want 1", started)
	}
	if v, err := p.wait(context.Background()); err != nil || v.n != 1 {
		t.Fatalf("wait = %v, %v; want 1, nil", v, err)
	}
	// Once it is taken, the next read starts again.
	if err := p.read(issue); err != nil {
		t.Fatalf("read after taking: %v", err)
	}
	if started != 2 {
		t.Fatalf("started %d reads after taking the result; want 2", started)
	}
}

func TestReaderPumpFailedReadCanBeRetried(t *testing.T) {
	p, _ := newTestPump()
	fail := func() error { return context.DeadlineExceeded }
	if err := p.read(fail); err != context.DeadlineExceeded {
		t.Fatalf("read = %v; want the start's error", err)
	}
	if err := p.read(noRead); err != nil {
		t.Fatalf("read after a failed start: %v", err)
	}
	p.deliver(pumpResult{n: 4})
	if v, err := p.wait(context.Background()); err != nil || v.n != 4 {
		t.Fatalf("wait = %v, %v; want 4, nil", v, err)
	}
}

// TestReaderPumpConcurrentDeliverAndReset is the race a seek makes: its reset
// and the sample the reader was already finishing arrive together, and
// whichever comes first the sample is handed over once — taken by the waiter
// or released by the pump — and the waiting caller is told to read again.
func TestReaderPumpConcurrentDeliverAndReset(t *testing.T) {
	for range 100 {
		p, released := newTestPump()
		if err := p.read(noRead); err != nil {
			t.Fatalf("read: %v", err)
		}
		var taken atomic.Int32
		done := make(chan error, 1)
		go func() {
			for {
				_, err := p.wait(context.Background())
				if err != nil {
					done <- err
					return
				}
				taken.Add(1)
			}
		}()
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.deliver(pumpResult{n: 1})
		}()
		p.reset()
		wg.Wait()
		if err := <-done; err != errPumpRestarted {
			t.Fatalf("wait = %v; want %v", err, errPumpRestarted)
		}
		if got := released.n.Load() + taken.Load(); got != 1 {
			t.Fatalf("handed over %d results; want 1", got)
		}
	}
}

// TestReaderPumpConcurrentDeliverAndClose is the same race on close.
func TestReaderPumpConcurrentDeliverAndClose(t *testing.T) {
	for range 100 {
		closed := make(chan struct{})
		p, released := newTestPump()
		if err := p.read(noRead); err != nil {
			t.Fatalf("read: %v", err)
		}
		var taken atomic.Int32
		done := make(chan error, 1)
		go func() {
			for {
				_, err := p.wait(context.Background())
				if err != nil {
					done <- err
					return
				}
				taken.Add(1)
				// The single read was consumed. Do not wait for another
				// nonexistent read before close has actually happened.
				<-closed
			}
		}()
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.deliver(pumpResult{n: 1})
		}()
		p.close()
		close(closed)
		wg.Wait()
		if err := <-done; err != errPumpClosed {
			t.Fatalf("wait = %v; want %v", err, errPumpClosed)
		}
		if got := released.n.Load() + taken.Load(); got != 1 {
			t.Fatalf("handed over %d results; want 1", got)
		}
	}
}

// pumpWaiting reports how many callers are waiting on p, so a test knows that
// every waiter it means to wake is in place before it wakes them.
func pumpWaiting(p *readerPump[pumpResult]) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.waiters
}

// pumpWait starts a wait in the background and returns the channel it reports
// its error on.
func pumpWait(p *readerPump[pumpResult], ctx context.Context) <-chan error {
	done := make(chan error, 1)
	go func() {
		_, err := p.wait(ctx)
		done <- err
	}()
	return done
}

// pumpWaitFor waits until n callers are waiting on p.
func pumpWaitFor(t *testing.T, p *readerPump[pumpResult], n int) {
	t.Helper()
	wait.Until(t, "every waiter to wait", func() bool { return pumpWaiting(p) == n })
}

// TestReaderPumpDeliversToEveryWaiter is two callers pulling pictures while one
// read reports: the one that takes the result gets it, and the other is told
// there is nothing to take rather than left waiting for a result that has
// already come and gone.
func TestReaderPumpDeliversToEveryWaiter(t *testing.T) {
	p, released := newTestPump()
	if err := p.read(noRead); err != nil {
		t.Fatalf("read: %v", err)
	}
	var done []<-chan error
	for range 3 {
		done = append(done, pumpWait(p, context.Background()))
	}
	pumpWaitFor(t, p, 3)
	p.deliver(pumpResult{n: 5})
	var taken int
	for _, errc := range done {
		switch err := wait.For(t, "a waiter to return", errc); err {
		case nil:
			taken++
		case errPumpRestarted:
		default:
			t.Fatalf("waiter = %v; want a result or %v", err, errPumpRestarted)
		}
	}
	if taken != 1 {
		t.Fatalf("%d waiters took the result; want 1", taken)
	}
	if got := released.n.Load(); got != 0 {
		t.Fatalf("released %d results; want 0", got)
	}
}

// TestReaderPumpResetWakesEveryWaiter is a seek while several callers wait: all
// of them hear that the reader moved.
func TestReaderPumpResetWakesEveryWaiter(t *testing.T) {
	p, _ := newTestPump()
	if err := p.read(noRead); err != nil {
		t.Fatalf("read: %v", err)
	}
	var done []<-chan error
	for range 2 {
		done = append(done, pumpWait(p, context.Background()))
	}
	pumpWaitFor(t, p, 2)
	p.reset()
	for _, errc := range done {
		if err := wait.For(t, "a waiter to return", errc); err != errPumpRestarted {
			t.Fatalf("waiter = %v; want %v", err, errPumpRestarted)
		}
	}
}

// TestReaderPumpCloseWakesEveryWaiter is closing the player while several
// callers wait: all of them hear that the reader was let go.
func TestReaderPumpCloseWakesEveryWaiter(t *testing.T) {
	p, _ := newTestPump()
	if err := p.read(noRead); err != nil {
		t.Fatalf("read: %v", err)
	}
	var done []<-chan error
	for range 2 {
		done = append(done, pumpWait(p, context.Background()))
	}
	pumpWaitFor(t, p, 2)
	p.close()
	for _, errc := range done {
		if err := wait.For(t, "a waiter to return", errc); err != errPumpClosed {
			t.Fatalf("waiter = %v; want %v", err, errPumpClosed)
		}
	}
}

// TestReaderPumpWaitersHaveTheirOwnContext is one caller giving up while
// another waits on the same read: the second one's wait and the read itself
// are untouched.
func TestReaderPumpWaitersHaveTheirOwnContext(t *testing.T) {
	p, _ := newTestPump()
	if err := p.read(noRead); err != nil {
		t.Fatalf("read: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	given := pumpWait(p, ctx)
	staying := pumpWait(p, context.Background())
	if err := wait.For(t, "the canceled waiter to return", given); err != context.Canceled {
		t.Fatalf("canceled waiter = %v; want %v", err, context.Canceled)
	}
	pumpWaitFor(t, p, 1)
	p.deliver(pumpResult{n: 8})
	if err := wait.For(t, "the staying waiter to return", staying); err != nil {
		t.Fatalf("staying waiter = %v; want a result", err)
	}
}

// TestReaderPumpFailedReadWakesWaiter is a read the platform refuses to start
// while a caller waits: the caller hears that nothing is coming instead of
// waiting for a sample that was never asked for.
func TestReaderPumpFailedReadWakesWaiter(t *testing.T) {
	p, _ := newTestPump()
	refuse := make(chan struct{})
	started := make(chan error, 1)
	go func() {
		started <- p.read(func() error {
			<-refuse
			return context.DeadlineExceeded
		})
	}()
	wait.Until(t, "the read to be in flight", p.hasRead)
	waiter := pumpWait(p, context.Background())
	pumpWaitFor(t, p, 1)
	close(refuse)
	if err := wait.For(t, "the read to fail", started); err != context.DeadlineExceeded {
		t.Fatalf("read = %v; want the start's error", err)
	}
	if err := wait.For(t, "the waiter to return", waiter); err != errPumpRestarted {
		t.Fatalf("waiter = %v; want %v", err, errPumpRestarted)
	}
}

func TestReaderPumpHasRead(t *testing.T) {
	p, _ := newTestPump()
	if p.hasRead() {
		t.Fatal("a pump with no read reports one")
	}
	if err := p.read(noRead); err != nil {
		t.Fatalf("read: %v", err)
	}
	if !p.hasRead() {
		t.Fatal("a started read is not reported")
	}
	p.deliver(pumpResult{n: 1})
	if p.hasRead() {
		t.Fatal("a delivered read is still reported")
	}
	if _, err := p.wait(context.Background()); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if err := p.read(noRead); err != nil {
		t.Fatalf("read: %v", err)
	}
	p.reset()
	if p.hasRead() {
		t.Fatal("a reset pump still reports a read")
	}
	p.close()
	if p.hasRead() {
		t.Fatal("a closed pump reports a read")
	}
}
