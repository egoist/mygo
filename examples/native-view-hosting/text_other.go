//go:build !darwin && !windows && !linux

package main

import (
	"errors"
	"fmt"
	"runtime"
)

// This build target has no native controls: the browser (GOOS=js
// GOARCH=wasm) and the platforms MyGo keeps compiling through
// internal/unsupported. MyGo reports the same thing through
// errors.ErrUnsupported, so the example still runs and says so.
func newTextView(_ host) (textView, error) {
	return nil, fmt.Errorf("native views are unsupported on %s: %w", runtime.GOOS, errors.ErrUnsupported)
}
