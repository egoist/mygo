package parser

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
)

type frontMatterBlock struct {
	kind  Kind
	start int
	end   int
}

func frontMatterBlocks(d *Document) []frontMatterBlock {
	var blocks []frontMatterBlock
	for i := 1; i < d.Len(); i++ {
		b := d.Block(i)
		blocks = append(blocks, frontMatterBlock{b.Kind, b.Start, b.End})
	}
	return blocks
}

func (b frontMatterBlock) String() string {
	return fmt.Sprintf("%v [%d,%d)", b.kind, b.start, b.end)
}

func frontMatterEqual(got, want []frontMatterBlock) string {
	if len(got) != len(want) {
		return fmt.Sprintf("blocks %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			return fmt.Sprintf("block %d is %v, want %v", i+1, got[i], want[i])
		}
	}
	return ""
}

func TestFrontMatter(t *testing.T) {
	for _, tt := range []struct {
		name string
		src  string
		want []frontMatterBlock
	}{
		{
			name: "closed by dashes",
			src:  "---\ntitle: Example\n---\n\n# Heading\n",
			want: []frontMatterBlock{
				{FrontMatter, 0, 22},
				{Heading, 24, 33},
			},
		},
		{
			name: "closed by dots",
			src:  "---\ntitle: Example\n...\n\n# Heading\n",
			want: []frontMatterBlock{
				{FrontMatter, 0, 22},
				{Heading, 24, 33},
			},
		},
		{
			name: "CRLF",
			src:  "---\r\ntitle: Example\r\n---\r\n\r\n# Heading\r\n",
			want: []frontMatterBlock{
				{FrontMatter, 0, 24},
				{Heading, 28, 37},
			},
		},
		{
			name: "empty is not front matter",
			src:  "---\n---\n\n# Heading\n",
			want: []frontMatterBlock{
				{ThematicBreak, 0, 3},
				{ThematicBreak, 4, 7},
				{Heading, 9, 18},
			},
		},
		{
			name: "unclosed is a thematic break",
			src:  "---\ntitle: Example\n\n# Heading\n",
			want: []frontMatterBlock{
				{ThematicBreak, 0, 3},
				{Paragraph, 4, 18},
				{Heading, 20, 29},
			},
		},
		{
			name: "setext heading is not front matter",
			src:  "Heading\n---\n",
			want: []frontMatterBlock{
				{Heading, 0, 7},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d := New(ExtFrontMatter)
			d.Set(tt.src)
			got := frontMatterBlocks(d)
			if msg := frontMatterEqual(got, tt.want); msg != "" {
				t.Fatalf("%q: %s", tt.src, msg)
			}
			if d.Source() != tt.src {
				t.Fatalf("source %q, want %q", d.Source(), tt.src)
			}
		})
	}
}

func TestFrontMatterDisabledByDefault(t *testing.T) {
	const src = "---\ntitle: Example\n---\n\n# Heading\n"
	for _, ext := range []Extensions{Default, GFM} {
		d := New(ext)
		d.Set(src)
		got := frontMatterBlocks(d)
		want := []frontMatterBlock{
			{ThematicBreak, 0, 3},
			{Heading, 4, 18},
			{Heading, 24, 33},
		}
		if msg := frontMatterEqual(got, want); msg != "" {
			t.Errorf("extensions %v: %s", ext, msg)
		}
	}
}

func TestFrontMatterHTML(t *testing.T) {
	d := New(ExtFrontMatter)
	d.Set("---\ntitle: <Example>\n---\n\n# Heading\n")
	if got, want := d.HTML(), "<h1>Heading</h1>\n"; got != want {
		t.Fatalf("HTML %q, want %q", got, want)
	}
}

func TestFrontMatterAppendMatchesWholeParse(t *testing.T) {
	for _, src := range []string{
		"---\r\ntitle: Example\r\n---\r\n\r\n# Heading\r\n",
		"---\ntitle: Example\n...\n\n# Heading\n",
	} {
		d := New(ExtFrontMatter)
		for i := range src {
			d.Append(src[i : i+1])
		}
		want := New(ExtFrontMatter)
		want.Set(src)
		if msg := frontMatterEqual(frontMatterBlocks(d), frontMatterBlocks(want)); msg != "" {
			t.Errorf("append %q: %s", src, msg)
		}
		if got, expected := d.HTML(), want.HTML(); got != expected {
			t.Errorf("append %q: HTML %q, want %q", src, got, expected)
		}
	}
}

func TestFrontMatterEditMatchesWholeParse(t *testing.T) {
	const body = "\n# Heading\n\nParagraph.\n"
	for _, tt := range []struct {
		name string
		prev string
		edit func(*Document)
		want string
	}{
		{
			name: "open",
			prev: "# Heading\n\nParagraph.\n",
			edit: func(d *Document) { d.Edit(0, 0, "---\r\ntitle: Example\r\n---\r\n") },
			want: "---\r\ntitle: Example\r\n---\r\n" + "# Heading\n\nParagraph.\n",
		},
		{
			name: "edit opening marker",
			prev: "---\ntitle: Example\n---\n" + body,
			edit: func(d *Document) { d.Edit(1, 1, "-") },
			want: "----\ntitle: Example\n---\n" + body,
		},
		{
			name: "edit content",
			prev: "---\ntitle: Example\n---\n" + body,
			edit: func(d *Document) { d.Edit(11, 18, "Changed") },
			want: "---\ntitle: Changed\n---\n" + body,
		},
		{
			name: "edit closing marker",
			prev: "---\ntitle: Example\n---\n" + body,
			edit: func(d *Document) { d.Edit(19, 19, "x") },
			want: "---\ntitle: Example\nx---\n" + body,
		},
		{
			name: "close",
			prev: "---\ntitle: Example\n" + body,
			edit: func(d *Document) { d.Edit(19, 19, "...\n") },
			want: "---\ntitle: Example\n...\n" + body,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d := New(ExtFrontMatter)
			d.Set(tt.prev)
			d.HTML()
			tt.edit(d)
			if d.Source() != tt.want {
				t.Fatalf("source %q, want %q", d.Source(), tt.want)
			}
			want := New(ExtFrontMatter)
			want.Set(tt.want)
			gotBlocks, wantBlocks := frontMatterBlocks(d), frontMatterBlocks(want)
			if msg := frontMatterEqual(gotBlocks, wantBlocks); msg != "" {
				t.Errorf("edit: %s", msg)
			}
			if got, expected := d.HTML(), want.HTML(); got != expected {
				t.Errorf("HTML %q, want %q", got, expected)
			}
		})
	}
}

func TestFrontMatterAppendKeepsBlocks(t *testing.T) {
	d := New(ExtFrontMatter)
	d.Set("---\ntitle: Example\n---\n\n# Heading\n\nBody.\n")
	if d.Len() < 4 || d.Block(1).Kind != FrontMatter {
		t.Fatalf("unexpected initial blocks: %+v", frontMatterBlocks(d))
	}
	inlinesAll(d)
	fmRev := d.Block(1).Revision()
	hRev := d.Block(2).Revision()

	d.Append("\nMore.\n")
	if d.Block(1).Kind != FrontMatter {
		t.Fatalf("block 1 = %v, want FrontMatter", d.Block(1).Kind)
	}
	if d.Block(1).Revision() != fmRev {
		t.Fatalf("FrontMatter block was reparsed on Append: rev %d, want %d", d.Block(1).Revision(), fmRev)
	}
	if d.Block(2).Revision() != hRev {
		t.Fatalf("Heading block was reparsed on Append: rev %d, want %d", d.Block(2).Revision(), hRev)
	}
}

func TestFrontMatterStreamingLinear(t *testing.T) {
	streamSteps := func(n int) (int, int) {
		src := "---\n" + strings.Repeat("key: value\n\n", n)
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		d := New(ExtFrontMatter)
		d.SetStreaming(true)
		const chunk = 64
		for i := 0; i < len(src); i += chunk {
			d.Append(src[i:min(i+chunk, len(src))])
		}
		runtime.ReadMemStats(&after)
		return d.steps, int(after.TotalAlloc - before.TotalAlloc)
	}
	const n = 2000
	smallSteps, smallBytes := streamSteps(n)
	largeSteps, largeBytes := streamSteps(4 * n)
	if largeSteps > smallSteps*6 {
		t.Errorf("unclosed front matter streaming steps grew superlinearly: %d for %d lines, %d for %d lines", smallSteps, n, largeSteps, 4*n)
	}
	if largeBytes > smallBytes*6 {
		t.Errorf("unclosed front matter streaming alloc grew superlinearly: %d B for %d lines, %d B for %d lines", smallBytes, n, largeBytes, 4*n)
	}
}

func TestFrontMatterStreamingFalseClose(t *testing.T) {
	for _, src := range []string{
		"---\r\ntitle: Example\r\n---x\r\n# Heading\r\n",
		"---\ntitle: Example\n---\n\n# Heading\n",
	} {
		d := New(ExtFrontMatter)
		d.SetStreaming(true)
		for i := range src {
			d.Append(src[i : i+1])
		}
		want := New(ExtFrontMatter)
		want.Set(src)
		if msg := frontMatterEqual(frontMatterBlocks(d), frontMatterBlocks(want)); msg != "" {
			t.Errorf("streaming %q: %s", src, msg)
		}
	}
}

func TestFrontMatterEditConstantWork(t *testing.T) {
	editSteps := func(n int) int {
		src := "---\n" + strings.Repeat("key: value\n\n", n)
		d := New(ExtFrontMatter)
		d.Set(src)
		d.steps = 0
		at := len(src) - len("value\n\n")
		d.Edit(at, at+len("value"), "other")
		if d.frontMatter != -1 {
			t.Fatalf("frontMatter = %d, want -1", d.frontMatter)
		}
		return d.steps
	}
	small := editSteps(2000)
	large := editSteps(8000)
	if large > small+64 {
		t.Errorf("edit steps grew with document size: %d for 2000 blocks, %d for 8000 blocks", small, large)
	}
}
