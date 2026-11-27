//go:build linux && !android && (amd64 || arm64)

package e2e

import (
	"image"
	"os"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/linux"
	"github.com/egoist/mygo/ui"
)

// GTK 3 tells a Wayland compositor what each paint of a window it
// composites with OpenGL changed in the window's coordinates unscaled,
// where EGL takes the buffer's pixels: at a scale of 2 the compositor
// learned of a change at the top of the window in its lower half, at half
// its size, and showed title buttons that flickered and a shadow missing
// on some sides (#1071). The window tells it what GTK repaints, in the
// surface's coordinates: a box that changes color within the GtkGLArea,
// below GTK's title bar and shadow, is in it. It runs on Wayland at a
// scale of 2 or more (weston --scale=2), with MYGO_GPU=1.
func TestGLDamageWayland(t *testing.T) {
	if os.Getenv("MYGO_GPU") != "1" {
		t.Skip("needs the GtkGLArea: MYGO_GPU=1")
	}
	var frames atomic.Int32
	var lit atomic.Bool
	box := ui.Rect{X: 300, Y: 200, W: 40, H: 30}
	view := func(c *ui.Context) {
		frames.Add(1)
		ui.Box(c).Fill().Background(ui.RGB(255, 255, 255)).Draw(func(pt *ui.Painter, r ui.Rect) {
			color := ui.RGB(200, 200, 200)
			if lit.Load() {
				color = ui.RGB(255, 0, 0)
			}
			pt.Fill(ui.Rect{X: r.X + box.X, Y: r.Y + box.Y, W: box.W, H: box.H}, color, 0)
		})
	}
	w := newWindow(t, mygo.WindowOptions{Title: "Damage", Width: 400, Height: 300, Content: ui.View(view)})
	eventually(t, "a frame with OpenGL", func() bool {
		how, _, _, _, _ := glSurface(w)
		return how == "opengl"
	})
	glDamage := func() (d []image.Rectangle, area image.Point, scale int, ok bool) {
		mygo.RunOnMain(func() { d, area, scale, ok = linux.TestGLDamage(w.NativeHandle()) })
		return d, area, scale, ok
	}
	_, area, scale, ok := glDamage()
	if !ok {
		t.Skip("GTK tells the compositor what changed itself but on Wayland")
	}
	if scale < 2 {
		t.Skipf("GTK tells the compositor what changed itself at a scale of %d", scale)
	}
	want := image.Rect(int(box.X), int(box.Y), int(box.X+box.W), int(box.Y+box.H)).Add(area)
	var damage []image.Rectangle
	before := frames.Load()
	w.Update(func() { lit.Store(true) })
	eventually(t, "the box's change told to the compositor", func() bool {
		d, _, _, _ := glDamage()
		damage = append(damage, d...)
		if frames.Load() == before {
			return false
		}
		for _, r := range damage {
			if want.In(r) {
				return true
			}
		}
		return false
	})
}

// A frame drawn on the GPU that changes a box repaints the box, not the
// window: asking a GtkGLArea to render repaints all of it, and GTK 3
// clears a CPU surface as large as what it repaints for each paint of a
// window it composites with OpenGL, milliseconds a frame for a large
// window. The frame is drawn before GTK paints, which repaints what it
// changed, and the area's framebuffer holds the frame as drawn whole. An
// allocation of the area, which has GTK make its buffers anew and render
// as it paints next, repaints all of it, whatever that paint was for. What
// GTK repaints is seen on Wayland at a scale of 2 or more (weston
// --scale=2), with MYGO_GPU=1, as TestGLDamageWayland sees it; elsewhere
// the test checks the framebuffer. Decorated, the area starts below GTK's
// title bar in the window's GdkWindow.
func TestGLRepaintsWhatChanged(t *testing.T) {
	if os.Getenv("MYGO_GPU") != "1" {
		t.Skip("needs the GtkGLArea: MYGO_GPU=1")
	}
	for _, frameless := range []bool{true, false} {
		name := "decorated"
		if frameless {
			name = "frameless"
		}
		t.Run(name, func(t *testing.T) { glRepaintsWhatChanged(t, frameless) })
	}
}

func glRepaintsWhatChanged(t *testing.T, frameless bool) {
	// changes counts the changes of lit, made on the main thread, and built
	// the changes the last frame built saw: GTK has the window build frames
	// by itself, as it allocates the area, which can come before a change
	// Update posts.
	var changes, built atomic.Int32
	// lit[i] lights boxes[i]: the first changes in frames the window asks
	// for, the second as GTK allocates the area.
	var lit [2]atomic.Bool
	boxes := [2]ui.Rect{{X: 300, Y: 200, W: 40, H: 30}, {X: 60, Y: 60, W: 40, H: 30}}
	white, red, gray := ui.RGB(255, 255, 255), ui.RGB(255, 0, 0), ui.RGB(200, 200, 200)
	colorOf := func(i int) ui.Color {
		if lit[i].Load() {
			return red
		}
		return gray
	}
	view := func(c *ui.Context) {
		built.Store(changes.Load())
		ui.Box(c).Fill().Background(white).Draw(func(pt *ui.Painter, r ui.Rect) {
			for i, b := range boxes {
				pt.Fill(ui.Rect{X: r.X + b.X, Y: r.Y + b.Y, W: b.W, H: b.H}, colorOf(i), 0)
			}
		})
	}
	rect := func(b ui.Rect) image.Rectangle {
		return image.Rect(int(b.X), int(b.Y), int(b.X+b.W), int(b.Y+b.H))
	}
	w := newWindow(t, mygo.WindowOptions{Title: "Repaint", Width: 600, Height: 400, Frameless: frameless, Content: ui.View(view)})
	eventually(t, "a frame with OpenGL", func() bool {
		how, _, _, _, _ := glSurface(w)
		return how == "opengl"
	})
	glDamage := func() (d []image.Rectangle, area image.Point, scale int, ok bool) {
		mygo.RunOnMain(func() { d, area, scale, ok = linux.TestGLDamage(w.NativeHandle()) })
		return d, area, scale, ok
	}
	_, area, scale, seen := glDamage()
	seen = seen && scale >= 2
	// What GPU damage grows by, rounded out to whole DIPs.
	near := rect(boxes[0]).Add(area).Inset(-8)
	// painted waits for a frame built with change n, and on Wayland for a
	// paint over all of box i, and returns the paints over it.
	painted := func(n int32, i int) []image.Rectangle {
		boxAt := rect(boxes[i]).Add(area)
		var over []image.Rectangle
		eventually(t, "the box's frame painted", func() bool {
			if built.Load() < n {
				return false
			}
			if !seen {
				return true
			}
			d, _, _, _ := glDamage()
			for _, r := range d {
				if r.Overlaps(boxAt) {
					over = append(over, r)
				}
			}
			return slices.ContainsFunc(over, boxAt.In)
		})
		return over
	}
	// GTK repaints with what a frame changed what the frames before it
	// changed in the buffer it paints into again (EGL's buffer age): after
	// the window's first paints, whole, and GTK's own of its title bar, the
	// box's shrink to the box within a few frames.
	for i := 0; ; i++ {
		n := changes.Load() + 1
		w.Update(func() {
			lit[0].Store(!lit[0].Load())
			changes.Store(n)
		})
		over := painted(n, 0)
		if !seen {
			break
		}
		if last := over[len(over)-1]; last.In(near) {
			break
		} else if i == 10 {
			t.Fatalf("the box's frames repainted %v, beyond the box's %v", last, near)
		}
	}
	// The second box changes in the frame GTK renders as it paints the
	// bottom right corner of the area after allocating it: the paint is of
	// all of it, not only of the corner and of what the paints before
	// changed (buffer age), around the first box.
	n := changes.Load() + 1
	s := max(scale, 1)
	corner := image.Rect(596*s, 396*s, 600*s, 400*s)
	mygo.RunOnMain(func() {
		lit[1].Store(true)
		changes.Store(n)
		linux.TestAllocateAndPaint(w.NativeHandle(), corner)
	})
	painted(n, 1)
	// The framebuffer holds what a frame drawn whole would: white but for
	// the boxes.
	eventually(t, "the frame drawn whole in the area's framebuffer", func() bool {
		_, pix, width, height, _ := glSurface(w)
		if width == 0 || len(pix) < width*height*4 {
			return false
		}
		scale := width / 600
		for y := range height {
			for x := range width {
				want := white
				for i, b := range boxes {
					if image.Pt(x/scale, y/scale).In(rect(b)) {
						want = colorOf(i)
					}
				}
				i := (y*width + x) * 4 // BGRA
				if pix[i+2] != want.R || pix[i+1] != want.G || pix[i] != want.B {
					return false
				}
			}
		}
		return true
	})
}
