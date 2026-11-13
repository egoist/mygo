//go:build !js

package mygo

import (
	"context"
	"errors"
	"testing"

	"github.com/egoist/mygo/internal/wait"
)

// cancelService is a service whose method waits for its context to be
// canceled.
type cancelService struct {
	started chan struct{}
	done    chan error
}

// Wait waits until the call is canceled.
func (s *cancelService) Wait(ctx context.Context) error {
	close(s.started)
	<-ctx.Done()
	s.done <- ctx.Err()
	return ctx.Err()
}

// A page may cancel a call: the method's context is canceled.
func TestCancelCall(t *testing.T) {
	svc := &cancelService{started: make(chan struct{}), done: make(chan error, 1)}
	Bind(svc)
	w := &Window{}
	raw := `{"id":1,"k":"tok","m":"cancelService.Wait","a":[],"cancel":true}`
	go handleCall(w, context.Background(), raw, true)
	wait.For(t, "the call to start", svc.started)
	w.cancelCall(1, "tok")
	if err := wait.For(t, "the call to be canceled", svc.done); !errors.Is(err, context.Canceled) {
		t.Errorf("the call ended with %v, want it canceled", err)
	}
}

// Only the page that made a call may cancel it.
func TestCancelCallOfAnotherPage(t *testing.T) {
	w := &Window{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.addCancel(1, "tok", cancel)
	// Another page's cancel does not cancel the call, and neither does
	// the cancel of another call.
	w.cancelCall(1, "other")
	w.cancelCall(2, "tok")
	select {
	case <-ctx.Done():
		t.Fatal("the call was canceled")
	default:
	}
	w.cancelCall(1, "tok")
	select {
	case <-ctx.Done():
	default:
		t.Fatal("the call was not canceled")
	}
	// A call that is over is not canceled again.
	w.addCancel(3, "tok", cancel)
	w.removeCancel(3, "tok")
	w.cancelCall(3, "tok")
}
