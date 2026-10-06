package ui

import (
	"math"

	"github.com/egoist/mygo/internal/text"
)

// RichTextEditor creates a multiline native editor for state. It shares the
// system shaping, scrolling, selection, IME and edit-menu infrastructure of
// TextArea, with styled paragraphs and character ranges. Cmd/Ctrl+B, I and U
// format the selection or typing style; Cmd/Ctrl-click opens a link. Changed
// reports committed document edits, including formatting, not IME previews.
// ReadOnly, Placeholder and ordinary Element styling work as on TextArea.
func RichTextEditor(c *Context, state *RichEditorState) *Element {
	if state == nil {
		panic("ui: RichTextEditor requires a state")
	}
	e := c.newElement(kindInput)
	e.flags |= flagEditable | flagFocusable | flagHover | flagScrollY
	e.widget = "RichTextEditor"
	st := e.st
	if st.editor == nil {
		st.editor = newEditor()
	}
	ed := st.editor
	if ed.rich != nil && ed.rich != state {
		ed.detachRich()
	}
	state.mu.Lock()
	if ed.rich != state || ed.richVersion != state.version {
		ed.rich, ed.richCompose, ed.richCommit, ed.richCommitPreview, ed.compose = state, nil, nil, nil, ""
		ed.loadRich(state.current)
		ed.richStarting, ed.richDeleted = false, false
	}
	state.wake = c.rt.host.invalidate
	state.owner = ed
	ed.richOpenLink = func(url string) { c.rt.host.openURL(url, nil) }
	ed.multiline = true
	if ed.area == nil {
		ed.area = &area{reveal: true}
	}
	before := state.current.document
	focused := c.rt.focused == e.id
	if focused || len(ed.queue) > 0 {
		if ed.richCompose == nil {
			ed.publishRichSelection()
		}
		ed.process(c, e)
	}
	if !focused {
		ed.cancelRichCompose()
		ed.clearRichCommit()
	}
	if !before.equal(state.current.document) {
		st.changed, c.rt.consumed = true, true
	}
	ed.richVersion = state.version
	state.mu.Unlock()
	ed.readOnly = false
	t := c.theme
	e.Padding(t.Space(1.5), t.Space(2.5)).Radius(t.Radius).Background(t.Surface).Border(1, t.Border).MinHeight(t.Space(20))
	e.styleFn = func(e *Element) { inputBorder(t, e, e) }
	return e
}

func (ed *editor) detachRich() {
	if s := ed.rich; s != nil {
		s.mu.Lock()
		if s.owner == ed {
			s.wake, s.owner = nil, nil
		}
		s.mu.Unlock()
	}
}

func (ed *editor) loadRich(snapshot richSnapshot) {
	ed.richDocument = snapshot.document
	ed.buf.set(snapshot.document.text)
	ed.anchor, ed.caret = snapshot.selection.Anchor, snapshot.selection.Caret
	ed.hasDesired = false
	if ed.area != nil {
		ed.area.reveal = true
	}
}

// publishRichSelection runs with the state's lock held. Pointer input updates
// the local selection between frames; a frame publishes it before commands.
func (ed *editor) publishRichSelection() {
	s := ed.rich
	sel := normalizedSelection(ed.richDocument, TextSelection{ed.anchor, ed.caret})
	ed.anchor, ed.caret = sel.Anchor, sel.Caret
	if sel != s.current.selection {
		s.current.selection = sel
		s.current.typingSet, s.typing = false, false
	}
}

// replaceRich follows record, which has already captured the full styled
// document and the directional selection before the edit.
func (ed *editor) replaceRich(r TextRange, fragment RichDocument) {
	s := ed.rich
	r = ed.richDocument.NormalizeRange(r)
	s.current.document = ed.richDocument.Replace(r, fragment)
	i := s.current.document.snap(r.Start+fragment.n, true)
	s.current.selection = TextSelection{i, i}
	s.finish()
	ed.loadRich(s.current)
}

func (ed *editor) cancelRichCompose() {
	if ed.richCompose != nil {
		ed.loadRich(*ed.richCompose)
		ed.richCompose = nil
	}
	ed.compose, ed.composeCaret = "", 0
	ed.richStarting = false
}

// richPreview returns the uncommitted text after the selection and any GTK
// delete-surrounding requests were removed. The canonical state stays intact.
func (ed *editor) richPreview() richSnapshot {
	preview := ed.rich.current
	preview.document = ed.richDocument
	preview.selection = TextSelection{ed.anchor, ed.caret}
	return preview
}

func (ed *editor) beginRichPreview(removeSelection bool) {
	before := ed.rich.current
	ed.richCompose = &before
	preview := before
	r := before.selection.Range()
	if removeSelection {
		preview.document = before.document.Replace(r, RichDocument{})
		preview.selection = TextSelection{r.Start, r.Start}
	}
	ed.richStarting = !removeSelection
	ed.loadRich(preview)
	ed.rich.typing = false
}

func (ed *editor) clearRichCommit() { ed.richCommit, ed.richCommitPreview = nil, nil }

func (ed *editor) processRich(c *Context, e *Element) {
	s := ed.rich
	for _, ev := range ed.queue {
		if ed.readOnly && ev.kind != editCommand && ev.kind != editKey {
			continue
		}
		// GTK deletes surrounding text separately from preedit/commit. Treat
		// those requests as part of an announced preedit transaction. Outside
		// preedit they are ordinary committed edits, as GTK requires.
		if ev.kind == editInsert && ev.text == "" && ev.replace {
			if ed.richCompose == nil {
				if ed.richCommit != nil && ed.richCommitPreview != nil {
					ed.richCompose = ed.richCommit
					ed.loadRich(*ed.richCommitPreview)
				} else {
					sel := normalizedSelection(ed.richDocument, TextSelection{ev.from, ev.to})
					if sel.Anchor == sel.Caret {
						continue
					}
					ed.anchor, ed.caret = sel.Anchor, sel.Caret
					ed.publishRichSelection()
					style := s.typingStyle()
					ed.deleteRange(sel.Range().Start, sel.Range().End)
					s.current.typing, s.current.typingSet = style, true
					s.finish()
					ed.richDeleted = true
					ed.clearRichCommit()
					s.version++
					continue
				}
			}
			r := ed.richDocument.NormalizeRange(TextRange{ev.from, ev.to})
			if ed.richStarting {
				s.current.selection = TextSelection{r.Start, r.End}
				ed.richCompose.selection = s.current.selection
			}
			preview := ed.richPreview()
			preview.document = preview.document.Replace(r, RichDocument{})
			preview.selection = TextSelection{r.Start, r.Start}
			ed.loadRich(preview)
			ed.richStarting = false
			ed.clearRichCommit()
			s.version++
			continue
		}
		// A native commit may follow end-preedit in the same frame or the
		// next. Its offsets refer to the preview, so apply it there while
		// recording the canonical document before any surrounding deletions.
		if ev.kind == editInsert && (ed.richCompose != nil || ed.richCommitPreview != nil) {
			preview := ed.richPreview()
			if ed.richCompose == nil {
				preview = *ed.richCommitPreview
			}
			fragment := s.textFragment(ev.text)
			s.record(false)
			ed.cancelRichCompose()
			ed.loadRich(preview)
			r := preview.selection.Range()
			if ev.replace {
				r = preview.document.NormalizeRange(TextRange{ev.from, ev.to})
			}
			ed.replaceRich(r, fragment)
			s.typing = false
			ed.clearRichCommit()
			s.version++
			continue
		}
		if ev.kind != editCompose {
			ed.cancelRichCompose()
			ed.clearRichCommit()
		}
		if ev.kind != editInsert {
			ed.richDeleted = false
		}
		if ev.replace && (ed.richCompose == nil || ed.richStarting) {
			sel := normalizedSelection(ed.richDocument, TextSelection{ev.from, ev.to})
			ed.anchor, ed.caret = sel.Anchor, sel.Caret
			if ed.richStarting {
				ed.publishRichSelection()
			}
		}
		if ed.richCompose == nil {
			ed.publishRichSelection()
		}
		switch ev.kind {
		case editKey:
			ed.key(c, e.st, ev)
		case editInsert:
			ed.insert(ev.text)
		case editCommand:
			ed.command(c, ev.text)
		case editCompose:
			if ed.readOnly {
				continue
			}
			if ev.begin {
				ed.cancelRichCompose()
				ed.clearRichCommit()
				ed.beginRichPreview(false)
				break
			}
			if ev.text == "" {
				ed.richCommit = ed.richCompose
				if ed.richCompose != nil {
					preview := ed.richPreview()
					ed.richCommitPreview = &preview
				}
				ed.cancelRichCompose()
				break
			}
			if ed.richCompose == nil || ed.richStarting {
				ed.clearRichCommit()
				ed.beginRichPreview(true)
			}
			ed.compose = cleanRichText(ev.text)
			ed.composeCaret = NewRichDocument(ed.compose).snap(ev.caret, false)
			if ed.area != nil {
				ed.area.reveal = true
			}
		}
		if ed.richCompose == nil {
			ed.publishRichSelection()
		}
		s.version++
	}
	ed.queue = ed.queue[:0]
	if ed.dragging && e.st.pressed {
		ed.drag(c.rt.pointerX-e.st.x, c.rt.pointerY-e.st.y)
		ed.publishRichSelection()
	}
}

type richClipboardHost interface {
	readRichClipboard() (plain, html, rtf string)
	writeRichClipboard(plain, html, rtf string)
}

func (ed *editor) richCommand(c *Context, name string) bool {
	h, ok := c.rt.host.(richClipboardHost)
	if !ok {
		return false
	}
	r := TextRange{}
	r.Start, r.End = ed.selection()
	switch name {
	case "copy", "cut":
		if r.Start != r.End {
			f := ed.richDocument.Slice(r)
			h.writeRichClipboard(f.String(), f.HTML(), f.RTF())
			if name == "cut" {
				ed.deleteRange(r.Start, r.End)
			}
		}
		return true
	case "paste":
		plain, html, rtf := h.readRichClipboard()
		fragment, err := ParseRichHTML(html)
		if html == "" || err != nil || fragment.n == 0 {
			fragment, err = ParseRichRTF(rtf)
		}
		if html == "" && rtf == "" || err != nil || fragment.n == 0 {
			fragment = ed.rich.textFragment(plain)
		}
		if fragment.n > 0 {
			ed.record(false)
			ed.replaceRich(r, fragment)
		}
		return true
	}
	return false
}

// richParaSpans styles a paragraph of the preview document, adding the IME
// preview at the caret in the typing style without mutating that document.
func (ed *editor) richParaSpans(p int, accent Color) []Span {
	start, end := ed.buf.paras[p].rune, ed.buf.end(p)
	var spans []Span
	added := false
	addCompose := func() {
		if !added && ed.compose != "" && ed.buf.para(ed.caret) == p {
			style := ed.richDocument.StyleAt(max(ed.caret-1, 0))
			if ed.richCompose != nil {
				style = ed.richCompose.typingStyle()
			}
			spans = append(spans, richSpan(ed.compose, style, accent))
			added = true
		}
	}
	for _, run := range ed.richDocument.runs {
		a, z := max(start, run.Range.Start), min(end, run.Range.End)
		if z <= a {
			continue
		}
		if ed.caret >= a && ed.caret < z && ed.compose != "" && ed.buf.para(ed.caret) == p {
			spans = append(spans, richSpan(ed.buf.slice(a, ed.caret), run.Style, accent))
			addCompose()
			a = ed.caret
		}
		spans = append(spans, richSpan(ed.buf.slice(a, z), run.Style, accent))
	}
	addCompose()
	return spans
}

// visualMove visits whole-grapheme caret positions in screen order. Shaping
// provides their geometry, including RTL runs within an LTR paragraph. At a
// line edge it continues onto the adjacent line in the paragraph's direction.
func (ed *editor) visualMove(right bool) int {
	a := ed.area
	if a == nil || a.version == 0 {
		if right {
			return ed.graphemes.next(&ed.buf, ed.caret)
		}
		return ed.graphemes.prev(&ed.buf, ed.caret)
	}
	p := ed.buf.para(ed.caret)
	l := a.paraLayout(ed, p)
	local := a.local(ed, p, ed.caret)
	line := &l.Lines[l.LineAt(local)]
	x, _, _ := l.Caret(local)
	best, distance := ed.caret, float32(math.MaxFloat32)
	for i := line.Start; i <= line.End; i++ {
		global := a.global(ed, p, i)
		if ed.richDocument.snap(global, false) != global {
			continue
		}
		cx, _, _ := l.Caret(i)
		delta := cx - x
		if !right {
			delta = -delta
		}
		if delta > 0.01 && delta < distance {
			best, distance = global, delta
		}
	}
	if best != ed.caret {
		return best
	}
	if right != line.RTL {
		return ed.graphemes.next(&ed.buf, ed.caret)
	}
	return ed.graphemes.prev(&ed.buf, ed.caret)
}

// richLayoutParams applies paragraph styles while fonts and glyph geometry
// remain the system text engine's responsibility.
func (ed *editor) collapseRichSelection(right bool) int {
	lo, hi := ed.selection()
	if ed.area == nil || ed.area.version == 0 {
		if right {
			return hi
		}
		return lo
	}
	lx, ly, _ := ed.area.caretAt(ed, lo, 0)
	hx, hy, _ := ed.area.caretAt(ed, hi, 0)
	if ly == hy && lx > hx {
		lo, hi = hi, lo
	}
	if right {
		return hi
	}
	return lo
}

func (ed *editor) richLayoutParams(p int, params text.Params, spans []Span) text.Params {
	style := ed.richDocument.paragraph(p)
	params.Align = text.Align(style.Alignment)
	params.Style.LineHeight = style.LineHeight
	params.Style.Italic = false
	params.Spans = encodeSpans(spans)
	return params
}
