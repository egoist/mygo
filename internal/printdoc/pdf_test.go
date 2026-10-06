package printdoc

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"image"
	"image/color"
	"io"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

func TestPDFPageImagesAndCrossReferences(t *testing.T) {
	// A subimage with a nonzero origin/stride catches row and alpha mistakes.
	back := image.NewRGBA(image.Rect(0, 0, 5, 5))
	img := back.SubImage(image.Rect(1, 2, 3, 3)).(*image.RGBA)
	img.SetRGBA(1, 2, color.RGBA{128, 0, 0, 128})
	img.SetRGBA(2, 2, color.RGBA{0, 0, 255, 255})
	job := &platform.PrintJob{Title: "Résumé 文", Width: 144, Height: 216, Pages: []*image.RGBA{img, img}}
	pdf, err := PDF(job)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF-1.4")) || !bytes.Contains(pdf, []byte("/Count 2")) {
		t.Fatal("invalid document envelope")
	}
	pattern := regexp.MustCompile(`/Subtype /Image /Width 2 /Height 1 /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /FlateDecode /Length (\d+) >>\nstream\n`)
	matches := pattern.FindAllSubmatchIndex(pdf, -1)
	if len(matches) != 2 {
		t.Fatal("images missing")
	}
	for _, match := range matches {
		n, _ := strconv.Atoi(string(pdf[match[2]:match[3]]))
		z, err := zlib.NewReader(bytes.NewReader(pdf[match[1] : match[1]+n]))
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(z)
		z.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(raw, []byte{255, 127, 127, 0, 0, 255}) {
			t.Fatalf("decoded page pixels: %v", raw)
		}
	}
	start := bytes.LastIndex(pdf, []byte("startxref\n")) + len("startxref\n")
	xref, _ := strconv.Atoi(strings.Split(string(pdf[start:]), "\n")[0])
	lines := strings.Split(string(pdf[xref:]), "\n")
	if lines[0] != "xref" || lines[1] != "0 10" {
		t.Fatal("cross-reference count", lines[:2])
	}
	for i, line := range lines[3:12] {
		off, _ := strconv.Atoi(strings.Fields(line)[0])
		if !bytes.HasPrefix(pdf[off:], []byte(fmt.Sprintf("%d 0 obj\n", i+1))) {
			t.Fatalf("object %d offset %d is incorrect", i+1, off)
		}
	}
	if !bytes.Contains(pdf, []byte("/Title <feff005200e900730075006d00e900206587>")) {
		t.Fatal("Unicode PDF title missing")
	}
	if got := BGRA(img); !bytes.Equal(got, []byte{127, 127, 255, 255, 255, 0, 0, 255}) {
		t.Fatal("printer pixel output differs from PDF", got)
	}
}

func TestPDFAvoidsEmptyAndUnboundedJobs(t *testing.T) {
	for _, job := range []*platform.PrintJob{nil, {}, {Width: 1, Height: 1, Pages: []*image.RGBA{nil}},
		{Width: 1, Height: 1, Pages: []*image.RGBA{image.NewRGBA(image.Rect(0, 0, 0, 0))}},
		{Width: 1, Height: 1, Pages: []*image.RGBA{{Rect: image.Rect(0, 0, 16384, 16384)}}},
		{Width: 1, Height: 1, Pages: make([]*image.RGBA, 1001)},
	} {
		if _, err := PDF(job); err == nil {
			t.Fatal("accepted invalid print job")
		}
	}
	x, y, w, h := Fit(200, 100, 100, 100)
	if x != 0 || y != 25 || w != 100 || h != 50 {
		t.Fatalf("page was cropped or distorted: %v %v %v %v", x, y, w, h)
	}
}
