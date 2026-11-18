package richtext

import (
	"encoding/json"
	"strings"
	"testing"
)

func lineBreakText() Text {
	return TextOf(Paragraph{Runs: []Run{
		{Text: "alpha" + string(LineSeparator) + "beta"},
	}})
}

func TestLineSeparatorString(t *testing.T) {
	const wantPlain = "alpha\nbeta"
	text := lineBreakText()
	if text.ParagraphCount() != 1 {
		t.Fatalf("%d paragraphs, want 1", text.ParagraphCount())
	}
	if got := text.String(); got != "alpha"+string(LineSeparator)+"beta" {
		t.Errorf("String() = %q, want raw LineSeparator", got)
	}
	if got := text.PlainText(); got != wantPlain {
		t.Errorf("PlainText() = %q, want %q", got, wantPlain)
	}
}

func TestLineSeparatorHTML(t *testing.T) {
	text := lineBreakText()
	html := text.HTML()
	if !strings.Contains(html, "alpha<br>beta") {
		t.Fatalf("HTML() = %q, want a <br>", html)
	}
	got := ParseHTML(html)
	if !got.Equal(text) {
		t.Fatalf("HTML reimport = %q, want %q", runsOf(got), runsOf(text))
	}

	bold := ParseHTML("<p><b>alpha<br>beta</b></p>")
	if bold.ParagraphCount() != 1 || bold.PlainText() != "alpha\nbeta" {
		t.Fatalf("<br> in bold = %d paragraphs, %q", bold.ParagraphCount(), bold.PlainText())
	}
	for _, r := range bold.Paragraphs() {
		for _, run := range r.Runs {
			if run.Attributes.Weight != 700 {
				t.Errorf("run %q lost bold across <br>", run.Text)
			}
		}
	}

	pre := ParseHTML("<pre><code>alpha<br>beta</code></pre>")
	if pre.ParagraphCount() != 2 || pre.PlainText() != "alpha\nbeta" {
		t.Fatalf("<br> in <pre> = %d paragraphs, %q", pre.ParagraphCount(), pre.PlainText())
	}
	for _, p := range pre.Paragraphs() {
		if !p.Style.CodeBlock {
			t.Errorf("<pre> paragraph is not code: %+v", p.Style)
		}
	}
}

func TestLineSeparatorRTF(t *testing.T) {
	got := ParseRTF([]byte(`{\rtf1 alpha\line beta}`))
	if got.ParagraphCount() != 1 {
		t.Fatalf("%d paragraphs, want 1", got.ParagraphCount())
	}
	if got.PlainText() != "alpha\nbeta" {
		t.Errorf("PlainText() = %q, want %q", got.PlainText(), "alpha\nbeta")
	}
}

func TestLineSeparatorClipboard(t *testing.T) {
	text := lineBreakText()
	encoded, err := text.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var clip clipText
	if err := json.Unmarshal(encoded, &clip); err != nil {
		t.Fatal(err)
	}
	if got := clip.Paragraphs[0].Runs[0].Text; got != "alpha\nbeta" {
		t.Errorf("clipboard JSON text = %q, want %q", got, "alpha\nbeta")
	}

	for _, c := range []struct {
		name, text string
	}{
		{"LF", `alpha\nbeta`},
		{"CRLF", `alpha\r\nbeta`},
		{"CR", `alpha\rbeta`},
	} {
		t.Run(c.name, func(t *testing.T) {
			src := []byte(`{"paragraphs":[{"style":{},"runs":[{"text":"` + c.text + `","attributes":{}}]}]}`)
			got, ok := decodeClip(src)
			if !ok {
				t.Fatal("not decoded")
			}
			for _, p := range got.Paragraphs() {
				if p.Runs[0].Text != "alpha"+string(LineSeparator)+"beta" || !got.Equal(lineBreakText()) {
					t.Errorf("decoded text = %q, want a LineSeparator in one paragraph", runsOf(got))
				}
			}
		})
	}
}

func TestLineSeparatorMarkdown(t *testing.T) {
	text := lineBreakText()
	const want = "alpha\\\nbeta"
	if got := text.Markdown(); got != want {
		t.Fatalf("Markdown() = %q, want %q", got, want)
	}
	parsed := ParseMarkdown(want)
	if !parsed.Equal(text) {
		t.Errorf("Markdown reparse = %q, want %q", runsOf(parsed), runsOf(text))
	}
}

func TestLineSeparatorMarkdownRuns(t *testing.T) {
	const line = "alpha" + string(LineSeparator) + "beta"
	for _, c := range []struct {
		name string
		text Text
		want string
	}{
		{"bold", TextOf(Paragraph{Runs: []Run{{Text: line, Attributes: Attributes{Weight: 700}}}}), "**alpha\\\nbeta**"},
		{"italic", TextOf(Paragraph{Runs: []Run{{Text: line, Attributes: Attributes{Italic: true}}}}), "*alpha\\\nbeta*"},
		{"strikethrough", TextOf(Paragraph{Runs: []Run{{Text: line, Attributes: Attributes{Strikethrough: true}}}}), "~~alpha\\\nbeta~~"},
		{"link", TextOf(Paragraph{Runs: []Run{{Text: line, Attributes: Attributes{Link: "https://e.org"}}}}), "[alpha\\\nbeta](https://e.org)"},
		{"code", TextOf(Paragraph{Runs: []Run{{Text: line, Attributes: Attributes{Code: true}}}}), "`alpha" + string(LineSeparator) + "beta`"},
		{"cross-run bold", TextOf(Paragraph{Runs: []Run{{Text: "alpha" + string(LineSeparator), Attributes: Attributes{Weight: 700}}, {Text: "beta"}}}), "**alpha**\\\nbeta"},
		{"trailing break", TextOf(Paragraph{Runs: []Run{{Text: "alpha" + string(LineSeparator)}}}), "alpha<br>"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := c.text.Markdown(); got != c.want {
				t.Fatalf("Markdown() = %q, want %q", got, c.want)
			}
			got := ParseMarkdown(c.want)
			if got.String() != c.text.String() {
				t.Fatalf("Markdown round-trip text = %q, want %q", got.String(), c.text.String())
			}
			for _, p := range c.text.Paragraphs() {
				if !readsAs(c.want, p.Runs) {
					t.Fatalf("readsAs(%q) = false", c.want)
				}
			}
		})
	}
}

func TestLineSeparatorMarkdownBlockStart(t *testing.T) {
	runs := []Run{{
		Text:       "alpha" + string(LineSeparator) + "# heading",
		Attributes: Attributes{Weight: 700},
	}}
	text := TextOf(Paragraph{Runs: runs})
	got := text.Markdown()
	const want = "**alpha\\\n\\# heading**"
	if got != want {
		t.Fatalf("Markdown() = %q, want %q", got, want)
	}
	if parsed := ParseMarkdown(got); !parsed.Equal(text) {
		t.Fatalf("Markdown round-trip = %q, want %q", runsOf(parsed), runsOf(text))
	}
}

func TestLineSeparatorRender(t *testing.T) {
	doc := NewDocument(TextOf(Paragraph{Runs: []Run{{Text: "alpha" + string(LineSeparator)}}}))
	tt, _ := richTester(t, doc)
	before := caretAt(t, tt, doc, 0)
	after := caretAt(t, tt, doc, doc.Text().Len())
	if after.Y <= before.Y {
		t.Errorf("caret after trailing break at %v, want below %v", after, before)
	}
	if after.X != before.X {
		t.Errorf("caret after trailing break at x=%v, want %v", after.X, before.X)
	}
	if _, ok := tt.TextCaret(); !ok {
		t.Error("no caret after trailing break")
	}
}
