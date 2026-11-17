package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The Android host runs Gradle with the wrapper's batch script on Windows,
// and the host the CLI copies into the app's directory is all a Windows
// build has: without the script, the build fails there and nowhere else.
func TestAndroidHostHasGradlewBat(t *testing.T) {
	dir := t.TempDir()
	if err := (&androidToolchain{}).project(dir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "gradlew.bat"))
	if err != nil {
		t.Fatalf("the host has no gradlew.bat: %v", err)
	}
	if !strings.Contains(string(data), "gradle-wrapper.jar") {
		t.Error("gradlew.bat does not run the wrapper")
	}
	// Windows' command interpreter reads a batch file's lines as they are
	// ended, and a label it jumps to is one of them.
	if !strings.Contains(string(data), "\r\n") {
		t.Error("gradlew.bat's lines do not end with CRLF")
	}
}
