package main

import (
	"testing"

	"github.com/egoist/mygo"
)

// stub is a textView that records what mygo asks of it, so the adapter's
// forwarding can be checked without a window, a display or a platform.
type stub struct {
	h         uintptr
	focuses   []bool
	destroyed int
}

func (s *stub) handle() uintptr { return s.h }
func (s *stub) focus(back bool) { s.focuses = append(s.focuses, back) }
func (s *stub) destroy()        { s.destroyed++ }

func TestAdapter(t *testing.T) {
	var a mygo.NativeViewAdapter = &adapter{&stub{h: 0x42}}
	if got := a.NativeHandle(); got != 0x42 {
		t.Errorf("NativeHandle() = %#x, want 0x42", got)
	}

	s := &stub{}
	a = &adapter{s}
	// mygo focuses into the view (false) and out of it backwards (true).
	a.Focus(false)
	a.Focus(true)
	if len(s.focuses) != 2 || s.focuses[0] != false || s.focuses[1] != true {
		t.Errorf("Focus calls = %v, want [false true]", s.focuses)
	}
	a.Destroy()
	if s.destroyed != 1 {
		t.Errorf("Destroy called %d times, want 1", s.destroyed)
	}
}
