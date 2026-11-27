package wait

import (
	"context"
	"sync/atomic"
	"testing"
)

func TestUntil(t *testing.T) {
	var n atomic.Int32
	go func() {
		for range 5 {
			n.Add(1)
		}
	}()
	Until(t, "five", func() bool { return n.Load() == 5 })
}

func TestFor(t *testing.T) {
	ch := make(chan int, 1)
	go func() { ch <- 7 }()
	if v := For(t, "a value", ch); v != 7 {
		t.Fatalf("For = %d", v)
	}
}

func TestPoll(t *testing.T) {
	var calls atomic.Int32
	if !Poll(func() bool { return calls.Add(1) >= 3 }) {
		t.Fatal("Poll gave up on a condition that came true")
	}
	if n := calls.Load(); n < 3 {
		t.Fatalf("Poll stopped after %d calls", n)
	}
}

// TestContext checks the context a test hands to calls that wait on one:
// it is live while the test runs and done when the test is over.
func TestContext(t *testing.T) {
	var ctx context.Context
	t.Cleanup(func() {
		if ctx.Err() == nil {
			t.Error("the context outlives the test uncanceled")
		}
	})
	ctx = Context(t)
	if err := ctx.Err(); err != nil {
		t.Errorf("the context is done at once: %v", err)
	}
}
