package main

import (
	"runtime"
	"testing"
)

// The simulator of an Intel Mac runs x86_64 apps, which must not ask for
// an arm64 device.
func TestIOSSimulatorArch(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Xcode is on macOS")
	}
	for _, tc := range []struct {
		tc       iosToolchain
		arch     string
		requires bool
	}{
		{iosToolchain{goarch: "arm64", sdkName: "iphoneos"}, "arm64", true},
		{iosToolchain{simulator: true, goarch: "arm64", sdkName: "iphonesimulator"}, "arm64", true},
		{iosToolchain{simulator: true, goarch: "amd64", sdkName: "iphonesimulator"}, "x86_64", false},
	} {
		if got := tc.tc.arch(); got != tc.arch {
			t.Errorf("%s: arch %s, want %s", tc.tc.goarch, got, tc.arch)
		}
		m, err := tc.tc.infoPlist(&Config{Name: "App", Identifier: "com.example.app", Version: "1.0.0"}, "App")
		if err != nil {
			t.Skip("no iOS SDK:", err)
		}
		if _, ok := m["UIRequiredDeviceCapabilities"]; ok != tc.requires {
			t.Errorf("%s (simulator %v): UIRequiredDeviceCapabilities %v, want %v", tc.tc.goarch, tc.tc.simulator, m["UIRequiredDeviceCapabilities"], tc.requires)
		}
	}
}
