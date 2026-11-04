//go:build linux && (amd64 || arm64)

package text

import (
	"sync"

	"github.com/ebitengine/purego"
)

// shapedSetKey names the glyphs of a font that the OpenType features
// shaping applies with a feature list may change or start at.
type shapedSetKey struct {
	font     uintptr // the PangoFont
	features string
}

// Tags of the OpenType and AAT tables HarfBuzz shapes with.
const (
	tagGSUB = 'G'<<24 | 'S'<<16 | 'U'<<8 | 'B'
	tagGPOS = 'G'<<24 | 'P'<<16 | 'O'<<8 | 'S'
	tagKern = 'k'<<24 | 'e'<<16 | 'r'<<8 | 'n'
	tagKerx = 'k'<<24 | 'e'<<16 | 'r'<<8 | 'x'
	tagMorx = 'm'<<24 | 'o'<<16 | 'r'<<8 | 'x'
)

// hbDefaultFeatures are the features HarfBuzz applies to horizontal text
// of the default shaper (hb-ot-shape.cc), but for mark and mkmk, which
// only attach marks. Pango adds none of its own to them.
var hbDefaultFeatures = [...]string{
	"abvm", "blwm", "ccmp", "locl", "rlig", "rvrn", "ltra", "ltrm",
	"calt", "clig", "curs", "dist", "kern", "liga", "rclt",
}

// hbGlyphClassMark is GDEF's class of mark glyphs.
const hbGlyphClassMark = 3

// hbFeature is HarfBuzz's hb_feature_t.
type hbFeature struct {
	tag, value, start, end uint32
}

// hbLib holds the HarfBuzz functions alone uses. They are bound on its
// first call rather than in loadPango, so that apps which never lay out
// text alone (no terminal) do not link them.
var hbLib struct {
	once sync.Once
	ok   bool

	fontGetFace         func(font uintptr) uintptr
	setCreate           func() uintptr
	setDestroy          func(set uintptr)
	setHas              func(set uintptr, v uint32) int32
	setNext             func(set uintptr, v *uint32) int32
	collectLookups      func(face uintptr, table uint32, scripts, languages, features *uint32, lookups uintptr)
	lookupCollectGlyphs func(face uintptr, table, lookup uint32, before, input, after, output uintptr)
	glyphClass          func(face uintptr, glyph uint32) int32
	faceReferenceTable  func(face uintptr, tag uint32) uintptr
	blobGetLength       func(blob uintptr) uint32
	blobDestroy         func(blob uintptr)
	featureFromString   func(s *byte, n int32, f *hbFeature) int32
	// fcFontGetPattern is Pango's: the fontconfig pattern of a PangoFcFont,
	// whose font features Pango applies. Optional.
	fcFontGetPattern func(font uintptr) uintptr
}

// loadHB binds hbLib once, and tells whether alone can use it.
func loadHB() bool {
	l := &hbLib
	l.once.Do(func() {
		if pangoLib.fontGetHBFont == nil || pangoLib.hbNominalGlyph == nil {
			return
		}
		hb, err := purego.Dlopen("libharfbuzz.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return
		}
		ok := true
		bind := func(lib uintptr, fn any, name string) bool {
			sym, err := purego.Dlsym(lib, name)
			if err != nil || sym == 0 {
				return false
			}
			purego.RegisterFunc(fn, sym)
			return true
		}
		for _, f := range [...]struct {
			fn   any
			name string
		}{
			{&l.fontGetFace, "hb_font_get_face"},
			{&l.setCreate, "hb_set_create"},
			{&l.setDestroy, "hb_set_destroy"},
			{&l.setHas, "hb_set_has"},
			{&l.setNext, "hb_set_next"},
			{&l.collectLookups, "hb_ot_layout_collect_lookups"},
			{&l.lookupCollectGlyphs, "hb_ot_layout_lookup_collect_glyphs"},
			{&l.glyphClass, "hb_ot_layout_get_glyph_class"},
			{&l.faceReferenceTable, "hb_face_reference_table"},
			{&l.blobGetLength, "hb_blob_get_length"},
			{&l.blobDestroy, "hb_blob_destroy"},
			{&l.featureFromString, "hb_feature_from_string"},
		} {
			ok = bind(hb, f.fn, f.name) && ok
		}
		if ft, err := purego.Dlopen("libpangoft2-1.0.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL); err == nil {
			bind(ft, &l.fcFontGetPattern, "pango_fc_font_get_pattern")
		}
		l.ok = ok
	})
	return l.ok
}

// alone returns the glyph of r in f, and whether shaping a text in f with
// features gives r that glyph and advance wherever it stands: the font has
// the glyph, it is no mark, and no lookup of a feature HarfBuzz applies
// can match at it. Lookups only match at a glyph in their input: text of
// such glyphs alone shapes as each of them does alone.
func (e *pangoEngine) alone(f *Font, features string, r rune) (uint32, bool) {
	if f == nil || !loadHB() {
		return 0, false
	}
	l := &hbLib
	hb := pangoLib.fontGetHBFont(f.native)
	if hb == 0 {
		return 0, false
	}
	var id uint32
	if pangoLib.hbNominalGlyph(hb, r, &id) == 0 || id == 0 {
		return 0, false
	}
	face := l.fontGetFace(hb)
	if l.glyphClass(face, id) == hbGlyphClassMark {
		return 0, false
	}
	a, _ := e.aloneState.(*pangoAlone)
	if a == nil {
		a = &pangoAlone{sets: map[shapedSetKey]uintptr{}}
		e.aloneState = a
	}
	key := shapedSetKey{f.native, features}
	set, ok := a.sets[key]
	if !ok {
		set = shapedGlyphs(f.native, face, features)
		a.sets[key] = set
	}
	return id, set != 0 && l.setHas(set, id) == 0
}

// shapedGlyphs returns the set of glyphs of a face that the lookups of the
// features shaping applies with a feature list (Style.Features) take as
// input, in GSUB and GPOS: the glyphs they may match at, substitute or
// move. It returns 0, no glyph being safe, for a face HarfBuzz shapes with
// tables other than GSUB and GPOS: AAT's morx and kerx, and the legacy
// kern table while the kern feature is on.
func shapedGlyphs(font, face uintptr, list string) uintptr {
	l := &hbLib
	on := map[string]bool{}
	for _, t := range hbDefaultFeatures {
		on[t] = true
	}
	// Pango applies the features fontconfig gives the font, then those of
	// the text, which win.
	for _, f := range fcFontFeatures(font) {
		t := string([]byte{byte(f.tag >> 24), byte(f.tag >> 16), byte(f.tag >> 8), byte(f.tag)})
		switch {
		case f.value != 0:
			on[t] = true
		case f.start == 0 && f.end == ^uint32(0):
			on[t] = false // off for all the text; off for a part leaves it on
		}
	}
	for _, f := range features(list) {
		on[string(f.tag[:])] = f.value != 0
	}
	if hasTable(face, tagMorx) || hasTable(face, tagKerx) || on["kern"] && hasTable(face, tagKern) {
		return 0
	}
	// The mark features attach marks to what comes before them: a text
	// without marks, which alone leaves out, never reaches them. The
	// others any value turns on.
	delete(on, "mark")
	delete(on, "mkmk")
	tags := make([]uint32, 0, len(on)+1)
	for t, ok := range on {
		if ok {
			tags = append(tags, uint32(t[0])<<24|uint32(t[1])<<16|uint32(t[2])<<8|uint32(t[3]))
		}
	}
	tags = append(tags, 0) // HB_TAG_NONE ends the list
	input, context := l.setCreate(), l.setCreate()
	defer l.setDestroy(context)
	for _, table := range [...]uint32{tagGSUB, tagGPOS} {
		// All scripts and languages: Pango picks them by the text and
		// the locale.
		lookups := l.setCreate()
		l.collectLookups(face, table, nil, nil, &tags[0], lookups)
		lookup := ^uint32(0) // HB_SET_VALUE_INVALID starts the iteration
		for l.setNext(lookups, &lookup) != 0 {
			l.lookupCollectGlyphs(face, table, lookup, context, input, context, context)
		}
		l.setDestroy(lookups)
	}
	return input
}

// hasTable tells whether a face has a table.
func hasTable(face uintptr, tag uint32) bool {
	l := &hbLib
	blob := l.faceReferenceTable(face, tag)
	defer l.blobDestroy(blob)
	return l.blobGetLength(blob) > 0
}

// fcFontFeatures returns the features fontconfig gives a PangoFcFont
// (FC_FONT_FEATURES), which Pango applies before those of the text.
func fcFontFeatures(font uintptr) []hbFeature {
	l := &hbLib
	if l.fcFontGetPattern == nil || pangoLib.fcPatternGetString == nil {
		return nil
	}
	pattern := l.fcFontGetPattern(font)
	if pattern == 0 {
		return nil
	}
	var out []hbFeature
	for i := int32(0); ; i++ {
		var s *byte
		if pangoLib.fcPatternGetString(pattern, "fontfeatures", i, &s) != fcResultMatch || s == nil {
			return out
		}
		var f hbFeature
		if l.featureFromString(s, -1, &f) != 0 {
			out = append(out, f)
		}
	}
}

// pangoAlone is what alone keeps in the Pango engine: the glyphs of fonts
// that shaping may change, HarfBuzz sets, by font and features.
type pangoAlone struct {
	sets map[shapedSetKey]uintptr
}

// drop frees the sets of a PangoFont, which another font can take the
// address of once released, or of every font when font is 0.
func (a *pangoAlone) drop(font uintptr) {
	for key, set := range a.sets {
		if font == 0 || key.font == font {
			if set != 0 {
				hbLib.setDestroy(set)
			}
			delete(a.sets, key)
		}
	}
}
