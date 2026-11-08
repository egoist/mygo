package markdown

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// longReply is the first 25 KB of the benchmarks' answer, cut between
// blocks, with a footnote.
func longReply(t testing.TB) string {
	data, err := os.ReadFile("parser/testdata/llm.md")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data[:25<<10])
	src = src[:strings.LastIndex(src, "\n\n")+1]
	return src + "\nA claim with a source.[^src]\n\n[^src]: Where it comes from.\n"
}

func partKeys(p *Parts) []int {
	keys := make([]int, len(p.parts))
	for i := range keys {
		keys[i] = p.Key(i)
	}
	return keys
}

// Parts stacked without a gap are as high as the view, their text is
// the view's, and Text gives it as copying the parts built does.
func TestPartsShowAsView(t *testing.T) {
	src := longReply(t)
	var p Parts
	shared := &Options{ShareSelection: true}
	n := 0
	tt := ui.NewTester(func(c *ui.Context) {
		n = p.Update(c, src, shared, false)
		ui.Row(c).AlignItems(ui.Start).Children(func() {
			ui.Column(c).Width(380).Selectable().Label("view").Children(func() { View(c, src, shared) })
			ui.Column(c).Width(380).Selectable().Label("parts").Children(func() {
				for i := range n {
					p.View(c, i)
				}
			})
		})
	}, 800, 900)
	if n < 10 || p.Key(n-1) != -1 {
		t.Fatalf("%d parts, keys %v: want many, the footnotes last", n, partKeys(&p))
	}
	view, _ := tt.Find("view")
	parts, _ := tt.Find("parts")
	if view.H != parts.H {
		t.Errorf("the parts are %g high, the view %g", parts.H, view.H)
	}

	copyAll := func(label string) string {
		r, _ := tt.Find(label)
		tt.ClickAt(r.X+10, r.Y+10)
		tt.Key(ui.Cmd, ui.KeyA)
		tt.Key(ui.Cmd, ui.KeyC)
		return tt.Clipboard()
	}
	fromView := copyAll("view")
	fromParts := copyAll("parts")
	var texts []string
	for i := range n {
		texts = append(texts, p.Text(i))
	}
	if fromParts != fromView {
		t.Errorf("the parts copy otherwise than the view:\n%s", firstDifference(fromParts, fromView))
	}
	if got := strings.Join(texts, "\n"); got != fromParts {
		t.Errorf("Text differs from what copying the parts gives:\n%s", firstDifference(got, fromParts))
	}
}

func firstDifference(a, b string) string {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	return "…" + a[max(0, i-40):min(len(a), i+40)] + "…\n…" + b[max(0, i-40):min(len(b), i+40)] + "…"
}

// The parts of a reply streaming in keep their keys and their blocks:
// only the last one grows, and a new one comes after it.
func TestPartsStreamKeepKeys(t *testing.T) {
	src := longReply(t)
	p := streamParts(t, src, 16)
	var whole Parts
	ui.NewTester(func(c *ui.Context) { whole.Update(c, src, nil, false) }, 400, 300)
	if got, want := partKeys(p), partKeys(&whole); !slices.Equal(got, want) {
		t.Errorf("streamed in, the keys are %v; parsed whole, %v", got, want)
	}
	if len(p.parts) < 20 {
		t.Fatalf("streamed in, the reply has %d parts", len(p.parts))
	}
}

// Lines that may still turn into another block start no part until they
// cannot: a line of # that more # would make text, and a line of text
// starting with # that a line of - below makes a heading.
func TestPartsStreamHeadingsWait(t *testing.T) {
	src := "[^n]: A note.\n\nIntro.\n\n#x\n----\nx\n\n" + strings.Repeat("#", 4) + "###\n\n## Real\n\nText.[^n]\n"
	p := streamParts(t, src, 1)
	var firsts []string
	for _, pt := range p.parts {
		if pt.key >= 0 {
			firsts = append(firsts, strings.SplitN(src[pt.key:], "\n", 2)[0])
		}
	}
	if want := []string{"Intro.", "## Real"}; !slices.Equal(firsts, want) {
		t.Errorf("parts start at %q, want %q", firsts, want)
	}
}

// streamParts streams src into Parts step bytes a frame, failing t when
// a part but the last changes or goes; the last can go only as its first
// block turns into one that shows elsewhere, as a footnote's definition.
func streamParts(t *testing.T, src string, step int) *Parts {
	t.Helper()
	var p Parts
	o := &Options{Reveal: &Reveal{Unit: AtOnce}}
	shown := 0
	streaming := true
	tt := ui.NewTester(func(c *ui.Context) {
		p.Update(c, src[:shown], o, streaming)
	}, 400, 300)
	var before []part
	for shown < len(src) {
		shown = min(shown+step, len(src))
		tt.Frame()
		now := slices.DeleteFunc(slices.Clone(p.parts), func(pt part) bool { return pt.from < 0 })
		if len(now) < len(before)-1 {
			t.Fatalf("at %d bytes, %d parts after %d", shown, len(now), len(before))
		}
		for k, pt := range before {
			switch {
			case k < len(before)-1 && now[k] != pt:
				t.Fatalf("at %d bytes, part %d went from %+v to %+v", shown, k, pt, now[k])
			case k == len(before)-1 && k < len(now) && (now[k].key != pt.key || now[k].from != pt.from):
				t.Fatalf("at %d bytes, the last part went from %+v to %+v", shown, pt, now[k])
			case k == len(before)-1 && k == len(now) && p.st.shows(pt.from):
				t.Fatalf("at %d bytes, the last part %+v went", shown, pt)
			}
		}
		before = now
	}
	streaming = false
	tt.Frame()
	return &p
}

// A part starts at each heading of #, which nothing turns into another
// block, and at the first block after a kilobyte; a heading of a line
// of = below its text does not start one.
func TestPartsBreaks(t *testing.T) {
	long := strings.Repeat("word ", 250) // 1250 bytes
	src := "Intro.\n\n# One\n\nText.\n\nSetext\n======\n\n" + long + "\n\nAfter.\n\nMore.\n"
	var p Parts
	ui.NewTester(func(c *ui.Context) { p.Update(c, src, nil, false) }, 400, 300)
	var firsts []string
	for _, pt := range p.parts {
		firsts = append(firsts, strings.SplitN(src[pt.key:], "\n", 2)[0])
	}
	want := []string{"Intro.", "# One", "After."}
	if !slices.Equal(firsts, want) {
		t.Errorf("parts start at %q, want %q", firsts, want)
	}
}

// In a List, a long reply builds only the parts in view and the rows
// beyond them the list builds.
func TestPartsInListBuildPartsInView(t *testing.T) {
	src := longReply(t)
	var p Parts
	var list ui.ListState
	list.Key = func(i int) any { return p.Key(i) }
	built := map[int]bool{}
	n := 0
	tt := ui.NewTester(func(c *ui.Context) {
		n = p.Update(c, src, &Options{ShareSelection: true}, false)
		clear(built)
		ui.Column(c).Selectable().Fill().Children(func() {
			ui.List(c, &list, n, func(i int) {
				built[i] = true
				p.View(c, i)
			}).Grow(1)
		})
	}, 800, 900)
	tt.Frame()
	t.Logf("built %d parts of %d", len(built), n)
	if len(built) >= n/2 {
		t.Fatalf("the list built %d parts of %d", len(built), n)
	}
	if !tt.HasText("Building a fast user service") || tt.HasText("Where it comes from.") {
		t.Error("the first part does not show, or the footnotes at the end do")
	}
	list.ScrollToEnd()
	tt.Frame()
	if !built[n-1] || built[0] || !tt.HasText("Where it comes from.") {
		t.Errorf("at the end, the list built %v of %d parts", built, n)
	}
}

// A link to a heading in a part the list does not build asks the app to
// scroll to that part, once, and the view scrolls to the heading as it
// is built.
func TestPartsScrollToHeading(t *testing.T) {
	src := "[Jump](#target) to the target.\n\n" + strings.Repeat("A paragraph before the destination, long enough to fill a line.\n\n", 60) + "# Target\n\nDestination.\n\n" + strings.Repeat("After.\n\n", 40)
	var p Parts
	var list ui.ListState
	list.Key = func(i int) any { return p.Key(i) }
	var asked []int
	tt := ui.NewTester(func(c *ui.Context) {
		c.DisableAnimations()
		n := p.Update(c, src, nil, false)
		ui.List(c, &list, n, func(i int) { p.View(c, i) }).Grow(1)
		if i, ok := p.Scroll(); ok {
			asked = append(asked, i)
			list.ScrollTo(i, ui.Start)
		}
	}, 400, 300)
	if err := tt.Click("Jump"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	target, ok := p.partOf(p.st.headingNamed("target"))
	if !ok || len(asked) != 1 || asked[0] != target {
		t.Fatalf("asked to scroll to %v, want part %d once", asked, target)
	}
	if r, ok := tt.Find("Target"); !ok || r.Y < 0 || r.Y > 300 {
		t.Errorf("the heading is at %+v, out of view", r)
	}
	if p.st.scrollTo != -1 {
		t.Errorf("the view still waits to scroll to block %d", p.st.scrollTo)
	}

	// A footnote is in the part of the footnotes, the last.
	var fn Parts
	ui.NewTester(func(c *ui.Context) { fn.Update(c, "Text.[^a]\n\n[^a]: A note.\n", nil, false) }, 400, 300)
	note, _ := fn.st.doc.Footnote("a")
	if i, ok := fn.partOf(note); !ok || i != len(fn.parts)-1 || fn.Key(i) != -1 {
		t.Errorf("the footnote is in part %d (%v), want the last", i, ok)
	}
}

// Nothing asking for it, a target to scroll to does not wait for the
// user to scroll its part into view.
func TestPartsScrollExpires(t *testing.T) {
	var p Parts
	tt := ui.NewTester(func(c *ui.Context) { p.Update(c, "# A\n\nText.\n", nil, false) }, 400, 300)
	p.st.scrollTo = 1
	for range 4 {
		tt.Frame()
	}
	if p.st.scrollTo != -1 {
		t.Errorf("block %d still waits to be scrolled to", p.st.scrollTo)
	}
}

// A selection across the parts of a reply in a List keeps its ends as
// the list scrolls them away, and copies the parts the list does not
// build from Text: Select All copies the reply as the parts built
// together copy it.
func TestPartsSelectionInList(t *testing.T) {
	src := longReply(t)
	var p Parts
	var list ui.ListState
	list.Key = func(i int) any { return p.Key(i) }
	list.Text = func(i int) string { return p.Text(i) }
	n := 0
	tt := ui.NewTester(func(c *ui.Context) {
		n = p.Update(c, src, nil, false)
		ui.Column(c).Selectable().Fill().Children(func() {
			ui.List(c, &list, n, func(i int) { p.View(c, i) }).Grow(1)
		})
	}, 800, 600)
	var texts []string
	for i := range n {
		texts = append(texts, p.Text(i))
	}
	whole := strings.Join(texts, "\n")

	r, _ := tt.Find("Building a fast user service")
	tt.ClickAt(r.X+2, r.Y+r.H/2)
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Key(ui.Cmd, ui.KeyC)
	if got := tt.Clipboard(); got != whole {
		t.Fatalf("Select All copied otherwise than the parts' text:\n%s", firstDifference(got, whole))
	}

	// From the title to the end of the footnote, the reply's last text, by
	// a Shift-click once the list scrolled there.
	tt.ClickAt(r.X+2, r.Y+r.H/2)
	list.ScrollToEnd()
	tt.Frame()
	if err := tt.ClickWith(ui.Shift, "Where it comes from."); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyC)
	got := tt.Clipboard()
	if got != whole {
		t.Errorf("the selection from the first part to the last copied otherwise:\n%s", firstDifference(got, whole))
	}
	list.ScrollTo(0, ui.Start)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyC)
	if tt.Clipboard() != got {
		t.Error("scrolled back to the start, the selection copies otherwise")
	}
}

// A reply regenerated shorter while its text is selected: SelectedTexts,
// asked as the app builds with the reply's new parts, does not ask for
// parts by the last frame's rows, and Key and Text of a part the reply
// no longer has are -2 and "".
func TestPartsRegenerated(t *testing.T) {
	src := longReply(t)
	var p Parts
	var list ui.ListState
	list.Key = func(i int) any { return p.Key(i) }
	list.Text = func(i int) string { return p.Text(i) }
	n := 0
	tt := ui.NewTester(func(c *ui.Context) {
		n = p.Update(c, src, nil, false)
		col := ui.Column(c).Selectable().Fill()
		col.SelectedTexts()
		col.Children(func() {
			ui.List(c, &list, n, func(i int) { p.View(c, i) }).Grow(1)
		})
	}, 800, 600)
	r, _ := tt.Find("Building a fast user service")
	tt.ClickAt(r.X+2, r.Y+r.H/2)
	tt.Key(ui.Cmd, ui.KeyA)
	was := n
	src = "# Regenerated\n\nShorter.\n"
	tt.Frame()
	if n != 1 || p.Key(was-1) != -2 || p.Text(was-1) != "" || p.Key(-1) != -2 {
		t.Errorf("%d parts; Key and Text of part %d, gone: %d, %q", n, was-1, p.Key(was-1), p.Text(was-1))
	}
}

// Text gives what the view shows of an image not loaded without a
// description, and of an empty code block.
func TestPartsTextPlaceholders(t *testing.T) {
	src := "![](missing.png)\n\n```\n\n```\n"
	var p Parts
	o := &Options{ShareSelection: true}
	tt := ui.NewTester(func(c *ui.Context) {
		n := p.Update(c, src, o, false)
		ui.Column(c).Selectable().Label("parts").Fill().Children(func() {
			for i := range n {
				p.View(c, i)
			}
		})
	}, 400, 300)
	r, _ := tt.Find("parts")
	tt.ClickAt(r.X+r.W-2, r.Y+r.H-2)
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Key(ui.Cmd, ui.KeyC)
	if got := p.Text(0); got != tt.Clipboard() {
		t.Errorf("Text is %q, the view copies %q", got, tt.Clipboard())
	}
}

// A document shortened past the block to scroll to forgets it, and the
// part Scroll told of is told once, while the app does not scroll there.
func TestPartsScrollTargetGoes(t *testing.T) {
	var p Parts
	src := "[Jump](#target) to it.\n\n" + strings.Repeat("A paragraph before the destination, long enough to fill a line.\n\n", 60) + "# Target\n\nText.\n"
	o := &Options{}
	told := 0
	var list ui.ListState
	list.Key = func(i int) any { return p.Key(i) }
	tt := ui.NewTester(func(c *ui.Context) {
		n := p.Update(c, src, o, false)
		ui.List(c, &list, n, func(i int) { p.View(c, i) }).Grow(1)
		if _, ok := p.Scroll(); ok {
			told++
		}
	}, 400, 300)
	if err := tt.Click("Jump"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if told != 1 {
		t.Errorf("Scroll told of the part %d times, want once", told)
	}

	o.Anchor = "#target"
	tt.Frame()
	src, o.Anchor = "", ""
	tt.Frame()
	if _, ok := p.Scroll(); ok || p.st.scrollTo != -1 {
		t.Errorf("the empty document still scrolls to block %d", p.st.scrollTo)
	}
}

// Headings start parts in text whose lines end with a carriage return.
func TestPartsBreakAtHeadingsAfterCR(t *testing.T) {
	src := "Intro.\r\r# A\r\rBody.\r"
	var p Parts
	ui.NewTester(func(c *ui.Context) { p.Update(c, src, nil, false) }, 400, 300)
	if got := partKeys(&p); !slices.Equal(got, []int{0, 8}) {
		t.Errorf("parts start at %v, want [0 8]", got)
	}
}
