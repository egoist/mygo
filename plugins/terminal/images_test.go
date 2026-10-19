package terminal

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	imgcolor "image/color"
	"image/png"
	"testing"

	"github.com/egoist/mygo/ui"
)

// An image a program places shows in the view where it is placed, in its
// layer, only when Options.ImageStorageBytes lets the terminal keep images.
func TestImages(t *testing.T) {
	loadLib(t)
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for i := range 16 {
		img.SetNRGBA(i%4, i/4, imgcolor.NRGBA{255, 0, 0, 255})
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	place := func(z int) string {
		// Over 4×2 cells at the top left, the cursor left where it is.
		return fmt.Sprintf("\x1b[H\x1b_Ga=T,f=100,i=1,c=4,r=2,C=1,z=%d,q=2;%s\x1b\\", z, base64.StdEncoding.EncodeToString(b.Bytes()))
	}
	// Cells with a blue background, under the image.
	const blueCells = "\x1b[H\x1b[44m    \r\n    \x1b[0m"
	blue := ui.RGB(0, 0, 255)
	for _, c := range []struct {
		name   string
		images int
		feed   string
		red    bool
	}{
		{"refused by default", 0, place(0), false},
		{"over the text", 16 << 20, place(0), true},
		{"below the cells' backgrounds", 16 << 20, blueCells + place(-1<<30-1), false},
		{"between the backgrounds and the text", 16 << 20, blueCells + place(-1), true},
	} {
		t.Run(c.name, func(t *testing.T) {
			term, err := New(Options{Conn: newPipe(), ImageStorageBytes: c.images, NoBlink: true, Theme: &Theme{Background: ui.RGB(10, 10, 10), Foreground: ui.RGB(200, 200, 200), Palette: [16]ui.Color{4: blue}}})
			if err != nil {
				t.Fatal(err)
			}
			defer term.Close()
			tt := ui.NewTester(func(c *ui.Context) { View(c, term).Fill() }, 400, 200)
			tt.Frame()
			term.Feed([]byte(c.feed))
			tt.Frame()
			x, y := cellCenter(term, 1, 1)
			s := term.v.scale
			px := tt.Image().RGBAAt(int(x*s), int(y*s))
			if got := px.R > 200 && px.G < 40 && px.B < 40; got != c.red {
				t.Errorf("the image's cell is %v; want red: %v", px, c.red)
			}
		})
	}
}
