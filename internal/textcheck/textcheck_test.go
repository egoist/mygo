package textcheck

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestUTF16Range(t *testing.T) {
	s := "😀 e\u0301 teh"
	for _, tt := range []struct {
		start, length, a, b int
		ok                  bool
	}{
		{0, 2, 0, 1, true}, {3, 2, 2, 4, true}, {6, 3, 5, 8, true},
		{1, 1, 0, 0, false}, {0, 1, 0, 0, false}, {9, 1, 0, 0, false}, {-1, 2, 0, 0, false}, {0, -1, 0, 0, false},
	} {
		a, b, ok := UTF16Range(s, tt.start, tt.length)
		if ok != tt.ok || ok && (a != tt.a || b != tt.b) {
			t.Errorf("range %d+%d = %d:%d,%v", tt.start, tt.length, a, b, ok)
		}
	}
}

func TestChunks(t *testing.T) {
	for _, s := range []string{"", strings.Repeat("😀 teh café.\n", 100), strings.Repeat("a", 512), strings.Repeat("a", 512) + " end", "don't stop", strings.Repeat("word ", 300)} {
		chunks, truncated := Chunks(s, 512)
		if truncated {
			t.Errorf("unexpected truncation of %d runes", utf8.RuneCountInString(s))
		}
		var joined strings.Builder
		n := 0
		for _, chunk := range chunks {
			if chunk.Start != n || utf8.RuneCountInString(chunk.Text) > 512 {
				t.Fatalf("invalid chunk %+v", chunk)
			}
			joined.WriteString(chunk.Text)
			n += utf8.RuneCountInString(chunk.Text)
		}
		if joined.String() != s {
			t.Error("chunks changed text")
		}
	}
	s := "teh " + strings.Repeat("a", 513) + " teh"
	chunks, truncated := Chunks(s, 512)
	if !truncated || len(chunks) != 2 || chunks[0].Text != "teh " || chunks[1].Text != "teh" || chunks[1].Start != 518 {
		t.Fatalf("oversized token: %+v, truncated=%v", chunks, truncated)
	}
}

func TestWordsAndLanguage(t *testing.T) {
	s := "😀 don't l’amour cafe\u0301 123 --"
	var words []string
	for _, r := range Words(s) {
		words = append(words, Slice(s, r[0], r[1]))
	}
	if strings.Join(words, "/") != "don't/l’amour/cafe\u0301" {
		t.Fatal(words)
	}
	langs := []string{"en_US", "en-GB", "fr-FR"}
	if MatchLanguage("EN-us", langs) != "en-US" || MatchLanguage("en-GB", langs) != "en-GB" || MatchLanguage("fr", langs) != "fr-FR" || MatchLanguage("zz", langs) != "" {
		t.Fatal("language matching")
	}
}

func FuzzChunks(f *testing.F) {
	f.Add("😀 don't split words.\n", 64)
	f.Add(strings.Repeat("a", 65)+" end", 64)
	f.Fuzz(func(t *testing.T, s string, limit int) {
		if !utf8.ValidString(s) || len(s) > 64<<10 {
			t.Skip()
		}
		limit = 8 + int(uint(limit)%128)
		chunks, truncated := Chunks(s, limit)
		runes := []rune(s)
		prev := 0
		var joined strings.Builder
		for _, c := range chunks {
			n := utf8.RuneCountInString(c.Text)
			if n > limit || c.Start < prev || c.Start+n > len(runes) || string(runes[c.Start:c.Start+n]) != c.Text {
				t.Fatalf("bad chunk %+v", c)
			}
			joined.WriteString(c.Text)
			prev = c.Start + n
		}
		if !truncated && joined.String() != s {
			t.Fatal("changed text without reporting truncation")
		}
	})
}
