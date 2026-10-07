package e2e

import (
	"bytes"
	"image/color"
	"image/png"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// TestContentWindowRTL drives mirrored native UI through its actual surface
// and accessibility callbacks. No desktop locale preference is changed.
func TestContentWindowRTL(t *testing.T) {
	var frames, clicks atomic.Int32
	var first, second, slider, popup, tabs *ui.Element
	selected := 0
	volume := 30.0
	open := true
	w := newWindow(t, mygo.WindowOptions{Title: "RTL layout", Width: 420, Height: 300, Content: ui.View(func(c *ui.Context) {
		c.SetLayoutLocale("ar-EG")
		ui.Column(c).Fill().Padding(20).Gap(12).Children(func() {
			ui.Row(c).FillWidth().Gap(10).Children(func() {
				first = ui.ButtonBase(c).Size(80, 30).Label("العربية").Background(ui.RGB(255, 0, 0))
				first.Children(func() { ui.Text(c, "العربية") })
				if first.Clicked() {
					clicks.Add(1)
				}
				second = ui.ButtonBase(c).Size(80, 30).Label("עברית").Background(ui.RGB(0, 0, 255))
				second.Children(func() { ui.Text(c, "עברית") })
			})
			tabs = ui.Tabs(c, &selected, "ראשון", "ثاني", "English")
			slider = ui.Slider(c, &volume, 0, 100).Label("RTL volume")
			popup = ui.PopoverBase(c, first, &open, func(panel *ui.Element) { panel.Size(130, 30).Background(ui.RGB(0, 180, 0)).Label("RTL popup") })
		})
		frames.Add(1)
	})})
	eventually(t, "RTL frame", func() bool { return frames.Load() > 0 })
	rects := func() (a, b, s, p ui.Rect) {
		mygo.RunOnMain(func() { a, b, s, p = first.Bounds(), second.Bounds(), slider.Bounds(), popup.Bounds() })
		return
	}
	eventually(t, "mirrored boxes", func() bool { a, b, _, _ := rects(); return a.X > b.X && b.W > 0 })
	a, b, _, p := rects()
	if a.X+a.W != p.X+p.W || p.Y != a.Y+a.H {
		t.Fatalf("popup is not at RTL inline start: anchor %v, popup %v", a, p)
	}
	data, err := w.CapturePage()
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	scale := deviceScale(w)
	sample := func(r ui.Rect) color.RGBA {
		return color.RGBAModel.Convert(img.At(int(float64(r.X+3)*scale), int(float64(r.Y+3)*scale))).(color.RGBA)
	}
	if sample(a).R < 200 || sample(b).B < 200 {
		t.Fatalf("capture does not match visual boxes: %v %v", sample(a), sample(b))
	}
	if !click(w, float64(a.X+a.W/2), float64(a.Y+a.H/2)) {
		t.Skip("native click automation unavailable")
	}
	eventually(t, "RTL button click", func() bool { return clicks.Load() == 1 })
	nodes, ok := accessibility(w)
	if ok {
		found := false
		for _, n := range nodes {
			if n.label == "RTL volume" && n.role == roleSlider {
				found = true
			}
		}
		if !found {
			t.Fatal("native accessibility lost the RTL slider")
		}
		if !accessPerform(w, "RTL volume", "increment", "") {
			t.Fatal("native slider increment failed")
		}
		eventually(t, "numeric increment in RTL", func() bool { var value float64; mygo.RunOnMain(func() { value = volume }); return value > 30 })
	}
	t.Run("native arrows", func(t *testing.T) {
		before := frames.Load()
		w.Update(func() { open = false })
		eventually(t, "popup dismissed", func() bool { return frames.Load() > before })
		var tr ui.Rect
		mygo.RunOnMain(func() { tr = tabs.Bounds() })
		click(w, float64(tr.X+tr.W-16), float64(tr.Y+tr.H/2))
		// A direct surface key uses AppKit's existing test event path. Platforms
		// without that path still run the rendering, pointer and AX coverage.

		if !pressKey(w, 123, "\uf702") {
			t.Skip("native key injection unavailable")
		}
		eventually(t, "Left chooses the next RTL tab", func() bool { var tab int; mygo.RunOnMain(func() { tab = selected }); return tab == 1 })
	})
}
