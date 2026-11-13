package richtext

import (
	"math"
	"strconv"

	"github.com/egoist/mygo/ui"
)

// block is an attachment the editor shows as an element of its own: the
// paragraph it is alone in shows the element, and is as high as it is.
type block struct {
	// id is the paragraph's, which keeps the block's element and its
	// height from frame to frame.
	id uint64
	// p is the paragraph's index, as it was built.
	p int
	// handle is the element's, to give it the focus.
	handle ui.Handle
	// h is the height the element was laid out at, 0 until it is.
	h float32
	// x and y are where the element is in the editor's box, as the
	// presses on it are read.
	x, y float32
}

// maxBlocks is how many blocks the editor keeps the heights of: a
// document has few, and the heights of the paragraphs that are gone are
// of no use.
const maxBlocks = 256

// keptBlock is a block that is built whether it is in view or not.
type keptBlock struct {
	// index is the paragraph's.
	index int
	// id is the paragraph's, to tell whether it is the same paragraph.
	id uint64
	// block is the block of that paragraph.
	block *block
}

// blockAttachment returns the attachment of the paragraph, when it is one
// alone in it, with an element to build.
func blockAttachment(para *richPara) *Attachment {
	if para.n != 1 || len(para.runs) == 0 {
		return nil
	}
	r := para.runs[0]
	if r.n != 1 || r.a.Attachment == nil || r.a.Attachment.Element == nil {
		return nil
	}
	return r.a.Attachment
}

// blockAt returns the attachment of paragraph p when the editor shows it
// as an element of its own: an attachment alone in its paragraph, with an
// element to build.
func (v *view) blockAt(p int) *Attachment {
	if p < 0 || p >= len(v.paras) {
		return nil
	}
	at := blockAttachment(v.para(p))
	if at == nil {
		return nil
	}
	if v.compose != "" && v.shown.paraAt(v.caret) == p {
		// A paragraph being composed in is not a block: what is composed
		// shows where the element would be.
		return nil
	}
	return at
}

// blockFor returns the block of the paragraph with the id, and makes it if
// it is not there.
func (v *view) blockFor(id uint64) *block {
	if v.blocks == nil {
		v.blocks = make(map[uint64]*block)
	}
	b := v.blocks[id]
	if b == nil {
		if len(v.blocks) >= maxBlocks {
			clear(v.blocks)
		}
		b = &block{id: id}
		v.blocks[id] = b
	}
	return b
}

// blockOf returns the block of paragraph p, and whether it has one.
func (v *view) blockOf(p int) (*block, bool) {
	if v.blockAt(p) == nil {
		return nil, false
	}
	return v.blockFor(v.para(p).id), true
}

// findKept finds the blocks that are built whether they are in view or
// not: the attachments that ask for it. The text is walked when it
// changes, as the editor's own work of a change is.
func (v *view) findKept() {
	if v.kept != nil {
		return
	}
	v.kept = []keptBlock{}
	seqEach(v.shown.seq(), 0, func(i int, p *richPara) bool {
		at := blockAttachment(p)
		if at == nil || !at.KeepBuilt {
			return true
		}
		v.kept = append(v.kept, keptBlock{index: i, id: p.id, block: v.blockFor(p.id)})
		return true
	})
}

// blockHeight returns the height of the block of paragraph p, and whether
// it has one: the height its element was laid out at, or the height the
// attachment declares, until it is laid out.
func (v *view) blockHeight(p int) (float32, bool) {
	at := v.blockAt(p)
	if at == nil {
		return 0, false
	}
	if b, ok := v.blocks[v.para(p).id]; ok && b.h > 0 {
		return b.h, true
	}
	if at.Height > 0 {
		return at.Height, true
	}
	return v.look.size * 1.3, true
}

// buildBlocks builds the elements of the blocks in view, in the editor's
// box, over the text: they paint over it, and are clipped with it.
func (v *view) buildBlocks(c *ui.Context, e ui.Element) {
	v.built = v.built[:0]
	if len(v.paras) == 0 {
		return
	}
	e.Children(func() {
		for p := v.first; p <= v.last && p < len(v.paras); p++ {
			at := v.blockAt(p)
			if at == nil {
				continue
			}
			v.buildBlock(c, p, at, v.blockFor(v.para(p).id))
		}
		// The blocks that ask for it are built whether they are in view or
		// not: they keep their state and their height as the text is
		// scrolled away, and the text comes back to them as they were.
		for _, k := range v.kept {
			if k.index >= v.first && k.index <= v.last {
				// It is in view: it is built above.
				continue
			}
			if k.index >= len(v.paras) || v.para(k.index).id != k.id {
				// The text changed: the block is found again.
				continue
			}
			at := blockAttachment(v.para(k.index))
			if at == nil {
				continue
			}
			v.buildBlock(c, k.index, at, k.block)
		}
	})
}

// buildBlock builds the element of the block of the paragraph p, where the
// paragraph is.
func (v *view) buildBlock(c *ui.Context, p int, at *Attachment, b *block) {
	left, right := v.insets(v.para(p).style)
	w := max(v.width-left-right, 1)
	_, y := v.origin(p)
	b.p = p
	// The element is where the paragraph is: the text's own place, inside
	// the editor's border.
	b.x, b.y = v.padX+left, v.padY+float32(y-v.scroll)
	box := ui.Box(c).Key("richtext block " + strconv.FormatUint(b.id, 10)).
		Absolute().
		Left(b.x - v.border).
		Top(b.y - v.border).
		Width(w).
		Role(ui.RoleGroup)
	if at.Name != "" {
		box.Label(at.Name)
	}
	box.HandleInput(func(ev ui.InputEvent) bool { return v.blockInput(b, ev) })
	box.Children(func() {
		// The element of the app is what takes the focus, as it is what
		// holds the keys.
		at.Element(c, w).Bind(&b.handle)
	})
	v.built = append(v.built, b)
}

// blockInput takes the input of the element of a block: the wheel
// scrolls the text, as it does over it, and the presses go to what is in
// the element, as they do to a button, and to the text as well, so that
// the caret comes to the block.
func (v *view) blockInput(b *block, ev ui.InputEvent) bool {
	switch ev.Kind {
	case ui.InputScroll:
		// The wheel scrolls the text, as it does over it.
		return v.input(ev)
	case ui.InputPointerDown:
		// The press comes to the text too, so that the caret comes to
		// the block, but it takes no drag: what is in the element takes
		// the press, as a button does.
		ev.X += b.x
		ev.Y += b.y
		v.pressBlock = true
		v.input(ev)
		v.pressBlock = false
	}
	return false
}

// measureBlocks reads the heights the elements of the blocks were laid
// out at, so that the paragraphs below them move as they are high.
func (v *view) measureBlocks() {
	if v.c == nil {
		return
	}
	for _, b := range v.built {
		if r := b.handle.Bounds(v.c); r.H > 0 {
			b.h = r.H
		}
	}
	// A block out of view is not laid out with the paragraphs in view, so
	// its height is given to the text here: it comes back to it as it was.
	for _, k := range v.kept {
		if k.index >= v.first && k.index <= v.last {
			// It is in view: the layout places it.
			continue
		}
		if k.index >= len(v.paras) || v.para(k.index).id != k.id {
			continue
		}
		v.paraLayout(k.index)
	}
}

// blocksMoved asks for another frame when an element of a block is not
// where its paragraph is, as painting placed it otherwise: the frame
// after builds the elements again, where the paragraphs are.
func (v *view) blocksMoved() {
	if v.c == nil || len(v.built) == 0 {
		return
	}
	for _, b := range v.built {
		p := b.p
		if p < 0 || p >= len(v.paras) {
			continue
		}
		left, _ := v.insets(v.para(p).style)
		_, y := v.origin(p)
		r := b.handle.Bounds(v.c)
		// The element is where its paragraph is, in the editor's box.
		want := ui.Rect{
			X: v.rect.X + v.padX + left,
			Y: v.rect.Y + v.padY + float32(y-v.scroll),
		}
		if math.Abs(float64(r.X-want.X)) > 0.5 || math.Abs(float64(r.Y-want.Y)) > 0.5 {
			v.services.Invalidate()
			return
		}
	}
}

// paraOf returns the index of the paragraph with the id, and whether it
// is in the text shown.
func (v *view) paraOf(id uint64) (int, bool) {
	found := -1
	seqEach(v.shown.seq(), 0, func(i int, p *richPara) bool {
		if p.id == id {
			found = i
			return false
		}
		return true
	})
	return found, found >= 0
}

// focusBlock gives the focus to the element of the block the caret is in,
// and reports whether it has one.
func (v *view) focusBlock() bool {
	p := v.shown.paraAt(v.caret)
	b, ok := v.blockOf(p)
	if !ok {
		return false
	}
	v.blockFocus = b
	b.handle.Focus(v.c)
	return true
}

// backToText brings the focus back from a block to the text, with the
// caret on the attachment, as the Escape pressed in it does.
func (v *view) backToText() bool {
	b := v.blockFocus
	if b == nil || v.c == nil || !b.handle.FocusWithin(v.c) {
		return false
	}
	v.blockFocus = nil
	if p, ok := v.paraOf(b.id); ok {
		v.moveTo(v.shown.paraStart(p), ui.TextDownstream, false)
		v.publish()
	}
	v.editor.Focus(v.c)
	return true
}
