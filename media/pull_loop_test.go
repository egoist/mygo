package media

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/egoist/mygo/internal/wait"
)

// The pull loop is what the platform sources that poll a sink for their
// samples share: a source's lock, one bounded native attempt at a time, and
// the read lock let go of between attempts, so that a close or a seek that
// waits for the write lock is let in by a stream that has nothing to hand
// out. The tests here are the source's real protocol, with a fake attempt
// standing in for the platform's sink, because a close of a GStreamer sink
// and of an AVFoundation one wait the same way.

// TestPullSampleLetsACloseInBetweenAttempts is the reason the loop lets the
// read lock go: the source's close takes the write lock, and it must be let
// in while the stream keeps coming back with nothing, rather than after the
// stream has handed out a frame or ended.
func TestPullSampleLetsACloseInBetweenAttempts(t *testing.T) {
	var mu sync.RWMutex
	var closed bool
	attempted := make(chan struct{}, 1)
	stopped := make(chan error, 1)
	go func() {
		_, err := pullSample(&mu,
			func() error {
				if closed {
					return ErrEnd
				}
				return nil
			},
			func() (int, bool, error) {
				select {
				case attempted <- struct{}{}:
				default:
				}
				return 0, false, nil
			},
			func() error { return nil })
		stopped <- err
	}()
	wait.For(t, "a pull attempt", attempted)

	closedIn := make(chan struct{})
	go func() {
		mu.Lock()
		closed = true
		mu.Unlock()
		close(closedIn)
	}()
	wait.For(t, "the close to take the source's lock between attempts", closedIn)

	if err := wait.For(t, "the pull to stop", stopped); !errors.Is(err, ErrEnd) {
		t.Fatalf("the pull returned %v after the source closed, want %v", err, ErrEnd)
	}
}

// TestPullSampleNoticesACancelledContext checks the other interruption: a
// caller that gives up needs the pull to see it on the next attempt, not
// after the stream has produced a frame.
func TestPullSampleNoticesACancelledContext(t *testing.T) {
	var mu sync.RWMutex
	ctx, cancel := context.WithCancel(t.Context())
	attempted := make(chan struct{}, 1)
	stopped := make(chan error, 1)
	go func() {
		_, err := pullSample(&mu,
			func() error { return ctx.Err() },
			func() (int, bool, error) {
				select {
				case attempted <- struct{}{}:
				default:
				}
				return 0, false, nil
			},
			func() error { return nil })
		stopped <- err
	}()
	wait.For(t, "a pull attempt", attempted)
	cancel()

	if err := wait.For(t, "the pull to stop", stopped); !errors.Is(err, context.Canceled) {
		t.Fatalf("the pull returned %v after the context was cancelled, want %v", err, context.Canceled)
	}
}

// TestPullSampleHandsBackWhatAnAttemptHas checks that attempts that come back
// with nothing are retried, and what one that has something hands back.
func TestPullSampleHandsBackWhatAnAttemptHas(t *testing.T) {
	var mu sync.RWMutex
	attempts := 0
	got, err := pullSample(&mu,
		func() error { return nil },
		func() (string, bool, error) {
			attempts++
			if attempts < 3 {
				return "", false, nil
			}
			return "a frame", true, nil
		},
		func() error { return nil })
	if err != nil {
		t.Fatalf("the pull returned %v, want a frame", err)
	}
	if got != "a frame" {
		t.Fatalf("the pull handed back %q, want a frame", got)
	}
	if attempts != 3 {
		t.Fatalf("the pull made %d attempts, want 3", attempts)
	}
}

// TestPullSampleStopsWhenTheSinkEnds checks that what an attempt with nothing
// means is looked at under the same lock, and stops the pull.
func TestPullSampleStopsWhenTheSinkEnds(t *testing.T) {
	var mu sync.RWMutex
	attempts, idles := 0, 0
	_, err := pullSample(&mu,
		func() error { return nil },
		func() (int, bool, error) {
			attempts++
			return 0, false, nil
		},
		func() error {
			idles++
			return ErrEnd
		})
	if !errors.Is(err, ErrEnd) {
		t.Fatalf("the pull returned %v, want %v", err, ErrEnd)
	}
	if attempts != 1 || idles != 1 {
		t.Fatalf("the pull made %d attempts and %d idle checks, want 1 and 1", attempts, idles)
	}
}

// TestPullSampleStopsOnAnAttemptError checks that an attempt that fails
// stops the pull without being taken for an empty one.
func TestPullSampleStopsOnAnAttemptError(t *testing.T) {
	var mu sync.RWMutex
	bad := errors.New("the sink broke")
	idles := 0
	_, err := pullSample(&mu,
		func() error { return nil },
		func() (int, bool, error) { return 0, false, bad },
		func() error {
			idles++
			return nil
		})
	if !errors.Is(err, bad) {
		t.Fatalf("the pull returned %v, want %v", err, bad)
	}
	if idles != 0 {
		t.Fatalf("the pull checked the idle state %d times after an error, want 0", idles)
	}
}
