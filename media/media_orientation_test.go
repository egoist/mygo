//go:build darwin || (windows && (amd64 || arm64)) || (linux && (amd64 || arm64))

package media

import (
	"context"
	"testing"
)

// The fixture is a second of 160×120 H.264 whose top half is red and bottom
// half is blue, with no sound, made with ffmpeg.
const halvesPath = "testdata/halves.mp4"

// TestPicturesAreTheRightWayUp checks that the first row of a frame is the
// top row of what the picture shows. The platforms hand their pictures over
// with the rows the other way up — a bottom-up bitmap, a stride with a sign
// — so a picture that comes out upside down is a mistake that only a picture
// with an up and a down can catch.
func TestPicturesAreTheRightWayUp(t *testing.T) {
	s, err := openSource(halvesPath, options{noSound: true})
	if err != nil {
		t.Fatalf("openSource(%q): %v", halvesPath, err)
	}
	defer s.Close()
	f, err := s.NextFrame(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Release()
	img := f.Image()
	if img == nil {
		t.Fatalf("the frame is a %s picture with no image", f.Format)
	}
	if img.Rect.Dx() != 160 || img.Rect.Dy() != 120 {
		t.Fatalf("the picture is %v, want 160x120", img.Rect)
	}
	top := img.Pix[0:4]
	bottom := img.Pix[(f.Height-1)*img.Stride:]
	if top[0] < 128 || top[2] > 128 {
		t.Errorf("the top row is %v, want red", top[:3])
	}
	if bottom[2] < 128 || bottom[0] > 128 {
		t.Errorf("the bottom row is %v, want blue", bottom[:3])
	}
}
