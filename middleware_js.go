//go:build js

package mygo

import "context"

// Call describes a call to a bound method, for Middleware.
type Call struct {
	// Method is the call's "Service.Method" name.
	Method string
	// Args are the call's decoded arguments, without the leading
	// context.Context a method may take.
	Args []any
}

// Middleware wraps calls to bound methods. In browsers, where Bind does
// nothing, it is never run.
type Middleware func(ctx context.Context, call Call, next func(context.Context) (any, error)) (any, error)

// Intercept registers middleware around calls to bound methods. In
// browsers it does nothing, as Bind does.
func Intercept(ms ...Middleware) {}
