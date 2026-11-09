//go:build darwin || linux

package native

import "github.com/ebitengine/purego"

// openNative opens the library at path, which mygo.NativeLibrary names on
// a phone, and returns its handle.
func openNative(path string) (uintptr, error) {
	return purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
}

// nativeSymbol returns the address of the symbol name in an open library.
func nativeSymbol(handle uintptr, name string) (uintptr, error) {
	return purego.Dlsym(handle, name)
}
