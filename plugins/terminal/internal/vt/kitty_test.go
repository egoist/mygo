package vt

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"slices"
	"strings"
	"testing"
)

// kittyPNG is a Kitty graphics command transmitting a 2×1 PNG, red and
// half-transparent blue, as image id and placing it over cols×rows cells.
func kittyPNG(t *testing.T, id, cols, rows int, extra string) string {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	img.SetNRGBA(0, 0, color.NRGBA{255, 0, 0, 255})
	img.SetNRGBA(1, 0, color.NRGBA{0, 0, 255, 128})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return kittyCmd("a=T,f=100,i="+itoa(id)+",c="+itoa(cols)+",r="+itoa(rows)+extra, b.Bytes())
}

func kittyCmd(keys string, payload []byte) string {
	return "\x1b_G" + keys + ",q=2;" + base64.StdEncoding.EncodeToString(payload) + "\x1b\\"
}

func itoa(n int) string {
	if n < 0 {
		return "-" + itoa(-n)
	}
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}

func newImageTerm(t *testing.T, limit uint64) *Terminal {
	t.Helper()
	loadLib(t)
	term, err := NewTerminal(20, 5, Effects{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(term.Free)
	term.Resize(20, 5, 10, 20)
	if limit > 0 {
		if err := term.SetImageStorageLimit(limit); err != nil {
			t.Fatal(err)
		}
	}
	return term
}

// Without a storage limit, the default, images are refused: nothing is
// kept or placed.
func TestImagesRefusedByDefault(t *testing.T) {
	term := newImageTerm(t, 0)
	term.Write([]byte(kittyPNG(t, 1, 4, 2, "")))
	if ps := term.Placements(nil); len(ps) != 0 {
		t.Errorf("placements %+v", ps)
	}
	if _, ok := term.Image(1); ok {
		t.Error("an image was kept")
	}
}

func TestImagePlacements(t *testing.T) {
	term := newImageTerm(t, 16<<20)
	term.Write([]byte("ab"))
	term.Write([]byte(kittyPNG(t, 7, 4, 2, ",X=3,Y=5")))
	gen := term.ImagesGeneration()
	if gen == 0 {
		t.Fatal("no generation after an image")
	}
	ps := term.Placements(nil)
	if len(ps) != 1 {
		t.Fatalf("placements %+v", ps)
	}
	p := ps[0]
	// At the cursor, over 4×2 cells of 10×20 pixels less its offset in
	// the first cell, the whole image.
	want := Placement{Image: 7, Col: 2, Row: 0, X: 3, Y: 5, Width: 37, Height: 35, Source: image.Rect(0, 0, 2, 1)}
	if p != want {
		t.Errorf("placement %+v, want %+v", p, want)
	}
	// The PNG, decoded to straight RGBA.
	img, ok := term.Image(7)
	if !ok || img.Width != 2 || img.Height != 1 || img.Format != ImageRGBA {
		t.Fatalf("image %+v %v", img, ok)
	}
	if !bytes.Equal(img.Pix, []byte{255, 0, 0, 255, 0, 0, 255, 128}) {
		t.Errorf("pixels %v", img.Pix)
	}
	if img.Generation == 0 {
		t.Error("image generation 0")
	}

	// Raw RGB, below the text (z < 0), part of it (x, y, w, h).
	rgb := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	term.Write([]byte("\r\n" + kittyCmd("a=T,f=24,s=2,v=2,i=8,z=-5,x=1,y=0,w=1,h=2", rgb)))
	img8, ok := term.Image(8)
	if !ok || img8.Format != ImageRGB || !bytes.Equal(img8.Pix, rgb) {
		t.Fatalf("RGB image %+v %v", img8, ok)
	}
	ps = term.Placements(nil)
	// Placed on the line after the first image's last row.
	i := slices.IndexFunc(ps, func(p Placement) bool { return p.Image == 8 })
	if i < 0 || ps[i].Z != -5 || ps[i].Source != image.Rect(1, 0, 2, 2) || ps[i].Row != 2 || ps[i].Width != 1 || ps[i].Height != 2 {
		t.Errorf("RGB placement %+v", ps)
	}
	if term.ImagesGeneration() == gen {
		t.Error("the generation did not change")
	}

	// Sent again, the same image is a new generation.
	before := img.Generation
	term.Write([]byte(kittyPNG(t, 7, 4, 2, "")))
	if img, _ := term.Image(7); img.Generation == before {
		t.Error("a resent image kept its generation")
	}
}

// Placements move up as the screen scrolls, partly out of view, then out.
func TestImageScrolls(t *testing.T) {
	term := newImageTerm(t, 16<<20)
	term.Write([]byte(kittyPNG(t, 3, 2, 2, ",C=1")))
	row := func() (int, bool) {
		for _, p := range term.Placements(nil) {
			if p.Image == 3 {
				return p.Row, true
			}
		}
		return 0, false
	}
	if r, ok := row(); !ok || r != 0 {
		t.Fatalf("row %d %v", r, ok)
	}
	term.Write([]byte(strings.Repeat("\r\n", 5)))
	if r, ok := row(); !ok || r != -1 {
		t.Errorf("after scrolling 1 row: row %d %v", r, ok)
	}
	term.Write([]byte(strings.Repeat("\r\n", 3)))
	if r, ok := row(); ok {
		t.Errorf("scrolled out, still at row %d", r)
	}
}
