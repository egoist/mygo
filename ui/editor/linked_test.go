package editor

import (
	"regexp"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// tagNames is the word pattern of HTML's language server: anything but
// spaces, angle brackets and slashes.
var tagNames = regexp.MustCompile(`[^\s>/<]+`)

// linkTags answers as an HTML language server: the name of the tag at
// the offset and every tag's of that name, opening or closing.
func linkTags(text string, offset int) []Range {
	start, end := offset, offset
	for start > 0 && tagNames.MatchString(text[start-1:start]) {
		start--
	}
	for end < len(text) && tagNames.MatchString(text[end:end+1]) {
		end++
	}
	name := text[start:end]
	if name == "" {
		return nil
	}
	var ranges []Range
	for _, open := range []string{"<", "</"} {
		for i := 0; ; {
			j := strings.Index(text[i:], open+name)
			if j < 0 {
				break
			}
			at := i + j + len(open)
			if next := at + len(name); next == len(text) || !tagNames.MatchString(text[next:next+1]) {
				ranges = append(ranges, Range{at, at + len(name)})
			}
			i = at
		}
	}
	return ranges
}

// newLinkedTest is an editor whose OnLinkedEditing answers at once, as
// linkTags does with pattern, counting the requests.
func newLinkedTest(t *testing.T, text string, pattern *regexp.Regexp) (*editorTest, *int) {
	ct := newEditorTest(t, "", text)
	asked := new(int)
	ct.st.OnLinkedEditing = func(r LinkedEditingRequest) {
		*asked++
		ct.st.SetLinkedEditingRanges(r.Version, linkTags(ct.st.Buffer.String(), r.Offset), pattern)
	}
	ct.tt.Frame()
	return ct, asked
}

func TestLinkedEditingRenamesTheClosingTag(t *testing.T) {
	ct, _ := newLinkedTest(t, "<d|iv>\n</div>", tagNames)
	// Typing and deleting by turns, each a step of undo of its own.
	ct.typ("x")
	ct.want("<dx|iv>\n</dxiv>")
	ct.key(0, ui.KeyBackspace)
	ct.want("<d|iv>\n</div>")
	ct.typ("s")
	ct.key(0, ui.KeyDelete)
	ct.want("<ds|v>\n</dsv>")
	// Each undo takes an edit back with its copy.
	ct.run(Undo)
	ct.want("<ds|iv>\n</dsiv>")
	ct.run(Undo)
	ct.want("<d|iv>\n</div>")
	ct.run(Undo)
	ct.want("<dx|iv>\n</dxiv>")
	ct.run(Undo)
	ct.want("<d|iv>\n</div>")
	ct.run(Redo)
	ct.want("<dx|iv>\n</dxiv>")
	// The session goes on after undo and redo.
	ct.typ("y")
	ct.want("<dxy|iv>\n</dxyiv>")
}

func TestLinkedEditingFromTheClosingTag(t *testing.T) {
	ct, _ := newLinkedTest(t, "<p><b>a</b></p|>", tagNames)
	ct.key(ui.Shift, ui.KeyLeft)
	ct.typ("ul")
	ct.want("<ul><b>a</b></ul|>")
}

func TestLinkedEditingEndsWhenTheCaretLeaves(t *testing.T) {
	ct, asked := newLinkedTest(t, "<div|></div>", tagNames)
	if *asked != 1 || len(ct.st.LinkedEditingRanges()) != 2 {
		t.Fatalf("asked %d times, ranges %v", *asked, ct.st.LinkedEditingRanges())
	}
	ct.key(0, ui.KeyLeft)
	ct.key(0, ui.KeyLeft)
	if *asked != 1 {
		t.Fatalf("asked %d times moving within the ranges", *asked)
	}
	ct.key(0, ui.KeyEnd)
	if ct.st.LinkedEditingRanges() != nil {
		t.Fatal("the session outlived the caret leaving its ranges")
	}
	ct.typ("x")
	ct.want("<div></div>x|")
	// Back in the name, the ranges are asked for again.
	ct.st.Select(2, 2)
	ct.tt.Frame()
	ct.typ("y")
	ct.want("<dy|iv></dyiv>x")
}

func TestLinkedEditingEndsOutsideTheWordPattern(t *testing.T) {
	for _, pattern := range []*regexp.Regexp{tagNames, nil} {
		ct, _ := newLinkedTest(t, "<div|></div>", pattern)
		ct.typ(" a")
		ct.want("<div a|></div>")
		if ct.st.LinkedEditingRanges() != nil {
			t.Fatalf("pattern %v: the session outlived a space", pattern)
		}
	}
}

func TestLinkedEditingEmptiedNameKeepsTheSession(t *testing.T) {
	ct, _ := newLinkedTest(t, "<[div]></div>", tagNames)
	ct.key(0, ui.KeyBackspace)
	ct.want("<|></>")
	ct.typ("p")
	ct.want("<p|></p>")
}

func TestLinkedEditingEndsWithASecondCaret(t *testing.T) {
	ct, asked := newLinkedTest(t, "<div|></div>\n<div></div>", tagNames)
	ct.st.SetSelections(Selection{Anchor: 4, Head: 4}, Selection{Anchor: 16, Head: 16})
	ct.tt.Frame()
	if ct.st.LinkedEditingRanges() != nil || *asked != 1 {
		t.Fatalf("with two carets: ranges %v, asked %d times", ct.st.LinkedEditingRanges(), *asked)
	}
	ct.typ("x")
	ct.want("<divx|></div>\n<divx|></div>")
}

func TestLinkedEditingAsksAgainAfterASecondCaret(t *testing.T) {
	ct, asked := newLinkedTest(t, "<div|></div>", tagNames)
	ct.st.SetSelections(Selection{Anchor: 4, Head: 4}, Selection{Anchor: 10, Head: 10})
	ct.tt.Frame()
	// One caret again, at the place asked about before.
	ct.st.Select(4, 4)
	ct.tt.Frame()
	if *asked != 2 || ct.st.LinkedEditingRanges() == nil {
		t.Fatalf("asked %d times, ranges %v", *asked, ct.st.LinkedEditingRanges())
	}
	ct.typ("x")
	ct.want("<divx|></divx>")
}

func TestLinkedEditingFollowsEditsMadeElsewhere(t *testing.T) {
	ct, _ := newLinkedTest(t, "<a|></a>", tagNames)
	// Another device's edit, before the ranges and between them, moves
	// them.
	ct.st.Buffer.EditRemote(Edit{Start: 0, End: 0, Text: "<!-- -->"}, Edit{Start: 3, End: 3, Text: "text"})
	ct.tt.Frame()
	ct.typ("b")
	ct.want("<!-- --><ab|>text</ab>")
	// One in a range ends the session: the ranges hold different text.
	ct.st.Buffer.EditRemote(Edit{Start: 20, End: 20, Text: "c"})
	ct.tt.Frame()
	if ct.st.LinkedEditingRanges() != nil {
		t.Fatal("the session outlived an edit of one of its ranges")
	}
	ct.typ("d")
	ct.want("<!-- --><abd|>text</abc>")
}

func TestLinkedEditingRangesAnsweredLate(t *testing.T) {
	ct := newEditorTest(t, "", "<div|></div>")
	var asked []LinkedEditingRequest
	ct.st.OnLinkedEditing = func(r LinkedEditingRequest) { asked = append(asked, r) }
	ct.tt.Frame()
	if len(asked) != 1 || asked[0].Offset != 4 || asked[0].Version != ct.st.Buffer.Version() {
		t.Fatalf("asked %+v", asked)
	}
	ranges := []Range{{1, 4}, {7, 10}}
	// Ranges of different texts, of another version, or apart from the
	// caret start nothing.
	for _, bad := range [][]Range{{{1, 4}, {6, 9}}, {{1, 4}}, {{1, 4}, {3, 6}}, {{0, 0}, {11, 11}}} {
		ct.st.SetLinkedEditingRanges(asked[0].Version, bad, tagNames)
		if ct.st.LinkedEditingRanges() != nil {
			t.Fatalf("%v started a session", bad)
		}
	}
	ct.typ("x")
	ct.st.SetLinkedEditingRanges(asked[0].Version, ranges, tagNames)
	if ct.st.LinkedEditingRanges() != nil {
		t.Fatal("ranges of an older version started a session")
	}
	ct.key(0, ui.KeyBackspace)
	if len(asked) != 3 {
		t.Fatalf("asked %d times, once for each edit", len(asked))
	}
	ct.st.SetLinkedEditingRanges(asked[2].Version, ranges, tagNames)
	// A late answer leaves the session running.
	ct.st.SetLinkedEditingRanges(asked[0].Version, nil, nil)
	ct.typ("v")
	ct.want("<divv|></divv>")
	// No ranges for the text as it is end it.
	ct.st.SetLinkedEditingRanges(ct.st.Buffer.Version(), nil, nil)
	ct.typ("w")
	ct.want("<divvw|></divv>")
}

func TestLinkedEditingComposition(t *testing.T) {
	ct, _ := newLinkedTest(t, "<a|></a>", nil)
	ct.tt.Compose("に", 1)
	ct.tt.Compose("にほ", 2)
	ct.want("<aにほ|></aにほ>")
	ct.tt.Type("日本")
	ct.want("<a日本|></a日本>")
	ct.run(Undo)
	ct.want("<a|></a>")
	// A composition cancelled leaves both names as they were.
	ct.tt.Compose("に", 1)
	ct.tt.Compose("", 0)
	ct.want("<a|></a>")
	ct.typ("b")
	ct.want("<ab|></ab>")
}

func TestLinkedEditingEndsForASnippet(t *testing.T) {
	ct, _ := newLinkedTest(t, "<d|iv></div>", tagNames)
	if !ct.st.InsertSnippet(snippetFor(t, "a${1:x}b$0")) {
		t.Fatal("snippet not inserted")
	}
	ct.tt.Frame()
	ct.want("<da[x]biv></div>")
	ct.key(0, ui.KeyTab)
	ct.want("<daxb|iv></div>")
}

// Wrapping a selection in quotes is two changes of the range, each
// copied to where it is in the others.
func TestLinkedEditingCopiesEachChange(t *testing.T) {
	ct := newEditorTest(t, "html", "<d[iv]></div>")
	ct.st.OnLinkedEditing = func(r LinkedEditingRequest) {
		ct.st.SetLinkedEditingRanges(r.Version, linkTags(ct.st.Buffer.String(), r.Offset), tagNames)
	}
	ct.tt.Frame()
	ct.typ(`"`)
	ct.want(`<d"[iv]"></d"iv">`)
	ct.run(Undo)
	ct.want("<d[iv]></div>")
}

func TestLinkedEditingAsksFocusedEditorsOnly(t *testing.T) {
	focused := &State{Buffer: NewBuffer("<div></div>")}
	other := &State{Buffer: NewBuffer("<div></div>")}
	var asked []*State
	for _, st := range []*State{focused, other} {
		st.OnLinkedEditing = func(LinkedEditingRequest) { asked = append(asked, st) }
	}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() {
			View(c, focused).Grow(1).FillWidth().AutoFocus()
			View(c, other).Grow(1).FillWidth()
		})
	}, 600, 400)
	tt.Frame()
	tt.Frame()
	if len(asked) != 1 || asked[0] != focused {
		t.Fatalf("asked %d times, the unfocused editor among them: %v", len(asked), len(asked) > 0 && asked[len(asked)-1] == other)
	}
}
