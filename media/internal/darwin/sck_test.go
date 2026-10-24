//go:build darwin

package darwin

import "testing"

// TestErrorOf checks that a completion callback's error is turned into a Go
// error inside the callback: an error object is an error even when the text
// the system gives for it is empty, so its text must not be what says whether
// the callback failed.
func TestErrorOf(t *testing.T) {
	if err := errorOf(0); err != nil {
		t.Fatalf("errorOf(0) = %v, want nil", err)
	}
	if err := Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	var (
		nse error
		ref uintptr
	)
	WithPool(func() {
		ref = Alloc(Class("NSError"))
		ref = SendID3(ref, Sel("initWithDomain:code:userInfo:"), NSString("MyGoTestDomain"), 1, 0)
		if ref == 0 {
			return
		}
		nse = errorOf(ref)
		Release(ref)
	})
	if ref == 0 {
		t.Fatal("the system made no NSError")
	}
	if nse == nil {
		t.Fatal("an NSError became no error")
	}
}
