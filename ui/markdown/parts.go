package markdown

import (
	"strings"

	"github.com/egoist/mygo/internal/l10n"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown/parser"
)

// partSize is how much source a part holds before the next block starts
// another: less than a window of text, so that the parts a list builds
// beyond its view are short.
const partSize = 1024

// Parts shows one document in parts, each built on its own, as the rows
// of a List: a long reply then builds only the parts in view, and the
// rows the list keeps. It is parsed once, so link references, footnotes
// and headings' anchors are the whole document's, and with no gap
// between them, its parts are spaced as the blocks of a View. Keep one
// per document, as per message of a chat, in the app's state; its zero
// value is ready.
//
// A part is whole top-level blocks: it starts at a heading written with
// #, or at the first block once the part has about a kilobyte of
// source. Text appended to the source parses only its last top-level
// block again, so the parts before keep their keys as a reply streams
// in.
//
// The text of parts is selectable with that of a Selectable container
// around the list, as Options.ShareSelection has it, whatever it says:
// the list keeps the parts holding the selection's ends built, and its
// ListState.Text, given Text, copies the parts it does not build.
//
//	n := m.parts.Update(c, m.text, &app.md, !m.done)
//	ui.List(c, &m.list, n, func(i int) { m.parts.View(c, i) })
type Parts struct {
	st    *viewState
	parts []part
	// locale is the window's, for the text of images not loaded.
	locale string
	// target is the block st.scrollTo was when an update last looked,
	// asked the updates it has waited since, and told whether Scroll
	// reported it.
	target int
	asked  int
	told   bool
}

// part is the top-level blocks from from to to, spaced after prev, the
// block shown before them, -1 for none; or the footnotes, from -1.
type part struct {
	from, to, prev int
	key            int
}

// Update reads src for this frame, as View shows it, or as Stream does
// while streaming, and returns how many parts it shows in. Call it every
// frame, before the list, and for every document whose parts the list
// may show; o may be nil.
func (p *Parts) Update(c *ui.Context, src string, o *Options, streaming bool) int {
	if o == nil {
		o = &defaultOptions
	}
	if p.st == nil {
		p.st = newState()
		p.target = -1
	}
	st := p.st
	st.elems = st.elems[:0]
	if st.update(c, src, o, streaming) || st.reveal.fading() {
		c.AnimationFrame()
	}
	p.locale = c.Locale()
	d := st.doc
	if st.scrollTo >= d.Len() {
		st.scrollTo = -1 // a block of a document since shortened
	}
	p.parts = p.parts[:0]
	prev := -1
	for j := 1; j < d.Block(0).Next; j = d.Block(j).Next {
		if !st.shows(j) {
			continue
		}
		b := d.Block(j)
		if n := len(p.parts); n == 0 || atxHeading(d, b) || b.Start-p.parts[n-1].key >= partSize {
			if n > 0 {
				p.parts[n-1].to = j
			}
			p.parts = append(p.parts, part{from: j, prev: prev, key: b.Start})
		}
		prev = j
	}
	if n := len(p.parts); n > 0 {
		p.parts[n-1].to = d.Block(0).Next
	}
	if len(st.notes) > 0 {
		p.parts = append(p.parts, part{from: -1, key: -1})
	}
	// A block to scroll to waits for its part to be built, as the app
	// scrolls the list there, and no longer: the user scrolling there
	// later is not asking for it.
	switch {
	case st.scrollTo != p.target:
		p.target, p.asked, p.told = st.scrollTo, 0, false
	case st.scrollTo >= 0:
		if p.asked++; p.asked > 2 {
			st.scrollTo, p.target = -1, -1
		}
	}
	return len(p.parts)
}

// atxHeading reports whether block b is a heading written with #, whose
// line has ended, which no text after it turns into something else: one
// to six #, then a space, a tab or the end of the line.
func atxHeading(d *parser.Document, b *parser.Block) bool {
	if b.Kind != parser.Heading {
		return false
	}
	src := d.Source()
	end := strings.IndexAny(src[b.Start:], "\r\n")
	if end < 0 {
		return false // more # may come
	}
	line := strings.TrimLeft(src[b.Start:b.Start+end], " ")
	n := len(line) - len(strings.TrimLeft(line, "#"))
	return n >= 1 && n <= 6 && (n == len(line) || line[n] == ' ' || line[n] == '\t')
}

// Key returns the identity of part i: the offset in the source of its
// first block, which text appended to the source does not change, or -1
// for the footnotes, which come last. Key a list's rows by it. A part the
// document no longer has, as one asked for by the rows of a frame before
// the reply changed, is -2.
func (p *Parts) Key(i int) int {
	if i < 0 || i >= len(p.parts) {
		return -2
	}
	return p.parts[i].key
}

// View builds part i, as View, or Stream, builds those blocks of the
// document.
func (p *Parts) View(c *ui.Context, i int) ui.Element {
	st, pt := p.st, p.parts[i]
	root := ui.Column(c)
	st.style(root)
	return root.Children(func() {
		if pt.from < 0 {
			st.footnotes(c)
			return
		}
		st.blockRange(c, pt.from, pt.to, pt.prev, false, spanStyle{})
	})
}

// Revealed reports whether the document shows all of its source, done
// fading in, as the function Revealed does for a view.
func (p *Parts) Revealed() bool { return p.st != nil && p.st.reveal.revealed() }

// Scroll returns the part holding what a click on a link to a heading or
// a footnote, or Options.Anchor, asks to show, once: scroll the list to
// it, and the view scrolls to the heading or the footnote as the part is
// built. ok is false when nothing asks.
//
//	if i, ok := m.parts.Scroll(); ok {
//		app.list.ScrollTo(m.firstRow+i, ui.Start)
//	}
func (p *Parts) Scroll() (part int, ok bool) {
	if p.st == nil || p.st.scrollTo < 0 || p.told {
		return 0, false
	}
	// Told, it waits for the part as one asked by the last update does.
	p.target, p.asked, p.told = p.st.scrollTo, 0, true
	return p.partOf(p.st.scrollTo)
}

// partOf returns the part showing block i.
func (p *Parts) partOf(i int) (int, bool) {
	d := p.st.doc
	if i <= 0 || i >= d.Len() {
		return 0, false
	}
	for d.Block(i).Parent > 0 {
		i = d.Block(i).Parent
	}
	for k, pt := range p.parts {
		if d.Block(i).Kind == parser.FootnoteDefinition {
			if pt.from < 0 {
				return k, true
			}
		} else if pt.from >= 0 && pt.from <= i && i < pt.to {
			return k, true
		}
	}
	return 0, false
}

// Text returns the text of part i as a Selectable container copies it
// when the part is built: a line for each paragraph, heading and table
// cell, the lines of code blocks, and images as their descriptions.
// Blocks that Options builds otherwise give their source: the code of a
// code block, the TeX of math, the HTML of HTML. Give it to the list's
// ListState.Text, which copies the parts the list does not build. A part
// the document no longer has is "".
func (p *Parts) Text(i int) string {
	if i < 0 || i >= len(p.parts) {
		return ""
	}
	st, pt := p.st, p.parts[i]
	w := textWriter{st: st, locale: p.locale}
	if pt.from < 0 {
		d := st.doc
		for j := 1; j < d.Len(); j++ {
			if d.Block(j).Kind == parser.FootnoteDefinition {
				w.children(j)
			}
		}
	} else {
		w.blocks(pt.from, pt.to)
	}
	return w.b.String()
}

// textWriter writes the text of blocks, a line for each element of text
// a view builds of them.
type textWriter struct {
	st      *viewState
	locale  string
	b       strings.Builder
	written bool
	p       para
}

func (w *textWriter) line(s string) {
	if w.written {
		w.b.WriteByte('\n')
	}
	w.b.WriteString(s)
	w.written = true
}

func (w *textWriter) blocks(from, to int) {
	for j := from; j < to; j = w.st.doc.Block(j).Next {
		if w.st.shows(j) {
			w.block(j)
		}
	}
}

func (w *textWriter) children(i int) { w.blocks(i+1, w.st.doc.Block(i).Next) }

func (w *textWriter) block(i int) {
	st := w.st
	d := st.doc
	b := d.Block(i)
	switch b.Kind {
	case parser.Paragraph:
		if in := d.Inlines(i); imagesOnly(in) {
			for k := 0; k < len(in); k = in[k].Next {
				if in[k].Kind == parser.Image && (in[k].Pending || !st.imageLoaded(in[k].Dest)) {
					alt := parser.PlainText(in[k+1 : in[k].Next])
					if alt == "" {
						alt = l10n.In(w.locale).Image() // as the view labels it
					}
					w.line(alt)
				}
			}
			return
		}
		w.para(i)
	case parser.Heading:
		w.para(i)
	case parser.CodeBlock:
		var lines []string
		for k := range d.LineCount(i) {
			line, _ := d.Line(i, k)
			lines = append(lines, line)
		}
		code := strings.Join(lines, "\n")
		if code == "" {
			code = " " // as an empty block shows
		}
		w.line(code)
	case parser.HTMLBlock, parser.MathBlock:
		w.line(strings.TrimRight(d.Lines(i), "\n"))
	case parser.Quote:
		if b.Alert != parser.NoAlert {
			w.line(b.Alert.String())
		}
		w.children(i)
	case parser.List:
		for j := i + 1; j < b.Next; j = d.Block(j).Next {
			w.children(j)
		}
	case parser.Table:
		for r := i + 1; r < b.Next; r = d.Block(r).Next {
			k := 0
			for cell := r + 1; cell < d.Block(r).Next && k < len(b.Align); cell = d.Block(cell).Next {
				w.para(cell)
				k++
			}
		}
	}
}

// para writes the text of the paragraph of block i: what the view built
// of it last, or else as it would build it.
func (w *textWriter) para(i int) {
	st := w.st
	b := st.doc.Block(i)
	st.doc.Inlines(i) // their revision
	p := &w.p
	if i < len(st.paras) {
		if pc := &st.paras[i]; pc.ok && pc.start == b.Start && pc.end == b.End && pc.kind == b.Kind && pc.rev == b.Revision() {
			p = &pc.p
		}
	}
	if p == &w.p {
		st.buildPara(p, i, spanStyle{})
	}
	if w.written {
		w.b.WriteByte('\n')
	}
	for _, pc := range p.pieces {
		for _, sp := range pc.spans {
			w.b.WriteString(sp.Text)
		}
	}
	w.written = true
}
