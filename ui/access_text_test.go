package ui

import (
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/egoist/mygo/internal/platform"
)

func textNode(t *testing.T, tt *Tester, label string) platform.AccessNode {
	t.Helper()
	if tt.h.access == nil {
		tt.send(platform.SurfaceEvent{Kind: platform.AccessibilityOn})
	}
	for _, n := range tt.h.access.Nodes {
		if n.Label == label {
			return n
		}
	}
	t.Fatalf("no text node %q", label)
	return platform.AccessNode{}
}

func TestAccessTextUnicodeRanges(t *testing.T) {
	value := "A😀e\u0301 👩‍👩‍👧‍👦\r\nשלום world"
	tt := NewTester(func(c *Context) { TextArea(c, &value).Label("Editor").Width(240).Height(150) }, 300, 200)
	defer tt.rt.close()
	n := textNode(t, tt, "Editor")
	doc := n.Text
	if doc == nil || doc.Content != value || doc.Length != utf8.RuneCountInString(value) {
		t.Fatalf("text: %+v", doc)
	}
	want := []string{"A", "😀", "e\u0301", " ", "👩‍👩‍👧‍👦", "\r\n"}
	i := 0
	for _, s := range want {
		r := doc.Ask(platform.AccessTextQuery{Kind: platform.TextUnitRange, Unit: platform.TextCharacter, Start: i})
		got := doc.Ask(platform.AccessTextQuery{Kind: platform.TextSlice, Start: r.Start, End: r.End}).Text
		if got != s {
			t.Fatalf("grapheme at %d: %q want %q (%+v)", i, got, s, r)
		}
		i = r.End
	}
	for i := 0; i <= doc.Length; i++ {
		u := doc.Ask(platform.AccessTextQuery{Kind: platform.TextToUTF16, Start: i}).Start
		r := doc.Ask(platform.AccessTextQuery{Kind: platform.TextFromUTF16, Start: u}).Start
		b := doc.Ask(platform.AccessTextQuery{Kind: platform.TextToByte, Start: i}).Start
		br := doc.Ask(platform.AccessTextQuery{Kind: platform.TextFromByte, Start: b}).Start
		if r != i || br != i {
			t.Fatalf("offset %d: utf16 %d -> %d, byte %d -> %d", i, u, r, b, br)
		}
	}
	if got := doc.Ask(platform.AccessTextQuery{Kind: platform.TextFromUTF16, Start: 2}).Start; got != 1 {
		t.Fatalf("interior surrogate -> %d", got)
	}
	if got := doc.Ask(platform.AccessTextQuery{Kind: platform.TextFromByte, Start: 3}).Start; got != 1 {
		t.Fatalf("interior byte -> %d", got)
	}
	if units := doc.Ask(platform.AccessTextQuery{Kind: platform.TextToUTF16, Start: doc.Length}).Start; units != len(utf16.Encode([]rune(value))) {
		t.Fatal(units)
	}
	r := doc.Ask(platform.AccessTextQuery{Kind: platform.TextMoveOffset, Unit: platform.TextCharacter, Start: 0, Count: 5})
	back := doc.Ask(platform.AccessTextQuery{Kind: platform.TextMoveOffset, Unit: platform.TextCharacter, Start: r.Start, Count: -5})
	if r.Count != 5 || back.Count != -5 || back.Start != 0 {
		t.Fatalf("move: %+v back %+v", r, back)
	}
	wordStart := utf8.RuneCountInString(value[:strings.Index(value, "שלום")])
	word := doc.Ask(platform.AccessTextQuery{Kind: platform.TextUnitRange, Unit: platform.TextWord, Start: wordStart})
	if got := strings.TrimSpace(doc.Ask(platform.AccessTextQuery{Kind: platform.TextSlice, Start: word.Start, End: word.End}).Text); got != "שלום" {
		t.Fatalf("word: %q (%+v)", got, word)
	}
}

func TestAccessTextWordsEmptyAndInlineRanges(t *testing.T) {
	value := "e\u0301 can't שלום 中文"
	empty := ""
	tt := NewTester(func(c *Context) {
		TextInput(c, &empty).Label("Empty")
		Text(c, value).Label("Words")
		RichText(c).Label("Links").Children(func() {
			Link(c, "same", "https://example.com/one")
			Text(c, " and ")
			Link(c, "same", "https://example.com/two")
		})
	}, 400, 220)
	defer tt.rt.close()
	d := textNode(t, tt, "Words").Text
	for _, want := range []string{"e\u0301", "can't", "שלום"} {
		at := utf8.RuneCountInString(value[:strings.Index(value, want)])
		r := d.Ask(platform.AccessTextQuery{Kind: platform.TextUnitRange, Unit: platform.TextWord, Start: at})
		if got := strings.TrimSpace(d.Ask(platform.AccessTextQuery{Kind: platform.TextSlice, Start: r.Start, End: r.End}).Text); got != want {
			t.Fatalf("word %q -> %q", want, got)
		}
	}
	emptyDoc := textNode(t, tt, "Empty").Text
	if emptyDoc.Length != 0 || len(emptyDoc.Ask(platform.AccessTextQuery{Kind: platform.TextRangeBounds}).Rects) != 1 {
		t.Fatal("empty input lost caret geometry")
	}
	links := textNode(t, tt, "Links")
	if links.Text.Content != "same and same" {
		t.Fatal(links.Text.Content)
	}
	var ranges [][2]int
	for _, n := range tt.h.access.Nodes {
		if n.Role == platform.RoleLink {
			if n.Text != nil {
				t.Fatal("inline link has an independent text provider")
			}
			ranges = append(ranges, [2]int{n.TextStart, n.TextEnd})
		}
	}
	if len(ranges) != 2 || ranges[0] != [2]int{0, 4} || ranges[1] != [2]int{9, 13} {
		t.Fatalf("repeated inline links: %+v", ranges)
	}
}

func TestAccessTextSelectionReadOnlyAndPassword(t *testing.T) {
	value := "A😀e\u0301B"
	secret := "private🔒"
	disabled := false
	tt := NewTester(func(c *Context) {
		TextInput(c, &value).Label("Read only").ReadOnly(true).Disabled(disabled)
		RichText(c, Span{Text: value, Weight: 600}).Label("Selectable").Selectable()
		TextInput(c, &secret).Label("Password").Password()
	}, 300, 180)
	defer tt.rt.close()
	n := textNode(t, tt, "Read only")
	if n.Text == nil || !n.Text.Selectable || n.Actions&platform.ActionSetValue != 0 || n.States&platform.AccessReadOnly == 0 {
		t.Fatalf("read-only: %+v", n)
	}
	tt.send(platform.SurfaceEvent{Kind: platform.AccessAction, Action: platform.AccessSetSelection, ID: n.ID, From: 4, To: 1})
	n = textNode(t, tt, "Read only")
	if n.SelStart != 1 || n.SelEnd != 4 || n.Text.Caret != 1 {
		t.Fatalf("backward selection: %+v", n)
	}
	tt.send(platform.SurfaceEvent{Kind: platform.AccessAction, Action: platform.AccessSetSelection, ID: n.ID, From: 3, To: 3})
	n = textNode(t, tt, "Read only")
	if n.SelStart != 2 || n.SelEnd != 2 {
		t.Fatalf("inside grapheme: %d..%d", n.SelStart, n.SelEnd)
	}
	tt.send(platform.SurfaceEvent{Kind: platform.AccessAction, Action: platform.AccessSetValue, ID: n.ID, Text: "overwrite"})
	if value != "A😀e\u0301B" {
		t.Fatal("read-only mutated")
	}
	s := textNode(t, tt, "Selectable")
	if s.Text == nil || !s.Text.Selectable {
		t.Fatalf("selectable: %+v", s)
	}
	tt.send(platform.SurfaceEvent{Kind: platform.AccessAction, Action: platform.AccessSetSelection, ID: s.ID, From: 1, To: 4})
	s = textNode(t, tt, "Selectable")
	if s.Text.Content != value || s.SelStart != 1 || s.SelEnd != 4 {
		t.Fatalf("rich text: %+v", s)
	}
	if tt.rt.focused != s.ID {
		t.Fatal("selection did not focus selectable text")
	}
	tt.send(platform.SurfaceEvent{Kind: platform.SurfaceCommand, Text: "copy"})
	if tt.h.clipboard != "😀e\u0301" {
		t.Fatalf("copy after accessible selection: %q", tt.h.clipboard)
	}
	p := textNode(t, tt, "Password")
	if p.Text != nil || p.Value != "" || p.SelStart != 0 || p.SelEnd != 0 {
		t.Fatalf("password disclosed: %+v", p)
	}
	tt.send(platform.SurfaceEvent{Kind: platform.AccessAction, Action: platform.AccessSetSelection, ID: p.ID, From: 0, To: 4})
	if secret != "private🔒" {
		t.Fatal("password changed")
	}
	disabled = true
	tt.Frame()
	n = textNode(t, tt, "Read only")
	if n.Text == nil || n.Text.Selectable {
		t.Fatalf("disabled text: %+v", n)
	}
}

func TestAccessTextBoundsWrappedAndBidi(t *testing.T) {
	value := "hello world this line wraps into several lines\nabc אבג xyz\nend"
	tt := NewTester(func(c *Context) { TextArea(c, &value).Label("Editor").Width(150).Height(100) }, 220, 160)
	defer tt.rt.close()
	n := textNode(t, tt, "Editor")
	d := n.Text
	for i := 0; i < 4; i++ {
		r := d.Ask(platform.AccessTextQuery{Kind: platform.TextLineRange, Start: i})
		if !r.OK || r.Start >= r.End {
			t.Fatalf("line %d: %+v", i, r)
		}
		if got := d.Ask(platform.AccessTextQuery{Kind: platform.TextLineNumber, Start: r.Start}).Start; got != i {
			t.Fatalf("line %d starts on %d", i, got)
		}
	}
	rs := d.Ask(platform.AccessTextQuery{Kind: platform.TextVisibleRanges}).Ranges
	if len(rs) == 0 {
		t.Fatal("no visible ranges")
	}
	for _, r := range rs {
		bounds := d.Ask(platform.AccessTextQuery{Kind: platform.TextRangeBounds, Start: r.Start, End: r.End}).Rects
		if len(bounds) == 0 {
			t.Fatalf("no bounds for %+v", r)
		}
		for _, b := range bounds {
			if b.W <= 0 || b.H <= 0 || b.X < n.Bounds.X || b.Y < n.Bounds.Y || b.X+b.W > n.Bounds.X+n.Bounds.W+0.1 || b.Y+b.H > n.Bounds.Y+n.Bounds.H+0.1 {
				t.Fatalf("bounds %+v outside %+v", b, n.Bounds)
			}
		}
	}
	// Reveal without changing the selection.
	before := d.Caret
	tt.send(platform.SurfaceEvent{Kind: platform.AccessAction, Action: platform.AccessScrollText, ID: n.ID, From: 0, To: 1, Caret: 1})
	n = textNode(t, tt, "Editor")
	d = n.Text
	if d.Caret != before {
		t.Fatal("scroll changed caret")
	}
	caret := d.Ask(platform.AccessTextQuery{Kind: platform.TextRangeBounds, Start: 0, End: 0}).Rects
	if len(caret) != 1 {
		t.Fatalf("caret: %+v", caret)
	}
	b := caret[0]
	hit := d.Ask(platform.AccessTextQuery{Kind: platform.TextOffsetAtPoint, X: b.X, Y: b.Y + b.H/2}).Start
	if hit != 0 {
		t.Fatalf("point at first caret -> %d", hit)
	}

	bidi := NewTester(func(c *Context) { Text(c, "abc אבג xyz").Label("Bidi").Width(220) }, 260, 60)
	defer bidi.rt.close()
	bd := textNode(t, bidi, "Bidi").Text
	// "c א" crosses a bidi run boundary: it occupies disjoint visual spans.
	br := bd.Ask(platform.AccessTextQuery{Kind: platform.TextRangeBounds, Start: 2, End: 5}).Rects
	if len(br) < 2 {
		t.Fatalf("bidi logical range merged across unselected text: %+v", br)
	}
}

func TestAccessTextVirtualizationAndCleanup(t *testing.T) {
	value := strings.Repeat("paragraph 😀 with words\n", 20000)
	show := true
	tt := NewTester(func(c *Context) {
		if show {
			TextArea(c, &value).Label("Large").Width(250).Height(120)
		}
	}, 300, 180)
	n := textNode(t, tt, "Large")
	d := n.Text
	ed := tt.rt.states[n.ID].editor
	laid := ed.area.laid
	scroll := ed.area.scroll
	for _, q := range []platform.AccessTextQuery{
		{Kind: platform.TextRangeBounds, Start: 0, End: d.Length},
		{Kind: platform.TextVisibleRanges},
		{Kind: platform.TextUnitRange, Unit: platform.TextLine, Start: d.Length / 2},
		{Kind: platform.TextToUTF16, Start: d.Length / 2},
		{Kind: platform.TextUnitRange, Unit: platform.TextCharacter, Start: d.Length / 2},
	} {
		if !d.Ask(q).OK {
			t.Fatal(q)
		}
	}
	if ed.area.laid != laid || ed.area.scroll != scroll {
		t.Fatalf("queries changed virtualization: %d -> %d, %v -> %v", laid, ed.area.laid, scroll, ed.area.scroll)
	}
	show = false
	tt.Frame()
	if d.Ask(platform.AccessTextQuery{Kind: platform.TextSlice, End: -1}).OK {
		t.Fatal("removed range still answers")
	}
	show = true
	tt.Frame()
	d = textNode(t, tt, "Large").Text
	tt.rt.close()
	if d.Ask(platform.AccessTextQuery{Kind: platform.TextVisibleRanges}).OK {
		t.Fatal("closed editor still answers")
	}
}

func TestBufferNativeOffsetsAfterEdits(t *testing.T) {
	var b buffer
	b.set("a😀\nbé\nc")
	for _, change := range []struct {
		a, z int
		s    string
	}{{1, 2, "👩‍👧"}, {0, 4, "漢\n😀\n"}, {3, 5, ""}} {
		b.replace(change.a, change.z, change.s)
		var fresh buffer
		fresh.set(b.s)
		if b.n != fresh.n || b.u != fresh.u || len(b.paras) != len(fresh.paras) {
			t.Fatal("index differs")
		}
		for i, p := range b.paras {
			if p.rune != fresh.paras[i].rune || p.byte != fresh.paras[i].byte || p.unit != fresh.paras[i].unit {
				t.Fatalf("paragraph %d: %+v want %+v", i, p, fresh.paras[i])
			}
		}
		for i := 0; i <= b.n; i++ {
			if b.unitOf(i) != fresh.unitOf(i) || b.runeOf(b.unitOf(i), true) != i {
				t.Fatal("offset after edit", i)
			}
		}
	}
}

func TestAccessTextScrollRangesWithoutMovingCaret(t *testing.T) {
	value := strings.Repeat("word 😀 ", 40)
	tt := NewTester(func(c *Context) { TextInput(c, &value).Label("Long input").Width(160) }, 220, 80)
	defer tt.rt.close()
	n := textNode(t, tt, "Long input")
	caret := n.Text.Caret
	if tt.rt.states[n.ID].editor.scrollX <= 0 {
		t.Fatal("fixture did not scroll to its caret")
	}
	tt.send(platform.SurfaceEvent{Kind: platform.AccessAction, Action: platform.AccessScrollText, ID: n.ID, From: 0, To: 1, Caret: 1})
	tt.Frame()
	n = textNode(t, tt, "Long input")
	if n.Text.Caret != caret || len(n.Text.Ask(platform.AccessTextQuery{Kind: platform.TextRangeBounds, Start: 0, End: 1}).Rects) == 0 {
		t.Fatal("single-line range was not revealed independently of the caret")
	}
	static := strings.Repeat("line\n", 50) + "last"
	st := NewTester(func(c *Context) {
		Scroll(c).Size(180, 90).Children(func() { Text(c, static).Label("Static").Selectable() })
	}, 220, 140)
	defer st.rt.close()
	s := textNode(t, st, "Static")
	at := utf8.RuneCountInString(static) - 4
	st.send(platform.SurfaceEvent{Kind: platform.AccessAction, Action: platform.AccessScrollText, ID: s.ID, From: at, To: at + 4, Caret: 1})
	s = textNode(t, st, "Static")
	if s.Text.Caret != 0 || len(s.Text.Ask(platform.AccessTextQuery{Kind: platform.TextRangeBounds, Start: at, End: at + 4}).Rects) == 0 {
		t.Fatal("static range was not revealed independently of the caret")
	}
}
