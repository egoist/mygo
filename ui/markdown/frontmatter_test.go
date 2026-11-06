package markdown

import (
	"image"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown/parser"
	"github.com/egoist/mygo/ui/richtext"
)

const (
	fmBody = "# Title\n\nSome body text.\n\n- one\n- two\n"
	fmMeta = "title: Secret\ntags: [a, b]\n"
	fmDoc  = "---\n" + fmMeta + "---\n" + fmBody
)

func fmOptions(hide bool) *Options {
	return &Options{Extensions: parser.Default | parser.ExtFrontmatter, HideFrontmatter: hide}
}

// shows reports whether some text shown contains s.
func shows(tt *ui.Tester, s string) bool {
	return slices.ContainsFunc(tt.Texts(), func(t string) bool { return strings.Contains(t, s) })
}

func TestViewFrontmatterCollapsed(t *testing.T) {
	o := fmOptions(false)
	tt := ui.NewTester(func(c *ui.Context) { View(c, fmDoc, o) }, 600, 800)
	for _, s := range []string{"Front matter", "Title", "Some body text."} {
		if !shows(tt, s) {
			t.Errorf("no %q in %q", s, tt.Texts())
		}
	}
	for _, s := range []string{"Secret", "tags", "---", "Copy", "yaml"} {
		if shows(tt, s) {
			t.Errorf("shows %q in %q", s, tt.Texts())
		}
	}
	// The header opens and closes it.
	toggleFrontmatter(t, tt)
	tt.Advance(time.Second) // it opens in a moment
	if !shows(tt, "title: Secret") || !shows(tt, "tags: [a, b]") {
		t.Errorf("no metadata once opened, in %q", tt.Texts())
	}
	toggleFrontmatter(t, tt)
	tt.Advance(time.Second)
	if shows(tt, "Secret") {
		t.Errorf("shows the metadata once closed, in %q", tt.Texts())
	}
}

func TestViewFrontmatterHidden(t *testing.T) {
	o := fmOptions(true)
	tt := ui.NewTester(func(c *ui.Context) { View(c, fmDoc, o) }, 600, 800)
	body := ui.NewTester(func(c *ui.Context) { View(c, fmBody, o) }, 600, 800)
	for _, s := range []string{"Secret", "Front matter", "yaml", "---"} {
		if shows(tt, s) {
			t.Errorf("shows %q in %q", s, tt.Texts())
		}
	}
	if got, want := tt.Texts(), body.Texts(); !slices.Equal(got, want) {
		t.Errorf("shows\n%q\nwant\n%q", got, want)
	}
	// It leaves no room behind.
	a, _ := tt.Find("Title")
	b, _ := body.Find("Title")
	if a.Y != b.Y {
		t.Errorf("the title is at %v with front matter, %v without", a.Y, b.Y)
	}
}

func TestViewFrontmatterOff(t *testing.T) {
	// Without the extension, it reads as CommonMark does.
	tt := ui.NewTester(func(c *ui.Context) { View(c, fmDoc, nil) }, 600, 800)
	if !shows(tt, "title: Secret") || shows(tt, "Front matter") {
		t.Errorf("shows %q", tt.Texts())
	}
}

func TestViewFrontmatterTOML(t *testing.T) {
	src := "+++\ntitle = \"x\"\n+++\ntext\n"
	tt := ui.NewTester(func(c *ui.Context) { View(c, src, fmOptions(false)) }, 600, 800)
	if !shows(tt, "Front matter") || shows(tt, "toml") || shows(tt, `title = "x"`) {
		t.Errorf("shows %q", tt.Texts())
	}
	toggleFrontmatter(t, tt)
	tt.Advance(time.Second)
	if !shows(tt, `title = "x"`) {
		t.Errorf("no metadata once opened, in %q", tt.Texts())
	}
}

func TestViewFrontmatterBlock(t *testing.T) {
	o := fmOptions(false)
	var formats, lines []string
	o.Block = func(c *ui.Context, d *parser.Document, i int) bool {
		b := d.Block(i)
		if b.Kind != parser.Frontmatter {
			return false
		}
		formats, lines = append(formats, b.Info()), append(lines, d.Lines(i))
		ui.Text(c, "meta: "+strings.ReplaceAll(strings.TrimSpace(d.Lines(i)), "\n", "; "))
		return true
	}
	tt := ui.NewTester(func(c *ui.Context) { View(c, fmDoc, o) }, 600, 800)
	if !shows(tt, "meta: title: Secret; tags: [a, b]") || !shows(tt, "Title") || !shows(tt, "Some body text.") {
		t.Errorf("shows %q", tt.Texts())
	}
	if shows(tt, "Front matter") {
		t.Errorf("the default block shows too, in %q", tt.Texts())
	}
	if len(formats) == 0 || formats[0] != "yaml" || lines[0] != fmMeta {
		t.Errorf("Block got %q, %q", formats, lines)
	}

	// A Block that leaves it to the view gets the style's block, or none
	// and no room where it is hidden.
	o.Block = func(*ui.Context, *parser.Document, int) bool { return false }
	tt = ui.NewTester(func(c *ui.Context) { View(c, fmDoc, o) }, 600, 800)
	if !shows(tt, "Front matter") {
		t.Errorf("shows %q", tt.Texts())
	}
	o.HideFrontmatter = true
	tt = ui.NewTester(func(c *ui.Context) { View(c, fmDoc, o) }, 600, 800)
	body := ui.NewTester(func(c *ui.Context) { View(c, fmBody, o) }, 600, 800)
	if shows(tt, "Front matter") || shows(tt, "Secret") {
		t.Errorf("shows %q", tt.Texts())
	}
	a, _ := tt.Find("Title")
	b, _ := body.Find("Title")
	if a.Y != b.Y {
		t.Errorf("the title is at %v with front matter, %v without", a.Y, b.Y)
	}
}

// TestStreamFrontmatter streams a document a character at a time, as
// Stream shows it, and checks it shows as View shows it whole.
func TestStreamFrontmatter(t *testing.T) {
	o := fmOptions(false)
	o.Reveal = &Reveal{Unit: AtOnce}
	var text string
	done := false
	tt := ui.NewTester(func(c *ui.Context) {
		if done {
			View(c, text, o)
		} else {
			Stream(c, text, o)
		}
	}, 600, 800)
	closed := len("---\n" + fmMeta + "---\n")
	for i := 0; i < len(fmDoc); {
		_, n := utf8.DecodeRuneInString(fmDoc[i:])
		i += n
		text = fmDoc[:i]
		tt.Frame()
		// Once the closing fence has come, the metadata shows as such, not
		// as a thematic break and a heading.
		if i >= closed && !shows(tt, "Front matter") {
			t.Fatalf("after %q: shows %q", text, tt.Texts())
		}
		if i < closed && shows(tt, "Front matter") {
			t.Fatalf("after %q: shows front matter before its closing fence, %q", text, tt.Texts())
		}
	}
	done = true
	tt.Frame()
	whole := ui.NewTester(func(c *ui.Context) { View(c, fmDoc, o) }, 600, 800)
	if got, want := tt.Texts(), whole.Texts(); !slices.Equal(got, want) {
		t.Errorf("streamed shows\n%q\nwhole shows\n%q", got, want)
	}
}

// TestViewFrontmatterCopy checks that Copy as Markdown, with nothing
// selected, copies the whole document, front matter included.
func TestViewFrontmatterCopy(t *testing.T) {
	tt := ui.NewTester(func(c *ui.Context) { View(c, fmDoc, fmOptions(false)) }, 600, 800)
	r, _ := tt.Find("Some body text.")
	tt.ClickAt(r.X+2, r.Y+r.H/2)
	tt.Key(ui.Cmd|ui.Shift, ui.KeyC)
	if got := tt.Clipboard(); got != fmDoc {
		t.Errorf("copied as Markdown %q, want %q", got, fmDoc)
	}
}

// toggleFrontmatter clicks the header of the block of front matter: the box
// is the first element named Front matter, and its top is the header.
func toggleFrontmatter(t *testing.T, tt *ui.Tester) {
	t.Helper()
	r, ok := tt.Find("Front matter")
	if !ok {
		t.Fatalf("no front matter in %q", tt.Texts())
	}
	tt.ClickAt(r.X+r.W/2, r.Y+12)
}

// copied returns the representations of what the tester copied last.
func copied(t *testing.T, tt *ui.Tester) (text, html, md string) {
	t.Helper()
	items := tt.ClipboardData().Items()
	if len(items) == 0 {
		t.Fatal("nothing copied")
	}
	read := func(f transfer.Format) string {
		b, err := items[0].Read(f)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	return read(transfer.Text), read(transfer.HTML), read(transfer.Markdown)
}

// selectLines drags the pointer over lines [from, to) of the code shown as
// the text of r, which has n lines, to its end.
func selectLines(tt *ui.Tester, r ui.Rect, n, from, to int) {
	lh := r.H / float32(n)
	x0, y0 := r.X+1, r.Y+float32(from)*lh+lh/2
	x1, y1 := r.X+r.W-1, r.Y+float32(to)*lh-lh/2
	tt.Press(x0, y0)
	tt.Move(x1, y1)
	tt.Release(x1, y1)
}

// TestViewFrontmatterCopyMetadata checks that the metadata selected and
// copied pastes as what it is: its lines, in every representation, as a
// code block's lines selected in part do, not as front matter that the
// HTML of a selection leaves out.
func TestViewFrontmatterCopyMetadata(t *testing.T) {
	tt := ui.NewTester(func(c *ui.Context) { View(c, fmDoc, fmOptions(false)) }, 600, 400)
	toggleFrontmatter(t, tt)
	tt.Advance(time.Second)
	r, _ := tt.Find("title: Secret\ntags: [a, b]")
	selectLines(tt, r, 2, 0, 2)
	tt.Command("copy")
	text, html, md := copied(t, tt)
	for name, got := range map[string]string{"text": text, "Markdown": md} {
		if got != "title: Secret\ntags: [a, b]" {
			t.Errorf("%s copied %q", name, got)
		}
	}
	if want := `<pre><code class="language-yaml">title: Secret` + "\n" + `tags: [a, b]` + "\n" + `</code></pre>`; !strings.Contains(html, want) {
		t.Errorf("HTML copied %q, want %q", html, want)
	}
}

// TestViewFrontmatterCopyLiteral checks that metadata copied alone pastes
// into a rich text editor as it is written: its lines are not Markdown,
// whose HTML would turn a comment into a heading, a dash into a list item
// and a tag into an element.
func TestViewFrontmatterCopyLiteral(t *testing.T) {
	for _, meta := range []string{
		"title: \"<!-- keep me -->\"",
		"# a note\ntags:\n  - a\n  - b",
		"name: \"<b>bold</b> & *more*\"",
	} {
		src := "---\n" + meta + "\n---\nbody\n"
		tt := ui.NewTester(func(c *ui.Context) { View(c, src, fmOptions(false)) }, 600, 400)
		toggleFrontmatter(t, tt)
		tt.Advance(time.Second)
		r, ok := tt.Find(meta)
		if !ok {
			t.Fatalf("no %q in %q", meta, tt.Texts())
		}
		selectLines(tt, r, strings.Count(meta, "\n")+1, 0, strings.Count(meta, "\n")+1)
		tt.Command("copy")
		text, html, _ := copied(t, tt)
		if text != meta {
			t.Errorf("%q: copied text %q", meta, text)
		}
		if pasted := richtext.ParseHTML(html).String(); pasted != meta {
			t.Errorf("%q: pastes as %q, from HTML %q", meta, pasted, html)
		}
	}
}

// TestViewFrontmatterCopyWithBody checks that a selection of the metadata
// and what follows it is still the HTML of its Markdown.
func TestViewFrontmatterCopyWithBody(t *testing.T) {
	tt := ui.NewTester(func(c *ui.Context) { View(c, fmDoc, fmOptions(false)) }, 600, 400)
	toggleFrontmatter(t, tt)
	tt.Advance(time.Second)
	r, _ := tt.Find("Some body text.")
	tt.ClickAt(r.X+2, r.Y+r.H/2)
	tt.Command("selectAll")
	tt.Command("copy")
	_, html, _ := copied(t, tt)
	if !strings.Contains(html, "<h1>Title</h1>") || !strings.Contains(html, "<li>one</li>") {
		t.Errorf("HTML copied %q: the body is not Markdown's", html)
	}
}

// TestViewCopyCodeOfRules checks that code selected in part, whose lines
// are fences of front matter, copies its HTML too: a selection is not a
// document, whose start the parser would read as front matter.
func TestViewCopyCodeOfRules(t *testing.T) {
	src := "```yaml\n---\na: b\n---\nc: d\n```\n"
	tt := ui.NewTester(func(c *ui.Context) { View(c, src, fmOptions(false)) }, 600, 400)
	r, _ := tt.Find("---\na: b\n---\nc: d")
	selectLines(tt, r, 4, 0, 3)
	tt.Command("copy")
	text, html, md := copied(t, tt)
	if text != "---\na: b\n---" || md != "---\na: b\n---" {
		t.Errorf("copied %q, as Markdown %q", text, md)
	}
	if !strings.Contains(html, "a: b") {
		t.Errorf("HTML copied %q: the code is gone", html)
	}
}

// arrowPointsRight reports whether the arrow drawn in the left or right
// 30 DIPs of the first 40 rows of img points right: its ends, at the top
// of its ink, lie to the left of its tip.
func arrowPointsRight(t *testing.T, img *image.RGBA, right bool) bool {
	t.Helper()
	x0, x1 := 8, 34
	if right {
		x0, x1 = img.Bounds().Dx()-36, img.Bounds().Dx()-8
	}
	box := image.Rectangle{Min: image.Pt(x1, 40), Max: image.Pt(x0, 0)}
	for y := 8; y < 40; y++ {
		for x := x0; x < x1; x++ {
			c := img.RGBAAt(x, y)
			if c.R < 200 && c.R > 90 && int(c.B)-int(c.R) < 30 {
				box.Min.X, box.Min.Y = min(box.Min.X, x), min(box.Min.Y, y)
				box.Max.X, box.Max.Y = max(box.Max.X, x+1), max(box.Max.Y, y+1)
			}
		}
	}
	if box.Empty() {
		t.Fatal("no arrow found")
	}
	// The ink of the top rows, against the middle of the arrow.
	sum, n := 0, 0
	for y := box.Min.Y; y < box.Min.Y+2; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			if c := img.RGBAAt(x, y); c.R < 200 {
				sum, n = sum+x, n+1
			}
		}
	}
	return n > 0 && float64(sum)/float64(n) < float64(box.Min.X+box.Max.X)/2
}

// TestViewFrontmatterArrowRTL checks that the arrow of a collapsed block
// points the way of the line where it is: right in a left-to-right
// container, and left in a right-to-left one in a window that is not.
func TestViewFrontmatterArrowRTL(t *testing.T) {
	shot := func(rtl bool) *image.RGBA {
		tt := ui.NewTester(func(c *ui.Context) {
			col := ui.Column(c).Fill().Padding(8)
			if rtl {
				col.Direction(ui.RightToLeft)
			}
			col.Children(func() { View(c, fmDoc, fmOptions(false)) })
		}, 300, 120)
		return tt.Image()
	}
	if !arrowPointsRight(t, shot(false), false) {
		t.Error("the arrow does not point right in a left-to-right block")
	}
	if arrowPointsRight(t, shot(true), true) {
		t.Error("the arrow points right in a right-to-left block")
	}
}
