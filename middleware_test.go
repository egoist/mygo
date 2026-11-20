//go:build !js

package mygo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

type mwService struct {
	mu    sync.Mutex
	calls []string
}

func (s *mwService) record(what string) {
	s.mu.Lock()
	s.calls = append(s.calls, what)
	s.mu.Unlock()
}

func (s *mwService) Echo(ctx context.Context, msg string, n int) (string, error) {
	s.record("method")
	return fmt.Sprintf("%s:%d", msg, n), nil
}

func (s *mwService) ContextValue(ctx context.Context) (string, error) {
	s.record("method")
	v, _ := ctx.Value(mwKey{}).(string)
	return v, nil
}

func (s *mwService) Fail() error {
	s.record("method")
	return errors.New("method failed")
}

type mwKey struct{}

// resetMiddleware clears the chain after a test, which shares the process
// with the other tests' calls.
func resetMiddleware(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		middlewares.Lock()
		middlewares.chain = nil
		middlewares.Unlock()
	})
}

// mwReply runs a call and decodes the reply it queued.
func mwReply(t *testing.T, w *Window, method, args string) map[string]any {
	t.Helper()
	raw := fmt.Sprintf(`{"id":1,"k":"tok","m":%q,"a":%s}`, method, args)
	handleCall(w, context.Background(), raw, true)
	w.outMu.Lock()
	defer w.outMu.Unlock()
	var b []byte
	for _, m := range w.outbox {
		b = m.appendTo(b)
	}
	var reply map[string]any
	if err := json.Unmarshal(b, &reply); err != nil {
		t.Fatalf("cannot decode reply %q: %v", b, err)
	}
	return reply
}

func TestMiddlewareOrderAndArgs(t *testing.T) {
	resetMiddleware(t)
	svc := &mwService{}
	BindAs("mwOrder", svc)
	var order []string
	var gotCall Call
	Intercept(func(ctx context.Context, call Call, next func(context.Context) (any, error)) (any, error) {
		order = append(order, "a-before")
		gotCall = call
		v, err := next(ctx)
		order = append(order, "a-after")
		return v, err
	}, func(ctx context.Context, call Call, next func(context.Context) (any, error)) (any, error) {
		order = append(order, "b-before")
		v, err := next(ctx)
		order = append(order, "b-after")
		return v, err
	})
	w := &Window{}
	reply := mwReply(t, w, "mwOrder.Echo", `["hi",3]`)
	if reply["ok"] != true || reply["v"] != "hi:3" {
		t.Errorf("reply = %v, want ok with hi:3", reply)
	}
	want := []string{"a-before", "b-before", "b-after", "a-after"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Errorf("order = %v, want %v", order, want)
	}
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if strings.Join(svc.calls, ",") != "method" {
		t.Errorf("the method ran %v, want once", svc.calls)
	}
	if gotCall.Method != "mwOrder.Echo" || len(gotCall.Args) != 2 || gotCall.Args[0] != "hi" || gotCall.Args[1] != 3 {
		t.Errorf("Call = %+v, want mwOrder.Echo with [hi 3]", gotCall)
	}
}

func TestMiddlewareShortCircuit(t *testing.T) {
	resetMiddleware(t)
	svc := &mwService{}
	BindAs("mwShort", svc)
	Intercept(func(ctx context.Context, call Call, next func(context.Context) (any, error)) (any, error) {
		return nil, errors.New("denied")
	})
	w := &Window{}
	reply := mwReply(t, w, "mwShort.Echo", `["hi",3]`)
	if reply["ok"] != false || reply["e"] != "denied" {
		t.Errorf("reply = %v, want denied", reply)
	}
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if len(svc.calls) != 0 {
		t.Errorf("the method ran despite the short circuit: %v", svc.calls)
	}
}

func TestMiddlewareWrapsResultAndError(t *testing.T) {
	resetMiddleware(t)
	svc := &mwService{}
	BindAs("mwWrap", svc)
	Intercept(func(ctx context.Context, call Call, next func(context.Context) (any, error)) (any, error) {
		v, err := next(ctx)
		if err != nil {
			return nil, fmt.Errorf("wrapped: %w", err)
		}
		return strings.ToUpper(v.(string)), nil
	})
	w := &Window{}
	reply := mwReply(t, w, "mwWrap.Echo", `["hi",3]`)
	if reply["ok"] != true || reply["v"] != "HI:3" {
		t.Errorf("reply = %v, want HI:3", reply)
	}
	w2 := &Window{}
	reply = mwReply(t, w2, "mwWrap.Fail", `[]`)
	if reply["ok"] != false || reply["e"] != "wrapped: method failed" {
		t.Errorf("reply = %v, want the wrapped error", reply)
	}
}

func TestMiddlewareDerivesContext(t *testing.T) {
	resetMiddleware(t)
	svc := &mwService{}
	BindAs("mwCtx", svc)
	Intercept(func(ctx context.Context, call Call, next func(context.Context) (any, error)) (any, error) {
		return next(context.WithValue(ctx, mwKey{}, "from middleware"))
	})
	w := &Window{}
	reply := mwReply(t, w, "mwCtx.ContextValue", `[]`)
	if reply["ok"] != true || reply["v"] != "from middleware" {
		t.Errorf("reply = %v, want the middleware's context value", reply)
	}
}

func TestMiddlewarePanicBecomesError(t *testing.T) {
	resetMiddleware(t)
	svc := &mwService{}
	BindAs("mwPanic", svc)
	Intercept(func(ctx context.Context, call Call, next func(context.Context) (any, error)) (any, error) {
		panic("middleware exploded")
	})
	w := &Window{}
	reply := mwReply(t, w, "mwPanic.Echo", `["hi",3]`)
	if reply["ok"] != false || !strings.Contains(reply["e"].(string), "middleware exploded") {
		t.Errorf("reply = %v, want the panic as the error", reply)
	}
}

func TestMiddlewareSkipsUntrustedCalls(t *testing.T) {
	resetMiddleware(t)
	svc := &mwService{}
	BindAs("mwUntrusted", svc)
	ran := false
	Intercept(func(ctx context.Context, call Call, next func(context.Context) (any, error)) (any, error) {
		ran = true
		return next(ctx)
	})
	w := &Window{}
	raw := `{"id":1,"k":"tok","m":"mwUntrusted.Echo","a":["hi",3]}`
	handleCall(w, context.Background(), raw, false)
	if ran {
		t.Error("middleware ran for an untrusted call")
	}
	w.outMu.Lock()
	defer w.outMu.Unlock()
	var b []byte
	for _, m := range w.outbox {
		b = m.appendTo(b)
	}
	if !strings.Contains(string(b), "not allowed to call Go methods") {
		t.Errorf("reply = %q, want the trust rejection", b)
	}
}

func TestMiddlewareCallerWindow(t *testing.T) {
	resetMiddleware(t)
	svc := &mwService{}
	BindAs("mwWindow", svc)
	var seen *Window
	Intercept(func(ctx context.Context, call Call, next func(context.Context) (any, error)) (any, error) {
		seen = CallerWindow(ctx)
		return next(ctx)
	})
	w := &Window{}
	ctx := context.WithValue(context.Background(), callerKey{}, w)
	handleCall(w, ctx, `{"id":1,"k":"tok","m":"mwWindow.Echo","a":["hi",3]}`, true)
	if seen != w {
		t.Errorf("CallerWindow in middleware = %v, want the calling window %v", seen, w)
	}
}

func TestInterceptConcurrentWithCalls(t *testing.T) {
	resetMiddleware(t)
	svc := &mwService{}
	BindAs("mwConcurrent", svc)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				w := &Window{}
				handleCall(w, context.Background(), `{"id":1,"k":"tok","m":"mwConcurrent.Echo","a":["hi",3]}`, true)
			}
		}()
	}
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				Intercept(func(ctx context.Context, call Call, next func(context.Context) (any, error)) (any, error) {
					return next(ctx)
				})
			}
		}()
	}
	wg.Wait()
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if len(svc.calls) != 160 {
		t.Errorf("the method ran %d times, want 160", len(svc.calls))
	}
}