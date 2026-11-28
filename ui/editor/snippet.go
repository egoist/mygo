package editor

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Snippet is an immutable code template with numbered tab stops, parsed
// by ParseSnippet. It can be shared by completions and editor states.
// Its zero value is an empty template.
type Snippet struct {
	nodes    []snippetNode
	defaults map[int][]snippetNode // each number's text, the first given
	// The template with its variables unset, as Text returns it.
	text      string
	fields    []snippetField
	stops     []int
	variables bool // the text depends on variables
}

// snippetNode is literal text (id < 0) and its children (the default of
// an unknown variable), a variable (its name in text, its default in
// children) or a field (its number in id).
type snippetNode struct {
	text     string
	id       int
	variable bool
	children []snippetNode
}

type snippetField struct {
	id, start, end int
	depth          int // how many fields of its site enclose it
	site           int // insertion site, of a live session
}

// A template's bounds, as written and as laid out at each insertion
// site, where mirrors of nested placeholders and the values of variables
// in them can make it grow exponentially with its length.
const (
	maxSnippetText   = 1 << 20 // bytes
	maxSnippetFields = 1 << 14
	maxSnippetDepth  = 100 // fields and variables nested in one another
)

// Text returns the text inserted before the placeholders are edited,
// with the variables at their defaults.
func (s *Snippet) Text() string { return s.text }

// ParseSnippet parses TextMate/LSP snippets: $1 and ${1}, placeholders
// such as ${1:name}, nested ones such as ${1:a, ${2:b}}, choices such as
// ${1|one,two|}, $0 for the final stop, and variables such as
// $TM_SELECTED_TEXT and ${TM_FILENAME:default}. Numbers are non-negative
// 32-bit integers. Stops are visited in numeric order, with 0 last (the
// end when omitted).
// References to the same number share the first default given for it,
// nested placeholders included, and are edited together; empty stops at
// the same byte coalesce as ordinary carets do. A choice inserts its first
// option.
//
// Variables take their values when the snippet is inserted
// (State.SnippetVariable); an unset one inserts its default, or nothing.
// A variable VS Code does not define, written without a default,
// becomes a placeholder of its name, numbered after the others, as the
// LSP specification says.
//
// Escape $, } and backslash with a backslash, and also comma and pipe in
// choices. Transformations, malformed syntax, invalid UTF-8 and a
// placeholder whose default holds itself, directly or through other
// numbers, return an error. So does a template longer than 1 MiB,
// written or laid out, with more than 16,384 fields, or nesting more
// than 100 deep, mirrors' defaults included. Text is inserted literally,
// with CRLF normalized to LF, without adjusting indentation. Parsing an
// unsupported server snippet lets the app offer a plain-text completion
// instead.
func ParseSnippet(source string) (*Snippet, error) {
	if !utf8.ValidString(source) {
		return nil, fmt.Errorf("editor: snippet is not UTF-8")
	}
	if len(source) > maxSnippetText {
		return nil, fmt.Errorf("editor: snippet is longer than %d bytes", maxSnippetText)
	}
	p := &snippetParser{src: lf(source)}
	nodes, err := p.sequence(false)
	if err != nil {
		return nil, err
	}
	s := &Snippet{nodes: nodes, defaults: map[int][]snippetNode{}}
	last := 0
	walkSnippet(nodes, func(n *snippetNode) {
		if !n.variable && n.id > last {
			last = n.id
		}
	})
	unknown := map[string]int{}
	walkSnippet(nodes, func(n *snippetNode) {
		if !n.variable {
			return
		}
		if knownSnippetVariable(n.text) {
			s.variables = true
			return
		}
		if len(n.children) > 0 {
			*n = snippetNode{id: -1, children: n.children}
			return
		}
		id, ok := unknown[n.text]
		if !ok {
			if last == 1<<31-1 {
				err = fmt.Errorf("editor: snippet has no number left for variable %s", n.text)
			}
			last++
			id, unknown[n.text] = last, last
		}
		*n = snippetNode{id: id, children: []snippetNode{{id: -1, text: n.text}}}
	})
	if err != nil {
		return nil, err
	}
	walkSnippet(nodes, func(n *snippetNode) {
		if _, ok := s.defaults[n.id]; !ok && !n.variable && n.id >= 0 && len(n.children) > 0 {
			s.defaults[n.id] = n.children
		}
	})
	if id, ok := s.cycle(); ok {
		return nil, fmt.Errorf("editor: snippet's placeholder %d holds itself", id)
	}
	text, fields, err := s.render(nil)
	if err != nil {
		return nil, err
	}
	s.text, s.fields = text, fields
	for _, f := range fields {
		if f.id != 0 {
			s.stops = append(s.stops, f.id)
		}
	}
	slices.Sort(s.stops)
	s.stops = append(slices.Compact(s.stops), 0)
	return s, nil
}

type snippetParser struct {
	src   string
	i     int
	depth int // of the defaults being read
}

func (p *snippetParser) bad(at int) error {
	return fmt.Errorf("editor: unsupported or invalid snippet at byte %d", at)
}

// sequence reads text, fields and variables up to the end, or in a
// default (nested) up to its unescaped closing brace, which it leaves.
func (p *snippetParser) sequence(nested bool) ([]snippetNode, error) {
	var nodes []snippetNode
	var text strings.Builder
	flush := func() {
		if text.Len() > 0 {
			nodes = append(nodes, snippetNode{id: -1, text: text.String()})
			text.Reset()
		}
	}
	for p.i < len(p.src) {
		c := p.src[p.i]
		switch {
		case c == '}' && nested:
			flush()
			return nodes, nil
		case c == '\\' && p.i+1 < len(p.src) && strings.IndexByte(`$}\`, p.src[p.i+1]) >= 0:
			text.WriteByte(p.src[p.i+1])
			p.i += 2
		case c == '$':
			n, ok, err := p.dollar()
			if err != nil {
				return nil, err
			}
			if !ok {
				text.WriteByte('$')
				continue
			}
			flush()
			nodes = append(nodes, n)
		default:
			text.WriteByte(c)
			p.i++
		}
	}
	flush()
	return nodes, nil
}

// dollar reads the field or variable at a $, or reports a literal $
// (ok false) before text that starts neither.
func (p *snippetParser) dollar() (snippetNode, bool, error) {
	at := p.i
	p.i++
	if id, ok, err := p.number(at); ok || err != nil {
		return snippetNode{id: id}, ok, err
	}
	if name := p.name(); name != "" {
		return snippetNode{id: -1, variable: true, text: name}, true, nil
	}
	if !p.next('{') {
		return snippetNode{}, false, nil
	}
	id, number, err := p.number(at)
	if err != nil {
		return snippetNode{}, false, err
	}
	n := snippetNode{id: id}
	if !number {
		name := p.name()
		if name == "" {
			return snippetNode{}, false, p.bad(at)
		}
		n = snippetNode{id: -1, variable: true, text: name}
	}
	switch {
	case p.next(':'):
		if p.depth++; p.depth > maxSnippetDepth {
			return snippetNode{}, false, fmt.Errorf("editor: snippet nests more than %d deep at byte %d", maxSnippetDepth, at)
		}
		if n.children, err = p.sequence(true); err != nil {
			return snippetNode{}, false, err
		}
		p.depth--
	case number && id > 0 && p.next('|'):
		first, ok := p.choice()
		if !ok {
			return snippetNode{}, false, p.bad(at)
		}
		n.children = []snippetNode{{id: -1, text: first}}
	}
	// Transformations, as ${1/(.*)/$1/}, are not supported.
	if !p.next('}') {
		return snippetNode{}, false, p.bad(at)
	}
	return n, true, nil
}

// choice reads a choice's options through its closing pipe, and
// returns the first.
func (p *snippetParser) choice() (string, bool) {
	var first string
	for option := 0; ; option++ {
		var b strings.Builder
		for p.i < len(p.src) && p.src[p.i] != ',' && p.src[p.i] != '|' {
			if p.src[p.i] == '\\' && p.i+1 < len(p.src) && strings.IndexByte(`,|\`, p.src[p.i+1]) >= 0 {
				p.i++
			}
			b.WriteByte(p.src[p.i])
			p.i++
		}
		if b.Len() == 0 || p.i >= len(p.src) {
			return "", false
		}
		if option == 0 {
			first = b.String()
		}
		p.i++
		if p.src[p.i-1] == '|' {
			return first, true
		}
	}
}

func (p *snippetParser) number(at int) (int, bool, error) {
	start := p.i
	for p.i < len(p.src) && p.src[p.i] >= '0' && p.src[p.i] <= '9' {
		p.i++
	}
	if start == p.i {
		return 0, false, nil
	}
	id, err := strconv.Atoi(p.src[start:p.i])
	if err != nil || id > 1<<31-1 {
		return 0, false, p.bad(at)
	}
	return id, true, nil
}

func (p *snippetParser) name() string {
	start := p.i
	for p.i < len(p.src) {
		c := p.src[p.i]
		if c != '_' && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (p.i == start || c < '0' || c > '9') {
			break
		}
		p.i++
	}
	return p.src[start:p.i]
}

func (p *snippetParser) next(c byte) bool {
	if p.i < len(p.src) && p.src[p.i] == c {
		p.i++
		return true
	}
	return false
}

// walkSnippet calls visit on each node, before its children.
func walkSnippet(nodes []snippetNode, visit func(*snippetNode)) {
	for i := range nodes {
		visit(&nodes[i])
		walkSnippet(nodes[i].children, visit)
	}
}

// cycle finds a number whose default holds a field of its own number,
// directly or through the defaults of others, which its mirrors could not
// lay out alike.
func (s *Snippet) cycle() (int, bool) {
	const visiting, done = 1, 2
	state := map[int]int{}
	var visit func(id int) bool
	visit = func(id int) bool {
		switch state[id] {
		case visiting:
			return true
		case done:
			return false
		}
		state[id] = visiting
		var held func(nodes []snippetNode) bool
		held = func(nodes []snippetNode) bool {
			for _, n := range nodes {
				if !n.variable && n.id >= 0 && visit(n.id) || (n.variable || n.id < 0) && held(n.children) {
					return true
				}
			}
			return false
		}
		if held(s.defaults[id]) {
			return true
		}
		state[id] = done
		return false
	}
	for id := range s.defaults {
		if visit(id) {
			return id, true
		}
	}
	return 0, false
}

// knownSnippetVariable reports whether VS Code defines a variable, the
// only ones the editor and State.SnippetVariable give values, as VS
// Code's own resolvers do.
func knownSnippetVariable(name string) bool {
	switch name {
	case "TM_SELECTED_TEXT", "TM_CURRENT_LINE", "TM_CURRENT_WORD", "TM_LINE_INDEX", "TM_LINE_NUMBER",
		"TM_FILENAME", "TM_FILENAME_BASE", "TM_DIRECTORY", "TM_DIRECTORY_BASE", "TM_FILEPATH",
		"RELATIVE_FILEPATH", "CLIPBOARD", "SELECTION", "CURSOR_INDEX", "CURSOR_NUMBER",
		"WORKSPACE_NAME", "WORKSPACE_FOLDER", "LINE_COMMENT", "BLOCK_COMMENT_START", "BLOCK_COMMENT_END",
		"CURRENT_YEAR", "CURRENT_YEAR_SHORT", "CURRENT_MONTH", "CURRENT_MONTH_NAME", "CURRENT_MONTH_NAME_SHORT",
		"CURRENT_DATE", "CURRENT_DAY_NAME", "CURRENT_DAY_NAME_SHORT", "CURRENT_HOUR", "CURRENT_MINUTE",
		"CURRENT_SECOND", "CURRENT_MILLISECOND", "CURRENT_SECONDS_UNIX", "CURRENT_MILLISECONDS_UNIX",
		"CURRENT_TIMEZONE_OFFSET", "CURRENT_TIMEZONE_NAME", "RANDOM", "RANDOM_HEX", "UUID":
		return true
	}
	return false
}

// render lays out the template with the values variable gives (none
// when nil), its fields in template order, each before those it
// encloses, within the template's bounds.
func (s *Snippet) render(variable func(name string) string) (string, []snippetField, error) {
	r := snippetRender{s: s, variable: variable}
	r.nodes(s.nodes)
	if r.err != nil {
		return "", nil, r.err
	}
	text := r.text.String()
	if !slices.ContainsFunc(r.fields, func(f snippetField) bool { return f.id == 0 }) {
		r.fields = append(r.fields, snippetField{id: 0, start: len(text), end: len(text)})
	}
	return text, coalesceFields(r.fields), nil
}

type snippetRender struct {
	s        *Snippet
	variable func(string) string
	depth    int // of the nodes being laid out
	// How many fields enclose the next one.
	fieldDepth int
	text       strings.Builder
	fields     []snippetField
	err        error
}

func (r *snippetRender) nodes(nodes []snippetNode) {
	if len(nodes) == 0 {
		return
	}
	// The template's own nodes are the first level, outside any default.
	if r.depth++; r.depth > maxSnippetDepth+1 {
		r.err = fmt.Errorf("editor: snippet nests more than %d deep", maxSnippetDepth)
	}
	for _, n := range nodes {
		switch {
		case r.err != nil:
		case n.variable:
			value := ""
			if r.variable != nil {
				value = r.variable(n.text)
			}
			if value == "" {
				r.nodes(n.children)
			} else {
				r.write(value)
			}
		case n.id < 0:
			r.write(n.text)
			r.nodes(n.children)
		default:
			if len(r.fields) == maxSnippetFields {
				r.err = fmt.Errorf("editor: snippet has more than %d fields", maxSnippetFields)
				break
			}
			at := len(r.fields)
			r.fields = append(r.fields, snippetField{id: n.id, start: r.text.Len(), depth: r.fieldDepth})
			r.fieldDepth++
			r.nodes(r.s.defaults[n.id])
			r.fieldDepth--
			r.fields[at].end = r.text.Len()
		}
	}
	r.depth--
}

func (r *snippetRender) write(s string) {
	if r.text.Len()+len(s) > maxSnippetText {
		r.err = fmt.Errorf("editor: snippet expands past %d bytes", maxSnippetText)
		return
	}
	r.text.WriteString(s)
}

// coalesceFields drops a field at the same place as one of its number
// before it, in the same field, as empty mirrors next to each other.
// Coincident ones in different fields stay: their carets merge, so the
// session ends rather than growing one enclosing field and not another.
func coalesceFields(fields []snippetField) []snippetField {
	type place struct{ id, start, end, parent int }
	kept := make(map[place]int, len(fields))
	as := make([]int, len(fields)) // the field each one coalesced into
	var open []int                 // the fields enclosing this one
	out := make([]snippetField, 0, len(fields))
	for n, f := range fields {
		for len(open) > 0 && fields[open[len(open)-1]].depth >= f.depth {
			open = open[:len(open)-1]
		}
		key := place{f.id, f.start, f.end, -1}
		if len(open) > 0 {
			key.parent = as[open[len(open)-1]]
		}
		if m, ok := kept[key]; ok {
			as[n] = m
		} else {
			kept[key], as[n] = n, n
			out = append(out, f)
		}
		open = append(open, n)
	}
	return out
}
