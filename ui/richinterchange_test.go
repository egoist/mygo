package ui

import (
	"slices"
	"strings"
	"testing"
)

func TestRichClipboardHTMLAndRTFRoundTrips(t *testing.T) {
	d := NewRichDocument("Bold 😀\nשלום & <tag>\n")
	d = d.WithStyle(TextRange{0, 4}, RichStyle{Weight: 700, Size: 16, Font: "serif", Color: RGB(10, 20, 30), Underline: true})
	d = d.WithStyle(TextRange{7, 11}, RichStyle{Italic: true, Strikethrough: true, Background: RGB(250, 230, 50), Link: "https://example.com/?a=1&b=2"})
	d = d.WithParagraphStyle(TextRange{7, 11}, ParagraphStyle{Alignment: End, LineHeight: 1.5})
	d = d.WithParagraphStyle(TextRange{d.n, d.n}, ParagraphStyle{Alignment: Center})
	for _, tc := range []struct {
		name, data string
		parse      func(string) (RichDocument, error)
	}{{"HTML", d.HTML(), ParseRichHTML}, {"RTF", d.RTF(), ParseRichRTF}} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := tc.parse(tc.data)
			if err != nil {
				t.Fatal(err)
			}
			if out.String() != d.String() || !slices.Equal(out.Paragraphs(), d.Paragraphs()) {
				t.Fatalf("round trip text/paragraphs: %q %+v, want %q %+v\n%s", out.String(), out.Paragraphs(), d.String(), d.Paragraphs(), tc.data)
			}
			for i, r := range []rune(d.String()) {
				if r != '\n' && out.StyleAt(i) != d.StyleAt(i) {
					t.Fatalf("style at %d: %+v, want %+v\n%s", i, out.StyleAt(i), d.StyleAt(i), tc.data)
				}
			}
			checkRichDocument(t, out)
		})
	}
}

func TestRichHTMLExternalMarkupAndSafeLinks(t *testing.T) {
	d, err := ParseRichHTML(`<div><p style="text-align:center;line-height:150%">One <strong>bold <em>italic</em></strong></p><p><a href="javascript:alert(1)">unsafe</a> <a href="mailto:test@example.com">mail</a><br><span style="color:rgb(1,2,3);background-color:#ffc;font-size:12pt">color</span></p><script>hidden()</script><img src="https://invalid.test/remote"><iframe>hidden</iframe></div>`)
	if err != nil {
		t.Fatal(err)
	}
	if d.String() != "One bold italic\nunsafe mail\ncolor" || d.StyleAt(4).Weight != 700 || !d.StyleAt(9).Italic || d.StyleAt(16).Link != "" || d.StyleAt(23).Link != "mailto:test@example.com" {
		t.Fatalf("HTML import = %q %+v", d.String(), d.Runs())
	}
	if d.paragraph(0) != (ParagraphStyle{Alignment: Center, LineHeight: 1.5}) || d.StyleAt(d.n-1).Color != RGB(1, 2, 3) || d.StyleAt(d.n-1).Size != 16 {
		t.Fatal("CSS import", d)
	}
	cluster, err := ParseRichHTML(`<b>e</b><i>́</i> x`)
	if err != nil || cluster.StyleAt(0) != cluster.StyleAt(1) || cluster.StyleAt(0).Weight != 700 {
		t.Fatal("external HTML split a grapheme", err, cluster.Runs())
	}
	checkRichDocument(t, cluster)
	alpha := NewRichDocument("x").WithStyle(TextRange{0, 1}, RichStyle{Color: RGBA(10, 20, 30, 0.5)})
	out, err := ParseRichHTML(alpha.HTML())
	if err != nil || out.StyleAt(0).Color != alpha.StyleAt(0).Color {
		t.Fatal("HTML alpha round trip")
	}
}

func TestRichRTFExternalEscapesDestinationsAndFields(t *testing.T) {
	d, err := ParseRichRTF(`{\rtf1\ansi\ansicpg1252\uc1{\fonttbl{\f0\fnil Arial;}}{\colortbl;\red255\green0\blue0;}\f0\cf1 caf\'e9 \u-10179?\u-8704? {\b bold} {\field{\*\fldinst HYPERLINK "https://example.com/x"}{\fldrslt link}}{\*\unknown hidden}{\pict\bin3 {}x}}`)
	if err != nil {
		t.Fatal(err)
	}
	if d.String() != "café 😀 bold link" || d.StyleAt(0).Font != "Arial" || d.StyleAt(0).Color != RGB(255, 0, 0) || d.StyleAt(7).Weight != 700 || d.StyleAt(12).Link != "https://example.com/x" {
		t.Fatalf("RTF = %q %+v", d.String(), d.Runs())
	}
	unsafe, err := ParseRichRTF(`{\rtf1{\field{\*\fldinst HYPERLINK "file:///private"}{\fldrslt text}}}`)
	if err != nil || unsafe.StyleAt(0).Link != "" || unsafe.String() != "text" {
		t.Fatal("unsafe RTF link retained")
	}
	for _, bad := range []string{`{\rtf1`, `{\rtf1 text}}`, `{\rtf1\bin100 short}`, `{\rtf1\'xx}`, `{\rtf1\ansicpg932 text}`, `{\rtf1\uc100 text}`, `{\rtf1}garbage`, strings.Repeat("{", 129) + `\rtf1` + strings.Repeat("}", 129)} {
		if _, err := ParseRichRTF(bad); err == nil {
			t.Fatalf("accepted malformed or unsupported RTF %q", bad)
		}
	}
}

func TestRichRTFUTF8DefaultFontAndUnicodeLink(t *testing.T) {
	d, err := ParseRichRTF(`{\rtf1\ansi\ansicpg65001\deff0{\fonttbl{\f0 Arial;}}\'d7\'a9\'d7\'9c\'d7\'95\'d7\'9d}`)
	if err != nil || d.String() != "שלום" || d.StyleAt(0).Font != "Arial" {
		t.Fatalf("UTF-8/default font import: %q %+v %v", d.String(), d.Runs(), err)
	}
	url := "https://example.com/😀"
	d = NewRichDocument("link").WithStyle(TextRange{0, 4}, RichStyle{Link: url})
	out, err := ParseRichRTF(d.RTF())
	if err != nil || out.StyleAt(0).Link != url {
		t.Fatal("Unicode hyperlink round trip", out.Runs(), err)
	}
}

func TestRichInterchangeLogicalRTLAlignment(t *testing.T) {
	for _, alignment := range []Align{Start, Center, End} {
		d := NewRichDocument("שלום").WithParagraphStyle(TextRange{}, ParagraphStyle{Alignment: alignment})
		for _, tc := range []struct {
			data  string
			parse func(string) (RichDocument, error)
		}{{d.HTML(), ParseRichHTML}, {d.RTF(), ParseRichRTF}} {
			out, err := tc.parse(tc.data)
			if err != nil || out.paragraph(0).Alignment != alignment {
				t.Fatal("RTL alignment round trip", alignment, out.Paragraphs(), err)
			}
		}
	}
	d, err := ParseRichHTML(`<p style="text-align:right">שלום</p><p style="text-align:left">שלום</p>`)
	if err != nil || d.paragraph(0).Alignment != Start || d.paragraph(1).Alignment != End {
		t.Fatal("physical HTML alignment", d.Paragraphs(), err)
	}
	d, err = ParseRichRTF(`{\rtf1\ansi\ansicpg65001\qr שלום\par\ql שלום}`)
	if err != nil || d.paragraph(0).Alignment != Start || d.paragraph(1).Alignment != End {
		t.Fatal("physical RTF alignment", d.Paragraphs(), err)
	}
}

func FuzzRichDocumentEdits(f *testing.F) {
	f.Add("abc\nאבג é", "👍🏽", 1, 5)
	f.Add("é", "\u200d", 1, 1)
	f.Fuzz(func(t *testing.T, value, fragment string, start, end int) {
		if len(value)+len(fragment) > 4096 {
			return
		}
		d := NewRichDocument(value)
		out := d.WithStyle(TextRange{start, end}, RichStyle{Italic: true}).Replace(TextRange{start, end}, NewRichDocument(fragment))
		checkRichDocument(t, out)
	})
}

func FuzzRichInterchange(f *testing.F) {
	f.Add("hello 😀", `<p><b>hello</b></p>`, `{\rtf1\b hello}`)
	f.Fuzz(func(t *testing.T, plain, html, rtf string) {
		if len(plain)+len(html)+len(rtf) > 4096 {
			return
		}
		for _, d := range []RichDocument{NewRichDocument(plain)} {
			h, err := ParseRichHTML(d.HTML())
			if err != nil || h.String() != d.String() {
				t.Fatalf("HTML text round trip: %q -> %q (%v)", d.String(), h.String(), err)
			}
			r, err := ParseRichRTF(d.RTF())
			if err != nil || r.String() != d.String() {
				t.Fatalf("RTF text round trip: %q -> %q (%v)", d.String(), r.String(), err)
			}
		}
		if d, err := ParseRichHTML(html); err == nil {
			checkRichDocument(t, d)
		}
		if d, err := ParseRichRTF(rtf); err == nil {
			checkRichDocument(t, d)
		}
	})
}
