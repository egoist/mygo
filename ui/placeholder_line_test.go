package ui_test

import (
	"encoding/binary"
	"image"
	"slices"
	"sort"
	"strings"
	"testing"
	"unicode/utf16"

	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"

	"github.com/egoist/mygo/ui"
)

// An empty single-line input is as high as its placeholder needs, as it
// is with the same text typed, and draws the placeholder where it draws
// that text: its first line centered in the content box. The placeholder
// is in a fallback font whose line is 2.5 times as tall as the first
// font's, as Noto Nastaliq Urdu's beside a Latin font's (#2016): before,
// the field was as high as the first font's line and drew the placeholder
// at the empty value's origin, low and clipped at its bottom edge.
func TestInputPlaceholderInATallerFont(t *testing.T) {
	const short, tall = "MyGo Test Placeholder Short", "MyGo Test Placeholder Tall"
	// The tall font has only these private-use runes, drawn as H and g,
	// which the first font lacks: they fall back to it.
	const runes = "\ue000\ue001"
	if err := ui.RegisterFont(goregular.TTF, short); err != nil {
		t.Fatal(err)
	}
	if err := ui.RegisterFont(tallFont(t, goregular.TTF, tall, 2.5, map[rune]rune{0xe000: 'H', 0xe001: 'g'}), tall); err != nil {
		t.Fatal(err)
	}
	family := short + ", " + tall
	ink := ui.RGB(0, 0, 0)
	empty, typed, plain := "", runes, ""
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Padding(10).Gap(12).Width(300).Children(func() {
			ui.TextInput(c.Key("empty"), &empty).Placeholder(runes).PlaceholderColor(ink).
				Font(family).FontSize(15).TextColor(ink).Label("empty")
			ui.TextInput(c.Key("typed"), &typed).Font(family).FontSize(15).TextColor(ink).Label("typed")
			ui.TextInput(c.Key("plain"), &plain).Font(family).FontSize(15).Label("plain")
		})
	}, 320, 200)
	tt.Frame()
	e, _ := tt.Find("empty")
	w, _ := tt.Find("typed")
	p, _ := tt.Find("plain")
	if e.H != w.H {
		t.Errorf("the empty input is %v high, %v with its placeholder's text typed", e.H, w.H)
	}
	if p.H >= w.H {
		t.Fatalf("the fallback's line is no taller: an input without a placeholder is %v high, with the tall text typed %v", p.H, w.H)
	}
	img := tt.Image()
	top, bottom := inkRows(img, e)
	wantTop, wantBottom := inkRows(img, w)
	if top < 0 || wantTop < 0 {
		t.Fatalf("no ink: the placeholder's rows %d to %d, the typed text's %d to %d", top, bottom, wantTop, wantBottom)
	}
	// Within a row: the value and the placeholder are drawn by different
	// paths, whose antialiasing (ClearType on Windows) can darken one row
	// more or less at an edge. The clipped placeholder of the bug is many
	// rows off.
	if abs(top-wantTop) > 1 || abs(bottom-wantBottom) > 1 {
		t.Errorf("the placeholder's ink is in rows %d to %d of its field, the typed text's in %d to %d", top, bottom, wantTop, wantBottom)
	}
}

// inkRows returns the first and last rows of box in img, from its top,
// with dark pixels in them, or -1s without any.
func inkRows(img *image.RGBA, box ui.Rect) (first, last int) {
	first, last = -1, -1
	for y := int(box.Y) + 2; y < int(box.Y+box.H)-2; y++ {
		for x := int(box.X) + 2; x < int(box.X+box.W)-2; x++ {
			if px := img.RGBAAt(x, y); px.R < 100 && px.G < 100 && px.B < 100 {
				if first < 0 {
					first = y - int(box.Y)
				}
				last = y - int(box.Y)
				break
			}
		}
	}
	return first, last
}

// tallFont returns the TrueType font data made over as a font of family
// whose ascent, descent and line gap are scale times data's, with a cmap
// that maps only the runes of glyphs, to the glyphs of theirs in data.
func tallFont(t *testing.T, data []byte, family string, scale float64, glyphs map[rune]rune) []byte {
	t.Helper()
	f, err := sfnt.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	be := binary.BigEndian
	tables := map[string][]byte{}
	for i := range int(be.Uint16(data[4:])) {
		rec := data[12+16*i:]
		off, n := be.Uint32(rec[8:]), be.Uint32(rec[12:])
		tables[string(rec[:4])] = slices.Clone(data[off : off+n])
	}
	scaleAt := func(b []byte, offsets ...int) {
		for _, o := range offsets {
			be.PutUint16(b[o:], uint16(int16(float64(int16(be.Uint16(b[o:])))*scale)))
		}
	}
	scaleAt(tables["hhea"], 4, 6, 8)    // ascender, descender, line gap
	scaleAt(tables["OS/2"], 68, 70, 72) // the typographic ones
	scaleAt(tables["OS/2"], 74, 76)     // usWinAscent, usWinDescent: unsigned, but small enough
	be.PutUint32(tables["head"][8:], 0) // checkSumAdjustment, set below
	var buf sfnt.Buffer
	var segs [][2]uint16 // rune, glyph
	for r, of := range glyphs {
		g, err := f.GlyphIndex(&buf, of)
		if err != nil || g == 0 {
			t.Fatalf("%q has no glyph", of)
		}
		segs = append(segs, [2]uint16{uint16(r), uint16(g)})
	}
	sort.Slice(segs, func(i, j int) bool { return segs[i][0] < segs[j][0] })
	segs = append(segs, [2]uint16{0xffff, 0})
	// A format 4 subtable, a segment for each rune.
	n := len(segs)
	sub := make([]byte, 16+8*n)
	be.PutUint16(sub[0:], 4)
	be.PutUint16(sub[2:], uint16(len(sub)))
	be.PutUint16(sub[6:], uint16(2*n))
	sr, sel := 1, 0
	for sr*2 <= n {
		sr, sel = sr*2, sel+1
	}
	be.PutUint16(sub[8:], uint16(2*sr))
	be.PutUint16(sub[10:], uint16(sel))
	be.PutUint16(sub[12:], uint16(2*n-2*sr))
	for i, s := range segs {
		delta := s[1] - s[0]
		if s[0] == 0xffff {
			delta = 1
		}
		be.PutUint16(sub[14+2*i:], s[0])      // endCode
		be.PutUint16(sub[16+2*n+2*i:], s[0])  // startCode
		be.PutUint16(sub[16+4*n+2*i:], delta) // idDelta
		be.PutUint16(sub[16+6*n+2*i:], 0)     // idRangeOffset
	}
	cmap := make([]byte, 12, 12+len(sub))
	be.PutUint16(cmap[2:], 1)  // one encoding:
	be.PutUint16(cmap[4:], 3)  // Windows,
	be.PutUint16(cmap[6:], 1)  // Unicode BMP
	be.PutUint32(cmap[8:], 12) // at the subtable
	tables["cmap"] = append(cmap, sub...)
	// The names: family, subfamily, unique, full and PostScript name.
	names := []string{family, "Regular", family, family, strings.ReplaceAll(family, " ", "")}
	ids := []uint16{1, 2, 3, 4, 6}
	name := make([]byte, 6+12*len(names))
	be.PutUint16(name[2:], uint16(len(names)))
	be.PutUint16(name[4:], uint16(len(name)))
	var strs []byte
	for i, s := range names {
		var u []byte
		for _, c := range utf16.Encode([]rune(s)) {
			u = be.AppendUint16(u, c)
		}
		rec := name[6+12*i:]
		be.PutUint16(rec[0:], 3)
		be.PutUint16(rec[2:], 1)
		be.PutUint16(rec[4:], 0x409)
		be.PutUint16(rec[6:], ids[i])
		be.PutUint16(rec[8:], uint16(len(u)))
		be.PutUint16(rec[10:], uint16(len(strs)))
		strs = append(strs, u...)
	}
	tables["name"] = append(name, strs...)

	tags := make([]string, 0, len(tables))
	for tag := range tables {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	checksum := func(b []byte) uint32 {
		var sum uint32
		for i := 0; i < len(b); i += 4 {
			var w [4]byte
			copy(w[:], b[i:])
			sum += be.Uint32(w[:])
		}
		return sum
	}
	out := slices.Clone(data[:12])
	be.PutUint16(out[4:], uint16(len(tags)))
	out = append(out, make([]byte, 16*len(tags))...)
	head := 0
	for i, tag := range tags {
		b := tables[tag]
		if tag == "head" {
			head = len(out)
		}
		rec := out[12+16*i:]
		copy(rec, tag)
		be.PutUint32(rec[4:], checksum(b))
		be.PutUint32(rec[8:], uint32(len(out)))
		be.PutUint32(rec[12:], uint32(len(b)))
		out = append(out, b...)
		for len(out)%4 != 0 {
			out = append(out, 0)
		}
	}
	be.PutUint32(out[head+8:], 0xb1b0afba-checksum(out))
	return out
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
