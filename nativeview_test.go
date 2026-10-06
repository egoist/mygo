package mygo

import (
	"errors"
	"reflect"
	"testing"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/unsupported"
	"github.com/egoist/mygo/ui"
)

func fakeControl() NativeViewOptions {
	return NativeViewOptions{Create: func(c NativeViewContext) (uintptr, error) { return c.Parent + 100, nil }}
}

func TestNativeViewLifecycle(t *testing.T) {
	var v *NativeView
	show := true
	w, _, s := contentWindow(t, func(c *ui.Context) {
		if show {
			ui.HostView(c, v).Key("native").Size(120, 40)
		}
	})
	var calls []string
	opts := fakeControl()
	opts.Create = func(c NativeViewContext) (uintptr, error) {
		if !fb.IsMainThread() || c.Platform != "fake" || c.Parent == 0 || c.View != 0 || c.Visible || !c.Enabled {
			t.Errorf("creation context %+v, main=%v", c, fb.IsMainThread())
		}
		calls = append(calls, "create")
		return 101, nil
	}
	opts.Update = func(c NativeViewContext) {
		if !fb.IsMainThread() || c.View != 101 {
			t.Errorf("update context %+v", c)
		}
		calls = append(calls, "update")
	}
	opts.Dispose = func(c NativeViewContext) {
		if !fb.IsMainThread() || c.Parent == 0 || c.View != 101 || s.NativeViews()[0].View() != 101 {
			t.Errorf("dispose before releasing live handles: %+v", c)
		}
		calls = append(calls, "dispose")
	}
	var err error
	v, err = w.NewNativeView(opts) // called from the test goroutine
	if err != nil {
		t.Fatal(err)
	}
	if v.Window() != w || v.IsDestroyed() {
		t.Fatal("incorrect ownership")
	}
	onMain(func() { s.Frame() })
	n := s.NativeViews()[0]
	if !n.Placement().Visible {
		t.Fatal("not shown")
	}
	if err := v.Update(func(c NativeViewContext) {
		calls = append(calls, "explicit")
		if !fb.IsMainThread() {
			t.Error("not on main")
		}
	}); err != nil {
		t.Fatal(err)
	}
	onMain(func() { s.Frame() })
	w.Update(func() { show = false })
	onMain(func() { s.Frame() })
	if n.Placement().Visible || v.IsDestroyed() || n.View() != 101 {
		t.Fatal("omitting the element must hide and retain the control")
	}
	w.Destroy()
	v.Destroy()
	if !v.IsDestroyed() || !n.IsClosed() || n.View() != 0 {
		t.Fatal("window close did not release the control")
	}
	if err := v.Update(func(NativeViewContext) { t.Error("called after destruction") }); !errors.Is(err, errDestroyed) {
		t.Errorf("Update after close: %v", err)
	}
	want := []string{"create", "update", "explicit", "update", "update", "dispose"}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("hooks %v, want %v", calls, want)
	}
}

func TestNativeViewCreationErrors(t *testing.T) {
	w := NewWindow(WindowOptions{Hidden: true})
	defer w.Destroy()
	if _, err := w.NewNativeView(fakeControl()); err == nil {
		t.Error("web page accepted a native view")
	}
	nw, _, s := contentWindow(t, func(*ui.Context) {})
	if _, err := nw.NewNativeView(NativeViewOptions{}); err == nil {
		t.Error("accepted no Create hook")
	}
	broken := errors.New("factory failed")
	for _, opts := range []NativeViewOptions{
		{Create: func(NativeViewContext) (uintptr, error) { return 0, broken }},
		{Create: func(NativeViewContext) (uintptr, error) { return 0, nil }},
	} {
		opts.Dispose = func(NativeViewContext) { t.Error("Dispose called without adopting a view") }
		if v, err := nw.NewNativeView(opts); err == nil || v != nil {
			t.Errorf("creation failure returned %v, %v", v, err)
		}
	}
	for _, n := range s.NativeViews() {
		if !n.IsClosed() {
			t.Error("creation failure leaked the container")
		}
	}
	nw.Destroy()
	if _, err := nw.NewNativeView(fakeControl()); !errors.Is(err, errDestroyed) {
		t.Errorf("create after close: %v", err)
	}
	if _, err := unsupported.New().NewNativeView(nil, nil); !errors.Is(err, platform.ErrUnsupported) {
		t.Errorf("unsupported: %v", err)
	}
}

func TestNativeViewWindowClosesDuringCreate(t *testing.T) {
	w, _, s := contentWindow(t, func(*ui.Context) {})
	v, err := w.NewNativeView(NativeViewOptions{Create: func(NativeViewContext) (uintptr, error) { w.Destroy(); return 0, nil }})
	if !errors.Is(err, errDestroyed) || v != nil || !s.NativeViews()[0].IsClosed() {
		t.Fatalf("view=%v error=%v", v, err)
	}
}

func TestNativeViewLayoutClippingAndVisibility(t *testing.T) {
	var v *NativeView
	var overlay, disabled, hidden bool
	w, _, s := contentWindow(t, func(c *ui.Context) {
		ui.Box(c).Absolute().Left(30).Top(20).Size(100, 40).Clip().Disabled(disabled).Children(func() {
			e := ui.HostView(c, v).Absolute().Left(-20).Top(-10).Size(150, 80)
			if hidden {
				e.Invisible()
			}
		})
		if overlay {
			ui.Button(c, "Overlap").Absolute().Left(50).Top(20).Size(60, 30)
		}
	})
	var err error
	v, err = w.NewNativeView(fakeControl())
	if err != nil {
		t.Fatal(err)
	}
	onMain(func() { s.Frame() })
	n := s.NativeViews()[0]
	p := n.Placement()
	if p.Bounds != (platform.RectF{X: 10, Y: 10, W: 150, H: 80}) || p.Clip != (platform.RectF{X: 30, Y: 20, W: 100, H: 40}) || !p.Visible || !p.Enabled {
		t.Fatalf("placement %+v", p)
	}
	w.Update(func() { disabled = true })
	onMain(func() { s.Frame() })
	v.Focus()
	if n.Placement().Enabled || n.HasFocus() {
		t.Error("disabled parent must disable native input")
	}
	w.Update(func() { disabled, overlay = false, true })
	onMain(func() { s.Frame() })
	if n.Placement().Visible {
		t.Error("native view covered the later widget")
	}
	w.Update(func() { overlay = false })
	onMain(func() { s.Frame() })
	if !n.Placement().Visible {
		t.Error("not restored after overlay left")
	}
	w.Update(func() { hidden = true })
	onMain(func() { s.Frame() })
	if n.Placement().Visible {
		t.Error("Invisible did not hide the native control")
	}
}

func TestNativeViewFocusAndAccessibility(t *testing.T) {
	var v *NativeView
	var before, after bool
	w, _, s := contentWindow(t, func(c *ui.Context) {
		before = ui.Button(c, "Before").AutoFocus().Focused()
		ui.HostView(c, v).Key("native").Label("Native field").Size(120, 40)
		after = ui.Button(c, "After").Focused()
	})
	var err error
	v, err = w.NewNativeView(fakeControl())
	if err != nil {
		t.Fatal(err)
	}
	onMain(func() {
		s.Frame()
		s.Send(platform.SurfaceEvent{Kind: platform.AccessibilityOn})
		s.Send(platform.SurfaceEvent{Kind: platform.KeyPressed, Key: platform.KeyTab})
		s.Frame()
	})
	n := s.NativeViews()[0]
	if !n.HasFocus() {
		t.Fatal("Tab did not enter the native control")
	}
	onMain(func() { w.contentCommand("copy") })
	if n.LastCommand() != "copy" {
		t.Error("Edit menu role did not reach the focused native control")
	}
	tree, _ := s.Accessibility()
	var id uint64
	for _, a := range tree.Nodes {
		if a.Label == "Native field" {
			id = a.ID
			if a.NativeView != n.Parent() {
				t.Errorf("not grafted: %+v", a)
			}
		}
	}
	if id == 0 || tree.Focus != id {
		t.Fatalf("native accessibility focus=%d node=%d", tree.Focus, id)
	}
	onMain(func() { n.Tab(false); s.Frame() })
	if n.HasFocus() || !after {
		t.Error("Tab did not leave the native control for After")
	}
	onMain(func() {
		s.Send(platform.SurfaceEvent{Kind: platform.KeyPressed, Key: platform.KeyTab, Mods: platform.ModShift})
		s.Frame()
	})
	if !n.HasFocus() {
		t.Error("Shift+Tab did not enter the native control")
	}
	v.MoveFocus(true)
	onMain(func() { s.Frame() })
	if n.HasFocus() || !before {
		t.Error("backward native traversal did not reach Before")
	}
	v.Focus()
	onMain(func() { s.Frame() })
	if !n.HasFocus() {
		t.Error("programmatic native focus failed")
	}
	v.Destroy()
	onMain(func() { s.Frame() })
	tree, _ = s.Accessibility()
	for _, a := range tree.Nodes {
		if a.NativeView != 0 {
			t.Error("destroyed native subtree is still accessible")
		}
	}
}

func TestNativeViewCannotMoveBetweenWindows(t *testing.T) {
	var v *NativeView
	w, _, s := contentWindow(t, func(c *ui.Context) { ui.HostView(c, v).Size(120, 40) })
	var err error
	v, err = w.NewNativeView(fakeControl())
	if err != nil {
		t.Fatal(err)
	}
	onMain(func() { s.Frame() })
	n := s.NativeViews()[0]
	p := n.Placement()
	other, _, os := contentWindow(t, func(c *ui.Context) { ui.HostView(c, v).Size(50, 20) })
	other.Invalidate()
	onMain(func() { os.Frame() })
	if n.Placement() != p {
		t.Error("a foreign window moved the view")
	}
}

func TestNativeViewReentrantDispose(t *testing.T) {
	var v *NativeView
	w, _, s := contentWindow(t, func(c *ui.Context) { ui.HostView(c, v).Size(120, 40) })
	disposals := 0
	opts := fakeControl()
	opts.Update = func(NativeViewContext) {
		if v != nil {
			v.Destroy()
		}
	}
	opts.Dispose = func(NativeViewContext) { disposals++; v.Destroy() }
	var err error
	v, err = w.NewNativeView(opts)
	if err != nil {
		t.Fatal(err)
	}
	onMain(func() { s.Frame(); s.Frame() })
	if !v.IsDestroyed() || disposals != 1 {
		t.Errorf("destroyed=%v disposals=%d", v.IsDestroyed(), disposals)
	}
}

func TestNativeViewModalScope(t *testing.T) {
	var v *NativeView
	open := false
	w, _, s := contentWindow(t, func(c *ui.Context) {
		ui.HostView(c, v).Key("control").Size(100, 40)
		ui.Modal(c, &open, func() { ui.Button(c, "Close") })
	})
	var err error
	v, err = w.NewNativeView(fakeControl())
	if err != nil {
		t.Fatal(err)
	}
	onMain(func() { s.Frame() })
	v.Focus()
	n := s.NativeViews()[0]
	if !n.HasFocus() {
		t.Fatal("native control did not get focus")
	}
	w.Update(func() { open = true })
	onMain(func() { s.Frame() })
	if n.Placement().Visible || n.HasFocus() {
		t.Error("native view behind a modal retained input")
	}
	v.Focus()
	if n.HasFocus() {
		t.Error("native Focus escaped a modal")
	}
	w.Update(func() { open = false })
	onMain(func() { s.Frame() })
	if !n.Placement().Visible || !n.HasFocus() {
		t.Error("closing the modal did not restore native focus")
	}
}

func TestNativeViewDisposeClosesWindow(t *testing.T) {
	w, _, s := contentWindow(t, func(*ui.Context) {})
	opts := fakeControl()
	disposals := 0
	opts.Dispose = func(NativeViewContext) { disposals++; w.Destroy() }
	v, err := w.NewNativeView(opts)
	if err != nil {
		t.Fatal(err)
	}
	v.Destroy()
	if !w.IsDestroyed() || !v.IsDestroyed() || !s.NativeViews()[0].IsClosed() || disposals != 1 {
		t.Errorf("window=%v view=%v disposals=%d", w.IsDestroyed(), v.IsDestroyed(), disposals)
	}
}
