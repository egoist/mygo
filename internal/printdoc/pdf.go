// Package printdoc encodes the same fixed raster pages that native printers
// draw. It depends on no GUI or external program.
package printdoc

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"image"
	"math"
	"unicode/utf16"

	"github.com/egoist/mygo/internal/platform"
)

// PDF writes lossless RGB image XObjects, one per page, with exact point
// dimensions and a byte-offset cross-reference table. Alpha is flattened
// onto white; no font files, native handles or live UI state escape here.
func PDF(job *platform.PrintJob) ([]byte, error) {
	if job == nil || len(job.Pages) == 0 || len(job.Pages) > platform.MaxPrintPages {
		return nil, errors.New("mygo: printing requires between 1 and 1000 pages")
	}
	if job.Width <= 0 || job.Height <= 0 || math.IsNaN(job.Width) || math.IsNaN(job.Height) || math.IsInf(job.Width, 0) || math.IsInf(job.Height, 0) {
		return nil, errors.New("mygo: invalid print page dimensions")
	}
	pixels := int64(0)
	for _, img := range job.Pages {
		if img == nil || img.Rect.Empty() {
			return nil, errors.New("mygo: empty print page")
		}
		pixels += int64(img.Rect.Dx()) * int64(img.Rect.Dy())
		if pixels > platform.MaxPrintPixels {
			return nil, errors.New("mygo: print job exceeds the pixel limit; reduce DPI or page count")
		}
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	offsets := []int{0}
	object := func(number int, data []byte) {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n", number)
		b.Write(data)
		b.WriteString("\nendobj\n")
	}
	object(1, []byte("<< /Type /Catalog /Pages 2 0 R >>"))
	var kids bytes.Buffer
	fmt.Fprintf(&kids, "<< /Type /Pages /Count %d /Kids [", len(job.Pages))
	for i := range job.Pages {
		fmt.Fprintf(&kids, "%d 0 R ", 3+3*i)
	}
	kids.WriteString("] >>")
	object(2, kids.Bytes())
	for i, img := range job.Pages {
		n := 3 + 3*i
		object(n, fmt.Appendf(nil, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.6f %.6f] /Resources << /XObject << /Im0 %d 0 R >> >> /Contents %d 0 R >>", job.Width, job.Height, n+2, n+1))
		commands := fmt.Appendf(nil, "q\n%.6f 0 0 %.6f 0 0 cm\n/Im0 Do\nQ\n", job.Width, job.Height)
		object(n+1, stream("", commands))
		data, err := compressedRGB(img)
		if err != nil {
			return nil, err
		}
		props := fmt.Sprintf("/Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /FlateDecode", img.Rect.Dx(), img.Rect.Dy())
		object(n+2, stream(props, data))
	}
	info := 3 + 3*len(job.Pages)
	var title bytes.Buffer
	title.WriteString("feff")
	for _, r := range utf16.Encode([]rune(job.Title)) {
		fmt.Fprintf(&title, "%04x", r)
	}
	object(info, fmt.Appendf(nil, "<< /Title <%s> /Producer (MyGo) >>", title.Bytes()))
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, off := range offsets[1:] {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R /Info %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), info, xref)
	return b.Bytes(), nil
}

func stream(properties string, data []byte) []byte {
	b := fmt.Appendf(nil, "<< %s /Length %d >>\nstream\n", properties, len(data))
	b = append(b, data...)
	return append(b, []byte("\nendstream")...)
}

func compressedRGB(img *image.RGBA) ([]byte, error) {
	var b bytes.Buffer
	z := zlib.NewWriter(&b)
	row := make([]byte, img.Rect.Dx()*3)
	for y := img.Rect.Min.Y; y < img.Rect.Max.Y; y++ {
		for x := img.Rect.Min.X; x < img.Rect.Max.X; x++ {
			p := img.RGBAAt(x, y)
			i := (x - img.Rect.Min.X) * 3
			white := uint16(255 - p.A)
			row[i], row[i+1], row[i+2] = uint8(min(255, uint16(p.R)+white)), uint8(min(255, uint16(p.G)+white)), uint8(min(255, uint16(p.B)+white))
		}
		if _, err := z.Write(row); err != nil {
			z.Close()
			return nil, err
		}
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
