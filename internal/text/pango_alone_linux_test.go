//go:build linux && (amd64 || arm64)

package text

import (
	"encoding/binary"
	"slices"
	"testing"
	"unicode/utf16"

	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/sfnt"
)

// TestAloneLegacyKern checks that alone declines a font HarfBuzz kerns
// with a legacy kern table, which it applies without GPOS, while the kern
// feature is on; and lays it out alone, as Shape does, once the text's
// features or fontconfig's turn kern off.
func TestAloneLegacyKern(t *testing.T) {
	s := newSystem()
	if _, ok := s.engine().(*pangoEngine); !ok {
		t.Skip("no Pango")
	}
	const family = "MyGo Alone Kern"
	if err := s.RegisterFont(kernedGoMono(t, family, 'A', 'V', -400), family); err != nil {
		t.Fatal(err)
	}
	advances := func(style Style, text string) (shaped, alone []float32, ok bool) {
		l := s.Shape(Params{Text: text, Style: style, NoWrap: true, KeepSpaces: true})
		for _, g := range l.Lines[0].Glyphs {
			shaped = append(shaped, g.Advance)
		}
		ok = s.Alone([]rune(text), style, func(g Glyph) { alone = append(alone, g.Advance) })
		return shaped, alone, ok
	}
	style := Style{Family: family, Size: 20}
	shaped, _, ok := advances(style, "AV")
	if ok {
		t.Error("a font with a legacy kern table laid out alone with kern on")
	}
	plain, _, _ := advances(Style{Family: family, Size: 20, Features: "kern=0"}, "AV")
	if shaped[0] >= plain[0] {
		t.Fatalf("the kern table does not kern AV (advance %v, %v without kern): the test checks nothing", shaped[0], plain[0])
	}
	check := func(style Style, why string) {
		t.Helper()
		shaped, alone, ok := advances(style, "AVAV")
		if !ok {
			t.Errorf("%s: not laid out alone", why)
		} else if !slices.Equal(shaped, alone) {
			t.Errorf("%s: alone %v, Shape %v", why, alone, shaped)
		}
	}
	check(Style{Family: family, Size: 20, Features: "kern=0"}, "kern=0 in the text")

	// fontconfig's features for the font, as a user's fonts.conf gives
	// them, apply too.
	l := &pangoLib
	if l.fcConfigParseFromMemory == nil {
		t.Skip("no fontconfig")
	}
	rule := `<?xml version="1.0"?><!DOCTYPE fontconfig SYSTEM "fonts.dtd"><fontconfig><match target="font">` +
		`<test name="family"><string>` + family + `</string></test>` +
		`<edit name="fontfeatures" mode="append"><string>kern=0</string></edit></match></fontconfig>`
	if l.fcConfigParseFromMemory(l.fcConfigGetCurrent(), rule, int32(gTrue)) == 0 {
		t.Fatal("fontconfig refused the rule")
	}
	s = newSystem()
	if err := s.RegisterFont(kernedGoMono(t, family, 'A', 'V', -400), family); err != nil {
		t.Fatal(err)
	}
	if shaped, _, _ := advances(style, "AV"); shaped[0] != plain[0] {
		t.Skip("this Pango applies no fontconfig features")
	}
	check(style, "kern=0 from fontconfig")
}

// kernedGoMono returns Go Mono under another family name, with a legacy
// kern table that kerns the pair left, right by value font units.
func kernedGoMono(t *testing.T, family string, left, right rune, value int16) []byte {
	t.Helper()
	f, err := sfnt.Parse(gomono.TTF)
	if err != nil {
		t.Fatal(err)
	}
	var buf sfnt.Buffer
	l, err := f.GlyphIndex(&buf, left)
	if err != nil {
		t.Fatal(err)
	}
	r, err := f.GlyphIndex(&buf, right)
	if err != nil {
		t.Fatal(err)
	}
	be := binary.BigEndian
	// kern version 0, one subtable of format 0, horizontal, with one pair.
	kern := be.AppendUint16(nil, 0)
	kern = be.AppendUint16(kern, 1)
	kern = be.AppendUint16(kern, 0)      // subtable version
	kern = be.AppendUint16(kern, 6+8+6)  // length
	kern = be.AppendUint16(kern, 0x0001) // horizontal, format 0
	kern = be.AppendUint16(kern, 1)      // nPairs
	kern = be.AppendUint16(kern, 6)      // searchRange
	kern = be.AppendUint16(kern, 0)      // entrySelector
	kern = be.AppendUint16(kern, 0)      // rangeShift
	kern = be.AppendUint16(kern, uint16(l))
	kern = be.AppendUint16(kern, uint16(r))
	kern = be.AppendUint16(kern, uint16(value))
	// A name table of the family, regular, for Windows in Unicode.
	strs := [][2]any{{1, family}, {2, "Regular"}, {4, family}, {6, "MyGoAloneKern"}}
	name := be.AppendUint16(nil, 0)
	name = be.AppendUint16(name, uint16(len(strs)))
	name = be.AppendUint16(name, uint16(6+12*len(strs)))
	var data []byte
	for _, s := range strs {
		u := utf16.Encode([]rune(s[1].(string)))
		name = be.AppendUint16(name, 3)
		name = be.AppendUint16(name, 1)
		name = be.AppendUint16(name, 0x409)
		name = be.AppendUint16(name, uint16(s[0].(int)))
		name = be.AppendUint16(name, uint16(2*len(u)))
		name = be.AppendUint16(name, uint16(len(data)))
		for _, c := range u {
			data = be.AppendUint16(data, c)
		}
	}
	name = append(name, data...)

	// Copy the other tables, sorted by tag, as the directory must be.
	src := gomono.TTF
	tables := map[string][]byte{"kern": kern, "name": name}
	for i := range int(be.Uint16(src[4:])) {
		rec := src[12+16*i:]
		tag := string(rec[:4])
		if _, ok := tables[tag]; !ok {
			off, n := be.Uint32(rec[8:]), be.Uint32(rec[12:])
			tables[tag] = src[off : off+n]
		}
	}
	tags := make([]string, 0, len(tables))
	for tag := range tables {
		tags = append(tags, tag)
	}
	slices.Sort(tags)
	out := append([]byte(nil), src[:4]...)
	out = be.AppendUint16(out, uint16(len(tags)))
	out = append(out, src[6:12]...) // the search fields, which readers ignore
	off := 12 + 16*len(tags)
	var body []byte
	for _, tag := range tags {
		tbl := tables[tag]
		out = append(out, tag...)
		out = be.AppendUint32(out, 0) // checksum, which readers ignore
		out = be.AppendUint32(out, uint32(off+len(body)))
		out = be.AppendUint32(out, uint32(len(tbl)))
		body = append(body, tbl...)
		for len(body)%4 != 0 {
			body = append(body, 0)
		}
	}
	return append(out, body...)
}
