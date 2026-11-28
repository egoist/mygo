package editor

import (
	"github.com/egoist/mygo/internal/rope"
	"slices"
	"strings"
	"unicode"
)

// rewrapColumn is the column Rewrap fills to without WrapGuides or a
// WrapColumn.
const rewrapColumn = 80

// rewrap refills the paragraphs of the selections' lines, or of each
// caret's line, to the first wrap guide, as Zed's Rewrap and Visual
// Studio Code's Rewrap extension do. Each line keeps the paragraph's
// prefix: its indentation, the language's line comment and quote markers;
// a list item's lines after its first are indented by its marker's width.
// A paragraph ends at a blank line, a line left as it is, a change of
// prefix or a new item.
func (ed *editor) rewrap() bool {
	r := &ed.view.text
	w := ed.rewrapper()
	width := rewrapColumn
	switch {
	case len(ed.o.wrapGuides) > 0 && ed.o.wrapGuides[0] > 0:
		width = ed.o.wrapGuides[0]
	case ed.o.wrapColumn > 0:
		width = ed.o.wrapColumn
	}
	kept := w.keptLines(r)
	skip := func(line int) bool { return kept[line] || strings.TrimSpace(r.Line(line)) == "" }
	joins := func(line int) bool { return !skip(line+1) && w.continues(r.Line(line), r.Line(line+1)) }
	var edits []Edit
	var olds []string
	done := -1
	for _, s := range ed.sels {
		first, last := ed.lineSpanOf(s)
		if s.empty() {
			if skip(first) {
				continue
			}
			for first > 0 && !skip(first-1) && joins(first-1) {
				first--
			}
			last = r.LineCount() - 1
		}
		for line := max(first, done+1); line <= last; {
			if skip(line) {
				line++
				continue
			}
			end := line
			for end < last && joins(end) {
				end++
			}
			lines := make([]string, 0, end-line+1)
			for i := line; i <= end; i++ {
				lines = append(lines, r.Line(i))
			}
			old := strings.Join(lines, "\n")
			if text := w.fill(lines, width); text != old {
				edits = append(edits, Edit{Start: r.LineStart(line), End: r.LineEnd(end), Text: text})
				olds = append(olds, old)
			}
			line = end + 1
			if s.empty() {
				last = end
			}
		}
		done = max(done, last)
	}
	sels := slices.Clone(ed.sels)
	return ed.apply(edits, editOther, func(move func(int, bias) int) []sel {
		// The ends of selections stay by the same characters.
		at := func(off int, b bias) int {
			shift := 0
			for j, e := range edits {
				if off >= e.Start && off <= e.End {
					return e.Start + shift + w.offsetOf(e.Text, w.count(olds[j][:off-e.Start]), b == biasRight)
				}
				shift += len(e.Text) - (e.End - e.Start)
			}
			return move(off, b)
		}
		for i := range sels {
			s := &sels[i]
			if s.empty() {
				s.collapse(at(s.head, biasLeft))
				continue
			}
			a, z := at(s.start(), biasRight), at(s.end(), biasLeft)
			if s.anchor <= s.head {
				s.anchor, s.head = a, z
			} else {
				s.anchor, s.head = z, a
			}
		}
		return sels
	})
}

// rewrapper is what Rewrap knows of the language of the text.
type rewrapper struct {
	comment string
	block   [2]string
	rules   RewrapRules
	tab     int
}

func (ed *editor) rewrapper() rewrapper {
	w := rewrapper{tab: ed.tabSize()}
	if l := ed.language(); l != nil {
		w.comment, w.block, w.rules = l.LineComment, l.BlockComment, l.Rewrap
	}
	return w
}

// keptLines reports the lines Rewrap leaves as they are: those of block
// comments and of the rules' fenced blocks, and those the rules keep.
func (w rewrapper) keptLines(r *rope.Text) []bool {
	kept := make([]bool, r.LineCount())
	inBlock, inFence := false, false
	for i := range kept {
		line := r.Line(i)
		if w.block[0] != "" {
			code := line
			if c := strings.Index(code, w.comment); w.comment != "" && c >= 0 && !inBlock {
				code = code[:c]
			}
			for code != "" {
				if inBlock {
					kept[i] = true
					c := strings.Index(code, w.block[1])
					if c < 0 {
						break
					}
					code, inBlock = code[c+len(w.block[1]):], false
					continue
				}
				c := strings.Index(code, w.block[0])
				if c < 0 {
					break
				}
				kept[i] = true
				code, inBlock = code[c+len(w.block[0]):], true
			}
			if inBlock {
				kept[i] = true
			}
		}
		prefix, _, body := w.split(line)
		if w.comment != "" && strings.HasPrefix(body, w.comment[len(w.comment)-1:]) && strings.Contains(prefix, w.comment) {
			// A banner, as "////" or "###".
			kept[i] = true
		}
		if f := w.rules.Fence; f != nil && f.MatchString(body) {
			kept[i], inFence = true, !inFence
			continue
		}
		if k := w.rules.Keep; inFence || k != nil && k.MatchString(body) {
			kept[i] = true
		}
	}
	return kept
}

// split splits line into its prefix (indentation, the line comment, with
// a / or ! after it as Rust's "///" and "//!", and quote markers, with
// their spaces), the list marker after it, and its text.
func (w rewrapper) split(line string) (prefix, marker, body string) {
	i := len(indentOf(line))
	if w.comment != "" && strings.HasPrefix(line[i:], w.comment) {
		i += len(w.comment)
		if strings.HasPrefix(line[i:], "/") || strings.HasPrefix(line[i:], "!") {
			i++
		}
		i += len(indentOf(line[i:]))
	}
	for q := w.rules.Quote; q != nil; {
		m := q.FindStringIndex(line[i:])
		if m == nil || m[0] != 0 || m[1] == 0 {
			break
		}
		i += m[1]
		i += len(indentOf(line[i:]))
	}
	prefix, body = line[:i], line[i:]
	if it := w.rules.ListItem; it != nil {
		if m := it.FindStringIndex(body); m != nil && m[0] == 0 && m[1] > 0 {
			return prefix, body[:m[1]], body[m[1]:]
		}
	}
	return prefix, "", body
}

// continuation is the prefix of the lines after line in its paragraph,
// a list marker's width in spaces after the prefix.
func (w rewrapper) continuation(line string) string {
	prefix, marker, _ := w.split(line)
	return prefix + spaces(columnOf(prefix+marker, w.tab)-columnOf(prefix, w.tab))
}

// continues reports whether next goes on the paragraph line is in: it has
// text, starts no list item, and has the prefix line's next lines take.
func (w rewrapper) continues(line, next string) bool {
	prefix, marker, body := w.split(next)
	return strings.TrimSpace(body) != "" && marker == "" && prefix == w.continuation(line)
}

// fill refills lines, one paragraph, to width columns, with at least one
// word on each line.
func (w rewrapper) fill(lines []string, width int) string {
	prefix, marker, _ := w.split(lines[0])
	var words []string
	for _, l := range lines {
		_, _, body := w.split(l)
		words = append(words, strings.Fields(body)...)
	}
	lead := prefix + marker
	next := w.continuation(lines[0])
	var b strings.Builder
	col := 0
	for i, word := range words {
		n := columnOf(word, w.tab)
		switch {
		case i == 0:
			b.WriteString(lead)
			col = columnOf(lead, w.tab)
		case col+1+n > width:
			b.WriteByte('\n')
			b.WriteString(next)
			col = columnOf(next, w.tab)
		default:
			b.WriteByte(' ')
			col++
		}
		b.WriteString(word)
		col += n
	}
	return b.String()
}

// count counts the characters of text's lines other than their prefixes
// and spaces.
func (w rewrapper) count(text string) int {
	n := 0
	for _, line := range strings.Split(text, "\n") {
		_, _, body := w.split(line)
		for _, c := range body {
			if !unicode.IsSpace(c) {
				n++
			}
		}
	}
	return n
}

// offsetOf is the offset in text just after the first n characters count
// counts, or, with before, just before the next one.
func (w rewrapper) offsetOf(text string, n int, before bool) int {
	at := 0
	for _, line := range strings.Split(text, "\n") {
		_, _, body := w.split(line)
		start := at + len(line) - len(body)
		for i, c := range body {
			if unicode.IsSpace(c) {
				if n == 0 && !before {
					return start + i
				}
				continue
			}
			if n == 0 {
				return start + i
			}
			n--
		}
		if n == 0 && !before {
			return start + len(body)
		}
		at += len(line) + 1
	}
	return len(text)
}
