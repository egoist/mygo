//go:build windows && integration

package main

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"testing/synctest"
	"time"
)

// This exact test is compiled against both original and candidate replacePath.
// It does not replace, hook, or inject any production rename operation.
func TestReplacePathOwnedChildReleasedProduction(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		base := t.TempDir()
		src, dst := filepath.Join(base, "stage"), filepath.Join(base, "final")
		for path, value := range map[string]string{src: "new package", dst: "old package"} {
			if err := os.Mkdir(path, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(path, "payload"), []byte(value), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		name, err := syscall.UTF16PtrFromString(filepath.Join(src, "payload"))
		if err != nil {
			t.Fatal(err)
		}
		handle, err := syscall.CreateFile(name, syscall.GENERIC_READ,
			syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil,
			syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
		if err != nil {
			t.Fatal(err)
		}
		var once sync.Once
		var closeErr error
		release := func() error {
			once.Do(func() { closeErr = syscall.CloseHandle(handle) })
			return closeErr
		}
		t.Cleanup(func() {
			if err := release(); err != nil {
				t.Errorf("close owned child: %v", err)
			}
		})
		// Confirm that the owned handle really rejects a native directory rename
		// on this filesystem before testing either production implementation.
		probe := filepath.Join(base, "probe")
		probeErr := os.Rename(src, probe)
		if !errors.Is(probeErr, syscall.Errno(5)) && !errors.Is(probeErr, syscall.Errno(32)) {
			t.Fatalf("owned child must reject native probe with 5/32, got %v", probeErr)
		}
		t.Logf("actual owned-handle probe: %v", probeErr)
		done := make(chan error, 1)
		go func() {
			time.Sleep(50 * time.Millisecond)
			done <- release()
		}()
		joined := false
		defer func() {
			if !joined {
				<-done
			}
		}()
		started := time.Now()
		commitErr := replacePath(src, dst)
		returnedAt := time.Since(started)
		releaseErr := <-done
		joined = true
		if releaseErr != nil {
			t.Fatalf("release owned child: %v", releaseErr)
		}
		t.Logf("replacePath returned at fake elapsed %v with error %v; release joined", returnedAt, commitErr)
		if commitErr != nil {
			var link *os.LinkError
			if !errors.As(commitErr, &link) || link.Old != src || link.New != dst ||
				!errors.Is(commitErr, syscall.Errno(5)) && !errors.Is(commitErr, syscall.Errno(32)) {
				t.Fatalf("unexpected setup or rename phase error: %v", commitErr)
			}
			t.Fatalf("production final rename returned before owned hold was released: %v", commitErr)
		}
		if got, err := os.ReadFile(filepath.Join(dst, "payload")); err != nil || string(got) != "new package" {
			t.Fatalf("destination = %q, %v; want committed new package", got, err)
		}
		if _, err := os.Lstat(src); !os.IsNotExist(err) {
			t.Fatalf("source must have moved, stat = %v", err)
		}
		if old, err := filepath.Glob(filepath.Join(base, ".old-*")); err != nil || len(old) != 0 {
			t.Fatalf("old destinations after successful commit = %v, %v", old, err)
		}
	})
}
