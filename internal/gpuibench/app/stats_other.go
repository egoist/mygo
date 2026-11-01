//go:build !windows

package main

// The comparison runs on Windows, where its driver does.
func threadCycles() uint64  { return 0 }
func processCycles() uint64 { return 0 }
func threadID() uint32      { return 0 }
