package parser

import (
	"math/rand/v2"
	"os"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

const fmExt = Default | ExtFrontmatter

func TestFrontmatterNotDefault(t *testing.T) {
	if Default&ExtFrontmatter != 0 || GFM&ExtFrontmatter != 0 {
		t.Fatal("front matter is read by default")
	}
}

func TestFrontmatter(t *testing.T) {
	for _, c := range []struct {
		name, src string
		format    string // "" for no front matter
		raw       string // the source of the block, fences included
		lines     string
		body      string // the HTML of what follows
	}{
		{"yaml", "---\ntitle: x\n---\n# H\n", "yaml", "---\ntitle: x\n---", "title: x\n", "<h1>H</h1>\n"},
		{"toml", "+++\ntitle = \"x\"\n+++\ntext\n", "toml", "+++\ntitle = \"x\"\n+++", "title = \"x\"\n", "<p>text</p>\n"},
		{"empty", "---\n---\ntext", "yaml", "---\n---", "", "<p>text</p>\n"},
		{"several lines", "---\na: 1\nb:\n  - c\n---\ntext", "yaml", "---\na: 1\nb:\n  - c\n---", "a: 1\nb:\n  - c\n", "<p>text</p>\n"},
		{"markdown inside", "---\n# not a heading\n\n- not a list\n---\n# H", "yaml", "---\n# not a heading\n\n- not a list\n---", "# not a heading\n\n- not a list\n", "<h1>H</h1>\n"},
		{"blank line after", "---\na: b\n---\n\ntext", "yaml", "---\na: b\n---", "a: b\n", "<p>text</p>\n"},
		{"nothing after", "---\na: b\n---\n", "yaml", "---\na: b\n---", "a: b\n", ""},
		{"closing fence ends the source", "---\na: b\n---", "yaml", "---\na: b\n---", "a: b\n", ""},
		{"spaces after fences", "--- \t\na: b\n---  \ntext", "yaml", "--- \t\na: b\n---  ", "a: b\n", "<p>text</p>\n"},
		{"CRLF", "---\r\na: b\r\n---\r\n\r\ntext", "yaml", "---\r\na: b\r\n---", "a: b\n", "<p>text</p>\n"},
		{"CR", "---\ra: b\r---\rtext", "yaml", "---\ra: b\r---", "a: b\n", "<p>text</p>\n"},
		{"byte order mark", "\xef\xbb\xbf---\na: b\n---\ntext", "yaml", "---\na: b\n---", "a: b\n", "<p>text</p>\n"},
		{"the first closing fence closes", "---\na\n---\nb\n---\n", "yaml", "---\na\n---", "a\n", "<h2>b</h2>\n"},
		{"a thematic break after", "---\na: b\n---\ntext\n\n---\n", "yaml", "---\na: b\n---", "a: b\n", "<p>text</p>\n<hr />\n"},
		{"the other fence is content", "---\na\n+++\nb\n---\ntext", "yaml", "---\na\n+++\nb\n---", "a\n+++\nb\n", "<p>text</p>\n"},

		{"no closing fence", "---\na: b\n", "", "", "", ""},
		{"no closing fence, a table-like line", "---\na: b\n- c\n", "", "", "", ""},
		{"only an opening fence", "---\n", "", "", "", ""},
		{"toml fence closed by yaml's", "+++\na\n---\n", "", "", "", ""},
		{"yaml fence closed by toml's", "---\na\n+++\n", "", "", "", ""},
		{"indented opening fence", " ---\na: b\n---\n", "", "", "", ""},
		{"indented closing fence", "---\na: b\n ---\ntext", "", "", "", ""},
		{"longer closing fence", "---\na: b\n----\ntext", "", "", "", ""},
		{"longer opening fence", "----\na: b\n----\n", "", "", "", ""},
		{"text on the opening line", "--- x\na: b\n---\n", "", "", "", ""},
		{"not the first line", "\n---\na: b\n---\n", "", "", "", ""},
		{"after text", "text\n---\na: b\n---\n", "", "", "", ""},
		{"two dashes", "--\na\n--\n", "", "", "", ""},
		{"a fence line that is too long", "---" + strings.Repeat(" ", maxFenceLine) + "\na: b\n---\n", "", "", "", ""},
	} {
		d := New(fmExt)
		d.Set(c.src)
		if c.format == "" {
			// Read as without the extension.
			if got, want := d.HTML(), render(c.src, Default); got != want {
				t.Errorf("%s: %q:\ngot  %q\nwant %q", c.name, c.src, got, want)
			}
			if d.Len() > 1 && d.Block(1).Kind == Frontmatter {
				t.Errorf("%s: %q is front matter", c.name, c.src)
			}
			continue
		}
		if d.Len() < 2 || d.Block(1).Kind != Frontmatter {
			t.Errorf("%s: %q: no front matter, blocks:\n%s", c.name, c.src, zzDump(d))
			continue
		}
		b := d.Block(1)
		if got := b.Info(); got != c.format {
			t.Errorf("%s: format %q, want %q", c.name, got, c.format)
		}
		if got := b.Language(); got != c.format {
			t.Errorf("%s: language %q, want %q", c.name, got, c.format)
		}
		if got := d.Lines(1); got != c.lines {
			t.Errorf("%s: lines %q, want %q", c.name, got, c.lines)
		}
		if got := d.Source()[b.Start:b.End]; got != c.raw {
			t.Errorf("%s: source %q, want %q", c.name, got, c.raw)
		}
		if b.Parent != 0 || b.Next != 2 || b.Open {
			t.Errorf("%s: block %+v", c.name, *b)
		}
		if got := d.HTML(); got != c.body {
			t.Errorf("%s: HTML %q, want %q", c.name, got, c.body)
		}
		checkInvariants(t, d, c.name)
		// Without the extension, the same lines read as CommonMark.
		if c.name == "yaml" {
			if got := render(c.src, Default); !strings.Contains(got, "<hr />") {
				t.Errorf("%s: without the extension %q", c.name, got)
			}
		}
	}
}

// TestFrontmatterBlocks checks the blocks after front matter, which parse
// as those of a document without it.
func TestFrontmatterBlocks(t *testing.T) {
	const body = "# H\n\n[a]: /u\n\n[a] *b*\n\n- x\n- y\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n```go\ncode\n```\n\nSetext\n---\n"
	with := New(fmExt)
	with.Set("---\na: b\n---\n" + body)
	without := New(Default)
	without.Set(body)
	if got, want := with.HTML(), without.HTML(); got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	if with.Len() != without.Len()+1 {
		t.Fatalf("%d blocks, want %d", with.Len(), without.Len()+1)
	}
	off := len("---\na: b\n---\n")
	for i := 1; i < without.Len(); i++ {
		a, b := without.Block(i), with.Block(i+1)
		if a.Kind != b.Kind || a.Start+off != b.Start || a.End+off != b.End {
			t.Errorf("block %d: %v [%d,%d), with front matter %v [%d,%d)", i, a.Kind, a.Start, a.End, b.Kind, b.Start, b.End)
		}
	}
	checkInvariants(t, with, "blocks")
}

// fmSame checks that d, parsed by appends and edits, has the blocks of the
// source parsed whole.
func fmSame(t *testing.T, d *Document, stream bool, ctx string) {
	t.Helper()
	checkInvariants(t, d, ctx)
	whole := New(d.Extensions())
	whole.SetStreaming(stream)
	whole.Set(d.Source())
	if got, want := zzDump(d), zzDump(whole); got != want {
		t.Fatalf("%s: %q:\ngot\n%s\nwant\n%s", ctx, d.Source(), got, want)
	}
	if got, want := d.HTML(), whole.HTML(); got != want {
		t.Fatalf("%s: %q:\ngot  %q\nwant %q", ctx, d.Source(), got, want)
	}
}

var fmDocs = []string{
	"---\ntitle: x\n---\n# H\n",
	"+++\ntitle = 1\n+++\ntext\n",
	"---\n---\ntext",
	"---\r\na: b\r\n---\r\n\r\ntext\r\n",
	"---\ra: b\r---\rtext",
	"\xef\xbb\xbf---\na: b\n---\ntext",
	"---\na: b\n---",
	"---\na: b\n",
	"---\na: b\n- c\n\ntext\n---\n",
	"---\na\n+++\nb\n---\ntext\n\n---\n",
	"---\n# a\n\n- b\n---\n[a]: /u\n\n[a]\n",
	"----\na\n----\n",
}

// TestFrontmatterAppend appends documents a character at a time, as text
// streams in and is typed.
func TestFrontmatterAppend(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, src := range fmDocs {
			d := New(fmExt)
			d.SetStreaming(stream)
			for i := 0; i < len(src); {
				_, n := utf8.DecodeRuneInString(src[i:])
				d.Append(src[i : i+n])
				i += n
				fmSame(t, d, stream, "append")
			}
			if stream {
				d.SetStreaming(false)
				fmSame(t, d, false, "ended")
			}
		}
	}
}

// TestFrontmatterAppearsLater checks that the closing fence, the last
// thing to come, makes the lines before it front matter, and that
// streaming waits for the whole line.
func TestFrontmatterAppearsLater(t *testing.T) {
	d := New(fmExt)
	d.Append("---\ntitle: x\n")
	if d.Block(1).Kind == Frontmatter {
		t.Fatal("front matter without a closing fence")
	}
	d.Append("more: y\n---\n")
	if d.Block(1).Kind != Frontmatter || d.Lines(1) != "title: x\nmore: y\n" {
		t.Fatalf("no front matter:\n%s", zzDump(d))
	}
	if d.HTML() != "" {
		t.Fatalf("HTML %q", d.HTML())
	}

	s := New(fmExt)
	s.SetStreaming(true)
	s.Append("---\ntitle: x\n---")
	if s.Block(1).Kind == Frontmatter {
		t.Fatal("streaming shows the closing fence before its line ends")
	}
	s.Append("\n")
	if s.Block(1).Kind != Frontmatter {
		t.Fatalf("no front matter once the line ended:\n%s", zzDump(s))
	}
	s.SetStreaming(false)
	fmSame(t, s, false, "ended")

	// Text that makes the closing fence something else takes it back.
	u := New(fmExt)
	u.Append("---\na\n---")
	if u.Block(1).Kind != Frontmatter {
		t.Fatal("a closing fence at the end of the source is not one")
	}
	u.Append("-")
	if u.Block(1).Kind == Frontmatter {
		t.Fatalf("---- closes front matter:\n%s", zzDump(u))
	}
	fmSame(t, u, false, "longer fence")
}

func TestFrontmatterEdits(t *testing.T) {
	for _, states := range [][]string{
		{"---\na: b\n", "---\na: b\n---\n", "---\na: b\n---\ntext\n", "---\na: b\n--\ntext\n", "---\na: b\n---\ntext\n"},
		{"---\na: b\n---\ntext\n", "---\na: b\ntext\n", "--\na: b\n---\ntext\n", "---\na: b\n---\ntext\n"},
		{"---\na: b\n---\ntext\n", "x---\na: b\n---\ntext\n", "---\na: b\n---\ntext\n", "\n---\na: b\n---\ntext\n"},
		{"---\na: b\n---\ntext\n", "---\na: b\n---\ntext\n\nmore\n", "---\na: b\n---\ntext\n\n---\nmore\n---\n"},
		{"---\na\nb\nc\n---\n", "---\na\nb\n---\nc\n---\n", "---\n---\nb\n---\nc\n---\n"},
		{"---\ntext\n\nmore\n\nand\n", "---\ntext\n\nmore\n\nand\n---\n", "---\ntext\n\nmore\n\nand\n"},
		{"+++\na\n---\n", "+++\na\n+++\n", "---\na\n+++\n"},
		{"text\n---\na: b\n---\n", "---\na: b\n---\n", "text\n---\na: b\n---\n"},
	} {
		for _, stream := range []bool{false, true} {
			d := New(fmExt)
			for _, s := range states {
				d.SetStreaming(stream)
				d.Set(s)
				d.SetStreaming(false)
				fmSame(t, d, false, "edit")
			}
		}
	}
}

// TestFrontmatterRandom parses sources of fences and a few other lines
// again and again, by edits, appends and streaming, and checks each
// state against its source parsed whole.
func TestFrontmatterRandom(t *testing.T) {
	pieces := []string{"---\n", "---", "+++\n", "+++", "a: b\n", "a", "\n", "\r\n", "\r", " ", "\t", "# h\n", "- ", "-", "+", "```\n", "x\n\n", "[a]: /u\n", "[a]", "---  \n", "----\n"}
	r := rand.New(rand.NewPCG(7, 8))
	doc := func(n int) string {
		var sb strings.Builder
		for range n {
			sb.WriteString(pieces[r.IntN(len(pieces))])
		}
		return sb.String()
	}
	iterations := 3000
	if os.Getenv("ZZFUZZ") != "" {
		iterations = 300000
	}
	for iter := range iterations {
		cur := doc(1 + r.IntN(8))
		d := New(fmExt)
		d.Set(cur)
		for step := range 8 {
			// The change: from, to and text replace [from, to) of cur.
			from, to, text := len(cur), len(cur), ""
			switch r.IntN(3) {
			case 0: // an append
				text = doc(1 + r.IntN(2))
			case 1: // an edit
				from = r.IntN(len(cur) + 1)
				to = min(len(cur), from+r.IntN(6))
				text = pieces[r.IntN(len(pieces))]
			default: // another source
				from, to, text = 0, len(cur), doc(1+r.IntN(8))
			}
			next := cur[:from] + text + cur[to:]
			if !utf8.ValidString(next) {
				continue
			}
			stream := r.IntN(3) == 0
			d.SetStreaming(stream)
			d.Edit(from, to, text)
			if stream {
				d.HTML()
				d.SetStreaming(false)
			}
			cur = next
			fmSame(t, d, false, "iteration "+strconv.Itoa(iter)+", step "+strconv.Itoa(step))
		}
	}
}

func TestFrontmatterLinear(t *testing.T) {
	for _, c := range []struct {
		name string
		src  func(n int) string
	}{
		{"unclosed front matter", func(n int) string { return "---\n" + strings.Repeat("a: b\n", n) }},
		{"closed front matter", func(n int) string { return "---\n" + strings.Repeat("a: b\n", n) + "---\n" }},
		{"front matter, then text", func(n int) string { return "---\na\n---\n" + strings.Repeat("a\n\n", n) }},
		{"opening fences", func(n int) string { return "---\n" + strings.Repeat("--- x\n", n) }},
		{"lines of spaces", func(n int) string { return "---\n" + strings.Repeat(" \n", n) }},
		{"CR line endings", func(n int) string { return "---\r" + strings.Repeat("a\r", n) }},
		{"long fence lines", func(n int) string { return "---" + strings.Repeat(" ", n) + "\na\n" }},
	} {
		checkLinear(t, c.name, fmExt, c.src)
		// The search for the closing fence reads the source about once.
		for _, n := range []int{5000, 20000} {
			src := c.src(n)
			d := New(fmExt)
			d.Set(src)
			if d.fmSteps > 2*len(src) {
				t.Errorf("%s: %d bytes searched for closing fences in %d", c.name, d.fmSteps, len(src))
			}
		}
	}
}

// TestFrontmatterAppendLinear checks that appending to a source that
// opens with a fence nothing closes searches only the text appended, and
// that so does editing its end.
func TestFrontmatterAppendLinear(t *testing.T) {
	for _, edit := range []bool{false, true} {
		for _, unit := range []string{"some text\n\n", "some text\r\n\r\n", "some text\r\r"} {
			d := New(fmExt)
			d.Append("---\n")
			n := 2000
			for range n {
				if edit {
					d.Edit(len(d.Source()), len(d.Source()), unit)
					d.Edit(len(d.Source())-1, len(d.Source()), "")
					d.Edit(len(d.Source()), len(d.Source()), unit[len(unit)-1:])
				} else {
					d.Append(unit)
				}
			}
			if d.Block(1).Kind == Frontmatter {
				t.Fatal("front matter without a closing fence")
			}
			if limit := 3 * len(d.Source()); d.fmSteps > limit {
				t.Errorf("edit=%v %q: %d bytes searched for closing fences in %d", edit, unit, d.fmSteps, len(d.Source()))
			}
		}
	}
}
