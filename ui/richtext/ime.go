package richtext

import (
	"unicode/utf16"
	"unicode/utf8"

	"github.com/egoist/mygo/internal/rope"
	"github.com/egoist/mygo/ui"
)

// The editor is the client of the system's input methods: they read the
// text around the caret and compose there, in UTF-16 units of the text
// with the composition, which is a virtual insertion at the caret until it
// is committed, as one step of undo with its start.

var _ ui.TextInputClient = (*view)(nil)

// units returns the UTF-16 offset of rune i of the text shown.
func (v *view) units(i int) int {
	return rope.Prefix(v.shown.text, rope.ByteOfRune(v.shown.text, max(0, min(i, v.shown.Len())))).Units
}

func countUnits(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

// virtualRune returns the rune of the text with the composition at UTF-16
// unit u of it.
func (v *view) virtualRune(u int) int {
	text := v.composedText()
	return rope.RuneOfUnit(text.text, max(0, u))
}

// virtualUnits returns the UTF-16 offset of rune i of the text with the
// composition.
func (v *view) virtualUnits(i int) int {
	text := v.composedText()
	return rope.Prefix(text.text, rope.ByteOfRune(text.text, max(0, min(i, text.Len())))).Units
}

// composeStart returns where the composition starts in the text an input
// method sees.
func (v *view) composeStart() int {
	if v.pendingSet {
		return v.pendingRange[0]
	}
	return v.caret
}

// composeRange returns the range in shown that the active composition stands
// in for until committed.
func (v *view) composeRange() (int, int) {
	if v.pendingSet {
		return v.pendingRange[0], v.pendingRange[1]
	}
	return v.caret, v.caret
}

// composedText returns the text an input method sees: compose stands in for
// pendingRange until the composition is committed.
func (v *view) composedText() Text {
	if v.composedActive {
		return v.composed
	}
	return v.shown
}

// updateComposition derives the text an input method sees for the current
// pendingRange and compose string, splicing only the affected paragraphs so
// layouts and measured heights elsewhere stay intact.
func (v *view) updateComposition() {
	a, z := v.composeRange()
	v.doc.lock()
	attrs := v.doc.typingAttributes()
	v.doc.mu.Unlock()
	was := v.composedText()
	now := v.shown.ReplaceString(a, z, v.compose, attrs)
	newLen := utf8.RuneCountInString(v.compose)
	c := change{start: a, oldEnd: z, newEnd: a + newLen}
	if v.composedActive && v.composedRange == [2]int{a, z} {
		c = change{start: a, oldEnd: a + v.composedLen, newEnd: a + newLen}
	}
	if len(v.paras) == was.ParagraphCount() {
		v.replacedParas(was, now, c, nil)
	} else if len(v.paras) != now.ParagraphCount() {
		v.paras = make([]paraView, now.ParagraphCount())
		v.laid = 0
		v.resetHeights()
	}
	v.setComposed(now, a, z, newLen)
}

func (v *view) setComposed(now Text, a, z, cl int) {
	v.composed = now
	v.composedActive = true
	v.composedRange = [2]int{a, z}
	v.composedLen = cl
	v.composedParaA = v.shown.paraAt(a)
	v.composedDocZ = v.shown.paraAt(z)
	v.composedParaZ = now.paraAt(a + cl)
}

// docPara maps a paragraph index in composedText() to its index in v.shown.
func (v *view) docPara(p int) int {
	if !v.composedActive || p <= v.composedParaA {
		return p
	}
	if p <= v.composedParaZ {
		return v.composedParaA
	}
	return p + v.composedDocZ - v.composedParaZ
}

// composedPara maps a paragraph index in v.shown to its index in composedText().
func (v *view) composedPara(p int) int {
	if !v.composedActive || p <= v.composedParaA {
		return p
	}
	if p <= v.composedDocZ {
		return v.composedParaA
	}
	return p + v.composedParaZ - v.composedDocZ
}

// composedLocal returns where rune i of the document is in the text an
// input method sees.
func (v *view) composedLocal(i int) int {
	if !v.pendingSet && v.compose == "" {
		return i
	}
	a, z := v.composeRange()
	if i <= a {
		return i
	}
	if i < z {
		return a
	}
	return a + utf8.RuneCountInString(v.compose) + i - z
}

// documentRune returns the rune of the document at rune i of the text an
// input method sees.
func (v *view) documentRune(i int) int {
	if !v.pendingSet && v.compose == "" {
		return min(i, v.shown.Len())
	}
	a, z := v.composeRange()
	if i <= a {
		return i
	}
	if i < a+utf8.RuneCountInString(v.compose) {
		return a
	}
	return min(z+i-a-utf8.RuneCountInString(v.compose), v.shown.Len())
}

func (v *view) resetCompositionState() {
	v.compose, v.composing, v.composeStep = "", false, false
	v.composeCaret, v.composeSelected = 0, ui.TextInputRange{}
	v.pendingRange, v.pendingSet = [2]int{}, false
	v.composed, v.composedActive = Text{}, false
	v.composedRange, v.composedLen = [2]int{}, 0
	v.composedParaA, v.composedParaZ, v.composedDocZ = 0, 0, 0
}

func (v *view) clearComposition() {
	if !v.composedActive && !v.pendingSet && v.compose == "" && !v.composing && !v.composeStep {
		return
	}
	wasActive := v.composedActive
	was := v.composed
	r := v.composedRange
	cl := v.composedLen
	v.resetCompositionState()
	v.coalesce = false
	if !wasActive {
		return
	}
	if len(v.paras) == was.ParagraphCount() {
		v.replacedParas(was, v.shown, change{start: r[0], oldEnd: r[0] + cl, newEnd: r[1]}, nil)
	} else if len(v.paras) != v.shown.ParagraphCount() {
		v.paras = make([]paraView, v.shown.ParagraphCount())
		v.laid = 0
		v.resetHeights()
	}
}

func (v *view) TextForRange(r ui.TextInputRange) (string, ui.TextInputRange) {
	v.sync()
	if r.Start > r.End {
		r.Start, r.End = r.End, r.Start
	}
	text := v.composedText()
	a, z := v.virtualRune(r.Start), v.virtualRune(r.End)
	actual := ui.TextInputRange{Start: v.virtualUnits(a), End: v.virtualUnits(z)}
	return text.Slice(a, z).String(), actual
}

func (v *view) Selection() ui.TextInputSelection {
	v.sync()
	if v.compose != "" || v.pendingSet {
		start := v.virtualUnits(v.composeStart())
		return ui.TextInputSelection{Range: ui.TextInputRange{Start: start + v.composeSelected.Start, End: start + v.composeSelected.End}}
	}
	a, z := v.selection()
	return ui.TextInputSelection{Range: ui.TextInputRange{Start: v.units(a), End: v.units(z)}, Reversed: v.caret == a && a != z}
}

func (v *view) MarkedRange() (ui.TextInputRange, bool) {
	if v.compose == "" && !v.pendingSet {
		return ui.TextInputRange{}, false
	}
	start := v.virtualUnits(v.composeStart())
	return ui.TextInputRange{Start: start, End: start + countUnits(v.compose)}, true
}

func (v *view) ReplaceText(r *ui.TextInputRange, s string) {
	v.sync()
	if v.opts.ReadOnly {
		return
	}
	// As typed text goes in.
	s = normalizeNewlines(s)
	switch {
	case r == nil && (v.compose != "" || v.pendingSet):
		a, z := v.composeRange()
		join := v.composeStep
		v.clearComposition()
		if join {
			v.joinStep()
		} else {
			v.record(false)
		}
		v.replace(a, z, s)
	case r == nil && v.composing:
		join := v.composeStep
		v.clearComposition()
		if join {
			v.joinStep()
		} else {
			v.record(false)
		}
		v.replace(v.caret, v.caret, s)
	case r == nil:
		v.insert(s)
		v.typed(s)
	default:
		join := v.composeStep || v.compose != ""
		v.commitCompose()
		a, z := v.documentRune(v.virtualRune(r.Start)), v.documentRune(v.virtualRune(r.End))
		if a > z {
			a, z = z, a
		}
		if join {
			v.joinStep()
		} else {
			v.record(false)
		}
		v.replace(a, z, s)
	}
	v.clearComposition()
	v.reveal = true
	v.publish()
}

func (v *view) SetMarkedText(r *ui.TextInputRange, s string, selected ui.TextInputRange) {
	v.sync()
	if v.opts.ReadOnly {
		return
	}
	if s == "" && r == nil && v.compose == "" && !v.pendingSet && !v.composing {
		return
	}
	if s == "" && r == nil {
		v.clearComposition()
		v.reveal = true
		v.publish()
		return
	}
	if r != nil {
		if marked, ok := v.MarkedRange(); !v.pendingSet || !ok || *r != marked {
			hadStep := v.compose != "" || v.composeStep
			if v.compose != "" {
				v.commitCompose()
			} else if v.pendingSet {
				v.clearComposition()
			}
			a, z := v.documentRune(v.virtualRune(r.Start)), v.documentRune(v.virtualRune(r.End))
			if a > z {
				a, z = z, a
			}
			if s == "" {
				v.clearComposition()
				if a != z {
					if hadStep {
						v.joinStep()
					} else {
						v.record(false)
					}
					v.replace(a, z, "")
				}
				v.reveal = true
				v.publish()
				return
			}
			v.pendingRange, v.pendingSet = [2]int{a, z}, true
			v.composing = true
			v.composeStep = hadStep
		} else if s == "" {
			v.clearComposition()
			v.reveal = true
			v.publish()
			return
		}
	} else if !v.pendingSet {
		a, z := v.selection()
		v.pendingRange, v.pendingSet = [2]int{a, z}, true
		v.composing = true
	}
	if selected.Start > selected.End {
		selected.Start, selected.End = selected.End, selected.Start
	}
	n := countUnits(s)
	selected.Start, selected.End = min(selected.Start, n), min(selected.End, n)
	selected.Start = countUnits(s[:ui.UTF16ByteOffset(s, selected.Start)])
	selected.End = countUnits(s[:ui.UTF16ByteOffset(s, selected.End)])
	v.compose, v.composeSelected = s, selected
	v.composeCaret = utf8.RuneCountInString(s[:ui.UTF16ByteOffset(s, selected.End)])
	v.updateComposition()
	v.reveal = true
	v.publish()
}

func (v *view) UnmarkText() {
	hadCompose := v.compose != ""
	hadStep := v.composeStep
	if v.compose != "" {
		v.commitCompose()
	} else {
		v.clearComposition()
	}
	// GTK and IMM32 can end the composition before committing their
	// replacement, which shares its step of undo.
	if hadCompose || hadStep {
		v.composing = true
		v.composeStep = true
	}
	v.publish()
}

func (v *view) BoundsForRange(r ui.TextInputRange) (ui.Rect, ui.TextInputRange, bool) {
	v.sync()
	if len(v.paras) == 0 || v.width <= 0 {
		return ui.Rect{}, ui.TextInputRange{}, false
	}
	start, end := v.virtualRune(min(r.Start, r.End)), v.virtualRune(max(r.Start, r.End))
	aff := ui.TextDownstream
	if end == start && v.documentRune(start) == v.caret && v.compose == "" {
		aff = v.affinity
	}
	x, y, h := v.composedCaretAt(start, aff)
	b := ui.Rect{X: v.padX + x, Y: v.padY + float32(y-v.scroll), W: 1, H: h}
	u := v.virtualUnits(start)
	actual := ui.TextInputRange{Start: u, End: u}
	if end > start {
		if x2, y2, _ := v.composedCaretAt(end, ui.TextUpstream); y2 == y {
			b.W = max(1, x2-x)
			actual.End = v.virtualUnits(end)
		}
	}
	visible := b.Y+b.H > 0 && b.Y < v.rect.H
	return b, actual, visible
}

func (v *view) IndexForPoint(p ui.Point) (int, bool) {
	v.sync()
	if v.width <= 0 {
		return 0, false
	}
	x, y := v.contentPoint(p.X, p.Y)
	para, l, _ := v.layoutPositionAt(x, y)
	i := v.composedText().Len()
	if para >= 0 {
		i = v.composedText().paraStart(para) + l
	}
	return v.virtualUnits(i), true
}
