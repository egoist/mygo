package media

// A readLocker is the read side of a source's lock: a pull holds it while it
// reads the source's sink, and the source's writers (a close, a seek) wait
// for one attempt and no more.
type readLocker interface {
	RLock()
	RUnlock()
}

// pullSample pulls from a platform source one attempt at a time.
//
// The read lock is held for one attempt and let go before the next: a writer
// that is waiting for it is let in between two attempts, rather than after a
// stream that has nothing to hand out has produced a frame or ended. That is
// what makes a close, a seek or a choice of tracks able to interrupt a pull.
//
// state runs under the lock and reports whether there is still a sink to pull
// from, returning non-nil to stop. attempt makes one bounded native pull
// under the same lock: it returns ok with what it has to hand back, err to
// stop, or neither to try again. idle, still under the lock, says what an
// attempt that came back with nothing means and returns non-nil to stop.
func pullSample[T any](mu readLocker, state func() error, attempt func() (T, bool, error), idle func() error) (T, error) {
	var zero T
	for {
		mu.RLock()
		if err := state(); err != nil {
			mu.RUnlock()
			return zero, err
		}
		v, ok, err := attempt()
		if ok {
			mu.RUnlock()
			return v, nil
		}
		if err == nil {
			err = idle()
		}
		mu.RUnlock()
		if err != nil {
			return zero, err
		}
	}
}
