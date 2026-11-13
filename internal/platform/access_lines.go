package platform

import (
	"sort"
	"unicode/utf16"
)

// The lines of a text field's Value, for the bridges that count its text
// in UTF-16 units, as AppKit does: a line ends after its line feed, as
// the Windows and Linux bridges take lines (isBoundary, segment), so that
// every unit is on a line and the text after the last line feed, empty or
// not, is the last line.

// LineOfUnit returns the line of value that UTF-16 unit u is on, from 0:
// how many line feeds come before it. A unit past the end is on the last
// line.
func LineOfUnit(value string, u int) int {
	line, at := 0, 0
	for _, r := range value {
		if at >= u {
			break
		}
		at += utf16.RuneLen(r)
		if r == '\n' {
			line++
		}
	}
	return line
}

// LineUnits returns the UTF-16 units of line of value, from start, n of
// them, its line feed included, and false for a line value does not have:
// one below 0 or past the last.
func LineUnits(value string, line int) (start, n int, ok bool) {
	if line < 0 {
		return 0, 0, false
	}
	cur := 0
	for _, r := range value {
		units := utf16.RuneLen(r)
		if cur < line {
			start += units
		} else {
			n += units
		}
		if r == '\n' {
			if cur == line {
				return start, n, true
			}
			cur++
		}
	}
	return start, n, cur == line
}

// RangeBounds returns where UTF-16 units from u, count of them, of n's
// Value show: the union of the boxes of the lines they are on
// (LineBoxes), or the node's Bounds where it gives none of them, as for
// lines out of view.
func RangeBounds(n *AccessNode, u, count int) RectF {
	if len(n.LineBoxes) == 0 {
		return n.Bounds
	}
	first := LineOfUnit(n.Value, u)
	last := first
	if count > 0 {
		last = LineOfUnit(n.Value, u+count-1)
	}
	boxes := n.LineBoxes
	i := sort.Search(len(boxes), func(i int) bool { return boxes[i].Line >= first })
	var out RectF
	found := false
	for ; i < len(boxes) && boxes[i].Line <= last; i++ {
		b := boxes[i].Bounds
		if !found {
			out, found = b, true
			continue
		}
		x0, y0 := min(out.X, b.X), min(out.Y, b.Y)
		x1, y1 := max(out.X+out.W, b.X+b.W), max(out.Y+out.H, b.Y+b.H)
		out = RectF{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
	}
	if !found {
		return n.Bounds
	}
	return out
}
