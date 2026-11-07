package editor

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/internal/text"
)

// typstLexer lexes Typst: markup, with the code a # starts and the math
// between $s inside it, which nest in each other as brackets do. Its state
// packs the length of the fence of a raw block that goes on (bits 0-2),
// whether a string does (bit 3), the depth of the block comment open (bits
// 4-5), what an embedded expression takes next (bits 6-7), and the
// brackets open: how many (bits 8-10), and each one's kind and the
// embedded expression it is part of (four bits each from bit 11).
type typstLexer struct {
	words map[string]TokenKind
}

// The kinds of brackets Typst nests: content is markup, blocks and
// parentheses are code, and math goes between dollars.
const (
	typstContent = iota // [ ]
	typstBlock          // { }
	typstParen          // ( )
	typstMath           // $ $
)

// What the embedded expression a # starts, in markup or in math, takes next.
const (
	typstNone      = iota
	typstPrimary   // after #: a name, a bracket or a string
	typstPostfix   // after a name or a bracket: .field, (arguments) or [content]
	typstStatement // after a keyword: code, to the end of the line
)

const typstMaxDepth = 7

type typstState struct {
	raw     int  // the length of the fence of a raw block that goes on
	str     bool // a string goes on
	comment int  // the depth of the block comment open
	embed   int
	depth   int
	stack   uint64 // four bits per bracket open: its kind, and the embed to go back to
}

func typstUnpack(s uint64) typstState {
	return typstState{raw: int(s & 7), str: s>>3&1 != 0, comment: int(s >> 4 & 3), embed: int(s >> 6 & 3), depth: int(s >> 8 & 7), stack: s >> 11 & (1<<28 - 1)}
}

func (s typstState) pack() uint64 {
	v := uint64(s.raw) | uint64(s.comment)<<4 | uint64(s.embed)<<6 | uint64(s.depth)<<8 | s.stack<<11
	if s.str {
		v |= 8
	}
	return v
}

// typstLine is a line being lexed.
type typstLine struct {
	typstState
	lx   *typstLexer
	line string
	out  lexOut
	// markerAt is where a heading's =, a list's - or + or a term's / may
	// be: the first character that is no space of a line of markup, or of a
	// content block, or -1.
	markerAt int
	// A heading goes on to the end of the line, or of the content block it
	// is in, which is the depth of the brackets it started at.
	heading      bool
	headingDepth int
	// A strong or emphasized span ends at spanEnd, with the delimiter
	// that closes it, or -1.
	spanEnd  int
	spanKind TokenKind
}

// add ends a token of kind k at end, unless it has no width.
func (l *typstLine) add(end int, k TokenKind) {
	if end > 0 {
		l.out.add(end, k)
	}
}

func (lx *typstLexer) Tokenize(line string, state uint64, tokens []Token) ([]Token, uint64) {
	l := typstLine{typstState: typstUnpack(state), lx: lx, line: line, out: lexOut{toks: tokens}, markerAt: -1, spanEnd: -1}
	n := len(line)
	if l.raw == 0 && l.comment == 0 && !l.str && l.embed == typstNone && l.ctx() == typstContent {
		l.markerAt = n - len(strings.TrimLeft(line, " \t"))
	}
	for i, steps := 0, 0; i < n; steps++ {
		if steps > 3*n+8 {
			// Every step moves on, or ends an embedded expression.
			break
		}
		switch {
		case l.raw != 0:
			i = l.rawBody(i, l.raw)
		case l.comment > 0:
			i = l.inComment(i)
		case l.str:
			i = l.stringBody(i)
		case l.embed != typstNone || l.inCode():
			i = l.code(i)
		case l.ctx() == typstMath:
			i = l.math(i)
		default:
			i = l.markup(i)
		}
	}
	l.add(n, TokenText)
	// An embedded expression ends with the line, unless a bracket holds
	// it open.
	if c := l.ctx(); c == typstContent || c == typstMath {
		l.embed = typstNone
	}
	return l.out.toks, l.pack()
}

// ctx returns the kind of the innermost bracket open: content outside any.
func (l *typstLine) ctx() int {
	if l.depth == 0 {
		return typstContent
	}
	return int(l.stack>>(4*(l.depth-1))) & 3
}

func (l *typstLine) inCode() bool {
	c := l.ctx()
	return c == typstBlock || c == typstParen
}

// textKind is the kind of the text of markup: a heading's, a strong or
// emphasized span's, or plain.
func (l *typstLine) textKind() TokenKind {
	switch {
	case l.spanEnd >= 0:
		return l.spanKind
	case l.heading && l.depth >= l.headingDepth:
		return TokenHeading
	}
	return TokenText
}

// push opens a bracket, which ends the embedded expression it opens in
// for its time, and which the embedded expression goes on after.
func (l *typstLine) push(kind int) {
	if l.depth == typstMaxDepth {
		if l.embed != typstStatement {
			l.embed = typstNone
		}
		return
	}
	resume := l.embed
	if resume == typstPrimary {
		resume = typstPostfix
	}
	l.stack |= uint64(kind|resume<<2) << (4 * l.depth)
	l.depth++
	l.embed = typstNone
}

// pop closes the innermost bracket when it is of kind, and goes back to
// the embedded expression it was part of.
func (l *typstLine) pop(kind int) bool {
	if l.depth == 0 || l.ctx() != kind {
		return false
	}
	e := int(l.stack >> (4 * (l.depth - 1)) & 15)
	l.stack &^= 15 << (4 * (l.depth - 1))
	l.depth--
	l.embed = e >> 2
	return true
}

// closed ends a statement, as #set text(red), that something other than
// more of itself follows a closing bracket of, at i.
func (l *typstLine) closed(i int) {
	if l.embed != typstStatement {
		return
	}
	rest := strings.TrimLeft(l.line[i:], " \t")
	if rest != "" && !strings.HasPrefix(rest, "else") && strings.IndexByte("+-*/=<>!&|%^.,;:([{", rest[0]) < 0 {
		l.embed = typstNone
	}
}

// afterPrimary goes on to what may follow the name, string or number a
// # starts.
func (l *typstLine) afterPrimary() {
	if l.embed == typstPrimary {
		l.embed = typstPostfix
	}
}

func typstSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\f' }

// typstMarkupStop reports whether a run of text of markup ends before c.
func typstMarkupStop(c byte) bool {
	switch c {
	case '/', '\\', '#', '$', '`', '@', '<', '*', '_', '[', ']', '~', '-', '.', 'h':
		return true
	}
	return false
}

// typstEscapeEnd returns where the escape the backslash at i starts ends:
// after \u{…}, else after the character it escapes.
func typstEscapeEnd(line string, i int) int {
	n := len(line)
	if i+1 >= n {
		return n
	}
	if line[i+1] == 'u' && i+2 < n && line[i+2] == '{' {
		if e := strings.IndexByte(line[i+3:], '}'); e >= 0 {
			return i + 4 + e
		}
	}
	_, w := utf8.DecodeRuneInString(line[i+1:])
	return i + 1 + w
}

// typstLabelEnd returns where the name of a label or a reference that starts
// at i ends, or i when none does: identifier characters, which are letters,
// digits, marks, _ and -, and : and . inside it.
func typstLabelEnd(line string, i int) int {
	if i >= len(line) || line[i] == '.' || line[i] == ':' {
		return i
	}
	for i < len(line) {
		if c := line[i]; c < utf8.RuneSelf {
			if !isIdentByte(c) && c != '-' && c != ':' && c != '.' {
				break
			}
			i++
			continue
		}
		r, w := utf8.DecodeRuneInString(line[i:])
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsMark(r) {
			break
		}
		i += w
	}
	return i
}

// typstRefEnd returns where the reference whose name starts at i ends, or i
// when it has none: the name of a label, but for the : and . it ends with.
func typstRefEnd(line string, i int) int {
	j := typstLabelEnd(line, i)
	for j > i && (line[j-1] == '.' || line[j-1] == ':') {
		j--
	}
	return j
}

// typstWordy reports whether r is of a word: a letter or a digit, but not
// one of Chinese, Japanese or Korean, where no word has two characters
// joined by a * or a _.
func typstWordy(r rune) bool {
	return (unicode.IsLetter(r) || unicode.IsNumber(r)) && !text.StandsAlone(r) && !typstHangul(r)
}

func typstHangul(r rune) bool {
	return r >= 0x1100 && r <= 0x11ff || r >= 0x3130 && r <= 0x318f || r >= 0xa960 && r <= 0xa97f || r >= 0xac00 && r <= 0xd7ff || r >= 0xffa0 && r <= 0xffdc
}

// typstInWord reports whether the * or _ at i is inside a word, which
// then holds it as text.
func typstInWord(line string, i int) bool {
	if i == 0 || i+1 >= len(line) {
		return false
	}
	a, _ := utf8.DecodeLastRuneInString(line[:i])
	b, _ := utf8.DecodeRuneInString(line[i+1:])
	return typstWordy(a) && typstWordy(b)
}

// typstFenceAt returns where the first run of exactly fence backticks in s
// starts, or -1.
func typstFenceAt(s string, fence int) int {
	for i := 0; i < len(s); {
		if s[i] != '`' {
			i++
			continue
		}
		j := i
		for j < len(s) && s[j] == '`' {
			j++
		}
		if j-i == fence {
			return i
		}
		i = j
	}
	return -1
}

// typstNumberEnd returns where the number at i, with its unit, as 1.5em
// or 50%, ends.
func typstNumberEnd(line string, i int) int {
	n := len(line)
	j := i
	for j < n {
		d := line[j]
		switch {
		case d >= '0' && d <= '9', d >= 'a' && d <= 'z', d >= 'A' && d <= 'Z', d == '_', d == '%':
			j++
			continue
		case d == '.' && j+1 < n && line[j+1] >= '0' && line[j+1] <= '9':
			j++
			continue
		case (d == '+' || d == '-') && j > i && (line[j-1] == 'e' || line[j-1] == 'E') && j+1 < n && line[j+1] >= '0' && line[j+1] <= '9':
			j++
			continue
		}
		break
	}
	return j
}

func (l *typstLine) inComment(i int) int {
	line := l.line
	j := i
	for j < len(line) {
		switch {
		case strings.HasPrefix(line[j:], "/*"):
			if l.comment < 3 {
				l.comment++
			}
			j += 2
		case strings.HasPrefix(line[j:], "*/"):
			j += 2
			l.comment--
			if l.comment == 0 {
				l.add(j, TokenComment)
				return j
			}
		default:
			j++
		}
	}
	l.add(j, TokenComment)
	return j
}

// stringBody lexes a string from i, just after its opening quote or at the
// start of a line it goes on in.
func (l *typstLine) stringBody(i int) int {
	line, n := l.line, len(l.line)
	l.str = true
	j := i
	for j < n {
		switch line[j] {
		case '\\':
			e := typstEscapeEnd(line, j)
			l.add(j, TokenString)
			l.add(e, TokenEscape)
			j = e
		case '"':
			l.add(j+1, TokenString)
			l.str = false
			return j + 1
		default:
			j++
		}
	}
	l.add(n, TokenString)
	return n
}

// rawBody lexes the code of raw text from i, to the run of backticks of the
// length of fence that closes it when it is on the line.
func (l *typstLine) rawBody(i, fence int) int {
	line := l.line
	if e := typstFenceAt(line[i:], fence); e >= 0 {
		l.add(i+e, TokenCode)
		// The backticks of a span are code, and a block's fences are not.
		kind := TokenCode
		if fence >= 3 {
			kind = TokenPunctuation
		}
		l.add(i+e+fence, kind)
		l.raw = 0
		return i + e + fence
	}
	l.add(len(line), TokenCode)
	l.raw = fence
	return len(line)
}

// rawStart lexes raw text from the backticks at i: code up to as many, which
// goes on over lines when they are not on this one. From three backticks it
// is a block, where a language may follow them.
func (l *typstLine) rawStart(i int) int {
	line, n := l.line, len(l.line)
	run := 1
	for i+run < n && line[i+run] == '`' {
		run++
	}
	switch {
	case run == 2:
		// Two backticks are raw text with nothing in it.
		l.add(i+2, TokenCode)
		return i + 2
	case run > 7:
		l.add(i+run, l.textKind())
		return i + run
	case run < 3:
		l.add(i+run, TokenCode)
		return l.rawBody(i+run, run)
	}
	l.add(i+run, TokenPunctuation)
	j := i + run
	if j < n && identStart(line[j:], "") {
		for j < n && identPart(line[j:], "-") {
			_, w := utf8.DecodeRuneInString(line[j:])
			j += w
		}
	}
	l.add(j, TokenType)
	return l.rawBody(j, run)
}

// span lexes the text between the * or _ at i and the next one as strong or
// emphasized, and reports whether there is one.
func (l *typstLine) span(i int) bool {
	line, n := l.line, len(l.line)
	c := line[i]
	for j := i + 1; j < n; j++ {
		switch {
		case line[j] == '\\':
			j++
		case line[j] == c && !typstInWord(line, j):
			l.spanEnd = j
			l.spanKind = TokenStrong
			if c == '_' {
				l.spanKind = TokenEmphasis
			}
			l.add(i+1, l.spanKind)
			return true
		}
	}
	return false
}

// lineStart lexes the marker a line of markup starts with, as = of a heading,
// a list's - or + or a term's /, and returns where it ends, or i.
func (l *typstLine) lineStart(i int) int {
	line, n := l.line, len(l.line)
	c := line[i]
	end := func(j int) bool { return j >= n || line[j] == ' ' || line[j] == '\t' }
	switch {
	case c == '=':
		j := i
		for j < n && line[j] == '=' {
			j++
		}
		if end(j) {
			l.heading, l.headingDepth = true, l.depth
			l.add(j, TokenHeading)
			return j
		}
	case (c == '-' || c == '+') && end(i+1):
		l.add(i+1, TokenPunctuation)
		return i + 1
	case c >= '0' && c <= '9':
		j := i
		for j < n && line[j] >= '0' && line[j] <= '9' {
			j++
		}
		if j < n && line[j] == '.' && end(j+1) {
			l.add(j+1, TokenPunctuation)
			return j + 1
		}
	case c == '/' && i+1 < n && line[i+1] == ' ':
		// / Term: description
		if colon := strings.IndexByte(line[i:], ':'); colon > 0 {
			l.add(i+1, TokenPunctuation)
			l.add(i+colon, TokenStrong)
			l.add(i+colon+1, TokenPunctuation)
			return i + colon + 1
		}
	}
	return i
}

// markup lexes one token of markup from i, and returns where it ends.
func (l *typstLine) markup(i int) int {
	line, n := l.line, len(l.line)
	if l.spanEnd >= 0 && i >= l.spanEnd {
		if i == l.spanEnd {
			l.add(i+1, l.spanKind)
			l.spanEnd = -1
			return i + 1
		}
		// A token went past the end of the span.
		l.spanEnd = -1
	}
	if i < l.markerAt {
		l.add(l.markerAt, l.textKind())
		return l.markerAt
	}
	if i == l.markerAt && l.spanEnd < 0 {
		if j := l.lineStart(i); j > i {
			return j
		}
	}
	c := line[i]
	var next byte
	if i+1 < n {
		next = line[i+1]
	}
	switch {
	case c == '/' && next == '/':
		l.add(n, TokenComment)
		return n
	case c == '/' && next == '*':
		l.comment = 1
		l.add(i+2, TokenComment)
		return i + 2
	case c == '\\':
		e := typstEscapeEnd(line, i)
		l.add(e, TokenEscape)
		return e
	case c == '#':
		return l.hash(i)
	case c == '$':
		l.add(i+1, TokenPunctuation)
		l.push(typstMath)
		return i + 1
	case c == '`':
		return l.rawStart(i)
	case c == '[' && l.depth > 0:
		l.add(i+1, TokenPunctuation)
		l.push(typstContent)
		return i + 1
	case c == ']' && l.depth > 0:
		l.add(i+1, TokenPunctuation)
		if l.pop(typstContent) {
			l.closed(i + 1)
		}
		return i + 1
	case c == '@':
		if j := typstRefEnd(line, i+1); j > i+1 {
			l.add(j, TokenLink)
			return j
		}
	case c == '<':
		if j := typstLabelEnd(line, i+1); j > i+1 && j < n && line[j] == '>' {
			l.add(j+1, TokenTag)
			return j + 1
		}
	case (c == '*' || c == '_') && l.spanEnd < 0 && !typstInWord(line, i):
		if l.span(i) {
			return i + 1
		}
	case c == 'h' && (strings.HasPrefix(line[i:], "http://") || strings.HasPrefix(line[i:], "https://")):
		j := i
		for j < n && !typstSpace(line[j]) && line[j] != '<' && line[j] != '>' && line[j] != '"' {
			j++
		}
		for j > i && strings.IndexByte(".,;:!?)]", line[j-1]) >= 0 {
			j--
		}
		l.add(j, TokenLink)
		return j
	case c == '~':
		l.add(i+1, TokenEscape)
		return i + 1
	case c == '-' && next == '-':
		j := i + 2
		if j < n && line[j] == '-' {
			j++
		}
		l.add(j, TokenEscape)
		return j
	case c == '.' && strings.HasPrefix(line[i:], "..."):
		l.add(i+3, TokenEscape)
		return i + 3
	}
	j := i + 1
	if c >= utf8.RuneSelf {
		_, w := utf8.DecodeRuneInString(line[i:])
		j = i + w
	}
	for j < n && !typstMarkupStop(line[j]) {
		j++
	}
	l.add(j, l.textKind())
	return j
}

// hash lexes the # at i, which starts an embedded expression when a name,
// a bracket, a string or a number follows it.
func (l *typstLine) hash(i int) int {
	line, n := l.line, len(l.line)
	if i+1 < n {
		c := line[i+1]
		if identStart(line[i+1:], "") || c == '(' || c == '{' || c == '[' || c == '"' || c >= '0' && c <= '9' {
			l.add(i+1, TokenMeta)
			l.embed = typstPrimary
			return i + 1
		}
	}
	l.add(i+1, l.textKind())
	return i + 1
}

// ident lexes the name at i.
func (l *typstLine) ident(i int) int {
	line, n := l.line, len(l.line)
	j := i
	for j < n && identPart(line[j:], "-") {
		_, w := utf8.DecodeRuneInString(line[j:])
		j += w
	}
	for j > i+1 && line[j-1] == '-' {
		j--
	}
	kind := l.lx.words[line[i:j]]
	switch kind {
	case TokenKeyword:
		if l.embed == typstPrimary {
			l.embed = typstStatement
		}
	case TokenConstant:
		l.afterPrimary()
	default:
		var after byte
		if j < n {
			after = line[j]
		}
		kind = TokenVariable
		switch {
		case after == '(' || after == '[':
			kind = TokenFunction
		case i > 0 && line[i-1] == '.' && (i < 2 || line[i-2] != '.'):
			kind = TokenProperty
		case l.ctx() == typstParen || l.embed == typstStatement:
			// A named argument, as size: 11pt.
			k := j
			for k < n && typstSpace(line[k]) {
				k++
			}
			if k < n && line[k] == ':' && (k+1 >= n || line[k+1] != ':') {
				kind = TokenProperty
			}
		}
		l.afterPrimary()
	}
	l.add(j, kind)
	return j
}

// code lexes one token of code from i, and returns where it ends.
func (l *typstLine) code(i int) int {
	line, n := l.line, len(l.line)
	c := line[i]
	// An embedded expression ends where nothing can go on with it.
	if e := l.embed; e == typstPrimary || e == typstPostfix {
		ok := false
		switch {
		case c == '(' || c == '[':
			ok = true
		case e == typstPrimary && (c == '{' || c == '"' || c >= '0' && c <= '9' || identStart(line[i:], "")):
			ok = true
		case e == typstPostfix && c == '.' && i+1 < n && identStart(line[i+1:], ""):
			ok = true
		}
		if !ok {
			l.embed = typstNone
			return i
		}
	}
	var next byte
	if i+1 < n {
		next = line[i+1]
	}
	switch {
	case typstSpace(c):
		j := i + 1
		for j < n && typstSpace(line[j]) {
			j++
		}
		l.add(j, TokenText)
		return j
	case c == '/' && next == '/':
		l.add(n, TokenComment)
		return n
	case c == '/' && next == '*':
		l.comment = 1
		l.add(i+2, TokenComment)
		return i + 2
	case c == '"':
		l.afterPrimary()
		return l.stringBody(i + 1)
	case c >= '0' && c <= '9' || c == '.' && next >= '0' && next <= '9':
		j := typstNumberEnd(line, i)
		l.add(j, TokenNumber)
		l.afterPrimary()
		return j
	case identStart(line[i:], ""):
		return l.ident(i)
	case c == '(':
		l.add(i+1, TokenPunctuation)
		l.push(typstParen)
		return i + 1
	case c == '{':
		l.add(i+1, TokenPunctuation)
		l.push(typstBlock)
		return i + 1
	case c == '[':
		l.add(i+1, TokenPunctuation)
		depth := l.depth
		l.push(typstContent)
		if l.depth > depth {
			// A content block starts as a line of markup does.
			l.markerAt = n - len(strings.TrimLeft(line[i+1:], " \t"))
		}
		return i + 1
	case c == '$':
		l.add(i+1, TokenPunctuation)
		l.push(typstMath)
		return i + 1
	case c == ')' || c == '}' || c == ']':
		l.add(i+1, TokenPunctuation)
		kind := typstParen
		switch c {
		case '}':
			kind = typstBlock
		case ']':
			kind = typstContent
		}
		if l.pop(kind) {
			l.closed(i + 1)
		}
		return i + 1
	case c == '<':
		if j := typstLabelEnd(line, i+1); j > i+1 && j < n && line[j] == '>' {
			l.add(j+1, TokenTag)
			return j + 1
		}
	case c == '.':
		if i+1 < n && identStart(line[i+1:], "") {
			l.add(i+1, TokenPunctuation)
			return l.ident(i + 1)
		}
		if next == '.' {
			l.add(i+2, TokenOperator)
			return i + 2
		}
		l.add(i+1, TokenPunctuation)
		return i + 1
	case c == ',' || c == ';' || c == ':':
		l.add(i+1, TokenPunctuation)
		return i + 1
	}
	if strings.IndexByte("+-*/=!<>%&|^~?", c) >= 0 {
		j := i + 1
		for j < n && strings.IndexByte("+-*/=!<>%&|^~?", line[j]) >= 0 && !(line[j] == '/' && j+1 < n && (line[j+1] == '/' || line[j+1] == '*')) {
			j++
		}
		l.add(j, TokenOperator)
		return j
	}
	_, w := utf8.DecodeRuneInString(line[i:])
	l.add(i+w, TokenText)
	return i + w
}

// math lexes one token of math from i, and returns where it ends.
func (l *typstLine) math(i int) int {
	line, n := l.line, len(l.line)
	c := line[i]
	var next byte
	if i+1 < n {
		next = line[i+1]
	}
	switch {
	case c == '$':
		l.add(i+1, TokenPunctuation)
		if l.pop(typstMath) {
			l.closed(i + 1)
		}
		return i + 1
	case typstSpace(c):
		j := i + 1
		for j < n && typstSpace(line[j]) {
			j++
		}
		l.add(j, TokenText)
		return j
	case c == '/' && next == '/':
		l.add(n, TokenComment)
		return n
	case c == '/' && next == '*':
		l.comment = 1
		l.add(i+2, TokenComment)
		return i + 2
	case c == '\\':
		e := typstEscapeEnd(line, i)
		l.add(e, TokenEscape)
		return e
	case c == '"':
		return l.stringBody(i + 1)
	case c == '#':
		return l.hash(i)
	case c >= '0' && c <= '9':
		j := i
		for j < n && (line[j] >= '0' && line[j] <= '9' || line[j] == '.' && j+1 < n && line[j+1] >= '0' && line[j+1] <= '9') {
			j++
		}
		l.add(j, TokenNumber)
		return j
	case c >= utf8.RuneSelf || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z':
		// A name is a letter, and the letters and digits after it: one
		// character is a variable, more are a symbol or a function.
		j, letters := i, 0
		for j < n {
			r, w := utf8.DecodeRuneInString(line[j:])
			if !unicode.IsLetter(r) && (j == i || !unicode.IsDigit(r) && !unicode.IsMark(r)) {
				break
			}
			j += w
			letters++
		}
		if j == i {
			_, w := utf8.DecodeRuneInString(line[i:])
			l.add(i+w, TokenText)
			return i + w
		}
		kind := TokenVariable
		switch {
		case j < n && line[j] == '(':
			kind = TokenFunction
		case letters > 1:
			kind = TokenConstant
		}
		l.add(j, kind)
		return j
	case strings.IndexByte("+-*/=<>!?|&'^_~:,;.@%", c) >= 0:
		j := i + 1
		for j < n && strings.IndexByte("+-*/=<>!?|&'^_~:,;.@%", line[j]) >= 0 && !(line[j] == '/' && j+1 < n && (line[j+1] == '/' || line[j+1] == '*')) {
			j++
		}
		l.add(j, TokenOperator)
		return j
	case strings.IndexByte("()[]{}", c) >= 0:
		l.add(i+1, TokenPunctuation)
		return i + 1
	}
	l.add(i+1, TokenText)
	return i + 1
}
