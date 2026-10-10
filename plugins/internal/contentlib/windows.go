//go:build windows && (amd64 || arm64)

package contentlib

import (
	"errors"
	"path/filepath"
	"syscall"
	"unsafe"
)

func open(path string) (uintptr, error) {
	// Default DLL lookup must not search the current working directory.
	if !filepath.IsAbs(path) {
		return 0, errors.New("install DLL next to the executable or supply its absolute path")
	}
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	f := syscall.NewLazyDLL("kernel32.dll").NewProc("LoadLibraryExW")
	h, _, e := f.Call(uintptr(unsafe.Pointer(p)), 0, 0x100|0x1000) // DLL_LOAD_DIR | DEFAULT_DIRS
	if h == 0 {
		return 0, e
	}
	return h, nil
}
func symbol(h uintptr, name string) (uintptr, error) {
	return syscall.GetProcAddress(syscall.Handle(h), name)
}
func Release(h uintptr) { _ = syscall.FreeLibrary(syscall.Handle(h)) }
