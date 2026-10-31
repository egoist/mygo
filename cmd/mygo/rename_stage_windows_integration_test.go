//go:build windows && integration

package main

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func holdPackageChild(t *testing.T, path string) func() {
	t.Helper()
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil,
		syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	release := func() {
		if !closed {
			closed = true
			if err := syscall.CloseHandle(handle); err != nil {
				t.Errorf("close owned handle: %v", err)
			}
		}
	}
	t.Cleanup(release)
	return release
}

func TestRetryPackageStageRenameOwnedChildReleased(t *testing.T) {
	base := t.TempDir()
	src, dst := filepath.Join(base, "stage"), filepath.Join(base, "final")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(src, "payload")
	if err := os.WriteFile(child, []byte("new package"), 0o644); err != nil {
		t.Fatal(err)
	}
	release := holdPackageChild(t, child)
	attempts, pauses := 0, 0
	err := retryPackageStageRename(func() error {
		attempts++
		err := os.Rename(src, dst)
		if attempts <= 2 && !errors.Is(err, syscall.Errno(5)) && !errors.Is(err, syscall.Errno(32)) {
			t.Fatalf("owned handle must reject attempt %d with 5/32, got %v", attempts, err)
		}
		return err
	}, func(d time.Duration) {
		pauses++
		if d != 100*time.Millisecond {
			t.Fatalf("pause = %v, want 100ms", d)
		}
		// Delay CloseHandle until two actual rejected rename calls. Ordering is
		// event-based; test success never depends on a wall-clock deadline.
		if pauses == 2 {
			release()
		}
	})
	if err != nil || attempts != 3 || pauses != 2 {
		t.Fatalf("rename = %v, attempts/pauses = %d/%d; want success after release", err, attempts, pauses)
	}
	if got, err := os.ReadFile(filepath.Join(dst, "payload")); err != nil || string(got) != "new package" {
		t.Fatalf("committed payload = %q, %v", got, err)
	}
}

func TestReplacePathOwnedChildHeldRestoresDestination(t *testing.T) {
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
	holdPackageChild(t, filepath.Join(src, "payload"))
	err := replacePath(src, dst)
	if !errors.Is(err, syscall.Errno(5)) && !errors.Is(err, syscall.Errno(32)) {
		t.Fatalf("rename = %v, want original Windows in-use-eligible error", err)
	}
	var link *os.LinkError
	if !errors.As(err, &link) || link.Old != src || link.New != dst {
		t.Fatalf("error must come from final stage commit, got %v", err)
	}
	for path, want := range map[string]string{src: "new package", dst: "old package"} {
		if got, readErr := os.ReadFile(filepath.Join(path, "payload")); readErr != nil || string(got) != want {
			t.Fatalf("payload %s = %q, %v; want %q", path, got, readErr, want)
		}
	}
	if old, globErr := filepath.Glob(filepath.Join(base, ".old-*")); globErr != nil || len(old) != 0 {
		t.Fatalf("rollback left old destinations = %v, %v", old, globErr)
	}
}

func TestRetryPackageStageRenameOwnedChildPermanentlyHeld(t *testing.T) {
	base := t.TempDir()
	src, dst := filepath.Join(base, "stage"), filepath.Join(base, "final")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "payload"), []byte("new package"), 0o644); err != nil {
		t.Fatal(err)
	}
	holdPackageChild(t, filepath.Join(src, "payload"))
	attempts, pauses := 0, 0
	err := retryPackageStageRename(func() error {
		attempts++
		err := os.Rename(src, dst)
		if !errors.Is(err, syscall.Errno(5)) && !errors.Is(err, syscall.Errno(32)) {
			t.Fatalf("native held-child rename %d = %v, want 5/32", attempts, err)
		}
		return err
	}, func(d time.Duration) {
		pauses++
		if d != 100*time.Millisecond {
			t.Fatalf("pause = %v, want 100ms", d)
		}
	})
	if err == nil || attempts != 10 || pauses != 9 {
		t.Fatalf("held child error %v; actual attempts/pauses = %d/%d, want 10/9", err, attempts, pauses)
	}
	t.Logf("permanently held child: %d actual rejected os.Rename calls, %d scheduled pauses; terminal error %v", attempts, pauses, err)
	if got, err := os.ReadFile(filepath.Join(src, "payload")); err != nil || string(got) != "new package" {
		t.Fatalf("source = %q, %v; want unchanged new package", got, err)
	}
	if _, err := os.Lstat(dst); !os.IsNotExist(err) {
		t.Fatalf("destination must remain absent, stat = %v", err)
	}
}
