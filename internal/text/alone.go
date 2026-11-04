package text

import "unicode"

// aloneEngine is an engine that tells which characters shaping leaves as
// they are alone (System.AppendAlone).
type aloneEngine interface {
	// alone returns the glyph of r in f, and whether shaping text in f
	// with features gives r that glyph and its advance wherever it stands
	// among such characters.
	alone(f *Font, features string, r rune) (id uint32, ok bool)
}

// aloneTable holds, for a style, each character's glyph alone and whether
// text of such characters shapes as they do alone.
type aloneTable struct {
	key   Style
	ascii [128]aloneGlyph
	other map[rune]aloneGlyph
}

type aloneGlyph struct {
	known, ok bool
	g         Glyph
}

// aloneRune tells whether alone considers r: a Latin character, not a
// control, a mark or a format character, which may need the neighbours
// they act on.
func aloneRune(r rune) bool {
	switch {
	case r >= 0x20 && r < 0x7f:
		return true
	case r < 0xa0 || r > 0x24f || r == 0xad: // controls, soft hyphen
		return false
	}
	return !unicode.In(r, unicode.M, unicode.Cf)
}

// Alone lays out text in style on one line, as Shape does, when the
// engine need not shape it: each of its characters is one the font has and
// that no feature shaping applies changes or moves, as letters and digits
// are in most fonts, and takes its glyph alone, one after another. Then it
// calls glyph with each glyph in order, and returns true; otherwise it
// returns false without calling it. It shapes each character once, so text
// that changes in every frame, as a terminal's, is laid out without
// shaping.
func (s *System) Alone(text []rune, style Style, glyph func(Glyph)) bool {
	if len(text) == 0 || style.LetterSpacing != 0 {
		return len(text) == 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.engine().(aloneEngine); !ok {
		return false
	}
	t := s.aloneTable(style)
	for _, r := range text {
		if !t.glyph(s, style, r).ok {
			return false
		}
	}
	x := float32(0)
	for i, r := range text {
		g := t.glyph(s, style, r).g
		g.Cluster = i
		g.X += x
		x += g.Advance
		glyph(g)
	}
	return true
}

// aloneCache holds the glyphs of characters alone, by style, made in
// generation gen.
type aloneCache struct {
	tables map[Style]*aloneTable
	last   *aloneTable
	gen    uint64
}

// aloneTable returns the table of the glyphs of characters alone in style.
func (s *System) aloneTable(style Style) *aloneTable {
	c, _ := s.alone.(*aloneCache)
	if c == nil || c.gen != s.gen {
		c = &aloneCache{tables: map[Style]*aloneTable{}, gen: s.gen}
		s.alone = c
	}
	key := Style{Family: style.Family, Size: style.FontSize(), Weight: style.weight(), Italic: style.Italic, Features: style.Features, Lang: style.Lang}
	if c.last != nil && c.last.key == key {
		return c.last
	}
	t := c.tables[key]
	if t == nil {
		if len(c.tables) >= 64 {
			clear(c.tables)
		}
		t = &aloneTable{key: key}
		c.tables[key] = t
	}
	c.last = t
	return t
}

// glyph returns r's entry, shaping it alone the first time.
func (t *aloneTable) glyph(s *System, style Style, r rune) aloneGlyph {
	if r < 0x80 {
		if a := t.ascii[r]; a.known {
			return a
		}
	} else if a, ok := t.other[r]; ok {
		return a
	}
	a := s.aloneGlyph(style, r)
	if r < 0x80 {
		t.ascii[r] = a
	} else {
		if t.other == nil {
			t.other = map[rune]aloneGlyph{}
		}
		t.other[r] = a
	}
	return a
}

// aloneGlyph shapes r alone in style, and tells whether it takes the same
// glyph and advance in any text of such characters: the engine says no
// feature changes it, and the font of the style, not a fallback, has it.
func (s *System) aloneGlyph(style Style, r rune) aloneGlyph {
	a := aloneGlyph{known: true}
	if !aloneRune(r) {
		return a
	}
	f := s.font(style)
	id, ok := s.engine().(aloneEngine).alone(f, style.Features, r)
	if !ok {
		return a
	}
	c := s.layout(Params{Text: string(r), Style: style, NoWrap: true, KeepSpaces: true})
	l := &c.layout
	if len(l.Lines) != 1 || len(l.Lines[0].Glyphs) != 1 {
		return a
	}
	g := l.Lines[0].Glyphs[0]
	if g.Font != f || g.ID != id || g.Cluster != 0 || g.Runes != 1 || g.RTL || g.Object {
		return a
	}
	g.X += l.Lines[0].X
	a.g, a.ok = g, true
	return a
}
