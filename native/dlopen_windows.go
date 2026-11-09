//go:build windows

package native

import "syscall"

// openNative opens the library at path, which mygo.NativeLibrary names on
// a phone: on Windows it names none, module DLLs being the app's own files,
// which package ffi loads.
func openNative(path string) (uintptr, error) {
	h, err := syscall.LoadLibrary(path)
	return uintptr(h), err
}

// nativeSymbol returns the address of the symbol name in an open library.
func nativeSymbol(handle uintptr, name string) (uintptr, error) {
	return syscall.GetProcAddress(syscall.Handle(handle), name)
}
