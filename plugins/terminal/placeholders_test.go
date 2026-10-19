package terminal

import (
	"image"
	"slices"
	"testing"

	"github.com/egoist/mygo/plugins/terminal/internal/vt"
)

// A placeholder cell says the image in its foreground color, the
// placement in its underline color, and the row, column and the ID's
// high byte in its diacritics; a diacritic not of the table counts as
// left out.
func TestParsePlaceholder(t *testing.T) {
	d := func(n int) rune { return diacritics[n] }
	for _, c := range []struct {
		name  string
		style vt.Style
		marks []rune
		want  placeholderCell
	}{
		{"palette", vt.Style{FG: vt.Color{Kind: 1, Index: 7}}, nil, placeholderCell{low: 7, high: -1, row: -1, col: -1}},
		{"RGB with a placement", vt.Style{FG: vt.Color{Kind: 2, RGB: vt.RGB{R: 1, G: 2, B: 3}}, UnderlineColor: vt.Color{Kind: 1, Index: 9}},
			[]rune{d(3), d(7), d(2)}, placeholderCell{low: 0x010203, id: 9, high: 2, row: 3, col: 7}},
		{"row only", vt.Style{FG: vt.Color{Kind: 1, Index: 1}}, []rune{d(296)}, placeholderCell{low: 1, high: -1, row: 296, col: -1}},
		{"not a diacritic of the table", vt.Style{FG: vt.Color{Kind: 1, Index: 1}}, []rune{'a', d(4)}, placeholderCell{low: 1, high: -1, row: -1, col: 4}},
		{"a high byte past 255", vt.Style{FG: vt.Color{Kind: 1, Index: 1}}, []rune{d(0), d(0), d(256)}, placeholderCell{low: 1, high: -1, row: 0, col: 0}},
	} {
		if got := parsePlaceholder(c.style, c.marks); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}

// Cells make runs as kitty makes them: a cell continues the run before it
// when it shows the next tile of the same row of the same placement, or
// leaves that out.
func TestPlaceholderRuns(t *testing.T) {
	// cell is a placeholder of image 1, nil a cell of text.
	cell := func(row, col int) *placeholderCell {
		return &placeholderCell{low: 1, high: -1, row: row, col: col}
	}
	other := &placeholderCell{low: 2, high: -1, row: -1, col: -1}
	high := &placeholderCell{low: 1, high: 3, row: 0, col: 0}
	for _, c := range []struct {
		name  string
		cells []*placeholderCell
		want  []placeholderRun
	}{
		{"every cell says its tile", []*placeholderCell{cell(0, 0), cell(0, 1), cell(0, 2)},
			[]placeholderRun{{x: 0, width: 3, image: 1}}},
		{"cells that say nothing continue", []*placeholderCell{cell(1, 2), cell(-1, -1), cell(-1, -1)},
			[]placeholderRun{{x: 0, width: 3, image: 1, row: 1, col: 2}}},
		{"a row alone continues", []*placeholderCell{cell(1, 2), cell(1, -1)},
			[]placeholderRun{{x: 0, width: 2, image: 1, row: 1, col: 2}}},
		{"a first cell saying nothing is the top left", []*placeholderCell{cell(-1, -1), cell(-1, -1)},
			[]placeholderRun{{x: 0, width: 2, image: 1}}},
		{"another column breaks", []*placeholderCell{cell(0, 0), cell(0, 5)},
			[]placeholderRun{{x: 0, width: 1, image: 1}, {x: 1, width: 1, image: 1, col: 5}}},
		{"another row breaks", []*placeholderCell{cell(0, 0), cell(1, 1)},
			[]placeholderRun{{x: 0, width: 1, image: 1}, {x: 1, width: 1, image: 1, row: 1, col: 1}}},
		{"text ends a run", []*placeholderCell{cell(0, 3), nil, cell(-1, -1)},
			[]placeholderRun{{x: 0, width: 1, image: 1, col: 3}, {x: 2, width: 1, image: 1}}},
		{"another image breaks", []*placeholderCell{cell(0, 0), other},
			[]placeholderRun{{x: 0, width: 1, image: 1}, {x: 1, width: 1, image: 2}}},
		{"the high byte, kept by cells that leave it out", []*placeholderCell{high, cell(-1, -1)},
			[]placeholderRun{{x: 0, width: 2, image: 3<<24 | 1}}},
	} {
		var p placeholderRuns
		var got []placeholderRun
		for x, cc := range c.cells {
			if cc == nil {
				got = p.end(got)
				continue
			}
			got = p.add(got, x, *cc)
		}
		got = p.end(got)
		if !slices.Equal(got, c.want) {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}

// A run shows its tiles of the image fitted to the placement's grid,
// keeping its aspect ratio, centered: nothing of the margins.
func TestPlaceholderPlacement(t *testing.T) {
	for _, c := range []struct {
		name                   string
		run                    placeholderRun
		cols, rows, imgW, imgH int
		want                   vt.Placement
		shows                  bool
	}{
		{"an image the shape of the grid", placeholderRun{x: 3, width: 2, image: 1, row: 1, col: 1}, 4, 2, 20, 10,
			vt.Placement{Image: 1, Z: -1, Col: 3, Row: 4, Width: 20, Height: 10, Source: image.Rect(5, 5, 15, 10)}, true},
		{"a narrower image, the left margin", placeholderRun{width: 1, image: 1}, 4, 2, 10, 10, vt.Placement{}, false},
		{"a narrower image, inside it", placeholderRun{x: 1, width: 1, image: 1, col: 1}, 4, 2, 10, 10,
			vt.Placement{Image: 1, Z: -1, Col: 1, Row: 4, Width: 10, Height: 10, Source: image.Rect(0, 0, 5, 5)}, true},
		{"a narrower image, from the margin into it", placeholderRun{width: 2, image: 1}, 4, 2, 10, 10,
			vt.Placement{Image: 1, Z: -1, Row: 4, X: 10, Width: 10, Height: 10, Source: image.Rect(0, 0, 5, 5)}, true},
		{"a narrower image, the whole row", placeholderRun{width: 4, image: 1}, 4, 2, 10, 10,
			vt.Placement{Image: 1, Z: -1, Row: 4, X: 10, Width: 20, Height: 10, Source: image.Rect(0, 0, 10, 5)}, true},
		{"a narrower image, the right margin", placeholderRun{x: 3, width: 1, image: 1, col: 3}, 4, 2, 10, 10, vt.Placement{}, false},
		{"no grid: the cells the image covers", placeholderRun{width: 3, image: 1}, 0, 0, 25, 10,
			vt.Placement{Image: 1, Z: -1, Row: 4, X: 3, Width: 25, Height: 10, Source: image.Rect(0, 0, 25, 10)}, true},
	} {
		got, shows := placeholderPlacement(c.run, 4, c.cols, c.rows, c.imgW, c.imgH, 10, 10)
		if shows != c.shows || (shows && got != c.want) {
			t.Errorf("%s: %+v %v, want %+v %v", c.name, got, shows, c.want, c.shows)
		}
	}
}
