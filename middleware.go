//go:build !js

package mygo

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
)

// Call describes a call to a bound method, for Middleware.
type Call struct {
	// Method is the call's "Service.Method" name; a plugin's is
	// "plugin:<name>.<Method>".
	Method string
	// Args are the call's decoded arguments, without the leading
	// context.Context a method may take: the values the method receives,
	// to read, not to modify. A channel parameter (*Channel[T]) appears
	// as itself.
	Args []any
}

// Middleware wraps calls to bound methods, as net/http's middleware wraps
// requests and gRPC's interceptors wrap RPCs: authentication, logging,
// rate limiting, metrics. It runs on the call's own goroutine, after the
// page passed the trust checks and the arguments decoded, and before the
// method runs.
//
// ctx is the call's context, which CallerWindow resolves to the calling
// window. next runs the method, or the rest of the chain, with the context
// it is given, so a middleware may derive one with a deadline or a value;
// a nil context runs it with the call's own. A step's next runs at most
// once: a second call returns an error and runs no method again. Returning
// without calling next short-circuits the call: the method never runs, and
// the error rejects the page's promise. A middleware may wrap or replace
// the result, which must still encode as the method's result type. A panic
// in a middleware becomes the call's error, as one in the method does.
//
// Middleware wraps plugins' calls too; filter on Call.Method to leave them
// alone.
type Middleware func(ctx context.Context, call Call, next func(context.Context) (any, error)) (any, error)

var middlewares struct {
	sync.RWMutex
	chain []Middleware
}

// chainRunner runs a call's middleware chain. Apps that never call
// Intercept link none of it: method.call reaches it through runners, which
// only Intercept sets.
type chainRunner struct {
	run func(w *Window, m *method, page context.Context, token string, args []rawValue) (any, error)
}

var runners atomic.Pointer[chainRunner]

// Intercept registers middleware around every call to a bound method,
// outermost first: Intercept(a, b) runs a around b around the method.
// Later calls append to the chain. It is safe from any goroutine;
// register before App.Run to wrap every call. In browsers it does
// nothing, as Bind does.
func Intercept(ms ...Middleware) {
	middlewares.Lock()
	middlewares.chain = append(slices.Clone(middlewares.chain), ms...)
	middlewares.Unlock()
	if runners.Load() == nil {
		runners.CompareAndSwap(nil, &chainRunner{run: runMiddleware})
	}
}

// runMiddleware decodes the call, then runs the chain around the method:
// the same decoding method.call does, which the no-middleware path keeps to
// itself so it allocates nothing more than before Intercept existed.
// Each step's next runs the rest of the chain at most once, and a nil
// context stands for the call's own.
func runMiddleware(w *Window, m *method, page context.Context, token string, args []rawValue) (any, error) {
	ctx, cancel := page, context.CancelFunc(nil)
	var chans []*channel
	if m.streams {
		ctx, cancel = context.WithCancel(page)
	}
	defer func() {
		for _, c := range chans {
			c.close(closedByGo)
		}
		if cancel != nil {
			cancel()
		}
	}()
	in := make([]reflect.Value, 0, len(m.params)+1)
	if m.ctx {
		in = append(in, reflect.ValueOf(ctx))
	}
	last := len(m.params) - 1
	for i, t := range m.params {
		if m.chans[i] {
			var id int64
			if i < len(args) {
				_ = json.Unmarshal(args[i], &id)
			}
			if id <= 0 {
				return nil, fmt.Errorf("invalid argument %d: not a Channel", i+1)
			}
			c := w.newChannel(page, ctx, cancel, id, token)
			chans = append(chans, c)
			v := reflect.New(t.Elem())
			v.Interface().(channelParam).init(c)
			in = append(in, v)
			continue
		}
		if m.variadic && i == last {
			for j := i; j < len(args); j++ {
				v, err := decodeArg(args[j], t.Elem(), j)
				if err != nil {
					return nil, err
				}
				in = append(in, v)
			}
			break
		}
		if i >= len(args) {
			in = append(in, reflect.Zero(t))
			continue
		}
		v, err := decodeArg(args[i], t, i)
		if err != nil {
			return nil, err
		}
		in = append(in, v)
	}
	invoke := func(nextCtx context.Context) (any, error) { return m.invoke(nextCtx, in) }
	middlewares.RLock()
	chain := middlewares.chain
	middlewares.RUnlock()
	if len(chain) == 0 {
		return invoke(ctx)
	}
	call := Call{Method: m.service + "." + m.name}
	off := 0
	if m.ctx {
		off = 1
	}
	call.Args = make([]any, len(in)-off)
	for i, v := range in[off:] {
		call.Args[i] = v.Interface()
	}
	run := invoke
	for i := len(chain) - 1; i >= 0; i-- {
		mw, next := chain[i], run
		run = func(ctx context.Context) (any, error) {
			called := false
			return mw(ctx, call, func(nextCtx context.Context) (any, error) {
				if called {
					return nil, errors.New("mygo: a middleware called next twice")
				}
				called = true
				if nextCtx == nil {
					nextCtx = ctx
				}
				return next(nextCtx)
			})
		}
	}
	return run(ctx)
}

// invoke calls the method with ctx, the innermost step of a chain.
func (m *method) invoke(ctx context.Context, in []reflect.Value) (any, error) {
	if m.ctx {
		in[0] = reflect.ValueOf(ctx)
	}
	out := m.fn.Call(in)
	if m.hasErr {
		if e := out[len(out)-1]; !e.IsNil() {
			return nil, e.Interface().(error)
		}
	}
	if m.result != nil {
		return out[0].Interface(), nil
	}
	return nil, nil
}
