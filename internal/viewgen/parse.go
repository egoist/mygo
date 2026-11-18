// Package viewgen compiles .view files, markup that owns a screen's UI
// tree, into the Go builder code the ui package runs (#1457). Go keeps
// the state, the handlers and the control flow: nothing of the markup
// runs at the app's run time.
package viewgen

import (
	"fmt"
	"strings"
)

// Position is a place in a .view file: 1-based line and column, the
// column in bytes as Go's own //line directives count it.
type Position struct {
	Line, Col int
}

func (p Position) String() string { return fmt.Sprintf("%d:%d", p.Line, p.Col) }

// Diagnostic is one problem found in a .view file, at its place.
type Diagnostic struct {
	Pos  Position
	Path string // the .view file, as written in the message
	Msg  string
}

func (d Diagnostic) Error() string {
	if d.Path != "" {
		return fmt.Sprintf("%s:%s: %s", d.Path, d.Pos, d.Msg)
	}
	return fmt.Sprintf("%s: %s", d.Pos, d.Msg)
}

// TextPart is a stretch of an element's body or of an attribute value:
// literal text, or a Go expression in braces.
type TextPart struct {
	Lit     string // literal text, entities decoded, whitespace as written
	Expr    string // the Go expression inside the braces, spaces trimmed
	IsExpr  bool
	Pos     Position // of the text, or of the opening brace
	QuoteCh byte     // attribute values: the quote the expression was written in, 0 in body text
}

// Attr is one attribute of an element. Value holds its parts in source
// order; a bare attribute (no value) has none.
type Attr struct {
	Name  string
	Pos   Position // of the name
	Value []TextPart
	Bare  bool
	// Quote is the byte the value was quoted with; 0 when the value was
	// one brace expression without quotes, attr={expr}.
	Quote byte
}

// Node is an element of the markup tree.
type Node struct {
	Tag      string
	Pos      Position // of '<'
	Attrs    []*Attr
	Children []*Node
	// Text is the element's body when it holds text: either one literal
	// run (with no child elements), or mixed parts. Empty for elements
	// whose body holds only elements and indentation.
	Text []TextPart
	// HasBodyText reports text outside braces that is more than
	// indentation whitespace.
	HasBodyText bool
	// HasElements reports child elements in the body.
	HasElements bool
}

// File is a parsed .view file.
type File struct {
	Path string // slash path relative to the package directory
	// Type is the receiver type a <view type="..."> named; "" when the
	// file has none.
	Type      string
	Roots     []*Node
	HasViewEl bool
}

type viewParser struct {
	path string
	src  []byte
	off  int
	line int // 1-based
	col  int // 1-based, in bytes
	diag []Diagnostic
}

// Parse parses the .view source at path into a File. It collects the
// diagnostics it can and returns them with the file; on a fatal error
// the file is nil.
func Parse(path string, src []byte) (*File, []Diagnostic, error) {
	p := &viewParser{path: path, src: src, line: 1, col: 1}
	f, err := p.parse()
	if err != nil {
		return nil, p.diag, err
	}
	return f, p.diag, nil
}

func (p *viewParser) pos() Position { return Position{p.line, p.col} }
func (p *viewParser) errf(pos Position, format string, args ...any) error {
	p.diag = append(p.diag, Diagnostic{Pos: pos, Path: p.path, Msg: fmt.Sprintf(format, args...)})
	return p.diag[len(p.diag)-1]
}

// peek returns the byte at the offset, 0 past the end.
func (p *viewParser) peek(off int) byte {
	if i := p.off + off; i < len(p.src) {
		return p.src[i]
	}
	return 0
}

// next advances one byte, counting lines.
func (p *viewParser) next() byte {
	if p.off >= len(p.src) {
		return 0
	}
	b := p.src[p.off]
	p.off++
	if b == '\n' {
		p.line++
		p.col = 1
	} else {
		p.col++
	}
	return b
}

// spaces advances over ASCII whitespace.
func (p *viewParser) spaces() {
	for {
		switch p.peek(0) {
		case ' ', '\t', '\r', '\n':
			p.next()
		default:
			return
		}
	}
}

func isNameByte(b byte) bool {
	return b == '-' || b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b >= 'A' && b <= 'Z' || b == '_'
}

func (p *viewParser) name() (string, Position) {
	pos := p.pos()
	start := p.off
	for isNameByte(p.peek(0)) {
		p.next()
	}
	return string(p.src[start:p.off]), pos
}

// parse parses the file's elements. The roots are the elements at the
// top, or the children of the one <view> wrapper.
func (p *viewParser) parse() (*File, error) {
	f := &File{Path: p.path}
	var roots []*Node
	for {
		p.spaces()
		if p.off >= len(p.src) {
			break
		}
		if !strings.HasPrefix(string(p.src[p.off:]), "<") {
			return nil, p.errf(p.pos(), "unexpected text at the top of the file")
		}
		if strings.HasPrefix(string(p.src[p.off:]), "<!--") {
			if err := p.comment(); err != nil {
				return nil, err
			}
			continue
		}
		el, err := p.element()
		if err != nil {
			return nil, err
		}
		if el == nil {
			continue
		}
		if el.Tag == "view" {
			if f.HasViewEl {
				return nil, p.errf(el.Pos, "a second <view> element")
			}
			f.HasViewEl, f.Type = true, el.attr("type")
			if el.HasBodyText {
				return nil, p.errf(el.Pos, "<view> holds elements, not text")
			}
			if el.attr("type") == "" {
				return nil, p.errf(el.Pos, "<view> needs a type naming its receiver")
			}
			roots = append(roots, el.Children...)
			continue
		}
		roots = append(roots, el)
	}
	f.Roots = roots
	return f, nil
}

// comment skips <!-- ... -->.
func (p *viewParser) comment() error {
	pos := p.pos()
	p.next() // <
	for i := 0; i < 3; i++ {
		p.next()
	}
	for {
		if p.off >= len(p.src) {
			return p.errf(pos, "comment not closed")
		}
		if strings.HasPrefix(string(p.src[p.off:]), "-->") {
			for i := 0; i < 3; i++ {
				p.next()
			}
			return nil
		}
		p.next()
	}
}

// element parses one element with its attributes, body and end tag.
func (p *viewParser) element() (*Node, error) {
	pos := p.pos()
	if p.next() != '<' {
		return nil, p.errf(pos, "expected <")
	}
	tag, _ := p.name()
	if tag == "" {
		return nil, p.errf(pos, "expected an element name after <; a < in text is written &lt;")
	}
	n := &Node{Tag: tag, Pos: pos}
	// Attributes until > or />.
	for {
		p.spaces()
		switch {
		case strings.HasPrefix(string(p.src[p.off:]), "/>"):
			p.next()
			p.next()
			return n, nil
		case p.peek(0) == '>':
			p.next()
		case p.peek(0) == 0:
			return nil, p.errf(pos, "element <%s> not closed", tag)
		default:
			namePos := p.pos()
			name, _ := p.name()
			if name == "" {
				return nil, p.errf(p.pos(), "expected an attribute or > in <%s>", tag)
			}
			a := &Attr{Name: name, Pos: namePos}
			p.spaces()
			if p.peek(0) != '=' {
				// A bare attribute: fill, bold.
				a.Bare = true
				n.Attrs = append(n.Attrs, a)
				continue
			}
			p.next()
			p.spaces()
			if q := p.peek(0); q == '"' || q == '\'' {
				a.Quote = q
				p.next()
				parts, err := p.attrValue(q)
				if err != nil {
					return nil, err
				}
				a.Value = parts
			} else if q == '{' {
				part, err := p.braced(pos)
				if err != nil {
					return nil, err
				}
				a.Value = []TextPart{part}
			} else {
				return nil, p.errf(p.pos(), "attribute %s needs a value: quote it, or leave it bare", name)
			}
			n.Attrs = append(n.Attrs, a)
			continue
		}
		break
	}
	// The body: text and child elements until the end tag.
	for {
		text, err := p.bodyText(tag)
		if err != nil {
			return nil, err
		}
		if len(text) > 0 {
			n.Text = append(n.Text, text...)
			for _, t := range text {
				if !t.IsExpr && strings.TrimLeft(t.Lit, " \t\r\n") != "" {
					n.HasBodyText = true
				}
				if t.IsExpr {
					n.HasBodyText = true
				}
			}
		}
		p.spaces()
		if strings.HasPrefix(string(p.src[p.off:]), "</") {
			p.next()
			p.next()
			end, _ := p.name()
			p.spaces()
			if p.next() != '>' {
				return nil, p.errf(p.pos(), "expected > after </%s>", tag)
			}
			if end != tag {
				return nil, p.errf(pos, "</%s> closes <%s>", end, tag)
			}
			return n, nil
		}
		if p.off >= len(p.src) {
			return nil, p.errf(pos, "<%s> not closed", tag)
		}
		if strings.HasPrefix(string(p.src[p.off:]), "<!--") {
			if err := p.comment(); err != nil {
				return nil, err
			}
			continue
		}
		if strings.HasPrefix(string(p.src[p.off:]), "</") {
			return nil, p.errf(p.pos(), "unexpected </")
		}
		child, err := p.element()
		if err != nil {
			return nil, err
		}
		n.Children = append(n.Children, child)
		n.HasElements = true
	}
}

// attrValue reads an attribute's quoted value until the closing quote,
// as literal text and {expr} parts.
func (p *viewParser) attrValue(quote byte) ([]TextPart, error) {
	var parts []TextPart
	var lit strings.Builder
	litPos := p.pos()
	flush := func() {
		if lit.Len() > 0 {
			parts = append(parts, TextPart{Lit: decodeEntities(lit.String()), Pos: litPos})
			lit.Reset()
		}
	}
	for {
		if p.off >= len(p.src) {
			return nil, p.errf(litPos, "attribute value not closed")
		}
		b := p.peek(0)
		switch {
		case b == quote:
			p.next()
			flush()
			if len(parts) == 0 {
				// The empty attribute value is one empty literal.
				parts = append(parts, TextPart{Lit: "", Pos: litPos})
			}
			return parts, nil
		case b == '{':
			flush()
			part, err := p.braced(litPos)
			if err != nil {
				return nil, err
			}
			part.QuoteCh = quote
			parts = append(parts, part)
		case b == '<':
			return nil, p.errf(p.pos(), "a < in an attribute value: write it as an expression, as {\"<\"}")
		case b == '&':
			// Entities are decoded wholesale with the literal.
			lit.WriteByte(b)
			p.next()
		case b == '\n':
			return nil, p.errf(p.pos(), "an attribute value runs over lines: put it in one line, or in braces")
		default:
			lit.WriteByte(b)
			p.next()
		}
	}
}

// braced reads one {Go expression}, scanning it as Go so that braces in
// strings, runes, raw strings and comments, and nested braces of
// composite literals, do not end it.
func (p *viewParser) braced(pos Position) (TextPart, error) {
	open := p.pos()
	p.next() // {
	start := p.off
	depth := 1
	for {
		if p.off >= len(p.src) {
			return TextPart{}, p.errf(open, "{ not closed")
		}
		b := p.peek(0)
		switch b {
		case '}':
			depth--
			if depth == 0 {
				src := strings.TrimSpace(string(p.src[start:p.off]))
				p.next()
				if src == "" {
					return TextPart{}, p.errf(open, "empty {} expression")
				}
				return TextPart{Expr: src, IsExpr: true, Pos: open}, nil
			}
			p.next()
		case '{':
			depth++
			p.next()
		case '"':
			// A Go string: its braces do not count. A backquote inside is
			// plain text.
			p.next()
			for p.off < len(p.src) {
				c := p.next()
				if c == '\\' {
					if p.off < len(p.src) {
						p.next()
					}
					continue
				}
				if c == '"' || c == '\n' {
					break
				}
			}
		case '`':
			p.next()
			for p.off < len(p.src) {
				if p.next() == '`' {
					break
				}
			}
		case '\'':
			p.next()
			for p.off < len(p.src) {
				c := p.next()
				if c == '\\' {
					if p.off < len(p.src) {
						p.next()
					}
					continue
				}
				if c == '\'' || c == '\n' {
					break
				}
			}
		case '/':
			switch p.peek(1) {
			case '/':
				for p.off < len(p.src) && p.peek(0) != '\n' {
					p.next()
				}
			case '*':
				p.next()
				p.next()
				for p.off < len(p.src) {
					if p.peek(0) == '*' && p.peek(1) == '/' {
						p.next()
						p.next()
						break
					}
					p.next()
				}
			default:
				p.next()
			}
		default:
			p.next()
		}
	}
}

// bodyText reads the element's body until a '<' that opens a child, an
// end tag or a comment. It folds runs of ASCII whitespace to one space
// and trims the ends of the whole body, as HTML does; non-ASCII
// whitespace is kept.
func (p *viewParser) bodyText(tag string) ([]TextPart, error) {
	var parts []TextPart
	var lit strings.Builder
	litPos := p.pos()
	flush := func(end bool) {
		s := foldASCII(decodeEntities(lit.String()))
		lit.Reset()
		if s == "" {
			return
		}
		if len(parts) == 0 {
			s = strings.TrimLeft(s, " \t\r\n")
		}
		if end {
			s = strings.TrimRight(s, " \t\r\n")
		}
		if s != "" {
			parts = append(parts, TextPart{Lit: s, Pos: litPos})
		}
	}
	for {
		if p.off >= len(p.src) {
			flush(true)
			return parts, nil
		}
		b := p.peek(0)
		if b == '<' {
			// A child element, the end tag or a comment ends the text.
			flush(true)
			return parts, nil
		}
		if b == '{' {
			flush(false)
			part, err := p.braced(litPos)
			if err != nil {
				return nil, err
			}
			parts = append(parts, part)
			continue
		}
		lit.WriteByte(b)
		p.next()
	}
}

// foldASCII folds runs of ASCII whitespace to single spaces, including
// newlines with their indentation. A trailing newline's indentation
// joins the next run: leave one space for it, folded with what follows.
func foldASCII(s string) string {
	if !strings.ContainsAny(s, " \t\r\n") {
		return s
	}
	var b strings.Builder
	space := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
			space = true
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		b.WriteRune(r)
	}
	if space {
		b.WriteByte(' ')
	}
	return b.String()
}

// decodeEntities turns &lt; &gt; &amp; &quot; and &#39; into their
// characters. An unknown & stays as written.
func decodeEntities(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}
	r := strings.NewReplacer(
		"&lt;", "<",
		"&gt;", ">",
		"&amp;", "&",
		"&quot;", "\"",
		"&#39;", "'",
	)
	return r.Replace(s)
}

// attr returns the raw value of the attribute name, "" for none. Use
// for the generator's own attributes alone (type, key): expressions
// belong to the binder.
func (n *Node) attr(name string) string {
	for _, a := range n.Attrs {
		if a.Name == name && !a.Bare && len(a.Value) == 1 && !a.Value[0].IsExpr {
			return a.Value[0].Lit
		}
	}
	return ""
}
