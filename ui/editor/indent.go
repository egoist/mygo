package editor

import "strings"

// Indentation is how a text is indented: with tabs or with spaces, and how
// wide a level is.
type Indentation struct {
	// Style is IndentTabs or IndentSpaces, and IndentAuto when no line of
	// the text is indented.
	Style IndentStyle
	// Width is the columns of a level of spaces, 4 when the lines take no
	// step that says it, and 0 for tabs, whose width is the editor's.
	Width int
}

// indentSample is how much of a buffer the editor reads to detect its
// indentation: the first lines of a text say it, and a copy of a whole large
// file is not worth the answer.
const indentSample = 64 << 10

// DetectIndent returns the indentation text uses, as the lines it indents
// show it: the style those lines indent with, and the step they take.
//
// A line indents with a tab when its leading white space has one, and with
// spaces otherwise, and tabs win when more lines indent with a tab than with
// spaces: a tie is spaces, which a text that aligns with a tab in a
// space-indented line is more likely to be. The width of a level is the step
// most lines take from the line above them that indents less, of the steps
// of 2, 3, 4 and 8 columns, the smaller of two taken as often; a text that
// takes none of them is 4 wide, as IndentAuto is.
//
// ok is false when no line of text is indented, and Style is IndentAuto
// then. It is one pass over the text, and State.Indent's IndentDetect makes
// the editor run it when its buffer is set, and when the language's files
// are written with tabs, as detectIndent reads them.
func DetectIndent(text string) (ind Indentation, ok bool) {
	return detectIndent(text, false)
}

// detectIndent returns the indentation of text, as DetectIndent does, with
// the language's own indentation as the word on a text that only leans:
// hardTabs holds that its files are written with tabs, and a text of such a
// language keeps them unless spaces are more than twice as many of its
// indented lines. A Go file that embeds HTML is the case: half its lines are
// the HTML's, indented with spaces, and the other half are Go's, which is
// the language the text is in.
func detectIndent(text string, hardTabs bool) (ind Indentation, ok bool) {
	var tabs, spaces int
	// steps counts the lines that take a step, by its columns.
	var steps [9]int
	// stack holds the widths of the lines that indent less than the line
	// at hand, deepest last: the line above that it indents from.
	stack := make([]int, 0, 8)

	for text != "" {
		line := text
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			line, text = text[:i], text[i+1:]
		} else {
			text = ""
		}
		// A CRLF's return is not white space, but its line is over.
		line = strings.TrimSuffix(line, "\r")
		lead := 0
		for lead < len(line) && (line[lead] == ' ' || line[lead] == '\t') {
			lead++
		}
		if lead == len(line) {
			// A blank line, or one of white space alone, indents nothing.
			continue
		}
		if lead == 0 {
			// A line that does not indent says where the levels start.
			stack = stack[:0]
			continue
		}
		if strings.IndexByte(line[:lead], '\t') >= 0 {
			tabs++
			continue
		}
		spaces++
		w := lead
		for len(stack) > 0 && stack[len(stack)-1] >= w {
			stack = stack[:len(stack)-1]
		}
		parent := 0
		if len(stack) > 0 {
			parent = stack[len(stack)-1]
		}
		switch step := w - parent; step {
		case 2, 3, 4, 8:
			steps[step]++
		}
		stack = append(stack, w)
	}
	if tabs == 0 && spaces == 0 {
		return Indentation{}, false
	}
	if tabs > spaces || (hardTabs && tabs > 0 && spaces <= 2*tabs) {
		return Indentation{Style: IndentTabs}, true
	}
	width, best := 4, 0
	for _, step := range [...]int{2, 3, 4, 8} {
		if steps[step] > best {
			best, width = steps[step], step
		}
	}
	return Indentation{Style: IndentSpaces, Width: width}, true
}
