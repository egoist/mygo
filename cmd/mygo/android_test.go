package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// The host comes with both wrapper scripts, since runGradle runs
// gradlew.bat on Windows and gradlew elsewhere.
func TestAndroidProjectWritesGradleWrappers(t *testing.T) {
	dir := t.TempDir()
	if err := (&androidToolchain{}).project(dir); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "gradlew"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		t.Errorf("gradlew is not executable: %v", info.Mode())
	}
	bat, err := os.ReadFile(filepath.Join(dir, "gradlew.bat"))
	if err != nil {
		t.Fatal(err)
	}
	if lf, crlf := bytes.Count(bat, []byte("\n")), bytes.Count(bat, []byte("\r\n")); lf == 0 || lf != crlf {
		t.Errorf("gradlew.bat has %d line feeds, %d of them after a carriage return; cmd.exe needs CRLF", lf, crlf)
	}
	if !bytes.Contains(bat, []byte(`gradle\wrapper\gradle-wrapper.jar`)) {
		t.Errorf("gradlew.bat does not run the wrapper's jar")
	}
}
