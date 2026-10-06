package ui

import (
	"errors"
	"fmt"
	"html"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/internal/text"

	nethtml "golang.org/x/net/html"
)

const richInterchangeLimit = 8 << 20

// HTML returns a UTF-8 HTML fragment with paragraphs, inline styles and links.
// Only document content is exported; the editor's theme is not serialized.
func (d RichDocument) HTML() string {
	var b strings.Builder
	start, p := 0, 0
	for {
		end := d.n
		if at := strings.IndexByte(d.text[runeOffset(d.text, start):], '\n'); at >= 0 {
			end = start + utf8.RuneCountInString(d.text[runeOffset(d.text, start):][:at])
		}
		ps := d.paragraph(p)
		alignment := "start"
		if ps.Alignment == Center {
			alignment = "center"
		} else if ps.Alignment == End {
			alignment = "end"
		}
		fmt.Fprintf(&b, `<p dir="auto" style="white-space:pre-wrap;text-align:%s`, alignment)
		if ps.LineHeight > 0 {
			fmt.Fprintf(&b, ";line-height:%g", ps.LineHeight)
		}
		b.WriteString(`">`)
		for _, run := range d.runs {
			a, z := max(start, run.Range.Start), min(end, run.Range.End)
			if z <= a {
				continue
			}
			s := run.Style
			if s.Link != "" {
				fmt.Fprintf(&b, `<a href="%s">`, html.EscapeString(s.Link))
			}
			b.WriteString(`<span style="`)
			if s.Font != "" {
				// A quoted CSS family cannot turn into another declaration.
				family := strings.NewReplacer("\\", "\\\\", "'", "\\'", "\r", "", "\n", "").Replace(s.Font)
				fmt.Fprintf(&b, "font-family:%s;", html.EscapeString("'"+family+"'"))
			}
			if s.Size > 0 {
				fmt.Fprintf(&b, "font-size:%gpx;", s.Size)
			}
			if s.Weight > 0 {
				fmt.Fprintf(&b, "font-weight:%d;", s.Weight)
			}
			if s.Italic {
				b.WriteString("font-style:italic;")
			}
			if s.Underline || s.Strikethrough {
				b.WriteString("text-decoration:")
				if s.Underline {
					b.WriteString("underline ")
				}
				if s.Strikethrough {
					b.WriteString("line-through")
				}
				b.WriteByte(';')
			}
			if s.Color.A > 0 {
				fmt.Fprintf(&b, "color:%s;", richCSSColor(s.Color))
			}
			if s.Background.A > 0 {
				fmt.Fprintf(&b, "background-color:%s;", richCSSColor(s.Background))
			}
			b.WriteString(`">`)
			b.WriteString(html.EscapeString(d.text[runeOffset(d.text, a):runeOffset(d.text, z)]))
			b.WriteString("</span>")
			if s.Link != "" {
				b.WriteString("</a>")
			}
		}
		b.WriteString("</p>")
		if end == d.n {
			break
		}
		start, p = end+1, p+1
	}
	return b.String()
}

func richCSSColor(c Color) string { return fmt.Sprintf("#%02x%02x%02x%02x", c.R, c.G, c.B, c.A) }

type richBuilder struct {
	text       strings.Builder
	runs       []StyledRange
	paragraphs []ParagraphStyle
	physical   []bool // HTML left/right alignment, converted after text is known
	n          int
	pending    bool
	lastSpace  bool
}

func (b *richBuilder) append(s string, style RichStyle, paragraph ParagraphStyle) {
	if s == "" {
		return
	}
	if len(b.paragraphs) == 0 {
		b.paragraphs = append(b.paragraphs, paragraph)
	}
	start := b.n
	b.n += utf8.RuneCountInString(s)
	b.text.WriteString(s)
	b.runs = appendStyled(b.runs, TextRange{start, b.n}, validRichStyle(style))
	for range strings.Count(s, "\n") {
		b.paragraphs = append(b.paragraphs, paragraph)
	}
	b.lastSpace = strings.HasSuffix(s, " ") || strings.HasSuffix(s, "\n")
}

func (b *richBuilder) document() RichDocument {
	d := NewRichDocument(b.text.String())
	// Normalize external style boundaries (e.g. <b>e</b> + combining accent).
	d.runs = d.normalizeRuns(b.runs)
	if len(b.paragraphs) == len(d.paragraphs) {
		d.paragraphs = b.paragraphs
	}
	for i, paragraph := range strings.Split(d.String(), "\n") {
		if i < len(b.physical) && b.physical[i] && text.ParagraphRTL(paragraph) {
			d.paragraphs[i].Alignment = reverseRichAlignment(d.paragraphs[i].Alignment)
		}
	}
	return d
}

func reverseRichAlignment(a Align) Align {
	if a == Start {
		return End
	}
	if a == End {
		return Start
	}
	return a
}

func (b *richBuilder) markPhysical(physical bool) {
	for len(b.physical) < len(b.paragraphs) {
		b.physical = append(b.physical, physical)
	}
	if len(b.physical) > 0 {
		b.physical[len(b.physical)-1] = physical
	}
}

func (b *richBuilder) beginParagraph(style RichStyle, paragraph ParagraphStyle) {
	if b.pending || b.n > 0 && !strings.HasSuffix(b.text.String(), "\n") {
		b.append("\n", style, paragraph)
	}
	b.pending = false
	if len(b.paragraphs) == 0 {
		b.paragraphs = append(b.paragraphs, paragraph)
	}
	b.paragraphs[len(b.paragraphs)-1] = paragraph
	b.lastSpace = true
}

// ParseRichHTML imports paragraphs, basic character styles, inline CSS and
// http(s)/mailto links. Scripts, stylesheets, embedded objects and images are
// ignored; nothing is executed or fetched. Unsupported markup contributes its
// text. Inputs over 8 MiB or more than 128 nested elements return an error.
func ParseRichHTML(markup string) (RichDocument, error) {
	if markup == "" {
		return RichDocument{}, errors.New("ui: empty rich HTML")
	}
	if len(markup) > richInterchangeLimit {
		return RichDocument{}, errors.New("ui: rich HTML exceeds 8 MiB")
	}
	root, err := nethtml.Parse(strings.NewReader(markup))
	if err != nil {
		return RichDocument{}, fmt.Errorf("ui: rich HTML: %w", err)
	}
	var b richBuilder
	var walk func(*nethtml.Node, RichStyle, ParagraphStyle, bool, bool, int) error
	walk = func(n *nethtml.Node, style RichStyle, paragraph ParagraphStyle, pre, physical bool, depth int) error {
		if depth > 128 {
			return errors.New("ui: rich HTML nesting exceeds 128")
		}
		if n.Type == nethtml.TextNode {
			s := cleanRichText(n.Data)
			if !pre {
				if strings.TrimSpace(s) == "" && b.pending {
					return nil
				}
				var out strings.Builder
				space := b.lastSpace || b.n == 0
				for _, r := range s {
					if r == ' ' || r == '\n' || r == '\t' || r == '\f' {
						if !space {
							out.WriteByte(' ')
						}
						space = true
					} else {
						out.WriteRune(r)
						space = false
					}
				}
				s = out.String()
			}
			if b.pending && s != "" {
				b.beginParagraph(style, paragraph)
			}
			b.append(s, style, paragraph)
			b.markPhysical(physical)
			return nil
		}
		block := false
		if n.Type == nethtml.ElementNode {
			switch n.Data {
			case "head", "script", "style", "iframe", "object", "svg", "img", "template":
				return nil
			case "b", "strong":
				style.Weight = 700
			case "i", "em":
				style.Italic = true
			case "u":
				style.Underline = true
			case "s", "strike", "del":
				style.Strikethrough = true
			case "code", "pre":
				style.Font = "monospace"
			case "mark":
				style.Background = RGB(255, 255, 0)
			}
			for _, attr := range n.Attr {
				switch attr.Key {
				case "href":
					if n.Data == "a" && validRichLink(attr.Val) {
						style.Link = attr.Val
					}
				case "style":
					style, paragraph, pre, physical = parseRichCSS(attr.Val, style, paragraph, pre, physical)
				case "align":
					paragraph.Alignment = richAlignment(attr.Val)
					physical = attr.Val == "left" || attr.Val == "right"
				case "face":
					if n.Data == "font" {
						style.Font = attr.Val
					}
				case "color":
					if n.Data == "font" {
						style.Color = parseRichColor(attr.Val)
					}
				}
			}
			if n.Data == "br" {
				b.pending = false
				b.append("\n", style, paragraph)
				b.markPhysical(physical)
				return nil
			}
			switch n.Data {
			case "p", "div", "li", "blockquote", "pre", "h1", "h2", "h3", "h4", "h5", "h6":
				block = true
				if n.Data == "pre" {
					pre = true
				}
				b.beginParagraph(style, paragraph)
				b.markPhysical(physical)
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if err := walk(child, style, paragraph, pre, physical, depth+1); err != nil {
				return err
			}
		}
		if block {
			b.pending = true
		}
		return nil
	}
	if err := walk(root, RichStyle{}, ParagraphStyle{}, false, false, 0); err != nil {
		return RichDocument{}, err
	}
	return b.document(), nil
}

func richAlignment(s string) Align {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "center":
		return Center
	case "end", "right":
		return End
	}
	return Start
}

func parseRichCSS(css string, style RichStyle, paragraph ParagraphStyle, pre, physical bool) (RichStyle, ParagraphStyle, bool, bool) {
	for _, declaration := range strings.Split(css, ";") {
		key, value, ok := strings.Cut(declaration, ":")
		if !ok {
			continue
		}
		key, value = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(value)
		value = strings.TrimSpace(strings.TrimSuffix(value, "!important"))
		switch key {
		case "font-family":
			style.Font = strings.Trim(value, "\"'")
			style.Font = strings.NewReplacer("\\'", "'", "\\\\", "\\").Replace(style.Font)
		case "font-size":
			factor := float32(1)
			if strings.HasSuffix(value, "pt") {
				factor = 4.0 / 3
			}
			n, _ := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSuffix(value, "px"), "pt"), 32)
			style.Size = float32(n) * factor
		case "font-weight":
			switch value {
			case "bold", "bolder":
				style.Weight = 700
			case "normal":
				style.Weight = 400
			default:
				style.Weight, _ = strconv.Atoi(value)
			}
		case "font-style":
			style.Italic = value == "italic" || value == "oblique"
		case "text-decoration", "text-decoration-line":
			style.Underline = strings.Contains(value, "underline")
			style.Strikethrough = strings.Contains(value, "line-through")
		case "color":
			style.Color = parseRichColor(value)
		case "background", "background-color":
			style.Background = parseRichColor(value)
		case "text-align":
			paragraph.Alignment = richAlignment(value)
			physical = value == "left" || value == "right"
		case "line-height":
			n, _ := strconv.ParseFloat(strings.TrimSuffix(value, "%"), 32)
			paragraph.LineHeight = float32(n)
			if strings.HasSuffix(value, "%") {
				paragraph.LineHeight /= 100
			}
		case "white-space":
			pre = value == "pre" || value == "pre-wrap" || value == "break-spaces"
		}
	}
	return validRichStyle(style), validParagraphStyle(paragraph), pre, physical
}

func parseRichColor(s string) Color {
	s = strings.ToLower(strings.TrimSpace(s))
	if strings.HasPrefix(s, "#") {
		c, _ := parseHex(s)
		return c
	}
	if v, ok := map[string]string{"black": "#000", "white": "#fff", "red": "#f00", "green": "#008000", "blue": "#00f", "yellow": "#ff0", "gray": "#808080", "grey": "#808080"}[s]; ok {
		c, _ := parseHex(v)
		return c
	}
	if strings.HasPrefix(s, "rgb(") || strings.HasPrefix(s, "rgba(") {
		_, rest, _ := strings.Cut(s, "(")
		parts := strings.Split(strings.TrimSuffix(rest, ")"), ",")
		if len(parts) == 3 || len(parts) == 4 {
			var rgb [3]uint8
			for i := range rgb {
				n, _ := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(parts[i], "%")), 32)
				if strings.HasSuffix(strings.TrimSpace(parts[i]), "%") {
					n *= 2.55
				}
				rgb[i] = uint8(max(0, min(n, 255)))
			}
			a := float64(1)
			if len(parts) == 4 {
				a, _ = strconv.ParseFloat(strings.TrimSpace(parts[3]), 32)
			}
			return RGBA(rgb[0], rgb[1], rgb[2], float32(a))
		}
	}
	return Color{}
}
