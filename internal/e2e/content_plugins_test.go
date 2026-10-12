package e2e

import (
	"bytes"
	"context"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/imageview"
	"github.com/egoist/mygo/plugins/pdf"
	"github.com/egoist/mygo/plugins/video"
	"github.com/egoist/mygo/ui"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func contentPluginCapture(t *testing.T, w *mygo.Window) image.Image {
	t.Helper()
	data, err := w.CapturePage()
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return img
}
func TestSpecializedImageView(t *testing.T) {
	v := imageview.New()
	defer v.Close()
	img := image.NewRGBA(image.Rect(0, 0, 100, 50))
	for y := 0; y < 50; y++ {
		for x := 0; x < 100; x++ {
			img.SetRGBA(x, y, color.RGBA{30, 90, 220, 255})
		}
	}
	if err := v.SetImage(img); err != nil {
		t.Fatal(err)
	}
	w := newWindow(t, mygo.WindowOptions{Title: "Image plugin", Width: 500, Height: 350, Content: ui.View(func(c *ui.Context) {
		ui.Box(c).Fill().Padding(20).Background(ui.RGB(5, 10, 15)).Children(func() { imageview.View(c, v).Fill() })
	})})
	eventually(t, "image plugin frame", func() bool {
		m := contentPluginCapture(t, w)
		b := m.Bounds()
		c := color.RGBAModel.Convert(m.At(b.Dx()/2, b.Dy()/2)).(color.RGBA)
		return c.B > 200 && c.R < 50
	})
	v.Rotate(1)
	v.SetZoom(2)
	w.Invalidate()
	eventually(t, "rotated viewer", func() bool { return v.State().Transform.Rotation == 1 })
	m := contentPluginCapture(t, w)
	s := deviceScale(w)
	if c := color.RGBAModel.Convert(m.At(int(5*s), int(5*s))).(color.RGBA); c.R != 5 || c.G != 10 || c.B != 15 {
		t.Fatalf("image escaped viewport: %v", c)
	}
	v.Close()
	w.Destroy()
	if !v.State().Closed {
		t.Fatal("viewer retained after close")
	}
}
func TestSpecializedPDFView(t *testing.T) {
	library := os.Getenv("MYGO_PDFIUM_LIBRARY")
	if library == "" {
		t.Skip("MYGO_PDFIUM_LIBRARY required")
	}
	d, err := pdf.Open("../../plugins/pdf/testdata/sample.pdf", pdf.Options{Library: library})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	v, err := pdf.NewViewer(d)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { v.Close(); <-v.Done() }()
	w := newWindow(t, mygo.WindowOptions{Title: "PDF plugin", Width: 600, Height: 600, Content: ui.View(func(c *ui.Context) { pdf.View(c, v).Fill() })})
	eventually(t, "PDF page render", func() bool { return !v.State().Loading && v.State().Err == nil })
	matches, err := v.Find(context.Background(), "Hello", false)
	if err != nil || len(matches) != 2 {
		t.Fatal(matches, err)
	}
	eventually(t, "PDF selection", func() bool { return v.SelectedText() == "Hello" })
	v.NextMatch(1)
	eventually(t, "second PDF page", func() bool { return v.State().Page == 1 && !v.State().Loading })
	if v.State().Err != nil {
		t.Fatal(v.State().Err)
	}
	_ = contentPluginCapture(t, w)
	v.Close()
	w.Destroy()
}
func TestSpecializedVideoView(t *testing.T) {
	library := os.Getenv("MYGO_MPV_LIBRARY")
	if library == "" {
		t.Skip("MYGO_MPV_LIBRARY required")
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg required")
	}
	file := filepath.Join(t.TempDir(), "clip.mp4")
	if out, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "color=c=red:s=160x90:d=4", "-c:v", "mpeg4", "-y", file).CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	p, err := video.New(video.Options{Library: library, Silent: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		p.Close()
		select {
		case <-p.Done():
		case <-time.After(3 * time.Second):
			t.Fatal("video cleanup stuck")
		}
	}()
	w := newWindow(t, mygo.WindowOptions{Title: "Video plugin", Width: 500, Height: 350, Content: ui.View(func(c *ui.Context) { video.View(c, p).Fill() })})
	if err = p.Load(file); err != nil {
		t.Fatal(err)
	}
	eventually(t, "paused video", func() bool { return p.State().Phase == video.Paused })
	if err = p.Play(); err != nil {
		t.Fatal(err)
	}
	eventually(t, "video pixels", func() bool {
		m := contentPluginCapture(t, w)
		b := m.Bounds()
		c := color.RGBAModel.Convert(m.At(b.Dx()/2, b.Dy()/3)).(color.RGBA)
		return c.R > 200 && c.G < 60 && c.B < 60
	})
	if os.Getenv("MYGO_GPU") == "1" {
		if how, pixels, width, height, ok := glSurface(w); ok {
			if how != "opengl" || len(pixels) == 0 {
				t.Fatalf("video surface draws %q without pixels", how)
			}
			pixel := pixels[((height/3)*width+width/2)*4:][:4]
			if pixel[2] < 200 || pixel[0] > 60 || pixel[1] > 60 {
				t.Fatalf("video pixels in GTK GL surface: %v", pixel)
			}
		}
	}
	if err = p.Pause(); err != nil {
		t.Fatal(err)
	}
	eventually(t, "paused playback", func() bool { return p.State().Phase == video.Paused })
	p.Close()
	w.Destroy()
}
