package fake

import (
	"errors"
	"sync"

	"github.com/egoist/mygo/internal/platform"
)

// NativeView records the ownership, placement and focus of a hosted
// platform control without accessing a native toolkit.
type NativeView struct {
	command         string
	mu              sync.Mutex
	s               *Surface
	h               platform.NativeViewHandler
	parent, view    uintptr
	p               platform.NativeViewPlacement
	closed, focused bool
}

func (b *Backend) NewNativeView(s platform.Surface, h platform.NativeViewHandler) (platform.NativeViewHost, error) {
	f, ok := s.(*Surface)
	if !ok {
		return nil, platform.ErrUnsupported
	}
	f.mu.Lock()
	n := &NativeView{s: f, h: h, parent: uintptr(len(f.nativeViews) + 1)}
	f.nativeViews = append(f.nativeViews, n)
	f.mu.Unlock()
	return n, nil
}

func (n *NativeView) Parent() uintptr { return n.parent }
func (n *NativeView) Attach(v uintptr) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if v == 0 || n.view != 0 || n.closed {
		return errors.New("fake: invalid native view attachment")
	}
	n.view = v
	return nil
}
func (n *NativeView) Place(p platform.NativeViewPlacement) {
	n.mu.Lock()
	n.p = p
	if !p.Visible || !p.Enabled {
		n.focused = false
	}
	n.mu.Unlock()
}
func (n *NativeView) Focus(bool) {
	n.mu.Lock()
	can := !n.closed && n.p.Visible && n.p.Enabled && !n.focused
	if can {
		n.focused = true
	}
	n.mu.Unlock()
	if can {
		n.h.Focused()
	}
}
func (n *NativeView) Blur() { n.mu.Lock(); n.focused = false; n.mu.Unlock() }

func (n *NativeView) Command(command string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed || !n.focused || !n.p.Enabled || !n.p.Visible {
		return false
	}
	n.command = command
	return true
}
func (n *NativeView) LastCommand() string { n.mu.Lock(); defer n.mu.Unlock(); return n.command }
func (n *NativeView) Close() {
	n.mu.Lock()
	if n.closed {
		n.mu.Unlock()
		return
	}
	n.closed = true
	n.mu.Unlock()
	n.h.Closing()
	n.mu.Lock()
	n.view, n.focused = 0, false
	n.mu.Unlock()
}

// Placement is the last layout placement; safe from any goroutine.
func (n *NativeView) Placement() platform.NativeViewPlacement {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.p
}
func (n *NativeView) IsClosed() bool { n.mu.Lock(); defer n.mu.Unlock(); return n.closed }
func (n *NativeView) HasFocus() bool { n.mu.Lock(); defer n.mu.Unlock(); return n.focused }
func (n *NativeView) View() uintptr  { n.mu.Lock(); defer n.mu.Unlock(); return n.view }

// Tab simulates native Tab traversal on the main thread.
func (n *NativeView) Tab(backward bool) { n.h.Traverse(backward) }

// NativeViews returns the hosts created for this surface, including those
// closed, so tests can check disposal.
func (s *Surface) NativeViews() []*NativeView {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*NativeView(nil), s.nativeViews...)
}
