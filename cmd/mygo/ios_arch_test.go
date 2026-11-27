package main

import (
	"os/exec"
	"testing"
)

// The simulator of an Intel Mac runs x86_64 apps, which require no arm64
// (#2422); devices and Apple silicon's simulator stay arm64.
func TestIOSToolchainArch(t *testing.T) {
	for _, tc := range []struct {
		simulator    bool
		goarch       string
		target, arch string
		requiresArm  bool
	}{
		{false, "arm64", "arm64-apple-ios16.0", "arm64", true},
		{true, "arm64", "arm64-apple-ios16.0-simulator", "arm64", true},
		{true, "amd64", "x86_64-apple-ios16.0-simulator", "x86_64", false},
	} {
		sdk := "iphoneos"
		if tc.simulator {
			sdk = "iphonesimulator"
		}
		tt := &iosToolchain{simulator: tc.simulator, goarch: tc.goarch, sdkName: sdk, minimumVersion: "16.0"}
		if got := tt.clangTarget(); got != tc.target {
			t.Errorf("%s/%s: target %q, want %q", sdk, tc.goarch, got, tc.target)
		}
		if got := tt.arch(); got != tc.arch {
			t.Errorf("%s/%s: arch %q, want %q", sdk, tc.goarch, got, tc.arch)
		}
		if _, err := exec.LookPath("xcrun"); err != nil {
			continue
		}
		info, err := tt.infoPlist(&Config{Name: "A", Identifier: "com.example.a", Version: "1.0"}, "A")
		if err != nil {
			t.Skipf("no iOS SDK: %v", err)
		}
		if _, ok := info["UIRequiredDeviceCapabilities"]; ok != tc.requiresArm {
			t.Errorf("%s/%s: UIRequiredDeviceCapabilities %v, want it %v", sdk, tc.goarch, info["UIRequiredDeviceCapabilities"], tc.requiresArm)
		}
	}
}
