// Package appbackend holds the backend the app runs on. Package mygo makes
// the platform's on first use; package apptest makes it the fake one
// before that, for an app's own tests. It holds a variable, not a hook
// that mygo calls, so that apps link nothing of it but the variable.
package appbackend

import (
	"sync"

	"github.com/egoist/mygo/internal/platform"
)

var (
	// Once makes the backend, once: package mygo the platform's, or
	// package apptest the fake one, whichever comes first.
	Once sync.Once
	// Backend is the backend Once made.
	Backend platform.Backend
)
