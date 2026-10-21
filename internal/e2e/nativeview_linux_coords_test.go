//go:build linux && (amd64 || arm64)

package e2e

import (
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"testing"
	"unsafe"

	"github.com/ebitengine/purego"

	"github.com/egoist/mygo"
)

// The press a native view reports has to be the point the user pressed, in
// the control's own DIPs, and not the raw coordinates of the GdkWindow that
// GTK delivered the event on. A GtkEntry is windowless and makes a private
// text-area GdkWindow, a child of the window of the widget it sits in, at its
// content allocation: inset from the entry by its border and padding
// (gtkentry.c, and gdk_event_get_coords answers "the event window relative
// x/y coordinates from an event",
// https://docs.gtk.org/gdk3/method.Event.get_coords.html). With a zero inset
// the two spaces agree, so this test forces a large padding and then presses
// a known point of the text area.
//
// It runs at GDK_SCALE 1 and 2: GTK reads GDK_SCALE when it initializes the
// screen, so each scale is this test binary again, in its own process, with
// the scale in its environment — the subprocess pattern the other e2e tests
// use. Everything the fix touches is in logical units, so both scales must
// report the same DIPs.

const (
	// nvCoordScaleEnv marks the child process and holds the scale it runs
	// at; unset means this is the parent, which starts one child per scale.
	nvCoordScaleEnv = "MYGO_E2E_NATIVEVIEW_SCALE"

	// nvCoordPadV and nvCoordPadH are the entry padding the test forces. The
	// text area's inset is what a fixture that forwarded the raw event
	// coordinates would be wrong by, so it has to be far beyond the rounding
	// this test allows.
	nvCoordPadV = 18
	nvCoordPadH = 24

	// nvCoordTolerance is how far the reported point may be from the point
	// clicked: XTest moves the pointer in whole device pixels and GDK divides
	// them by the scale factor, so a scale of 2 can land half a DIP out. The
	// correction under test is at least nvCoordPadV DIPs.
	nvCoordTolerance = 1.5
)

// TestNativeViewPointerCoordinates presses a known point of the control's
// text area and checks that the app is told that very point, in the control's
// own DIPs, at GDK_SCALE 1 and 2.
func TestNativeViewPointerCoordinates(t *testing.T) {
	if os.Getenv(nvCoordScaleEnv) == "" {
		for _, scale := range []string{"1", "2"} {
			nvCoordinateAtScale(t, scale)
		}
		return
	}
	nvCoordinatePress(t)
}

// nvCoordinateAtScale runs TestNativeViewPointerCoordinates again in its own
// process with GDK_SCALE set, and reports what that process said when it
// failed.
func nvCoordinateAtScale(t *testing.T, scale string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestNativeViewPointerCoordinates$")
	cmd.Env = append(os.Environ(), nvCoordScaleEnv+"="+scale, "GDK_SCALE="+scale)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("GDK_SCALE=%s: %v\n%s", scale, err, out)
	}
}

// nvCoordinatePress is the test's body, in the process that runs at the scale
// nvCoordScaleEnv holds.
func nvCoordinatePress(t *testing.T) {
	scale := os.Getenv(nvCoordScaleEnv)
	n := newNativeViewTest(t)
	f, ok := n.fixture.(*gtkFixture)
	if !ok || f.entry == 0 {
		t.Skip("no native view fixture on this platform")
	}
	// The process has to be at the scale it claims: otherwise the run says
	// nothing about that scale.
	if got, want := gtkScaleFactor(f.entry), nvScaleValue(t, scale); got != want {
		t.Fatalf("GDK_SCALE=%s: gtk_widget_get_scale_factor says %d, want %d", scale, got, want)
	}
	if err := gtkPadEntry(f.entry, nvCoordPadV, nvCoordPadH); err != nil {
		t.Fatalf("padding the entry: %v", err)
	}
	// Wait for GTK to restyle and re-place the text area — a condition, not a
	// clock: adding the provider is noticed by the next style pass, not by the
	// call itself, and until it has been, a press lands on the inset the theme
	// alone gives. That inset is not zero, so waiting for a non-zero one would
	// accept the pre-provider allocation and race the restyle it is meant to
	// wait for: wait for the inset the padding this test asked for is worth.
	var inset gtkAlloc
	eventually(t, "the entry's text area inset by its padding", func() bool {
		inset, _ = gtkTextAreaRect(f.entry)
		return inset.X >= nvCoordPadH && inset.Y >= nvCoordPadV
	})
	if inset.X < nvCoordPadH/2 || inset.Y < nvCoordPadV/2 {
		t.Fatalf("the entry's text area starts at (%d, %d) inside the entry, want at least (%d, %d) for the padding the test asked for",
			inset.X, inset.Y, nvCoordPadH/2, nvCoordPadV/2)
	}
	box, visible := f.Box()
	if visible.Width <= 0 || visible.Height <= 0 {
		t.Skip("the control is not visible")
	}
	// The middle of the text area, in the surface's DIPs: a press there is
	// delivered on the text-area GdkWindow, so the event's coordinates differ
	// from the control's own DIPs by exactly the inset above.
	px := float64(box.X) + float64(inset.X) + float64(inset.Width)/2
	py := float64(box.Y) + float64(inset.Y) + float64(inset.Height)/2
	if px < float64(visible.X) || py < float64(visible.Y) ||
		px > float64(visible.X+visible.Width) || py > float64(visible.Y+visible.Height) {
		t.Fatalf("the middle of the text area (%v, %v) is not in the part of the control the window shows %+v", px, py, visible)
	}
	before := f.Reports().Presses()
	n.click(px, py)
	eventually(t, "the press reported", func() bool { return f.Reports().Presses() > before })
	gotX, gotY := f.Reports().At()
	wantX, wantY := px-float64(box.X), py-float64(box.Y)
	if math.Abs(gotX-wantX) > nvCoordTolerance || math.Abs(gotY-wantY) > nvCoordTolerance {
		t.Errorf("GDK_SCALE=%s: the press at (%v, %v) of the surface was reported at (%v, %v); want the control's own DIPs (%v, %v), the press less the control's origin %+v, within %v DIP",
			scale, px, py, gotX, gotY, wantX, wantY, box, float64(nvCoordTolerance))
	}
}

// nvScaleValue is the scale GDK_SCALE asks for.
func nvScaleValue(t *testing.T, scale string) int32 {
	t.Helper()
	switch scale {
	case "1":
		return 1
	case "2":
		return 2
	}
	t.Fatalf("unexpected %s=%q", nvCoordScaleEnv, scale)
	return 0
}

// The entry's padding and its text-area window are this test's own business,
// so its bindings live here: the fixture needs only the coordinates GDK
// reports for the events it is sent.
var (
	gtkCoordOnce sync.Once
	gtkCoordErr  error

	gtkCSSProviderNew     func() uintptr
	gtkCSSProviderData    func(provider uintptr, data uintptr, length int64, err uintptr) bool
	gtkStyleAddProvider   func(context, provider uintptr, priority uint32)
	gtkWidgetStyleContext func(widget uintptr) uintptr
	gtkEntryTextArea      func(entry uintptr, rect *gtkAlloc)
	gtkWidgetScaleFactor  func(widget uintptr) int32

	// gtkCoordProviders keeps every provider this test made: a style context
	// uses a provider without taking over the reference its maker holds, and
	// the providers have to outlive the entry's restyle.
	gtkCoordProviders []uintptr
)

// gtkStylePriorityApplication is GTK_STYLE_PROVIDER_PRIORITY_APPLICATION: the
// provider this test adds to the entry is placed above the theme's own.
const gtkStylePriorityApplication = 600

// gtkCoordLoad binds the GTK functions this file uses, once.
func gtkCoordLoad() error {
	gtkCoordOnce.Do(func() {
		gtk, err := purego.Dlopen("libgtk-3.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			gtkCoordErr = fmt.Errorf("e2e: cannot open GTK3: %w", err)
			return
		}
		for _, f := range []struct {
			fn   any
			name string
		}{
			{&gtkCSSProviderNew, "gtk_css_provider_new"},
			{&gtkCSSProviderData, "gtk_css_provider_load_from_data"},
			{&gtkStyleAddProvider, "gtk_style_context_add_provider"},
			{&gtkWidgetStyleContext, "gtk_widget_get_style_context"},
			{&gtkEntryTextArea, "gtk_entry_get_text_area"},
			{&gtkWidgetScaleFactor, "gtk_widget_get_scale_factor"},
		} {
			addr, err := purego.Dlsym(gtk, f.name)
			if err != nil {
				gtkCoordErr = fmt.Errorf("e2e: %s: %w", f.name, err)
				return
			}
			purego.RegisterFunc(f.fn, addr)
		}
	})
	return gtkCoordErr
}

// gtkScaleFactor is the scale factor GTK uses for the widget: the GDK_SCALE
// the process was started with, as GTK read it when the screen was made.
func gtkScaleFactor(widget uintptr) (scale int32) {
	mygo.RunOnMain(func() {
		if gtkCoordLoad() == nil && widget != 0 {
			scale = gtkWidgetScaleFactor(widget)
		}
	})
	return scale
}

// gtkTextAreaRect is the entry's text area — the child GdkWindow that
// receives the presses (gtk_entry_get_text_area) — in the entry's own DIPs:
// the accessor answers priv->text_allocation less the entry's own allocation
// (gtkentry.c), which is exactly the inset the padding and border put between
// the entry and the window a press is delivered on.
func gtkTextAreaRect(entry uintptr) (rect gtkAlloc, ok bool) {
	mygo.RunOnMain(func() {
		if entry == 0 || gtkCoordLoad() != nil {
			return
		}
		var area gtkAlloc
		gtkEntryTextArea(entry, &area)
		rect, ok = area, true
	})
	return rect, ok
}

// gtkPadEntry gives the entry's own style context a provider holding a
// padding, so its text area sits well inside it. GtkEntry has no padding API;
// the padding is CSS.
//
// The provider also turns the transition off. The theme animates the padding
// of the widgets it styles, so a padding change does not land in one step:
// the text area passes through the insets between the theme's own and the one
// asked for. The pointer test presses the middle of the text area, so it wants
// the padded layout observable, not animated — and a press during the
// animation would be placed from an allocation that is still moving.
func gtkPadEntry(entry uintptr, vertical, horizontal int) error {
	if err := gtkCoordLoad(); err != nil {
		return err
	}
	css := fmt.Sprintf("entry { padding: %dpx %dpx; transition: none; }", vertical, horizontal)
	var err error
	mygo.RunOnMain(func() {
		provider := gtkCSSProviderNew()
		if provider == 0 {
			err = errors.New("gtk_css_provider_new returned NULL")
			return
		}
		text := append([]byte(css), 0)
		if !gtkCSSProviderData(provider, uintptr(unsafe.Pointer(&text[0])), int64(len(css)), 0) {
			err = fmt.Errorf("gtk_css_provider_load_from_data(%q) failed", css)
			return
		}
		runtime.KeepAlive(text)
		gtkStyleAddProvider(gtkWidgetStyleContext(entry), provider, gtkStylePriorityApplication)
		gtkCoordProviders = append(gtkCoordProviders, provider)
	})
	return err
}
