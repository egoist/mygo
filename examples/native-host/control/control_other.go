//go:build !darwin && !(linux && (amd64 || arm64)) && !(windows && (amd64 || arm64))

package control

import (
	"errors"
	"github.com/egoist/mygo"
)

func Options(string, func(string)) mygo.NativeViewOptions {
	return mygo.NativeViewOptions{Create: func(mygo.NativeViewContext) (uintptr, error) { return 0, errors.New("native control unavailable") }}
}
func Text(mygo.NativeViewContext) string     { return "" }
func SetText(mygo.NativeViewContext, string) {}
