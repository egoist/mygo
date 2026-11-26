package editor

import (
	"slices"
	"strconv"
	"strings"
)

// markdownLine holds the parsed blockquote and list prefix of a Markdown
// line.
type markdownLine struct {
	indent    string
	quote     string
	pad       string
	marker    string
	number    int
	delimiter byte
	task      bool
	ordered   bool
	list      bool
	end       int
}

// parseMarkdownLine parses any leading blockquote and list item marker
// from s, reporting whether s starts with either.
func parseMarkdownLine(s string) (markdownLine, bool) {
	var p markdownLine
	n := len(s)
	i := markdownSpaceLen(s, 0)
	p.indent = s[:i]
	q := i
	for i < n && s[i] == '>' {
		i++
		if i < n && markdownSpace(s[i]) {
			i++
		}
	}
	p.quote = s[q:i]
	q = i
	i += markdownSpaceLen(s, i)
	p.pad = s[q:i]
	if i >= n {
		p.end = i
		return p, p.quote != ""
	}
	switch c := s[i]; {
	case strings.ContainsRune("*+-", rune(c)) && i+1 < n && markdownSpace(s[i+1]):
		start := i
		i++
		i += markdownSpaceLen(s, i)
		if i+2 < n && s[i] == '[' && (s[i+1] == ' ' || s[i+1] == 'x' || s[i+1] == 'X') && s[i+2] == ']' && i+3 < n && markdownSpace(s[i+3]) {
			p.marker, p.task, p.list = s[start:i+3], true, true
			i += 3
		} else {
			p.marker, p.list = s[start:i], true
		}
	case c >= '0' && c <= '9':
		start := i
		for i < n && i-start < 9 && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i >= n || s[i] != '.' && s[i] != ')' || i+1 >= n || !markdownSpace(s[i+1]) {
			return p, p.quote != ""
		}
		p.number, _ = strconv.Atoi(s[start:i])
		p.delimiter = s[i]
		i++
		p.marker, p.ordered, p.list = s[start:i], true, true
	default:
		p.end = i
		return p, p.quote != ""
	}
	i += markdownSpaceLen(s, i)
	p.end = i
	return p, true
}

// markdownSpaceLen returns the number of leading spaces or tabs in s[i:].
func markdownSpaceLen(s string, i int) int {
	start := i
	for i < len(s) && markdownSpace(s[i]) {
		i++
	}
	return i - start
}

// markdownSpace reports whether c is a space or tab.
func markdownSpace(c byte) bool { return c == ' ' || c == '\t' }

// continuation returns the prefix to insert on the new line after Enter.
func (p markdownLine) continuation() string {
	if !p.list {
		return strings.TrimRight(p.indent+p.quote, " \t") + " "
	}
	marker := strings.TrimRight(p.marker, " \t")
	switch {
	case p.task:
		marker = marker[:len(marker)-2] + " ]"
	case p.ordered:
		marker = strconv.Itoa(p.number+1) + string(p.delimiter)
	}
	return p.indent + p.quote + p.pad + marker + " "
}

// active reports whether the parsed line is an active Markdown quote or
// list item in ed (4-space indented items require a shallower or matching
// parent item in the same quote context).
func (p markdownLine) active(ed *editor, line int) bool {
	if !p.list {
		return p.quote != ""
	}
	if indentWidth(p.listIndent(), ed.tabSize()) < 4 {
		return true
	}
	r := &ed.view.text
	width := indentWidth(p.listIndent(), ed.tabSize())
	for i := line - 1; i >= 0; i-- {
		q, ok := parseMarkdownLine(r.Line(i))
		if ok && q.list && q.quote == p.quote && (p.quote == "" || q.indent == p.indent) && indentWidth(q.listIndent(), ed.tabSize()) <= width {
			return true
		}
	}
	return false
}

// listIndent returns the indentation columns governing list nesting.
func (p markdownLine) listIndent() string {
	if p.quote != "" {
		return p.pad
	}
	return p.indent
}

// markdownNewline continues lists and blockquotes on Enter in Markdown
// buffers, or clears an empty item's marker in place.
func (ed *editor) markdownNewline() bool {
	l := ed.language()
	if l == nil || l.Name != "Markdown" {
		return false
	}
	r := &ed.view.text
	var edits []Edit
	for _, s := range ed.sels {
		if !s.empty() {
			return false
		}
		line := r.LineOf(s.head)
		text := r.Line(line)
		p, ok := parseMarkdownLine(text)
		if !ok || !p.active(ed, line) || s.head < r.LineStart(line)+p.end || ed.markdownFenced(line) {
			return false
		}
		a := r.LineStart(line)
		if s.head == a+len(text) && text[p.end:] == "" {
			clearFrom := a
			if p.quote != "" && p.list {
				clearFrom = a + len(p.indent) + len(p.quote)
			}
			edits = append(edits, Edit{Start: clearFrom, End: s.head, Text: ""})
			continue
		}
		edits = append(edits, Edit{Start: s.head, End: s.head, Text: "\n" + p.continuation()})
	}
	sels := slices.Clone(ed.sels)
	return ed.apply(edits, editOther, func(move func(int, bias) int) []sel {
		for i := range sels {
			sels[i].collapse(move(sels[i].head, biasRight))
		}
		return sels
	})
}

// markdownFenced reports whether line lies inside a fenced code block.
func (ed *editor) markdownFenced(line int) bool {
	h, ok := ed.highlighter().(*tokenHighlighter)
	if ok {
		if state, final := h.stateOf(line); final {
			return state&mdFence != 0
		}
	}
	l := ed.language()
	if l == nil || l.Tokenizer == nil {
		return false
	}
	r := &ed.view.text
	var state uint64
	for i := 0; i < line; i++ {
		_, state = l.Tokenizer.Tokenize(r.Line(i), state, nil)
	}
	return state&mdFence != 0
}
