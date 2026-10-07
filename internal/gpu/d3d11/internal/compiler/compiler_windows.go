//go:build windows

// Package compiler compiles HLSL with the system's Direct3D compiler.
package compiler

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"github.com/egoist/mygo/internal/gpu/d3d11/device"
)

var (
	procCompile      = syscall.NewLazyDLL(device.SystemDir() + `\d3dcompiler_47.dll`).NewProc("D3DCompile")
	procCreateThread = syscall.NewLazyDLL("kernel32.dll").NewProc("CreateThread")
	procWaitThread   = syscall.NewLazyDLL("kernel32.dll").NewProc("WaitForSingleObject")
	requests         sync.Map
	nextRequest      atomic.Uintptr
	threadCallback   = syscall.NewCallback(func(id uintptr) uintptr {
		// Syscalls must stay on the thread whose native stack we reserved.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		value, _ := requests.Load(id)
		r := value.(*request)
		r.code, r.err = compile(r.source, r.entry, r.target)
		close(r.done)
		return 0
	})
)

type request struct {
	source, entry, target string
	code                  []byte
	err                   error
	done                  chan struct{}
}

// Compile compiles entry in source for target. D3DCompile can exhaust the
// 2 MiB native stacks of Go's Windows threads while optimizing transformed
// shaders. Reserve a larger stack for compilation alone; Windows commits
// its pages on demand and frees it when the thread returns. One callback
// routes every request, including the generators and effects.
func Compile(source, entry, target string) ([]byte, error) {
	if err := procCompile.Find(); err != nil {
		return nil, fmt.Errorf("d3d11: no shader compiler: %w", err)
	}
	r := &request{source: source, entry: entry, target: target, done: make(chan struct{})}
	id := nextRequest.Add(1)
	requests.Store(id, r)
	defer requests.Delete(id)
	const stackReserve = 16 << 20
	const stackSizeIsReservation = 0x10000
	thread, _, err := procCreateThread.Call(0, stackReserve, threadCallback, id, stackSizeIsReservation, 0)
	if thread == 0 {
		return nil, fmt.Errorf("d3d11: cannot start shader compiler: %w", err)
	}
	defer syscall.CloseHandle(syscall.Handle(thread))
	// Keep this wait visible to Go as an external syscall until the new
	// native thread enters its Go callback, and wait for its stack to leave
	// the compiler before returning the result.
	procWaitThread.Call(thread, 0xffffffff)
	<-r.done
	return r.code, r.err
}

func compile(source, entry, target string) ([]byte, error) {
	src := []byte(source)
	if len(src) == 0 {
		return nil, fmt.Errorf("d3d11: empty shader source")
	}
	name, _ := syscall.BytePtrFromString("shader.hlsl")
	e, _ := syscall.BytePtrFromString(entry)
	t, _ := syscall.BytePtrFromString(target)
	const optimize3 = 1 << 15
	var blob, errs uintptr
	hr, _, _ := procCompile.Call(uintptr(unsafe.Pointer(&src[0])), uintptr(len(src)), uintptr(unsafe.Pointer(name)),
		0, 0, uintptr(unsafe.Pointer(e)), uintptr(unsafe.Pointer(t)), optimize3, 0,
		uintptr(unsafe.Pointer(&blob)), uintptr(unsafe.Pointer(&errs)))
	if errs != 0 {
		defer call(errs, 2) // IUnknown.Release
	}
	if blob != 0 {
		defer call(blob, 2)
	}
	if int32(uint32(hr)) < 0 || blob == 0 {
		msg := "unknown error"
		if errs != 0 {
			msg = string(blobBytes(errs))
		}
		return nil, fmt.Errorf("d3d11: cannot compile the %s shader: %s", entry, msg)
	}
	return append([]byte(nil), blobBytes(blob)...), nil
}

func pointer(p uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&p)) }

func call(blob uintptr, method int) uintptr {
	vtbl := *(*uintptr)(pointer(blob))
	fn := *(*uintptr)(unsafe.Add(pointer(vtbl), method*int(unsafe.Sizeof(uintptr(0)))))
	r, _, _ := syscall.SyscallN(fn, blob)
	return r
}

func blobBytes(blob uintptr) []byte {
	p, n := call(blob, 3), call(blob, 4) // GetBufferPointer, GetBufferSize
	return unsafe.Slice((*byte)(pointer(p)), n)
}
