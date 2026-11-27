//go:build !darwin

package main

// simulatorArch is unused off a Mac, where iOS apps don't build.
func simulatorArch() string { return "arm64" }
