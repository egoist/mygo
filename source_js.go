//go:build js

package mygo

import (
	"errors"
	"iter"
)

// Source receives values of type T that the page sends to a bound method.
// Browsers show no web pages (see ipc_js.go): Recv fails.
type Source[T any] struct{}

// ErrSourceClosed is returned by Source.Recv once the source is closed
// without the page ending it.
var ErrSourceClosed = errors.New("mygo: source closed")

// Recv returns the next value the page sent: there is none, and it fails.
func (s *Source[T]) Recv() (T, error) {
	var v T
	return v, ErrSourceClosed
}

// Values iterates the source, which ends at once.
func (s *Source[T]) Values() iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		var v T
		yield(v, ErrSourceClosed)
	}
}

// Close closes the source.
func (s *Source[T]) Close() {}

// source is the stream of one call's argument, of which there are none,
// and earlySourceValue a value that arrived before its call.
type source struct{}

type earlySourceValue struct{}