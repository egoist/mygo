//go:build !js

package mygo

import (
	"context"
	"reflect"
	"slices"
	"sync"
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
// call it at most once. Returning without calling next short-circuits the
// call: the method never runs, and the error rejects the page's promise.
// A middleware may wrap or replace the result, which must still encode as
// the method's result type. A panic in a middleware becomes the call's
// error, as one in the method does.
//
// Middleware wraps plugins' calls too; filter on Call.Method to leave them
// alone.
type Middleware func(ctx context.Context, call Call, next func(context.Context) (any, error)) (any, error)

var middlewares struct {
	sync.RWMutex
	chain []Middleware
}

// Intercept registers middleware around every call to a bound method,
// outermost first: Intercept(a, b) runs a around b around the method.
// Later calls append to the chain. It is safe from any goroutine;
// register before App.Run to wrap every call. In browsers it does
// nothing, as Bind does.
func Intercept(ms ...Middleware) {
	middlewares.Lock()
	middlewares.chain = append(slices.Clone(middlewares.chain), ms...)
	middlewares.Unlock()
}

// runMiddleware runs the chain around invoke, or invoke alone when no
// middleware is registered, which then pays only the read lock.
func runMiddleware(ctx context.Context, m *method, in []reflect.Value, invoke func(context.Context) (any, error)) (any, error) {
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
		run = func(ctx context.Context) (any, error) { return mw(ctx, call, next) }
	}
	return run(ctx)
}