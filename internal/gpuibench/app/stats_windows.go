package main

import (
	"syscall"
	"unsafe"
)

var (
	kernel32              = syscall.NewLazyDLL("kernel32.dll")
	getCurrentThread      = kernel32.NewProc("GetCurrentThread")
	getCurrentThreadID    = kernel32.NewProc("GetCurrentThreadId")
	queryThreadCycleTime  = kernel32.NewProc("QueryThreadCycleTime")
	queryProcessCycleTime = kernel32.NewProc("QueryProcessCycleTime")
)

// threadCycles returns the cycles the calling thread has run, in units of the
// processor's time stamp counter. Windows' CPU times only count clock ticks.
func threadCycles() uint64 {
	var c uint64
	h, _, _ := getCurrentThread.Call()
	queryThreadCycleTime.Call(h, uintptr(unsafe.Pointer(&c)))
	return c
}

// processCycles returns the cycles all the process's threads have run.
func processCycles() uint64 {
	var c uint64
	h, _ := syscall.GetCurrentProcess()
	queryProcessCycleTime.Call(uintptr(h), uintptr(unsafe.Pointer(&c)))
	return c
}

func threadID() uint32 {
	id, _, _ := getCurrentThreadID.Call()
	return uint32(id)
}
