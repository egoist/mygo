package parser

import "strings"

// Front matter (ExtFrontmatter) is found before the block parser runs, not
// by it: whether the lines at the start of a document are front matter
// depends on a closing fence further down, which a parser reading line by
// line cannot know when it reads the opening one. So parse looks for it
// when it starts at the top of the source, adds its block, and goes on
// from the line after the closing fence.
//
// What the text that comes later can change is whether there is front
// matter at all: a source opening with a fence that no line closes reads
// as CommonMark does, and the closing fence may be what is appended or
// typed. parseTail and edit ask frontmatterArrived, which searches only
// the text not searched before (fmScanned), and parse the whole source
// again when it has.

// maxFenceLine bounds the length of a fence's line, which is its three
// characters and the spaces and tabs after them: finding a fence never
// reads far into a long line.
const maxFenceLine = 256

// fmInfo is the front matter at the start of a source.
type fmInfo struct {
	format string // "yaml" or "toml"
	// start is the offset of the opening fence, after a byte order mark.
	start int
	// from is where the lines between the fences start, and closeAt the
	// closing fence's line; closeEnd is the end of its text, and next
	// where the line after it starts.
	from, closeAt, closeEnd, next int
	// complete tells that the closing fence's line ended with a line
	// ending that no appended text can change.
	complete bool
}

// lineEndAt returns, for the line of src starting at pos, where its text
// ends and where the next line starts: a line ends at \n, \r or \r\n.
func lineEndAt(src string, pos int) (end, next int) {
	i := strings.IndexAny(src[pos:], "\r\n")
	if i < 0 {
		return len(src), len(src)
	}
	end = pos + i
	next = end + 1
	if src[end] == '\r' && next < len(src) && src[next] == '\n' {
		next++
	}
	return end, next
}

// isFenceLine reports whether line, without its ending, is the fence f,
// with nothing but spaces and tabs after it.
func isFenceLine(line, f string) bool {
	if len(line) < len(f) || len(line) > maxFenceLine || line[:len(f)] != f {
		return false
	}
	return strings.Trim(line[len(f):], " \t") == ""
}

// openingFence returns the fence the first line of src is, "---" or
// "+++", the offset it starts at, and where the line after it starts, or
// no fence.
func openingFence(src string) (fence string, start, next int) {
	if strings.HasPrefix(src, "\xef\xbb\xbf") {
		start = 3
	}
	if len(src)-start < 3 {
		return "", 0, 0
	}
	switch f := src[start : start+3]; f {
	case "---", "+++":
		fence = f
	default:
		return "", 0, 0
	}
	// The window holds the line and its ending, however long the line is.
	window := src[:min(len(src), start+maxFenceLine+2)]
	end, next := lineEndAt(window, start)
	if end == len(window) && len(window) < len(src) || !isFenceLine(window[start:end], fence) {
		return "", 0, 0
	}
	return fence, start, next
}

// frontmatter finds the front matter at the start of src, the source
// shown. While none closes, it remembers how far it searched.
func (d *Document) frontmatter(src string) (fmInfo, bool) {
	fence, start, from := openingFence(src)
	if fence == "" {
		d.fmScanned = 0
		return fmInfo{}, false
	}
	pos := max(d.fmScanned, from)
	if pos > len(src) {
		pos = from
	}
	for pos < len(src) {
		end, next := lineEndAt(src, pos)
		d.fmSteps += next - pos
		complete := next > end && !(next == len(src) && src[next-1] == '\r')
		if isFenceLine(src[pos:end], fence) {
			d.fmScanned = 0
			format := "yaml"
			if fence == "+++" {
				format = "toml"
			}
			return fmInfo{format: format, start: start, from: from, closeAt: pos, closeEnd: end, next: next, complete: complete}, true
		}
		if !complete {
			// The last line may yet grow into the closing fence.
			break
		}
		pos = next
	}
	d.fmScanned = pos
	return fmInfo{}, false
}

// frontmatterArrived reports whether the source shown starts with front
// matter that the blocks parsed so far lack: its closing fence came with
// the text appended or edited, so that parsing must start again at the top.
func (d *Document) frontmatterArrived() bool {
	if d.ext&ExtFrontmatter == 0 || len(d.blocks) > 1 && d.blocks[1].Kind == Frontmatter {
		return false
	}
	_, ok := d.frontmatter(d.src[:d.displayCut()])
	return ok
}

// addFrontmatter adds the block of fm, the first of the document, whose
// lines are those between its fences.
func (d *Document) addFrontmatter(src string, fm fmInfo) {
	i := len(d.blocks)
	n := int32(len(d.segs))
	root := &d.blocks[0]
	d.blocks = append(d.blocks, Block{Kind: Frontmatter, Parent: 0, Start: fm.start, End: fm.closeEnd, Next: i + 1, textS: -1, segA: n, segB: n, segMark: n, info: fm.format})
	b := &d.blocks[i]
	b.parse.lastChild = -1
	b.parse.prev = root.parse.lastChild
	b.parse.startLine = 1
	// Not quiet: it stands where it does because it starts the source, so
	// that no block parsed before an edit gives it again.
	root.parse.lastChild = int32(i)
	for pos := fm.from; pos < fm.closeAt; {
		end, next := lineEndAt(src, pos)
		d.appendSeg(i, seg{start: int32(pos), end: int32(end)})
		pos = next
	}
}
