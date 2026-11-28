package editor

import "strings"

// indentPasted returns text, pasted from start, re-indented, and where its
// edit starts. Its first line that is not blank takes the indentation
// Return would give on a caret line blank before start, replacing that
// line's own; after text on the line, the lines below take the caret
// line's indentation, a level more when the text starts on a new line
// below one that opens a block. The rest keep their indentation relative
// to the first, or, copied from mid-line, whose first line lost its
// indentation, to the least indented of them. Blank lines are left empty.
func (ed *editor) indentPasted(start int, text string) (int, string) {
	lines := strings.Split(text, "\n")
	if len(lines) < 2 {
		return start, text
	}
	first := 0
	for first < len(lines) && strings.TrimSpace(lines[first]) == "" {
		first++
	}
	if first == len(lines) {
		return start, text
	}
	r := &ed.view.text
	line := r.LineOf(start)
	lineStart := r.LineStart(line)
	prefix := r.Slice(lineStart, start)
	blankPrefix := strings.TrimSpace(prefix) == ""
	tab := ed.tabSize()
	var target int
	switch {
	case blankPrefix:
		target = ed.returnIndent(line)
		start = lineStart
	case first > 0:
		target = indentWidth(prefix, tab) + max(0, ed.indentAfter(start, prefix))*tab
	default:
		target = indentWidth(prefix, tab)
	}
	base := indentWidth(lines[first], tab)
	if first == 0 && !blankPrefix {
		base = -1
		for _, l := range lines[1:] {
			if strings.TrimSpace(l) != "" {
				if w := indentWidth(l, tab); base < 0 || w < base {
					base = w
				}
			}
		}
	}
	for i, l := range lines {
		switch {
		case strings.TrimSpace(l) == "":
			lines[i] = ""
		case i > 0 || blankPrefix:
			lines[i] = ed.indentOfWidth(target+max(0, indentWidth(l, tab)-base)) + strings.TrimLeft(l, " \t")
		}
	}
	return start, strings.Join(lines, "\n")
}

// returnIndent is the width of the indentation Return gives a line
// after the last line above line that is not blank.
func (ed *editor) returnIndent(line int) int {
	r := &ed.view.text
	for p := line - 1; p >= 0; p-- {
		t := r.Line(p)
		if strings.TrimSpace(t) == "" {
			continue
		}
		tab := ed.tabSize()
		return max(0, indentWidth(t, tab)+ed.indentAfter(r.LineEnd(p), t)*tab)
	}
	return 0
}

// indentOfWidth is cols columns of indentation in the editor's style:
// tabs, then spaces past the last tab stop, with hard tabs, else spaces.
func (ed *editor) indentOfWidth(cols int) string {
	if ed.hardTabs() {
		tab := ed.tabSize()
		return strings.Repeat("\t", cols/tab) + spaces(cols%tab)
	}
	return spaces(cols)
}
