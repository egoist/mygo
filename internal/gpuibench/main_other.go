//go:build !windows

package main

import "log"

func main() { log.Fatal("gpuibench compares MyGo and GPUI on Windows only") }
