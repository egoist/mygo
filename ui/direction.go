package ui

import "strings"

// LayoutDirection is the reading direction of an interface. It changes
// layout and navigation, independently of bidirectional text shaping.
type LayoutDirection uint8

const (
	// InheritDirection uses the nearest ancestor's direction (the default).
	InheritDirection LayoutDirection = iota
	// AutoDirection uses the configured locale, not the text of a label.
	AutoDirection
	LTR
	RTL
)

// DirectionForLocale returns a locale's interface direction. An explicit
// script takes precedence over the language (ar-Latn is LTR). It accepts
// BCP 47 tags and POSIX locale names; unknown/empty locales are LTR.
func DirectionForLocale(locale string) LayoutDirection {
	locale, _, _ = strings.Cut(locale, ".")
	locale, _, _ = strings.Cut(locale, "@")
	language, rest := locale, ""
	if i := strings.IndexAny(locale, "-_"); i >= 0 {
		language, rest = locale[:i], locale[i+1:]
	}
	for rest != "" {
		part := rest
		if i := strings.IndexAny(rest, "-_"); i >= 0 {
			part, rest = rest[:i], rest[i+1:]
		} else {
			rest = ""
		}
		if len(part) == 1 { // BCP 47 extensions are not script subtags.
			break
		}
		if len(part) == 4 {
			for _, script := range rtlScripts {
				if strings.EqualFold(part, script) {
					return RTL
				}
			}
			return LTR
		}
	}
	for _, lang := range rtlLanguages {
		if strings.EqualFold(language, lang) {
			return RTL
		}
	}
	return LTR
}

// SetLayoutLocale configures this frame's locale. AutoDirection follows it;
// an explicit LTR/RTL direction keeps its value. Call before building
// widgets. A window defaults to mygo.App.Locale; a Tester to en-US.
// An empty tag restores the host locale. This setting only controls layout.
func (c *Context) SetLayoutLocale(locale string) { c.root.layoutLocaleTag = locale }

// LayoutLocale returns the locale configured for the current subtree.
func (c *Context) LayoutLocale() string {
	if c.parent == c.overlay && c.overlayOwner != nil {
		return c.overlayOwner.layoutLocale()
	}
	return c.parent.layoutLocale()
}

// SetDirection sets the frame's interface direction. The window root
// defaults to AutoDirection. Call before building widgets.
func (c *Context) SetDirection(d LayoutDirection) { c.root.Direction(d) }

// Direction returns the resolved direction of the current subtree.
func (c *Context) Direction() LayoutDirection {
	if c.parent == c.overlay && c.overlayOwner != nil {
		return c.overlayOwner.LayoutDirection()
	}
	return c.parent.LayoutDirection()
}

// Direction overrides inherited interface direction for this element and
// its descendants. Set it before Children; widgets handling input as they
// are created should be placed inside a container with the override.
func (e *Element) Direction(d LayoutDirection) *Element {
	if d > RTL {
		panic("ui: invalid layout direction")
	}
	e.direction = d
	return e
}

// LayoutLocale overrides the locale inherited by AutoDirection in this subtree.
// It does not override an explicit direction; use Direction(AutoDirection)
// too when this subtree should choose its own direction from the locale.
func (e *Element) LayoutLocale(locale string) *Element { e.layoutLocaleTag = locale; return e }

func (e *Element) directionParent() *Element {
	if e.popover != nil {
		return e.popover
	}
	if e.directionOwner != nil {
		return e.directionOwner
	}
	if e.id == overlayID && e.parent == nil {
		return e.c.root
	}
	return e.parent
}

func (e *Element) layoutLocale() string {
	for p := e; p != nil; p = p.directionParent() {
		if p.layoutLocaleTag != "" {
			return p.layoutLocaleTag
		}
	}
	return e.c.hostLayoutLocale
}

// LayoutDirection returns LTR or RTL after resolving inheritance and
// automatic locale configuration. Attached overlays inherit their anchor.
func (e *Element) LayoutDirection() LayoutDirection {
	for p := e; p != nil; p = p.directionParent() {
		switch p.direction {
		case LTR, RTL:
			return p.direction
		case AutoDirection:
			return e.localeDirection()
		}
	}
	return e.localeDirection()
}

func (e *Element) rtl() bool { return e.LayoutDirection() == RTL }

// inlineReverse is the direction of horizontal values/rows. Reverse is
// relative to reading direction, just as CSS row-reverse is.
func (e *Element) inlineReverse() bool { return e.rtl() != e.reverse }

func (e *Element) inlineKeys() (previous, next Key) {
	previous, next = KeyLeft, KeyRight
	// Input belongs to the last committed layout, including overrides the
	// caller chains onto a widget after its constructor handled input.
	if e.st.inlineReverse {
		previous, next = next, previous
	}
	return
}

func (e *Element) directionKeys() (previous, next Key) {
	previous, next = KeyLeft, KeyRight
	if e.st.rtl {
		previous, next = next, previous
	}
	return
}

// logicalEdges keeps authored start/end values separate from the physical
// edges; they resolve when measuring, after all direction overrides exist.
type logicalEdges struct {
	values   [3][2]float32 // padding, margin, border
	set      [3]uint8
	inset    [2]length
	insetSet uint8
}

func (e *Element) logicalEdge(k, side int, v float32) *Element {
	if e.logical == nil {
		e.logical = &logicalEdges{}
	}
	e.logical.values[k][side] = v
	e.logical.set[k] |= 1 << side
	return e
}

// PaddingStart and PaddingEnd set padding at the inline reading edges:
// left/right in LTR and right/left in RTL. Logical edges override physical
// edges; Padding/PaddingX reset both logical padding overrides.
func (e *Element) PaddingStart(v float32) *Element { return e.logicalEdge(0, 0, v) }
func (e *Element) PaddingEnd(v float32) *Element   { return e.logicalEdge(0, 1, v) }

// MarginStart and MarginEnd set logical margins, including Auto. Margin
// and MarginX reset both logical margin overrides.
func (e *Element) MarginStart(v float32) *Element { return e.logicalEdge(1, 0, v) }
func (e *Element) MarginEnd(v float32) *Element   { return e.logicalEdge(1, 1, v) }

// BorderStart and BorderEnd set logical border widths. Border and
// BorderWidth reset both logical border overrides.
func (e *Element) BorderStart(v float32) *Element { return e.logicalEdge(2, 0, v) }
func (e *Element) BorderEnd(v float32) *Element   { return e.logicalEdge(2, 1, v) }

func (e *Element) logicalInset(side int, v length) *Element {
	if e.logical == nil {
		e.logical = &logicalEdges{}
	}
	e.logical.inset[side] = v
	e.logical.insetSet |= 1 << side
	return e
}

// InsetStart and InsetEnd place an absolute element from a logical edge,
// or move an in-flow element from its layout position. Logical insets
// override physical ones on the same edge, independently of call order.
func (e *Element) InsetStart(v float32) *Element { return e.logicalInset(0, px(v)) }
func (e *Element) InsetEnd(v float32) *Element   { return e.logicalInset(1, px(v)) }

// InsetStartPercent and InsetEndPercent use a percentage of parent width.
func (e *Element) InsetStartPercent(v float32) *Element { return e.logicalInset(0, percent(v)) }
func (e *Element) InsetEndPercent(v float32) *Element   { return e.logicalInset(1, percent(v)) }

func (e *Element) clearLogical(k int) {
	if e.logical != nil {
		e.logical.set[k] = 0
	}
}

func (e *Element) resolveEdges() {
	l := e.logical
	if l == nil {
		return
	}
	start, end := 3, 1
	if e.rtl() {
		start, end = end, start
	}
	for k, edges := range []*[4]float32{&e.pad, &e.margin, &e.border} {
		if l.set[k]&1 != 0 {
			edges[start] = l.values[k][0]
		}
		if l.set[k]&2 != 0 {
			edges[end] = l.values[k][1]
		}
	}
	if l.insetSet&1 != 0 {
		e.inset[start] = l.inset[0]
	}
	if l.insetSet&2 != 0 {
		e.inset[end] = l.inset[1]
	}
}

func resolveTreeEdges(e *Element) {
	e.resolveEdges()
	for ch := e.first; ch != nil; ch = ch.next {
		resolveTreeEdges(ch)
	}
}

func directionDelta(v float32, rtl bool) float32 {
	if rtl {
		return -v
	}
	return v
}

var rtlLanguages = [...]string{"ar", "he", "iw", "fa", "ur", "ps", "sd", "ug", "yi", "ji", "dv", "ckb", "ks", "syr", "arc", "nqo", "lrc", "bgn", "bal", "khw", "pnb", "prs"}
var rtlScripts = [...]string{"arab", "hebr", "syrc", "thaa", "nkoo", "adlm", "rohg", "mand", "samr", "mend", "merc", "mero", "armi", "avst", "phli", "phlp", "prti", "palm", "hatr", "hung", "lydi", "narb", "nbat", "orkh", "phnx", "sarb", "sogd", "sogo", "elym", "chrs", "yezi", "gara"}

// Locale parsing is independent of the element tree; keep a small per-window
// cache so measuring thousands of nodes does not parse the same tag again.
func (e *Element) localeDirection() LayoutDirection {
	tag := e.layoutLocale()
	c := e.c
	if d, ok := c.directionLocales[tag]; ok {
		return d
	}
	d := DirectionForLocale(tag)
	if c.directionLocales == nil {
		c.directionLocales = make(map[string]LayoutDirection)
	}
	if len(c.directionLocales) < 16 {
		c.directionLocales[tag] = d
	}
	return d
}
