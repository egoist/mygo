//go:build darwin

package e2e

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/darwin"
)

func responderState(w *mygo.Window) (s string) {
	mygo.RunOnMain(func() { s = darwin.TestResponderState(w.NativeHandle()) })
	return s
}
