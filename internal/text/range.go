package text

import "sort"

// RangeRects covers only the glyph advances belonging to a logical text
// range. Unlike a selection's line-wide highlight, bidi ranges may have
// several disjoint visual rectangles on one line.
func (l *Layout) RangeRects(start, end int) []Rect {
	var out []Rect
	for _, line := range l.Lines {
		var runs []Rect
		for _, g := range line.Glyphs {
			if g.Runes <= 0 || start >= g.Cluster+g.Runes || end <= g.Cluster {
				continue
			}
			a := float32(max(start, g.Cluster)-g.Cluster) / float32(g.Runes)
			b := float32(min(end, g.Cluster+g.Runes)-g.Cluster) / float32(g.Runes)
			if g.RTL {
				a, b = 1-b, 1-a
			}
			x0, x1 := g.X+g.Advance*a, g.X+g.Advance*b
			if x1 > x0 {
				runs = append(runs, Rect{x0, line.Y, x1 - x0, line.Height})
			}
		}
		sort.Slice(runs, func(i, j int) bool { return runs[i].X < runs[j].X })
		begin := len(out)
		for _, r := range runs {
			if n := len(out); n > begin && r.X <= out[n-1].X+out[n-1].W+0.01 {
				out[n-1].W = max(out[n-1].W, r.X+r.W-out[n-1].X)
			} else {
				out = append(out, r)
			}
		}
		// Hard line breaks have no glyph; give them the same small
		// advance as a selected newline.
		if start <= line.End && end > line.End {
			i := l.LineAt(line.End)
			if i+1 < len(l.Lines) && l.Lines[i+1].Start > line.End {
				x := line.lineCarets()[len(line.lineCarets())-1]
				out = append(out, Rect{x, line.Y, l.Params.Style.FontSize() / 3, line.Height})
			}
		}
	}
	return out
}
