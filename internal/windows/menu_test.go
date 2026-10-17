//go:build windows && (amd64 || arm64)

package windows

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestMenuBitmapPixels(t *testing.T) {
	// A half transparent red image becomes premultiplied BGRA.
	src := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for i := 0; i < len(src.Pix); i += 4 {
		src.SetNRGBA(i/4%32, i/4/32, color.NRGBA{R: 255, A: 128})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	px, err := menuBitmapPixels(buf.Bytes(), 16)
	if err != nil {
		t.Fatal(err)
	}
	if len(px) != 16*16*4 {
		t.Fatalf("%d bytes", len(px))
	}
	if b, g, r, a := px[0], px[1], px[2], px[3]; b != 0 || g != 0 || r != 128 || a != 128 {
		t.Errorf("pixel = %d %d %d %d, want 0 0 128 128", b, g, r, a)
	}
	// A smaller image is scaled up.
	buf.Reset()
	if err := png.Encode(&buf, src.SubImage(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	if px, err := menuBitmapPixels(buf.Bytes(), 20); err != nil || px[len(px)-2] != 128 || px[len(px)-1] != 128 {
		t.Errorf("scaled up: %v, last pixel %v", err, px[len(px)-4:])
	}
	if _, err := menuBitmapPixels([]byte("not a png"), 16); err == nil {
		t.Error("no error for an invalid image")
	}
}
