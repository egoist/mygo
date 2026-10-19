package vt

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"unsafe"
)

// Images of the Kitty graphics protocol in a VT snapshot: the commands
// that send the images placed again, with their IDs, and place them where
// they are, so that the terminal a snapshot restores in shows them too.
// A placement is written after the row it is on, which the rows written
// after it scroll up, into the scrollback when it is there.

// pointCoordinate is a GhosttyPointCoordinate.
type pointCoordinate struct {
	x uint16
	_ uint16
	y uint32
}

// snapPlacement is a placement as a snapshot restores it: what the
// program gave, and where it is.
type snapPlacement struct {
	image, id uint32
	virtual   bool
	// row is the row of its top left cell in the screen, scrollback
	// included, active its row of the active area, -1 in the scrollback,
	// and col its column.
	row, active, col int
	x, y             uint32 // the offset in that cell
	// src is the part of the image it shows, x, y, width and height, 0
	// for the whole width or height.
	src        [4]uint32
	cols, rows uint32 // 0 for the cells the image covers
	z          int32
}

// snapshotPlacements returns the placements of the active screen, with
// where they are. Placements relative to another are left out.
func (t *Terminal) snapshotPlacements() []snapPlacement {
	g, it := t.placementIterator()
	if it == 0 {
		return nil
	}
	var out []snapPlacement
	for cbool(call(fnKittyPlacementNext, it)) {
		var p snapPlacement
		get := func(data int, v unsafe.Pointer) bool {
			return ok(call(fnKittyPlacementGet, it, uintptr(data), uintptr(v)))
		}
		if !get(placementDataImageID, unsafe.Pointer(&p.image)) || !get(placementDataPlacementID, unsafe.Pointer(&p.id)) ||
			!get(placementDataIsVirtual, unsafe.Pointer(&p.virtual)) || !get(placementDataXOffset, unsafe.Pointer(&p.x)) ||
			!get(placementDataYOffset, unsafe.Pointer(&p.y)) || !get(placementDataSourceX, unsafe.Pointer(&p.src[0])) ||
			!get(placementDataSourceY, unsafe.Pointer(&p.src[1])) || !get(placementDataSourceWidth, unsafe.Pointer(&p.src[2])) ||
			!get(placementDataSourceHeight, unsafe.Pointer(&p.src[3])) || !get(placementDataColumns, unsafe.Pointer(&p.cols)) ||
			!get(placementDataRows, unsafe.Pointer(&p.rows)) || !get(placementDataZ, unsafe.Pointer(&p.z)) {
			continue
		}
		if !p.virtual {
			img := call(fnKittyGraphicsImage, g, uintptr(p.image))
			if img == 0 {
				continue
			}
			sel := Selection{size: unsafe.Sizeof(Selection{})}
			sel.start.size, sel.end.size = unsafe.Sizeof(GridRef{}), unsafe.Sizeof(GridRef{})
			if !ok(call(fnKittyPlacementRect, it, img, t.h, uintptr(unsafe.Pointer(&sel)))) {
				continue
			}
			var pc pointCoordinate
			if !ok(call(fnTerminalPointFromGridRef, t.h, uintptr(unsafe.Pointer(&sel.start)), pointTagScreen, uintptr(unsafe.Pointer(&pc)))) {
				continue
			}
			p.row, p.col, p.active = int(pc.y), int(pc.x), -1
			if ok(call(fnTerminalPointFromGridRef, t.h, uintptr(unsafe.Pointer(&sel.start)), pointTagActive, uintptr(unsafe.Pointer(&pc)))) {
				p.active = int(pc.y)
			}
		}
		out = append(out, p)
	}
	return out
}

// withImages adds to a VT snapshot b the commands that restore the
// images placed: first the images, then each placement at the end of its
// row, where the cursor is as the row is written, or for one on a row
// after the last written, at its row of the active area once the rest is
// restored; the placements of Unicode placeholders, whose cells are text,
// last. A terminal without images has b unchanged.
func (t *Terminal) withImages(b []byte) []byte {
	if t.ImagesGeneration() == 0 {
		return b
	}
	places := t.snapshotPlacements()
	if len(places) == 0 {
		return b
	}
	var head, tail bytes.Buffer
	sent := map[uint32]bool{}
	for _, p := range places {
		if _, done := sent[p.image]; done {
			continue
		}
		img, ok := t.Image(p.image)
		sent[p.image] = ok && sendImage(&head, p.image, img)
	}
	// The rows end with "\r\n", but the last.
	var ends []int
	for i := 0; ; {
		j := bytes.Index(b[i:], []byte("\r\n"))
		if j < 0 {
			break
		}
		ends = append(ends, i+j)
		i += j + 2
	}
	inserts := map[int][]byte{}
	for _, p := range places {
		switch {
		case !sent[p.image]:
		case p.virtual:
			tail.WriteString(placeCommand(p))
		case p.row < len(ends):
			s := "\r"
			if p.col > 0 {
				s += fmt.Sprintf("\x1b[%dC", p.col)
			}
			inserts[ends[p.row]] = append(inserts[ends[p.row]], s+placeCommand(p)...)
		case p.active >= 0:
			fmt.Fprintf(&tail, "\x1b7\x1b[%d;%dH%s\x1b8", p.active+1, p.col+1, placeCommand(p))
		}
	}
	out := make([]byte, 0, head.Len()+len(b)+tail.Len()+64*len(inserts))
	out = append(out, head.Bytes()...)
	last := 0
	for _, e := range ends {
		if s, ok := inserts[e]; ok {
			out = append(append(out, b[last:e]...), s...)
			last = e
		}
	}
	out = append(out, b[last:]...)
	return append(out, tail.Bytes()...)
}

// sendImage writes the commands that send an image as id: a PNG, in
// chunks of the protocol's size. It reports false for pixels it cannot
// read.
func sendImage(w *bytes.Buffer, id uint32, img Image) bool {
	m := imageOf(img)
	if m == nil {
		return false
	}
	var p bytes.Buffer
	if (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&p, m) != nil {
		return false
	}
	data := base64.StdEncoding.EncodeToString(p.Bytes())
	const chunk = 4096
	for i := 0; i < len(data); i += chunk {
		end := min(i+chunk, len(data))
		more := 0
		if end < len(data) {
			more = 1
		}
		if i == 0 {
			fmt.Fprintf(w, "\x1b_Ga=t,f=100,i=%d,q=2,m=%d;", id, more)
		} else {
			fmt.Fprintf(w, "\x1b_Gm=%d,q=2;", more)
		}
		w.WriteString(data[i:end])
		w.WriteString("\x1b\\")
	}
	return true
}

// imageOf returns an image's pixels as a Go image, or nil when there are
// fewer than its size needs.
func imageOf(img Image) image.Image {
	w, h, pix := img.Width, img.Height, img.Pix
	r := image.Rect(0, 0, w, h)
	n := w * h
	switch img.Format {
	case ImageRGBA:
		if len(pix) >= n*4 {
			return &image.NRGBA{Pix: pix[:n*4], Stride: w * 4, Rect: r}
		}
	case ImageGray:
		if len(pix) >= n {
			return &image.Gray{Pix: pix[:n], Stride: w, Rect: r}
		}
	case ImageRGB, ImageGrayAlpha:
		per := 3
		if img.Format == ImageGrayAlpha {
			per = 2
		}
		if len(pix) < n*per {
			return nil
		}
		out := image.NewNRGBA(r)
		for i := range n {
			s, d := pix[i*per:], out.Pix[i*4:]
			if per == 3 {
				d[0], d[1], d[2], d[3] = s[0], s[1], s[2], 255
			} else {
				d[0], d[1], d[2], d[3] = s[0], s[0], s[0], s[1]
			}
		}
		return out
	}
	return nil
}

// placeCommand is the command that places p at the cursor, which stays
// where it is.
func placeCommand(p snapPlacement) string {
	s := fmt.Sprintf("\x1b_Ga=p,q=2,i=%d", p.image)
	if p.id != 0 {
		s += fmt.Sprintf(",p=%d", p.id)
	}
	if p.virtual {
		s += ",U=1"
	} else {
		s += ",C=1"
	}
	if p.x != 0 || p.y != 0 {
		s += fmt.Sprintf(",X=%d,Y=%d", p.x, p.y)
	}
	if p.src != [4]uint32{} {
		s += fmt.Sprintf(",x=%d,y=%d,w=%d,h=%d", p.src[0], p.src[1], p.src[2], p.src[3])
	}
	if p.cols != 0 {
		s += fmt.Sprintf(",c=%d", p.cols)
	}
	if p.rows != 0 {
		s += fmt.Sprintf(",r=%d", p.rows)
	}
	if p.z != 0 {
		s += fmt.Sprintf(",z=%d", p.z)
	}
	return s + "\x1b\\"
}
