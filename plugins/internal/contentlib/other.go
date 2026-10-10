//go:build !darwin && !(linux && (amd64 || arm64)) && !(windows && (amd64 || arm64))

package contentlib

import "errors"

const Supported = false

func open(string) (uintptr, error)            { return 0, errors.ErrUnsupported }
func symbol(uintptr, string) (uintptr, error) { return 0, errors.ErrUnsupported }
func Release(uintptr)                         {}
func register(any, uintptr)                   {}
func Callback(any) uintptr                    { return 0 }
