package markdown

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func fixedTheme() *Theme {
	th := &Theme{FontSize: 14, FixedLineHeight: 22, CodeSize: 12.5 / 14, CodeFixedLineHeight: 18}
	for i, size := range []float32{19, 16, 15, 14, 14, 14} {
		th.Headings[i] = HeadingStyle{Size: size / 14, Weight: 600, FixedLineHeight: []float32{27, 24, 22, 22, 22, 22}[i]}
	}
	return th
}

func requireTextHeight(t *testing.T, tt *ui.Tester, text string, want float32) ui.Rect {
	t.Helper()
	r, ok := tt.Find(text)
	if !ok {
		t.Fatalf("text %q missing from %q", text, tt.Texts())
	}
	if math.Abs(float64(r.H-want)) > .02 {
		t.Fatalf("text %q height %v, want %v", text, r.H, want)
	}
	return r
}

func TestFixedLineHeightMetricsAndRelayout(t *testing.T) {
	th := fixedTheme()
	src := "# One\n\n## Two\n\n### Three\n\n#### Four\n\n##### Five\n\n###### Six\n\nbody first  \nbody second\n\n```text\ncode first\ncode second\n```\n"
	tt := ui.NewTester(func(c *ui.Context) { View(c, src, &Options{Theme: th}) }, 600, 1000)
	for i, name := range []string{"One", "Two", "Three", "Four", "Five", "Six"} {
		requireTextHeight(t, tt, name, th.Headings[i].FixedLineHeight)
	}
	requireTextHeight(t, tt, "body first\nbody second", 44)
	requireTextHeight(t, tt, "code first\ncode second", 36)
	th.FixedLineHeight, th.CodeFixedLineHeight, th.Headings[0].FixedLineHeight = 26, 20, 31
	tt.Frame()
	requireTextHeight(t, tt, "body first\nbody second", 52)
	requireTextHeight(t, tt, "code first\ncode second", 40)
	requireTextHeight(t, tt, "One", 31)
}

func TestFixedLineHeightKeepsCodeChunksAndCopyMapping(t *testing.T) {
	lines := make([]string, 64)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %02d", i)
	}
	src := "# Heading\n\nFirst **para** and a [link](https://go.dev).\n\n```text\n" + strings.Join(lines, "\n") + "\n```\n\nLast.\n"
	tt := ui.NewTester(func(c *ui.Context) { View(c, src, &Options{Theme: fixedTheme()}) }, 700, 2000)
	requireTextHeight(t, tt, strings.Join(lines[:32], "\n"), 32*18)
	requireTextHeight(t, tt, strings.Join(lines[32:], "\n"), 32*18)
	r, ok := tt.Find("First para and a link.")
	if !ok {
		t.Fatal("paragraph missing")
	}
	tt.ClickAt(r.X+2, r.Y+r.H/2)
	tt.Command("selectAll")
	tt.Key(ui.Cmd|ui.Shift, ui.KeyC)
	if tt.Clipboard() != strings.TrimSuffix(src, "\n") {
		t.Fatalf("copy mapping changed: %q", tt.Clipboard())
	}
	if err := tt.Click("link"); err != nil {
		t.Fatal(err)
	}
	if urls := tt.OpenedURLs(); len(urls) != 1 || urls[0] != "https://go.dev" {
		t.Fatalf("link behavior changed: %v", urls)
	}
}

func TestFixedLineHeightStreamingKeepsReadingAnchorAndSelection(t *testing.T) {
	var list ui.ListState
	list.Key = func(i int) any { return i }
	tail := "being written"
	o := &Options{Theme: fixedTheme(), Reveal: &Reveal{Unit: AtOnce}}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.List(c, &list, 1000, func(i int) {
			text := fmt.Sprintf("# Row %d\n\nBody of row %d.", i, i)
			if i == 999 {
				text += " " + tail
			}
			Stream(c, text, o)
		}).Grow(1)
	}, 600, 350)
	list.ScrollTo(500, ui.Start)
	tt.Frame()
	r, ok := tt.Find("Row 500")
	if !ok {
		t.Fatal("reading row missing")
	}
	tail += strings.Repeat(" another word", 200)
	tt.Frame()
	after, ok := tt.Find("Row 500")
	if !ok || math.Abs(float64(after.Y-r.Y)) > .02 {
		t.Fatal("tail growth moved reading anchor")
	}
	if len(tt.Texts()) > 100 {
		t.Fatal("virtual list built unbounded markdown rows")
	}
	// The same selection mapping survives appending text in a fixed-height view.
	src := "First paragraph.\n\nSecond paragraph is being"
	view := ui.NewTester(func(c *ui.Context) { Stream(c, src, o) }, 600, 350)
	first, _ := view.Find("First paragraph.")
	view.ClickAt(first.X+2, first.Y+first.H/2)
	view.Command("selectAll")
	src += " written."
	view.Frame()
	view.Command("copy")
	if !strings.HasPrefix(view.Clipboard(), "First paragraph.\n\nSecond paragraph is being") {
		t.Fatalf("streaming selection changed: %q", view.Clipboard())
	}
}
