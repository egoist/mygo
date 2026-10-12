//go:build windows && (amd64 || arm64)

package windows

import (
	"os"
	"path/filepath"
	"testing"
)

// TestClientDLL checks how a runtime's directory proves itself. The Edge
// browser ships the client library beside itself with no browser process
// behind it, and loading that one fails with 0x80070032 (ERROR_NOT_SUPPORTED),
// so the library alone may not pass for a runtime.
func TestClientDLL(t *testing.T) {
	client := func(dir string) string {
		return filepath.Join(dir, clientDirName, archDir(), clientDLLName)
	}
	write := func(t *testing.T, path string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("runtime", func(t *testing.T) {
		dir := t.TempDir()
		write(t, filepath.Join(dir, browserExe))
		write(t, client(dir))
		if got := clientDLL(dir); got != client(dir) {
			t.Errorf("clientDLL = %q, want %q", got, client(dir))
		}
	})

	t.Run("library alone", func(t *testing.T) {
		// The Edge browser's layout: EmbeddedBrowserWebView.dll under
		// EBWebView, and no msedgewebview2.exe at the root.
		dir := t.TempDir()
		write(t, client(dir))
		if got := clientDLL(dir); got != "" {
			t.Errorf("clientDLL of a library without a browser = %q, want \"\"", got)
		}
	})

	t.Run("browser alone", func(t *testing.T) {
		dir := t.TempDir()
		write(t, filepath.Join(dir, browserExe))
		if got := clientDLL(dir); got != "" {
			t.Errorf("clientDLL of a browser without a library = %q, want \"\"", got)
		}
	})

	t.Run("other architecture", func(t *testing.T) {
		other := "x64"
		if archDir() == "x64" {
			other = "arm64"
		}
		dir := t.TempDir()
		write(t, filepath.Join(dir, browserExe))
		write(t, filepath.Join(dir, clientDirName, other, clientDLLName))
		if got := clientDLL(dir); got != "" {
			t.Errorf("clientDLL found %q, which is no %s runtime", got, archDir())
		}
	})

	t.Run("missing", func(t *testing.T) {
		if got := clientDLL(t.TempDir()); got != "" {
			t.Errorf("clientDLL of an empty directory = %q, want \"\"", got)
		}
	})
}
