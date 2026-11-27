package viewgen

import (
	"strings"
	"testing"
)

func parseOne(t *testing.T, src string) (*File, error) {
	t.Helper()
	f, diags, err := Parse("test.view", []byte(src))
	if err != nil {
		return f, err
	}
	if len(diags) > 0 {
		t.Fatalf("diagnostics: %v", diags)
	}
	return f, nil
}

func TestParseElementsAndAttributes(t *testing.T) {
	f, err := parseOne(t, `<column fill gap="16" font-size={56}>
	<text font-size="56" bold>Count: {N}</text>
	<button label="Reset" disabled={N == 0} on-clicked="Reset"/>
	<row></row>
</column>`)
	if err != nil {
		t.Fatal(err)
	}
	col := f.Roots[0]
	if col.Tag != "column" || col.Pos.Line != 1 || col.Pos.Col != 1 {
		t.Fatalf("root: %+v", col)
	}
	if len(col.Attrs) != 3 {
		t.Fatalf("attrs: %+v", col.Attrs)
	}
	if !col.Attrs[0].Bare || col.Attrs[0].Name != "fill" {
		t.Fatalf("bare attr: %+v", col.Attrs[0])
	}
	if col.Attrs[1].Name != "gap" || col.Attrs[1].Value[0].Lit != "16" || col.Attrs[1].Quote != '"' {
		t.Fatalf("gap: %+v", col.Attrs[1])
	}
	if a := col.Attrs[2]; a.Value[0].IsExpr != true || a.Value[0].Expr != "56" || a.Quote != 0 {
		t.Fatalf("braced attr: %+v", a)
	}
	if len(col.Children) != 3 {
		t.Fatalf("children: %d", len(col.Children))
	}
	text := col.Children[0]
	if !text.HasBodyText || len(text.Text) != 2 || text.Text[0].Lit != "Count: " || text.Text[1].Expr != "N" {
		t.Fatalf("text parts: %+v", text.Text)
	}
	btn := col.Children[1]
	if len(btn.Children) != 0 || btn.attr("label") != "Reset" {
		t.Fatalf("self-closed button: %+v", btn)
	}
	if a := btn.attr2("disabled"); !a.Value[0].IsExpr || a.Value[0].Expr != "N == 0" {
		t.Fatalf("disabled: %+v", a)
	}
}

func (n *Node) attr2(name string) *Attr {
	for _, a := range n.Attrs {
		if a.Name == name {
			return a
		}
	}
	return nil
}

func TestParseViewWrapper(t *testing.T) {
	f, err := parseOne(t, `<view type="counter">
	<column fill><text>{N}</text></column>
</view>`)
	if err != nil {
		t.Fatal(err)
	}
	if !f.HasViewEl || f.Type != "counter" {
		t.Fatalf("view: %+v", f)
	}
	if len(f.Roots) != 1 || f.Roots[0].Tag != "column" {
		t.Fatalf("roots: %+v", f.Roots)
	}
}

func TestParseGoStringsInExpressions(t *testing.T) {
	// Braces, angles and quotes inside Go strings, a raw string and a
	// composite literal must not end the expression.
	src := "<text>{fmt.Sprintf(\"}%d{\", N) + \"\x60}\x60\" + T{x: \"a<b>\"}[x]}</text>"
	f, err := parseOne(t, src)
	if err != nil {
		t.Fatal(err)
	}
	want := "fmt.Sprintf(\"}%d{\", N) + \"\x60}\x60\" + T{x: \"a<b>\"}[x]"
	if got := f.Roots[0].Text[0].Expr; got != want {
		t.Fatalf("expr:\n got %q\nwant %q", got, want)
	}
}

func TestParseEntitiesAndBraceText(t *testing.T) {
	f, err := parseOne(t, `<text>&lt;tag&gt; &amp; {"{"} 100% &quot;ok&quot;</text>`)
	if err != nil {
		t.Fatal(err)
	}
	parts := f.Roots[0].Text
	if parts[0].Lit != "<tag> & " {
		t.Fatalf("lit: %q", parts[0].Lit)
	}
	if !parts[1].IsExpr || parts[1].Expr != `"{"` {
		t.Fatalf("brace text: %+v", parts[1])
	}
	if parts[2].Lit != ` 100% "ok"` {
		t.Fatalf("tail: %q", parts[2].Lit)
	}
}

func TestParseWhitespaceFolding(t *testing.T) {
	f, err := parseOne(t, "<text>\n\tHello\n\t  world\t{name}\n\tdone\n</text>")
	if err != nil {
		t.Fatal(err)
	}
	parts := f.Roots[0].Text
	if parts[0].Lit != "Hello world " {
		t.Fatalf("folded: %q", parts[0].Lit)
	}
	if parts[1].Expr != "name" {
		t.Fatalf("expr: %+v", parts[1])
	}
	if parts[2].Lit != " done" {
		t.Fatalf("tail: %q", parts[2].Lit)
	}
}

func TestParseComments(t *testing.T) {
	f, err := parseOne(t, `<!-- a comment > with < things -->
<column><!-- inner --><text>Hi</text></column>`)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Roots) != 1 || f.Roots[0].Children[0].Text[0].Lit != "Hi" {
		t.Fatalf("tree: %+v", f.Roots)
	}
}

func TestParseErrors(t *testing.T) {
	for _, test := range []struct{ src, want string }{
		{`<column><text>Hi</column>`, "</column> closes <text>"},
		{`<column>`, "not closed"},
		{`<column`, "not closed"},
		{`<column attr="x>`, "not closed"},
		{`<text>Hi`, "not closed"},
		{`<column><text>{unclosed(</text></column>`, "{ not closed"},
		{`text at top`, "unexpected text"},
		{`<column></column>text`, "unexpected text"},
		{`<view><text>x</text></view>`, "<view> needs a type"},
	} {
		_, _, err := Parse("test.view", []byte(test.src))
		if err == nil {
			t.Errorf("%q: no error", test.src)
			continue
		}
		if !strings.Contains(err.Error(), test.want) {
			t.Errorf("%q: got %q, want %q", test.src, err.Error(), test.want)
		}
	}
}

func TestParseNonASCII(t *testing.T) {
	f, err := parseOne(t, `<text>日本語 {名前} emoji 🎉</text>`)
	if err != nil {
		t.Fatal(err)
	}
	parts := f.Roots[0].Text
	if parts[0].Lit != "日本語 " || parts[1].Expr != "名前" || parts[2].Lit != " emoji 🎉" {
		t.Fatalf("parts: %+v", parts)
	}
}

func TestParseCommentsInsideBodyPreserveSpaces(t *testing.T) {
	f, err := parseOne(t, `<text>100% <!-- note --> complete</text>`)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Roots[0].Text) != 1 || f.Roots[0].Text[0].Lit != "100% complete" {
		t.Fatalf("parts: %+v", f.Roots[0].Text)
	}
}

func TestParseViewWrapperValidation(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{`<view type="counter" extra="1"><text>x</text></view>`, "<view> takes only type"},
		{`<text>before</text><view type="counter"><text>x</text></view>`, "<view> wraps the whole file"},
		{`<view type="counter"><text>x</text></view><text>after</text>`, "<view> wraps the whole file"},
	} {
		if _, _, err := Parse("test.view", []byte(tc.src)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: got %v, want %q", tc.src, err, tc.want)
		}
	}
}
