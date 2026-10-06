package e2e

import (
	"bytes"
	"image/png"
	"math"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// TestContentPreview exercises simulated size/DPI, live configuration,
// native pointer mapping and sample disposal through a real surface.
func TestContentPreview(t *testing.T) {
	type environment struct {
		width, height float32
		dark, motion  bool
		locale        string
	}
	var current atomic.Value
	var clicks, disposed atomic.Int32
	p := ui.NewPreview(ui.PreviewOptions{Config: ui.PreviewConfig{Width: 600, Height: 300, Scale: 1.25},
		Presets: []ui.PreviewPreset{{Name: "Counter", New: func() ui.PreviewSample {
			return ui.PreviewSample{View: func(c *ui.Context) {
				config, _ := c.PreviewConfig()
				width, height := c.Size()
				current.Store(environment{width, height, c.Theme().Dark, c.Preferences().ReduceMotion, config.Locale})
				ui.Column(c).Fill().Padding(20).Children(func() {
					if ui.Button(c, "Add").Width(100).Height(50).Clicked() {
						clicks.Add(1)
					}
					ui.Textf(c, "Count %d", clicks.Load())
				})
			}, Dispose: func() { disposed.Add(1) }}
		}}},
	})
	w := newWindow(t, mygo.WindowOptions{Title: "Native preview", Width: 800, Height: 480, Content: p.Content()})
	eventually(t, "the preview's first frame", func() bool { return current.Load() != nil })
	if e := current.Load().(environment); e.width != 600 || e.height != 300 {
		t.Fatalf("preview follows physical window size: %+v", e)
	}
	checkCapture := func(width, height int) {
		t.Helper()
		data, err := w.CapturePage()
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		if img.Bounds().Dx() != width || img.Bounds().Dy() != height {
			t.Fatalf("preview capture: %v, want %dx%d", img.Bounds(), width, height)
		}
	}
	checkCapture(750, 375)
	config := p.Config()
	config.Theme, config.Scale, config.Locale = ui.PreviewDark, 2, "ja-JP"
	config.Preferences = &ui.Preferences{ReduceMotion: true, HighContrast: true, TextScale: 1.5}
	if err := p.SetConfig(config); err != nil {
		t.Fatal(err)
	}
	eventually(t, "preview overrides on the native surface", func() bool {
		e := current.Load().(environment)
		return e.dark && e.motion && e.locale == "ja-JP"
	})
	checkCapture(1200, 600)
	width, height := w.ContentSize()
	scale := math.Min(float64(width)/600, float64(height)/300)
	x, y := (float64(width)-600*scale)/2+70*scale, (float64(height)-300*scale)/2+45*scale
	if !click(w, x, y) {
		t.Skip("native pointer automation unavailable")
	}
	eventually(t, "a click in the fitted preview", func() bool { return clicks.Load() == 1 })
	p.Reset()
	eventually(t, "old sample disposal after reset", func() bool { return disposed.Load() == 1 })
	w.Destroy()
	if disposed.Load() != 2 {
		t.Fatalf("native detach disposed %d samples, want 2", disposed.Load())
	}
}
