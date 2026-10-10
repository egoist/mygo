//go:build darwin || (linux && (amd64 || arm64)) || (windows && (amd64 || arm64))

package contentlib

import "github.com/ebitengine/purego"

const Supported = true

func register(fn any, addr uintptr) { purego.RegisterFunc(fn, addr) }
func Callback(fn any) uintptr       { return purego.NewCallback(fn) }
