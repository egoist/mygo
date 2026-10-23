package terminal

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/plugins/terminal/internal/vt"
	"github.com/egoist/mygo/ui"
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
		want                   placeholderTile
		shows                  bool
	}{
		{"an image the shape of the grid", placeholderRun{x: 3, width: 2, image: 1, row: 1, col: 1}, 4, 2, 20, 10,
			placeholderTile{x: 30, y: 40, w: 20, h: 10, src: ui.Rect{X: 5, Y: 5, W: 10, H: 5}}, true},
		{"a narrower image, the left margin", placeholderRun{width: 1, image: 1}, 4, 2, 10, 10, placeholderTile{}, false},
		{"a narrower image, inside it", placeholderRun{x: 1, width: 1, image: 1, col: 1}, 4, 2, 10, 10,
			placeholderTile{x: 10, y: 40, w: 10, h: 10, src: ui.Rect{W: 5, H: 5}}, true},
		{"a narrower image, from the margin into it", placeholderRun{width: 2, image: 1}, 4, 2, 10, 10,
			placeholderTile{x: 10, y: 40, w: 10, h: 10, src: ui.Rect{W: 5, H: 5}}, true},
		{"a narrower image, the whole row", placeholderRun{width: 4, image: 1}, 4, 2, 10, 10,
			placeholderTile{x: 10, y: 40, w: 20, h: 10, src: ui.Rect{W: 10, H: 5}}, true},
		{"a narrower image, the right margin", placeholderRun{x: 3, width: 1, image: 1, col: 3}, 4, 2, 10, 10, placeholderTile{}, false},
		{"no grid: the cells the image covers", placeholderRun{width: 3, image: 1}, 0, 0, 25, 10,
			placeholderTile{x: 3, y: 40, w: 25, h: 10, src: ui.Rect{W: 25, H: 10}}, true},
	} {
		got, shows := placeholderPlacement(c.run, 4, c.cols, c.rows, c.imgW, c.imgH, 10, 10)
		if shows != c.shows || (shows && got != c.want) {
			t.Errorf("%s: %+v %v, want %+v %v", c.name, got, shows, c.want, c.shows)
		}
	}
}

// placeholderImage returns what a program prints to show a w×h image of
// one color through Unicode placeholders at the cursor, as omp shows
// pasted images: the pixels alone, a virtual placement of a grid of
// cols×rows cells, and the cells, the image's ID in their foreground
// color, row under row.
func placeholderImage(id, w, h, cols, rows int, c ui.Color) string {
	px := bytes.Repeat([]byte{c.R, c.G, c.B, 0xff}, w*h)
	var b strings.Builder
	fmt.Fprintf(&b, "\x1b_Ga=t,f=32,s=%d,v=%d,i=%d,q=2;%s\x1b\\", w, h, id, base64.StdEncoding.EncodeToString(px))
	fmt.Fprintf(&b, "\x1b_Ga=p,U=1,i=%d,c=%d,r=%d,q=2\x1b\\", id, cols, rows)
	for r := range rows {
		fmt.Fprintf(&b, "\x1b[38;5;%dm", id)
		for col := range cols {
			b.WriteRune(placeholder)
			b.WriteRune(diacritics[r])
			b.WriteRune(diacritics[col])
		}
		fmt.Fprintf(&b, "\x1b[0m\x1b[B\x1b[%dD", cols) // the next row, under the first cell
	}
	return b.String()
}

// The cells of Unicode placeholders show their image, and no text: omp's
// previews of pasted images showed glyphs.
func TestPlaceholderImages(t *testing.T) {
	loadLib(t)
	term, err := New(Options{Conn: newPipe(), Theme: LightTheme()})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	tt := ui.NewTester(func(c *ui.Context) { View(c, term).Fill() }, 400, 200)
	tt.Frame()
	red := ui.RGB(255, 0, 0)
	bg := LightTheme().Background
	term.Feed([]byte("\x1b[2;3H" + placeholderImage(5, 2*term.v.cellW, 2*term.v.cellH, 2, 2, red)))
	// The first frame reads the image the placeholders show, the next
	// draws it.
	tt.Frame()
	tt.Frame()
	checkCells(t, tt, term, "", []cellColor{{2, 1, red}, {3, 2, red}, {4, 1, bg}, {2, 3, bg}})
	// Deleted, the cells show nothing.
	term.Feed([]byte("\x1b_Ga=d,d=I,i=5,q=2\x1b\\"))
	tt.Frame()
	checkCells(t, tt, term, "deleted: ", []cellColor{{2, 1, bg}, {3, 2, bg}})
}

type cellColor struct {
	col, row int
	want     ui.Color
}

// checkCells checks the colors at the centers of cells.
func checkCells(t *testing.T, tt *ui.Tester, term *Terminal, label string, cells []cellColor) {
	t.Helper()
	for _, c := range cells {
		x, y := cellCenter(term, c.col, c.row)
		px := tt.Image().RGBAAt(int(x*term.v.scale), int(y*term.v.scale))
		if got := ui.RGB(px.R, px.G, px.B); got != c.want {
			t.Errorf("%scell %d,%d is %v, want %v", label, c.col, c.row, got, c.want)
		}
	}
}

// A Mirror carries Unicode placeholders: the other terminal gets the
// virtual placement and the image, and its view draws the cells' image.
func TestMirrorPlaceholders(t *testing.T) {
	loadLib(t)
	src, err := New(Options{Conn: newPipe()})
	if err != nil {
		t.Fatal(err)
	}
	dst, err := New(Options{Conn: newPipe(), Theme: LightTheme()})
	if err != nil {
		t.Fatal(err)
	}
	m, err := src.NewMirror()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close(); src.Close(); dst.Close() })
	tt := ui.NewTester(func(c *ui.Context) { View(c, dst).Fill() }, 400, 200)
	tt.Frame()
	cols, rows := dst.Size()
	src.ResizePixels(cols, rows, dst.v.cellW, dst.v.cellH)
	frame := func() string {
		f, _ := m.Frame()
		dst.Feed(f)
		return string(f)
	}
	frame()
	red := ui.RGB(255, 0, 0)
	src.Feed([]byte("\x1b[2;3H" + placeholderImage(6, 2*dst.v.cellW, 2*dst.v.cellH, 2, 2, red)))
	if f := frame(); !strings.Contains(f, "a=t") || !strings.Contains(f, "U=1") {
		t.Fatalf("the image and its virtual placement were not sent: %q", f)
	}
	if f := frame(); strings.Contains(f, "_G") {
		t.Errorf("a frame with nothing new sent images again: %q", f)
	}
	tt.Frame()
	tt.Frame()
	bg := LightTheme().Background
	checkCells(t, tt, dst, "", []cellColor{{2, 1, red}, {3, 2, red}, {4, 1, bg}})
	// The program deletes the placement, keeping the image: the other
	// terminal's goes too.
	src.Feed([]byte("\x1b_Ga=d,d=i,i=6,q=2\x1b\\"))
	frame()
	tt.Frame()
	checkCells(t, tt, dst, "placement deleted: ", []cellColor{{2, 1, bg}, {3, 2, bg}})
}
