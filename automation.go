package mygo

import (
	"github.com/egoist/mygo/internal/surface"
	"github.com/egoist/mygo/ui/automation"
)

// Automation drives explicitly automatable native content. Its methods are
// safe from any goroutine, outside view construction and control callbacks.
type Automation struct{ window *Window }

// Automate returns a driver for w. The content must opt in through
// ui.Content.Automatable. No transport or endpoint is enabled.
// Keeping these operations off Window also keeps them out of apps whose
// IPC bindings reflect over exported methods but never use automation.
func Automate(w *Window) *Automation { return &Automation{window: w} }

// Inspect returns the last rendered semantic hierarchy of native content
// explicitly enabled with ui.Content.Automatable. It is safe from any
// goroutine and works without the inspector, including production builds.
// Before the first frame it returns an empty tree. Pending invalidation is
// reflected after the next rendered frame; this call does not run the view.
// Calls during view construction or control callbacks return ErrUnsupported.
func (a *Automation) Inspect() (automation.Snapshot, error) {
	var snapshot automation.Snapshot
	err := automation.ErrClosed
	w := a.window
	if w == nil {
		return snapshot, err
	}
	onMain(func() {
		if w.native == nil {
			return
		}
		err = automation.ErrUnsupported
		if w.conn != nil {
			if a, ok := w.conn.Automation.(*surface.Automation); ok {
				snapshot, err = a.Inspect()
			}
		}
	})
	return snapshot, err
}

// Perform validates a target from Inspect and performs a normal control
// action on the main thread. Each input renders its resulting frame before
// returning; animations and asynchronous work can require later snapshots.
// It returns automation.ErrClosed after destruction or app shutdown, and
// ErrUnsupported for web pages or content without the explicit opt-in.
func (a *Automation) Perform(target automation.Target, request automation.Request) error {
	err := automation.ErrClosed
	w := a.window
	if w == nil {
		return err
	}
	onMain(func() {
		if w.native == nil {
			return
		}
		err = automation.ErrUnsupported
		if w.conn != nil {
			if a, ok := w.conn.Automation.(*surface.Automation); ok {
				err = a.Perform(target, request)
			}
		}
	})
	return err
}

// Input sends a key chord, committed text or scrolling through the native
// content's normal input path. Keys and text go to the current focus; focus
// a control with Perform first. It has the same opt-in, threading and lifetime
// requirements as Perform. No operating-system input is synthesized.
func (a *Automation) Input(input automation.Input) error {
	err := automation.ErrClosed
	w := a.window
	if w == nil {
		return err
	}
	onMain(func() {
		if w.native == nil {
			return
		}
		err = automation.ErrUnsupported
		if w.conn != nil {
			if a, ok := w.conn.Automation.(*surface.Automation); ok {
				err = a.Input(input)
			}
		}
	})
	return err
}
