//go:build linux && (amd64 || arm64)

package linux

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"unsafe"

	"github.com/egoist/mygo/internal/platform"
)

// The tests here are the ledger of what MyGo does with GTK when it makes
// a native view: they replace the GTK entry points (gtkStub) with stubs
// that record the calls, so the reference bookkeeping, the refusal of the
// controls MyGo does not host, and what the window's close takes down can
// be checked without a display. They assert MyGo's calls, never what GTK
// does with them: only the real toolkit, in internal/e2e, says that.

// The handles the tests use, distinct so that the ledger reads by the
// role each pointer has.
const (
	testWin     = ptr(0x1000)
	testArea    = ptr(0x2000)
	testWeb     = ptr(0x3000)
	testLayers  = ptr(0x4000)
	testControl = ptr(0x5000)
	testWrap    = ptr(0x6000)
	testBox     = ptr(0x7000)
)

// fakeAdapter is the app's adapter of a native view, counting what MyGo
// calls on it. log, when it is set, writes its destroy into the stub's
// ledger, so that the ledger says when the app destroyed its control.
// onHandle, when it is set, runs while MyGo reads the control: the app's
// code, which may close the window under the creation.
type fakeAdapter struct {
	handle    ptr
	destroyed int
	focused   int
	log       func()
	onHandle  func()
}

func (f *fakeAdapter) NativeHandle() uintptr {
	if f.onHandle != nil {
		f.onHandle()
	}
	return uintptr(f.handle)
}
func (f *fakeAdapter) Focus(bool) { f.focused++ }
func (f *fakeAdapter) Destroy() {
	f.destroyed++
	if f.log != nil {
		f.log()
	}
}

// gtkStub is the GTK the native view code sees in a test: an ordered
// ledger of the calls (ops), and the answers the tests set up (layouts,
// parents, children, types). Only the calls the ownership ledger is about
// are recorded; sizing calls and focus are not.
type gtkStub struct {
	ops   []string
	keep  []any // the memory the stub hands out, to keep it alive
	types map[string]*gtkStubType
	// layouts are what gtk_layout_new hands out, one per call.
	layouts []ptr
	// parents is what gtk_widget_get_parent answers per widget.
	parents map[ptr]ptr
	// children is the children of a container, which gtk_layout_put adds
	// to like GTK's container does.
	children map[ptr][]ptr
	// isa is what g_type_check_instance_is_a answers: the types, by name,
	// each instance (or handle) is.
	isa map[ptr]map[string]bool
	// inDestruction is what gtk_widget_in_destruction answers per widget.
	inDestruction map[ptr]bool
}

// gtkStubType is a GType the stubs answer with, and the NUL-terminated
// name g_type_name returns for it.
type gtkStubType struct {
	value uintptr
	name  ptr
}

// stubInstance is a GTypeInstance: instanceType reads its g_class, and
// its class' g_type.
type stubInstance struct {
	class *stubClass
}

type stubClass struct {
	gtype uintptr
}

// stubGList is the GList gtk_container_get_children hands out: the child
// data, then the next node (glist.h).
type stubGList struct {
	data, next ptr
}

// newGTKStub replaces the GTK entry points the native view code calls
// with stubs, and puts them back when the test ends.
func newGTKStub(t *testing.T) *gtkStub {
	t.Helper()
	g := &gtkStub{
		types:         map[string]*gtkStubType{},
		parents:       map[ptr]ptr{},
		children:      map[ptr][]ptr{},
		isa:           map[ptr]map[string]bool{},
		inDestruction: map[ptr]bool{},
	}
	// The layers and the view bindings would load the libraries: the
	// stubs stand in for them.
	stubGTKFunc(t, &layersLoaded, true)
	stubGTKFunc(t, &nativeViewsLoaded, true)
	stubGTKFunc(t, &gtkWidgetGetType, func() uintptr { return g.gtype("GtkWidget") })
	stubGTKFunc(t, &gtkWindowGetType, func() uintptr { return g.gtype("GtkWindow") })
	stubGTKFunc(t, &gtkWindowGetFocus, func(ptr) ptr { return 0 })
	stubGTKFunc(t, &gtkWidgetInDestruction, func(w ptr) bool { return g.inDestruction[w] })
	stubGTKFunc(t, &gtkGLAreaGetType, func() uintptr { return g.gtype("GtkGLArea") })
	stubGTKFunc(t, &gtkSocketGetType, func() uintptr { return g.gtype("GtkSocket") })
	stubGTKFunc(t, &gtkPlugGetType, func() uintptr { return g.gtype("GtkPlug") })
	stubGTKFunc(t, &gTypeName, g.typeName)
	stubGTKFunc(t, &gTypeCheckInstanceIsA, g.instanceIsA)
	stubGTKFunc(t, &gObjectRefSink, func(o ptr) ptr {
		g.op("g_object_ref_sink %#x", o)
		return o
	})
	stubGTKFunc(t, &gObjectUnref, func(o ptr) { g.op("g_object_unref %#x", o) })
	stubGTKFunc(t, &gtkWidgetDestroy, func(w ptr) { g.op("gtk_widget_destroy %#x", w) })
	stubGTKFunc(t, &gListFree, func(ptr) { g.op("g_list_free") })
	stubGTKFunc(t, &gtkLayoutNew, func(h, v ptr) ptr { return g.layout() })
	stubGTKFunc(t, &gtkLayoutPut, func(l, child ptr, x, y int32) {
		g.op("gtk_layout_put %#x %#x", l, child)
		g.children[l] = append(g.children[l], child)
		g.parents[child] = l
	})
	stubGTKFunc(t, &gtkWidgetGetParent, func(w ptr) ptr { return g.parents[w] })
	stubGTKFunc(t, &gtkWidgetGetParentWindow, func(ptr) ptr { return 0 })
	stubGTKFunc(t, &gtkContainerGetChildren, func(c ptr) ptr {
		g.op("gtk_container_get_children %#x", c)
		return g.childList(c)
	})
	stubGTKFunc(t, &gtkContainerRemove, func(c, w ptr) { g.op("gtk_container_remove %#x %#x", c, w) })
	stubGTKFunc(t, &gtkWidgetSetNoShowAll, func(ptr, bool) {})
	stubGTKFunc(t, &gtkWidgetSetSizeRequest, func(ptr, int32, int32) {})
	stubGTKFunc(t, &gtkWidgetGetScaleFactor, func(ptr) int32 { return 1 })
	stubGTKFunc(t, &gtkWidgetGrabFocus, func(ptr) {})
	stubGTKFunc(t, &gtkWidgetGetVisible, func(ptr) bool { return true })
	stubGTKFunc(t, &gdkWindowRestack, func(w, sibling ptr, above bool) {})
	return g
}

// stubGTKFunc replaces a native entry point of the package for the test,
// and puts the old one back when the test ends.
func stubGTKFunc[T any](t *testing.T, v *T, f T) {
	t.Helper()
	old := *v
	*v = f
	t.Cleanup(func() { *v = old })
}

// layout is the next layout gtk_layout_new hands out.
func (g *gtkStub) layout() ptr {
	if len(g.layouts) == 0 {
		return 0
	}
	l := g.layouts[0]
	g.layouts = g.layouts[1:]
	g.op("gtk_layout_new")
	return l
}

// op records a call MyGo made.
func (g *gtkStub) op(format string, args ...any) {
	g.ops = append(g.ops, fmt.Sprintf(format, args...))
}

// ops returns how many recorded calls have the prefix.
func (g *gtkStub) count(prefix string) int {
	n := 0
	for _, op := range g.ops {
		if strings.HasPrefix(op, prefix) {
			n++
		}
	}
	return n
}

// clearOps forgets the calls recorded so far, for a test that walks
// through more than one of them.
func (g *gtkStub) clearOps() { g.ops = nil }

// gtype is the GType of a type name, made once.
func (g *gtkStub) gtype(name string) uintptr {
	if ty, ok := g.types[name]; ok {
		return ty.value
	}
	b := append([]byte(name), 0)
	g.keep = append(g.keep, b)
	ty := &gtkStubType{value: uintptr(len(g.types)+1) << 4, name: ptr(unsafe.Pointer(&b[0]))}
	g.types[name] = ty
	return ty.value
}

// typeName answers g_type_name.
func (g *gtkStub) typeName(t uintptr) ptr {
	for _, ty := range g.types {
		if ty.value == t {
			return ty.name
		}
	}
	return 0
}

// instanceIsA answers g_type_check_instance_is_a: the types the instance
// was registered with, by name.
func (g *gtkStub) instanceIsA(instance ptr, typ uintptr) bool {
	name := ""
	for n, ty := range g.types {
		if ty.value == typ {
			name = n
			break
		}
	}
	return name != "" && g.isa[instance][name]
}

// widget registers a handle as an instance of the types, without memory
// of its own: enough for the checks, not for an error that names it.
func (g *gtkStub) widget(h ptr, types ...string) {
	set := map[string]bool{}
	for _, ty := range types {
		set[ty] = true
		g.gtype(ty)
	}
	g.isa[h] = set
}

// instance is a handle for a real GTypeInstance of the types, so that an
// error can name it (typeName reads the class' GType).
func (g *gtkStub) instance(types ...string) ptr {
	inst := &stubInstance{class: &stubClass{gtype: g.gtype(types[0])}}
	g.keep = append(g.keep, inst)
	h := ptr(unsafe.Pointer(inst))
	g.widget(h, types...)
	return h
}

// childList is the GList of the children of a container, the memory of
// which the native view code walks.
func (g *gtkStub) childList(c ptr) ptr {
	kids := g.children[c]
	if len(kids) == 0 {
		return 0
	}
	nodes := make([]*stubGList, 0, len(kids))
	for _, kid := range kids {
		n := &stubGList{data: kid}
		if len(nodes) > 0 {
			nodes[len(nodes)-1].next = ptr(unsafe.Pointer(n))
		}
		nodes = append(nodes, n)
	}
	for _, n := range nodes {
		g.keep = append(g.keep, n)
	}
	return ptr(unsafe.Pointer(nodes[0]))
}

// nativeViewProvider is the platform.NativeViewSurface the core gets for a
// surface: the provider NativeViewSurface answers, through which the core
// makes and places the window's native views.
func nativeViewProvider(t *testing.T, s platform.Surface) platform.NativeViewSurface {
	t.Helper()
	ns, ok := NativeViewSurface(s)
	if !ok || ns == nil {
		t.Fatalf("NativeViewSurface(%T) answered no provider", s)
	}
	return ns
}

// viewsOf is the native views the surface's extension holds: what the
// feature keeps of them, which no always-linked field of the surface or
// the window names any more (surface.go, window.go).
func viewsOf(s *surface) []placedNative {
	p := s.extOf()
	if p == nil {
		return nil
	}
	return p.views
}

// newFakeView makes a native view of a fake adapter in a fake window,
// with the layouts the ledger expects, through the provider the core
// reaches (NativeViewSurface).
func newFakeView(t *testing.T, g *gtkStub) (*window, *surface, *fakeAdapter, platform.NativeView) {
	t.Helper()
	g.layouts = []ptr{testWrap, testBox}
	w := &window{win: testWin}
	s := &surface{w: w, area: testArea, web: testWeb, layers: testLayers}
	a := &fakeAdapter{handle: testControl, log: func() { g.op("adapter destroy") }}
	g.widget(testControl, "GtkEntry", "GtkWidget")
	v, err := nativeViewProvider(t, s).NewNativeView(func(platform.NativeViewHost) (platform.NativeView, error) { return a, nil })
	if err != nil {
		t.Fatalf("NewNativeView: %v", err)
	}
	return w, s, a, v
}

// diffOps names the first call of the ledger that is not what the test
// expects, "" when they match.
func diffOps(want, got []string) string {
	for i := 0; i < len(want) && i < len(got); i++ {
		if want[i] != got[i] {
			return fmt.Sprintf("call %d = %q, want %q\nall: %v", i, got[i], want[i], got)
		}
	}
	if len(want) != len(got) {
		return fmt.Sprintf("%d calls, want %d\nall: %v", len(got), len(want), got)
	}
	return ""
}

// TestNativeViewLayoutOwnership is the ledger of the references MyGo and
// GTK hold on the two layouts of a view. Each floating layout of
// gtk_layout_new is sunk exactly once for MyGo, its container adds the
// second reference, and the view goes down with exactly the reference
// MyGo owns: gtk_widget_destroy for the container's, g_object_unref for
// MyGo's. The app's control comes out of MyGo's box before the box is
// destroyed, the adapter destroys it, and MyGo never destroys it. A
// second Destroy does nothing.
func TestNativeViewLayoutOwnership(t *testing.T) {
	g := newGTKStub(t)
	_, s, a, v := newFakeView(t, g)
	if v.NativeHandle() != uintptr(testControl) {
		t.Errorf("NativeHandle = %#x, want %#x", v.NativeHandle(), testControl)
	}
	if len(viewsOf(s)) != 1 {
		t.Fatalf("the window has %d native views, want 1", len(viewsOf(s)))
	}
	want := []string{
		"gtk_layout_new", "g_object_ref_sink 0x6000",
		"gtk_layout_new", "g_object_ref_sink 0x7000",
		"gtk_layout_put 0x6000 0x7000", // the box in the clip
		"gtk_layout_put 0x3000 0x6000", // the clip under the area
		"gtk_layout_put 0x7000 0x5000", // the app's control in the box
	}
	if d := diffOps(want, g.ops); d != "" {
		t.Fatalf("while the view is up: %s", d)
	}
	v.Destroy()
	want = append(want,
		"gtk_container_get_children 0x7000",
		"gtk_container_remove 0x7000 0x5000",
		"g_list_free",
		"gtk_widget_destroy 0x7000", // the box: its container drops its reference
		"g_object_unref 0x7000",     // MyGo's reference to the box
		"gtk_widget_destroy 0x6000",
		"g_object_unref 0x6000",
		"adapter destroy", // the app's control is the app's to destroy
	)
	if d := diffOps(want, g.ops); d != "" {
		t.Fatalf("while the view goes down: %s", d)
	}
	if a.destroyed != 1 {
		t.Errorf("the adapter was destroyed %d times, want 1", a.destroyed)
	}
	if len(viewsOf(s)) != 0 {
		t.Errorf("the extension still lists the view: %v", viewsOf(s))
	}
	if nv, ok := v.(*nativeView); !ok || !nv.closed || nv.NativeHandle() != 0 {
		t.Errorf("the view is not closed after Destroy: %+v", v)
	}
	// Once is once: a second Destroy takes nothing down again.
	v.Destroy()
	if d := diffOps(want, g.ops); d != "" {
		t.Errorf("a second Destroy changed the ledger: %s", d)
	}
	if a.destroyed != 1 {
		t.Errorf("the adapter was destroyed %d times, want 1", a.destroyed)
	}
}

// TestNativeViewDropOfABoxTheWindowIsDestroying is the other half of the
// ledger: a box the window is already destroying (gtk_widget_in_destruction)
// destroyed its children itself, so MyGo does not walk its child list
// any more, and still drops the references it owns.
func TestNativeViewDropOfABoxTheWindowIsDestroying(t *testing.T) {
	g := newGTKStub(t)
	_, s, a, v := newFakeView(t, g)
	g.clearOps()
	g.inDestruction[testBox] = true
	v.Destroy()
	want := []string{
		"gtk_widget_destroy 0x7000",
		"g_object_unref 0x7000",
		"gtk_widget_destroy 0x6000",
		"g_object_unref 0x6000",
		"adapter destroy",
	}
	if d := diffOps(want, g.ops); d != "" {
		t.Fatalf("destroying a box the window destroys: %s", d)
	}
	if a.destroyed != 1 || len(viewsOf(s)) != 0 {
		t.Errorf("adapter destroyed %d times, %d views left", a.destroyed, len(viewsOf(s)))
	}
}

// TestNativeViewRefusesControlsItDoesNotHost checks that MyGo refuses the
// types it does not host, subclasses included, with an error that names
// the control, and that it takes its own layouts down and leaves the
// adapter to the core.
func TestNativeViewRefusesControlsItDoesNotHost(t *testing.T) {
	for _, tc := range []struct {
		name  string
		types []string
		want  string
	}{
		{"GtkGLArea", []string{"GtkGLArea", "GtkWidget"}, "is a GtkGLArea: mygo does not host a control that draws with OpenGL"},
		{"a subclass of GtkGLArea", []string{"GtkMyOwnArea", "GtkGLArea", "GtkWidget"}, "is a GtkMyOwnArea: mygo does not host a control that draws with OpenGL"},
		{"GtkSocket", []string{"GtkSocket", "GtkWidget"}, "another process's window"},
		{"GtkPlug", []string{"GtkPlug", "GtkWidget"}, "another window's own widget"},
		{"GtkWindow", []string{"GtkWindow", "GtkWidget"}, "is a GtkWindow"},
		{"something that is no widget", []string{"GtkSomething"}, "not a GtkWidget"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newGTKStub(t)
			g.layouts = []ptr{testWrap, testBox}
			h := g.instance(tc.types...)
			w := &window{win: testWin}
			s := &surface{w: w, area: testArea, web: testWeb, layers: testLayers}
			a := &fakeAdapter{handle: h}
			_, err := nativeViewProvider(t, s).NewNativeView(func(platform.NativeViewHost) (platform.NativeView, error) { return a, nil })
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("NewNativeView error = %v, want one with %q", err, tc.want)
			}
			if a.destroyed != 0 {
				t.Errorf("the platform destroyed the adapter: the core owns that")
			}
			if len(viewsOf(s)) != 0 {
				t.Errorf("the extension lists a refused control: %v", viewsOf(s))
			}
			if n := g.count("g_object_ref_sink"); n != 2 {
				t.Errorf("sunk %d references, want one per layout (2)", n)
			}
			if n := g.count("g_object_unref"); n != 2 {
				t.Errorf("dropped %d references, want one per layout (2)", n)
			}
			if g.count("gtk_widget_destroy 0x5000") != 0 {
				t.Errorf("the platform destroyed the app's control")
			}
		})
	}
}

// TestNativeViewFactoryOutcomes checks every way the app's factory can
// end without a view: MyGo takes its own layouts down, never destroys the
// adapter the core returns on an error, and adds nothing to the window.
func TestNativeViewFactoryOutcomes(t *testing.T) {
	boom := errors.New("mygo: test: the factory failed")
	for _, tc := range []struct {
		name string
		make func(w *window, a *fakeAdapter) (platform.NativeView, error)
		want string
	}{
		{
			name: "the factory fails",
			make: func(w *window, a *fakeAdapter) (platform.NativeView, error) { return a, boom },
			want: boom.Error(),
		},
		{
			name: "the factory makes none",
			make: func(w *window, a *fakeAdapter) (platform.NativeView, error) { return nil, nil },
			want: "the native view's factory made none",
		},
		{
			name: "the factory makes no widget",
			make: func(w *window, a *fakeAdapter) (platform.NativeView, error) {
				a.handle = 0
				return a, nil
			},
			want: "made no GtkWidget",
		},
		{
			name: "the window closes while the factory runs",
			make: func(w *window, a *fakeAdapter) (platform.NativeView, error) {
				w.closed = true
				return a, nil
			},
			want: "closed while its factory ran",
		},
		{
			name: "the window closes while the handle is read",
			make: func(w *window, a *fakeAdapter) (platform.NativeView, error) {
				// NativeHandle is the app's code as much as the factory
				// is: it can close the window under the creation before it
				// answers.
				a.onHandle = func() { w.closed = true }
				return a, nil
			},
			want: "closed while its handle was read",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newGTKStub(t)
			g.layouts = []ptr{testWrap, testBox}
			g.widget(testControl, "GtkEntry", "GtkWidget")
			w := &window{win: testWin}
			s := &surface{w: w, area: testArea, web: testWeb, layers: testLayers}
			a := &fakeAdapter{handle: testControl}
			_, err := nativeViewProvider(t, s).NewNativeView(func(platform.NativeViewHost) (platform.NativeView, error) {
				return tc.make(w, a)
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("NewNativeView error = %v, want one with %q", err, tc.want)
			}
			if len(viewsOf(s)) != 0 {
				t.Errorf("the extension lists a view that was never made: %v", viewsOf(s))
			}
			if a.destroyed != 0 {
				t.Errorf("the platform destroyed the adapter: the core owns that")
			}
			if n := g.count("g_object_ref_sink"); n != 2 {
				t.Errorf("sunk %d references, want one per layout (2)", n)
			}
			if n := g.count("g_object_unref"); n != 2 {
				t.Errorf("dropped %d references, want one per layout (2)", n)
			}
			if g.count("gtk_layout_put 0x7000 0x5000") != 0 {
				t.Errorf("the app's control was added to a box that goes away")
			}
			if g.count("gtk_widget_destroy 0x5000") != 0 {
				t.Errorf("the platform destroyed the app's control")
			}
		})
	}
}

// fakeNativeView is a platform.NativeView that is no placedNative: a
// placement of one is not this backend's to place.
type fakeNativeView struct{}

func (fakeNativeView) NativeHandle() uintptr { return 0 }
func (fakeNativeView) Focus(bool)            {}
func (fakeNativeView) Destroy()              {}

// TestNativeViewSurfaceIsOptIn is the contract the core's opt-in relies
// on: the window's surface does not implement platform.NativeViewSurface,
// so no always-linked assertion of one can keep the native view
// implementation in an app that makes none, and NativeViewSurface is what
// answers the provider that does.
func TestNativeViewSurfaceIsOptIn(t *testing.T) {
	s := &surface{}
	if _, ok := any(s).(platform.NativeViewSurface); ok {
		t.Fatal("the surface itself implements platform.NativeViewSurface: an always-linked assertion of it would keep the whole feature linked")
	}
	if ns, ok := NativeViewSurface(nil); ok || ns != nil {
		t.Errorf("NativeViewSurface(nil) = %v, %v, want no provider", ns, ok)
	}
	ns := nativeViewProvider(t, s)
	if _, ok := ns.(*nativeViewSurface); !ok {
		t.Errorf("the provider of the surface is a %T, want the backend's own provider", ns)
	}
}

// TestNativeViewStateIsBehindTheExtension is the isolation the size of a
// webview-only app rests on: a surface that made no native view carries no
// native state at all (its extension is nil), the always-linked placement,
// shape and ordering paths answer without one, and the constructor of the
// first native view is what installs it, once.
func TestNativeViewStateIsBehindTheExtension(t *testing.T) {
	g := newGTKStub(t)
	w := &window{win: testWin}
	s := &surface{w: w, area: testArea, web: testWeb, layers: testLayers}
	if s.ext != nil || s.extOf() != nil {
		t.Fatal("a surface that made no native view carries native state")
	}
	if got := s.nativeAccessibles(); got != nil {
		t.Errorf("nativeAccessibles = %v with no extension, want none", got)
	}
	// The web-only path of the always-linked code runs with no extension,
	// installs none, and keeps no order of its own to invalidate.
	s.PlaceWebViews(nil)
	s.shapeInput()
	if s.ext != nil {
		t.Fatal("the web-only paths installed an extension")
	}
	if len(s.ordered) != 0 || s.orderDirty {
		t.Errorf("the surface keeps order state (ordered %v, orderDirty %t) with no native view", s.ordered, s.orderDirty)
	}

	// The constructor of the first native view installs the extension.
	g.layouts = []ptr{testWrap, testBox}
	w2 := &window{win: testWin}
	s2 := &surface{w: w2, area: testArea, web: testWeb, layers: testLayers}
	a := &fakeAdapter{handle: testControl}
	g.widget(testControl, "GtkEntry", "GtkWidget")
	if _, err := nativeViewProvider(t, s2).NewNativeView(func(platform.NativeViewHost) (platform.NativeView, error) {
		return a, nil
	}); err != nil {
		t.Fatalf("NewNativeView: %v", err)
	}
	p := s2.extOf()
	if p == nil {
		t.Fatal("the constructor of the native view did not install the extension")
	}
	if p != s2.placement() {
		t.Error("placement() installed a second extension")
	}
	if len(p.views) != 1 {
		t.Errorf("the extension holds %d views, want 1", len(p.views))
	}
}

// TestNativeViewWindowCloseHook checks the hook the provider installs for
// the window's teardown: a window that hosts a native view closes its
// views, and the app's adapter with them, through it, once, and a window
// that hosts none calls nothing of the feature.
func TestNativeViewWindowCloseHook(t *testing.T) {
	var none window
	if none.closeNativeViews != nil {
		t.Error("a window with no native view has a close hook: the webview-only teardown names the feature")
	}
	g := newGTKStub(t)
	w, s, a, v := newFakeView(t, g)
	if w.closeNativeViews == nil {
		t.Fatal("the provider did not install the window's close hook")
	}
	w.closeNativeViews()
	if a.destroyed != 1 {
		t.Errorf("the adapter was destroyed %d times, want 1", a.destroyed)
	}
	if len(viewsOf(s)) != 0 {
		t.Errorf("the extension still lists its view: %v", viewsOf(s))
	}
	if nv, ok := v.(*nativeView); !ok || !nv.closed {
		t.Errorf("the view is not closed after the window's teardown: %+v", v)
	}
}

// TestNativeViewPlacementThroughTheProvider checks the placement flow the
// core drives through the provider: the content's placement places a live
// view at its clip and frame, the always-linked placement code reads it
// through the placedNative interface (a placement of another surface's
// view, or of a view that is none of this backend's, is not placed), the
// views the content does not place are hidden and keep their state, and
// the accessibility of the content sees a shown view's control and no
// longer a hidden one's.
func TestNativeViewPlacementThroughTheProvider(t *testing.T) {
	g := newGTKStub(t)
	visible := map[ptr]bool{}
	moved := map[string]string{}
	stubGTKFunc(t, &gtkWidgetGetVisible, func(w ptr) bool { return visible[w] })
	stubGTKFunc(t, &gtkWidgetShow, func(w ptr) { visible[w] = true })
	stubGTKFunc(t, &gtkWidgetHide, func(w ptr) { visible[w] = false })
	stubGTKFunc(t, &gtkLayoutMove, func(l, w ptr, x, y int32) {
		moved[fmt.Sprintf("%#x in %#x", w, l)] = fmt.Sprintf("%d,%d", x, y)
	})
	stubGTKFunc(t, &gtkWidgetGetWindow, func(w ptr) ptr { return w })
	stubGTKFunc(t, &gtkWidgetGetAccessible, func(w ptr) ptr { return w })
	_, s, _, v := newFakeView(t, g)
	ns := nativeViewProvider(t, s)

	ns.PlaceNativeViews([]platform.NativeViewPlacement{{
		View:  v,
		Clip:  platform.RectF{X: 10, Y: 20, W: 30, H: 40},
		Frame: platform.RectF{X: 12, Y: 22, W: 26, H: 36},
	}})
	var placed int
	if p := s.extOf(); p != nil {
		placed = len(p.placed)
	}
	if placed != 1 {
		t.Fatalf("the surface placed %d views, want 1", placed)
	}
	if got := moved["0x6000 in 0x3000"]; got != "10,20" {
		t.Errorf("the clip moved to %q, want 10,20", got)
	}
	if got := moved["0x7000 in 0x6000"]; got != "2,2" {
		t.Errorf("the frame moved to %q in its clip, want 2,2", got)
	}
	if !visible[testWrap] {
		t.Error("a placed view's clip is not shown")
	}
	if got := s.nativeAccessibles(); len(got) != 1 || got[0] != testControl {
		t.Errorf("nativeAccessibles = %v, want the placed view's control %#x", got, testControl)
	}

	// A surface that is not the view's window does not place it.
	other := nativeViewProvider(t, &surface{w: &window{win: testWin}, area: testArea, web: testWeb, layers: testLayers})
	clear(moved)
	other.PlaceNativeViews([]platform.NativeViewPlacement{{View: v, Clip: platform.RectF{X: 1, Y: 2, W: 3, H: 4}}})
	if len(moved) != 0 {
		t.Errorf("another surface placed the view: %v", moved)
	}

	// A view the content no longer places is hidden, keeping its state.
	ns.PlaceNativeViews(nil)
	if visible[testWrap] {
		t.Error("a view the content placed no more is still shown")
	}
	if got := s.nativeAccessibles(); len(got) != 0 {
		t.Errorf("nativeAccessibles = %v, want none for a hidden view", got)
	}

	// A placement of a view that is not this backend's is not placed.
	ns.PlaceNativeViews([]platform.NativeViewPlacement{{View: fakeNativeView{}}})
	if len(moved) != 0 {
		t.Errorf("an unknown view was placed: %v", moved)
	}
}
