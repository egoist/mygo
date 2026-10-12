package imageview

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/egoist/mygo/ui"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"sync"
	"testing"
)

func pattern() *image.RGBA {
	m := image.NewRGBA(image.Rect(0, 0, 2, 3))
	m.Set(0, 0, color.RGBA{255, 0, 0, 255})
	m.Set(1, 0, color.RGBA{0, 255, 0, 255})
	m.Set(0, 2, color.RGBA{0, 0, 255, 255})
	return m
}
func TestRotationPixelsAndRendering(t *testing.T) {
	v := New()
	defer v.Close()
	if err := v.SetImage(pattern()); err != nil {
		t.Fatal(err)
	}
	v.Rotate(1)
	if w, h := v.bitmap.Size(); w != 3 || h != 2 {
		t.Fatalf("rotated size %dx%d", w, h)
	}
	tt := ui.NewTester(func(c *ui.Context) { View(c, v).Fill() }, 60, 40)
	r, g, b, _ := tt.Image().At(50, 10).RGBA()
	if r < 60000 || g > 6000 || b > 6000 {
		t.Fatalf("clockwise corner %d,%d,%d", r, g, b)
	}
	v.Rotate(3)
	if got := v.State().Transform.Rotation; got != 0 {
		t.Fatal(got)
	}
	if v.bitmap == nil {
		t.Fatal("missing bitmap")
	}
}
func TestTransformZoomAnchorAndClamping(t *testing.T) {
	tr := Transform{}
	viewport := ui.Rect{W: 200, H: 100}
	r := tr.Rect(400, 200, viewport)
	if r.W != 200 || r.H != 100 {
		t.Fatal(r)
	}
	tr.ZoomAt(2, 150, 50, 400, 200, viewport)
	r = tr.Rect(400, 200, viewport)
	if math.Abs(float64(r.X)+150) > 0.01 {
		t.Fatal(r)
	}
	tr.PanX = 1e6
	tr.PanY = -1e6
	tr.Clamp(400, 200, viewport)
	if tr.PanX != 100 || tr.PanY != -50 {
		t.Fatal(tr)
	}
	before := tr
	tr.ZoomAt(math.NaN(), 0, 0, 400, 200, viewport)
	if tr != before {
		t.Fatal("invalid zoom changed viewport")
	}
}
func TestLoadFailureAndLifetime(t *testing.T) {
	v := New()
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, pattern()); err != nil {
		t.Fatal(err)
	}
	if err := v.Load(encoded.Bytes()); err != nil {
		t.Fatal(err)
	}
	if v.State().Height != 3 {
		t.Fatal(v.State())
	}
	if err := v.Load([]byte("bad")); err == nil {
		t.Fatal("invalid image loaded")
	}
	if v.bitmap != nil || v.State().Err == nil || v.State().Loading {
		t.Fatal("stale image after error")
	}
	v.Close()
	v.Close()
	if err := v.Load(encoded.Bytes()); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if v.source != nil || v.bitmap != nil {
		t.Fatal("retained closed image")
	}
}
func TestViewControlsAndPan(t *testing.T) {
	v := New()
	defer v.Close()
	_ = v.SetImage(pattern())
	tt := ui.NewTester(func(c *ui.Context) { View(c, v).Fill().AutoFocus() }, 120, 80)
	tt.Key(0, ui.KeyEqual)
	if v.State().Transform.Zoom != 1.25 {
		t.Fatal(v.State())
	}
	tt.Key(0, ui.KeyR)
	if v.State().Transform.Rotation != 1 {
		t.Fatal(v.State())
	}
	tt.Key(0, ui.Key0)
	if v.State().Transform.Zoom != 0 {
		t.Fatal(v.State())
	}
	v.SetFit(FitActual)
	v.SetZoom(64)
	tt.Frame()
	tt.Press(60, 40)
	tt.Move(80, 40)
	tt.Release(80, 40)
	if v.State().Transform.PanX != 20 {
		t.Fatal(v.State())
	}
}
func TestConcurrentControlsAndClose(t *testing.T) {
	v := New()
	_ = v.SetImage(pattern())
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Go(func() {
			for j := 0; j < 100; j++ {
				v.Pan(1, -1)
				v.SetZoom(2)
				_ = v.State()
			}
		})
	}
	v.Close()
	wg.Wait()
	if !v.State().Closed {
		t.Fatal("reopened viewer")
	}
}
func TestMalformedEXIF(t *testing.T) {
	data := []byte{255, 216, 255, 225, 0, 16, 'E', 'x', 'i', 'f', 0, 0, 'I', 'I', 42, 0, 255, 255, 255, 255}
	if exifOrientation(data) != 1 {
		t.Fatal("accepted out of bounds EXIF")
	}
}

func TestNewImageSupersedesInFlightDecode(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	magic := fmt.Sprintf("MYGOBLOCK-%p", entered)
	image.RegisterFormat(magic, magic, func(_ io.Reader) (image.Image, error) {
		close(entered)
		<-release
		return image.NewRGBA(image.Rect(0, 0, 1, 1)), nil
	}, func(_ io.Reader) (image.Config, error) { return image.Config{Width: 1, Height: 1}, nil })
	v := New()
	defer v.Close()
	done := make(chan error, 1)
	go func() { done <- v.Load([]byte(magic)) }()
	<-entered
	_ = v.SetImage(pattern())
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if s := v.State(); s.Width != 2 || s.Height != 3 || s.Loading {
		t.Fatal("stale decode replaced new image", s)
	}
}

func TestDragReturnsImmediatelyFromPanLimit(t *testing.T) {
	v := New()
	defer v.Close()
	_ = v.SetImage(pattern())
	v.SetFit(FitActual)
	v.SetZoom(64)
	tt := ui.NewTester(func(c *ui.Context) { View(c, v).Fill() }, 100, 100)
	tt.Press(50, 50)
	tt.Move(200, 50)
	if v.State().Transform.PanX != 14 {
		t.Fatal("pan overshot the displayed edge", v.State())
	}
	tt.Move(190, 50)
	if v.State().Transform.PanX != 4 {
		t.Fatal("return drag consumed hidden overshoot", v.State())
	}
	tt.Release(190, 50)
}
