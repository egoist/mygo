// Package control demonstrates a native text-field adapter built entirely
// with purego/syscall. It is sample code, not a portable MyGo text widget.
// Options can be passed to Window.NewNativeView. Text and SetText may be
// called only from its main-thread hooks (including NativeView.Update).
package control

import "unsafe"

func nativePointer(p uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&p)) }

func cString(p uintptr) string {
	if p == 0 {
		return ""
	}
	n := 0
	for *(*byte)(unsafe.Add(nativePointer(p), n)) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(nativePointer(p)), n))
}

func cBytes(s string) *byte { return &append([]byte(s), 0)[0] }
