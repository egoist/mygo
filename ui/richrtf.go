package ui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/egoist/mygo/internal/text"

	"golang.org/x/text/encoding/charmap"
)

// RTF returns an RTF 1 document with Unicode text, font and color tables,
// character formatting, paragraph alignment/spacing and hyperlink fields.
// RTF colors are opaque and sizes are rounded to half-points (2/3 DIP).
func (d RichDocument) RTF() string {
	fonts, colors := []string{"system-ui"}, []Color{{}}
	for _, run := range d.runs {
		if run.Style.Font != "" && !containsRichFont(fonts, run.Style.Font) {
			fonts = append(fonts, run.Style.Font)
		}
		for _, c := range []Color{run.Style.Color, run.Style.Background} {
			if c.A > 0 && richColorIndex(colors, c) == 0 {
				colors = append(colors, c)
			}
		}
	}
	var b strings.Builder
	b.WriteString(`{\rtf1\ansi\ansicpg1252\uc1{\fonttbl`)
	for i, font := range fonts {
		fmt.Fprintf(&b, `{\f%d\fnil `, i)
		b.WriteString(escapeRTF(strings.ReplaceAll(font, ";", "")))
		b.WriteString(";}")
	}
	b.WriteString(`}{\colortbl;`)
	for _, c := range colors[1:] {
		fmt.Fprintf(&b, `\red%d\green%d\blue%d;`, c.R, c.G, c.B)
	}
	b.WriteString("}")
	start, p := 0, 0
	for {
		end := d.n
		if at := strings.IndexByte(d.text[runeOffset(d.text, start):], '\n'); at >= 0 {
			end = start + utf8.RuneCountInString(d.text[runeOffset(d.text, start):][:at])
		}
		ps := d.paragraph(p)
		b.WriteString(`\pard`)
		if text.ParagraphRTL(d.text[runeOffset(d.text, start):runeOffset(d.text, end)]) {
			b.WriteString(`\rtlpar`)
			ps.Alignment = reverseRichAlignment(ps.Alignment)
		} else {
			b.WriteString(`\ltrpar`)
		}
		switch ps.Alignment {
		case Center:
			b.WriteString(`\qc`)
		case End:
			b.WriteString(`\qr`)
		default:
			b.WriteString(`\ql`)
		}
		if ps.LineHeight > 0 {
			fmt.Fprintf(&b, `\sl%d\slmult1`, int(ps.LineHeight*240+0.5))
		}
		b.WriteByte(' ')
		for _, run := range d.runs {
			a, z := max(start, run.Range.Start), min(end, run.Range.End)
			if z <= a {
				continue
			}
			s := run.Style
			if s.Link != "" {
				b.WriteString(`{\field{\*\fldinst HYPERLINK "`)
				b.WriteString(escapeRTF(strings.ReplaceAll(s.Link, "\"", "%22")))
				b.WriteString(`"}{\fldrslt `)
			}
			b.WriteString(`{\plain`)
			if s.Font != "" {
				for i, f := range fonts {
					if f == s.Font {
						fmt.Fprintf(&b, `\f%d`, i)
					}
				}
			}
			if s.Size > 0 {
				fmt.Fprintf(&b, `\fs%d`, int(s.Size*1.5+0.5))
			}
			if s.Weight >= 600 {
				b.WriteString(`\b`)
			}
			if s.Italic {
				b.WriteString(`\i`)
			}
			if s.Underline {
				b.WriteString(`\ul`)
			}
			if s.Strikethrough {
				b.WriteString(`\strike`)
			}
			if s.Color.A > 0 {
				fmt.Fprintf(&b, `\cf%d`, richColorIndex(colors, s.Color))
			}
			if s.Background.A > 0 {
				fmt.Fprintf(&b, `\highlight%d`, richColorIndex(colors, s.Background))
			}
			b.WriteByte(' ')
			b.WriteString(escapeRTF(d.text[runeOffset(d.text, a):runeOffset(d.text, z)]))
			b.WriteByte('}')
			if s.Link != "" {
				b.WriteString("}}")
			}
		}
		if end == d.n {
			break
		}
		b.WriteString(`\par `)
		start, p = end+1, p+1
	}
	b.WriteByte('}')
	return b.String()
}

func containsRichFont(fonts []string, font string) bool {
	for _, f := range fonts {
		if f == font {
			return true
		}
	}
	return false
}

func richColorIndex(colors []Color, color Color) int {
	for i, c := range colors {
		if c == color {
			return i
		}
	}
	return 0
}

func escapeRTF(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\\' || r == '{' || r == '}':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\t':
			b.WriteString(`\tab `)
		case r >= 0x20 && r < 0x7f:
			b.WriteRune(r)
		default:
			for _, u := range utf16.Encode([]rune{r}) {
				fmt.Fprintf(&b, `\u%d?`, int16(u))
			}
		}
	}
	return b.String()
}

type richRTFField struct {
	instruction []uint16
	link        string
}

func appendRichUTF16(dst []uint16, r rune) []uint16 {
	if r > 0xffff {
		hi, lo := utf16.EncodeRune(r)
		return append(dst, uint16(hi), uint16(lo))
	}
	return append(dst, uint16(r))
}

type richRTFState struct {
	style     RichStyle
	paragraph ParagraphStyle
	dest      string
	star      bool
	uc        int
	font      int
	field     *richRTFField
}

// ParseRichRTF reads RTF's basic text/formatting subset, Unicode (including
// surrogate pairs), ANSI escapes, fonts, colors, paragraphs and hyperlinks.
// Binary objects, pictures and unsupported destinations are ignored. ANSI
// code pages 1252 and UTF-8 are supported. Malformed groups, unsupported code
// pages, inputs over 8 MiB or nesting over 128 return an error.
func ParseRichRTF(data string) (RichDocument, error) {
	data = strings.TrimSpace(strings.TrimRight(data, "\x00"))
	if !strings.HasPrefix(data, `{\rtf`) {
		return RichDocument{}, errors.New("ui: invalid RTF header")
	}
	if len(data) > richInterchangeLimit {
		return RichDocument{}, errors.New("ui: rich RTF exceeds 8 MiB")
	}
	s := richRTFState{uc: 1}
	var stack []richRTFState
	var b richBuilder
	fonts := map[int]string{}
	colors := []Color{}
	var fontText []uint16
	color := Color{}
	fallback, codepage := 0, 1252
	defaultFont := 0
	family := func(index int) string {
		f := fonts[index]
		if f == "system-ui" {
			return ""
		}
		return f
	}
	var high uint16
	var highStyle RichStyle
	appendRune := func(r rune, style RichStyle) {
		if high != 0 {
			b.append("�", highStyle, s.paragraph)
			high = 0
		}
		b.append(string(r), style, s.paragraph)
	}
	appendUnit := func(u uint16) {
		if u >= 0xdc00 && u <= 0xdfff && high != 0 {
			r := utf16.DecodeRune(rune(high), rune(u))
			high = 0
			b.append(string(r), highStyle, s.paragraph)
		} else if u >= 0xd800 && u <= 0xdbff {
			if high != 0 {
				b.append("�", highStyle, s.paragraph)
			}
			high, highStyle = u, s.style
		} else {
			appendRune(rune(u), s.style)
		}
	}
	textRune := func(r rune) {
		if fallback > 0 {
			fallback--
			return
		}
		switch s.dest {
		case "fonttbl":
			if r == ';' {
				fonts[s.font] = strings.TrimSpace(string(utf16.Decode(fontText)))
				fontText = fontText[:0]
			} else {
				fontText = appendRichUTF16(fontText, r)
			}
		case "colortbl":
			if r == ';' {
				colors = append(colors, color)
				color = Color{}
			}
		case "fldinst":
			if s.field != nil {
				s.field.instruction = appendRichUTF16(s.field.instruction, r)
			}
		case "":
			appendRune(r, s.style)
		}
	}
	for i := 0; i < len(data); {
		c := data[i]
		i++
		switch c {
		case '{':
			if len(stack) >= 128 {
				return RichDocument{}, errors.New("ui: RTF nesting exceeds 128")
			}
			stack = append(stack, s)
			s.star = false
		case '}':
			if len(stack) == 0 {
				return RichDocument{}, errors.New("ui: unbalanced RTF group")
			}
			if s.dest == "fldinst" && s.field != nil {
				s.field.link = rtfHyperlink(string(utf16.Decode(s.field.instruction)))
			}
			previousDestination := s.dest
			s = stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if previousDestination == "fonttbl" && s.dest == "" {
				s.style.Font = family(defaultFont)
			}
			if len(stack) == 0 && strings.TrimSpace(data[i:]) != "" {
				return RichDocument{}, errors.New("ui: trailing content after RTF group")
			}
		case '\r', '\n':
		case '\\':
			if i == len(data) {
				return RichDocument{}, errors.New("ui: truncated RTF escape")
			}
			c = data[i]
			if c == '\\' || c == '{' || c == '}' {
				textRune(rune(c))
				i++
				continue
			}
			if c == '*' {
				s.star = true
				i++
				continue
			}
			if c == '\'' {
				if i+2 >= len(data) {
					return RichDocument{}, errors.New("ui: truncated RTF hex escape")
				}
				n, err := strconv.ParseUint(data[i+1:i+3], 16, 8)
				if err != nil {
					return RichDocument{}, errors.New("ui: invalid RTF hex escape")
				}
				i += 3
				if codepage == 65001 {
					bytes := []byte{byte(n)}
					for i+3 < len(data) && data[i] == '\\' && data[i+1] == '\'' {
						n, err := strconv.ParseUint(data[i+2:i+4], 16, 8)
						if err != nil {
							return RichDocument{}, errors.New("ui: invalid RTF hex escape")
						}
						bytes = append(bytes, byte(n))
						i += 4
					}
					skip := min(fallback, len(bytes))
					fallback -= skip
					for _, r := range strings.ToValidUTF8(string(bytes[skip:]), "�") {
						textRune(r)
					}
				} else {
					textRune(charmap.Windows1252.DecodeByte(byte(n)))
				}
				continue
			}
			if c < 'a' || c > 'z' {
				if c == '~' {
					textRune('\u00a0')
				} else if c == '_' {
					textRune('\u2011')
				}
				i++
				continue
			}
			start := i
			for i < len(data) && (data[i] >= 'a' && data[i] <= 'z' || data[i] >= 'A' && data[i] <= 'Z') {
				i++
			}
			word := data[start:i]
			start = i
			if i < len(data) && data[i] == '-' {
				i++
			}
			for i < len(data) && data[i] >= '0' && data[i] <= '9' {
				i++
			}
			n, err := strconv.Atoi(data[start:i])
			has := i > start
			if has && err != nil {
				return RichDocument{}, errors.New("ui: invalid RTF parameter")
			}
			if i < len(data) && data[i] == ' ' {
				i++
			}
			if word == "bin" {
				if !has || n < 0 || n > len(data)-i {
					return RichDocument{}, errors.New("ui: invalid RTF binary length")
				}
				i += n
				continue
			}
			if s.dest == "skip" {
				continue
			}
			if s.star && word != "fldinst" {
				s.dest, s.star = "skip", false
			}
			switch word {
			case "fonttbl", "colortbl", "fldinst":
				s.dest, s.star = word, false
			case "stylesheet", "info", "pict", "object", "header", "footer", "footnote", "annotation":
				s.dest = "skip"
			case "field":
				s.field = &richRTFField{}
			case "fldrslt":
				s.dest = ""
				if s.field != nil {
					s.style.Link = s.field.link
				}
			case "ansicpg":
				if n != 1252 && n != 65001 {
					return RichDocument{}, fmt.Errorf("ui: unsupported RTF code page %d", n)
				}
				codepage = n
			case "mac", "pc", "pca":
				return RichDocument{}, errors.New("ui: unsupported RTF character set")
			case "deff":
				defaultFont = n
			case "uc":
				if n < 0 || n > 16 {
					return RichDocument{}, errors.New("ui: invalid RTF Unicode fallback length")
				}
				s.uc = n
			case "u":
				if !has || n < -32768 || n > 65535 {
					return RichDocument{}, errors.New("ui: invalid RTF Unicode escape")
				}
				if s.dest == "" {
					appendUnit(uint16(n))
				} else if s.dest == "fonttbl" {
					fontText = append(fontText, uint16(n))
				} else if s.dest == "fldinst" && s.field != nil {
					s.field.instruction = append(s.field.instruction, uint16(n))
				}
				fallback = s.uc
			case "red", "green", "blue":
				if s.dest == "colortbl" {
					v := uint8(max(0, min(n, 255)))
					color.A = 255
					switch word {
					case "red":
						color.R = v
					case "green":
						color.G = v
					case "blue":
						color.B = v
					}
				}
			case "f":
				s.font = n
				if s.dest == "" {
					s.style.Font = family(n)
				}
			case "plain":
				link := s.style.Link
				s.style = RichStyle{Font: family(defaultFont), Link: link}
			case "b":
				s.style.Weight = 700
				if has && n == 0 {
					s.style.Weight = 400
				}
			case "i":
				s.style.Italic = !has || n != 0
			case "ul", "ulnone":
				s.style.Underline = word != "ulnone" && (!has || n != 0)
			case "strike":
				s.style.Strikethrough = !has || n != 0
			case "fs":
				s.style.Size = float32(n) * 2 / 3
			case "cf", "highlight", "cb":
				c := Color{}
				if n >= 0 && n < len(colors) {
					c = colors[n]
				}
				if word == "cf" {
					s.style.Color = c
				} else {
					s.style.Background = c
				}
			case "pard":
				s.paragraph = ParagraphStyle{}
			case "ql", "qc", "qr":
				s.paragraph.Alignment = map[string]Align{"ql": Start, "qc": Center, "qr": End}[word]
			case "sl":
				s.paragraph.LineHeight = float32(n) / 240
			case "par", "line":
				textRune('\n')
			case "tab":
				textRune('\t')
			case "emdash":
				textRune('—')
			case "endash":
				textRune('–')
			case "bullet":
				textRune('•')
			case "lquote", "rquote", "ldblquote", "rdblquote":
				textRune(map[string]rune{"lquote": '‘', "rquote": '’', "ldblquote": '“', "rdblquote": '”'}[word])
			}
			if s.dest == "" && len(b.paragraphs) > 0 {
				b.paragraphs[len(b.paragraphs)-1] = validParagraphStyle(s.paragraph)
			}
		default:
			if c >= 0x80 && codepage == 65001 {
				r, size := utf8.DecodeRuneInString(data[i-1:])
				if fallback > 0 {
					skip := min(fallback, size)
					fallback -= skip
					i += skip - 1
					continue
				}
				textRune(r)
				i += size - 1
			} else {
				textRune(charmap.Windows1252.DecodeByte(c))
			}
		}
	}
	if len(stack) != 0 {
		return RichDocument{}, errors.New("ui: unclosed RTF group")
	}
	if high != 0 {
		b.append("�", highStyle, s.paragraph)
	}
	d := b.document()
	for i, paragraph := range strings.Split(d.String(), "\n") {
		if text.ParagraphRTL(paragraph) {
			d.paragraphs[i].Alignment = reverseRichAlignment(d.paragraphs[i].Alignment)
		}
	}
	return d, nil
}

func rtfHyperlink(instruction string) string {
	instruction = strings.TrimSpace(instruction)
	if !strings.HasPrefix(strings.ToUpper(instruction), "HYPERLINK ") {
		return ""
	}
	value := strings.TrimSpace(instruction[len("HYPERLINK "):])
	if strings.HasPrefix(value, "\"") {
		value = strings.TrimPrefix(value, "\"")
		value, _, _ = strings.Cut(value, "\"")
	} else {
		value, _, _ = strings.Cut(value, " ")
	}
	if !validRichLink(value) {
		return ""
	}
	return value
}
