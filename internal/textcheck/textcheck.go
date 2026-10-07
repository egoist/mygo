// Package textcheck shares range conversion and bounded checking mechanics.
// Native services, rather than a bundled dictionary, decide spelling.
package textcheck

import (
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

func Language(s string) string { return strings.ReplaceAll(s, "_", "-") }

// MatchLanguage prefers an exact locale, then a dictionary for its language.
// The returned tag names the actual installed dictionary.
func MatchLanguage(want string, available []string) string {
	want = Language(want)
	for _, s := range available {
		if strings.EqualFold(want, Language(s)) {
			return Language(s)
		}
	}
	base, _, _ := strings.Cut(want, "-")
	for _, s := range available {
		b, _, _ := strings.Cut(Language(s), "-")
		if strings.EqualFold(base, b) {
			return Language(s)
		}
	}
	return ""
}

type Chunk struct {
	Text  string
	Start int
}

// Chunks splits at whitespace or punctuation. Overlong tokens are omitted,
// rather than sent to synchronous services or reported as split misspellings.
// Call this off the UI thread for long text.
func Chunks(s string, limit int) (chunks []Chunk, truncated bool) {
	start, runeStart, count, lastBreak, breakCount := 0, 0, 0, 0, 0
	for at, r := range s {
		boundary := unicode.IsSpace(r) || (!wordRune(r) && r != '\'' && r != '’')
		if count == limit {
			if lastBreak > start {
				chunks = append(chunks, Chunk{s[start:lastBreak], runeStart})
				start, runeStart, count = lastBreak, runeStart+breakCount, count-breakCount
				lastBreak, breakCount = start, 0
			} else if boundary {
				chunks = append(chunks, Chunk{s[start:at], runeStart})
				start, runeStart, count = at, runeStart+count, 0
				lastBreak, breakCount = start, 0
			} else {
				// Skip this whole token. Its tail must not be checked alone.
				truncated = true
			}
		}
		count++
		if boundary {
			end := at + utf8.RuneLen(r)
			if count > limit {
				start, runeStart, count = end, runeStart+count, 0
			}
			lastBreak, breakCount = end, count
		}
	}
	if start < len(s) {
		if count > limit {
			truncated = true
		} else {
			chunks = append(chunks, Chunk{s[start:], runeStart})
		}
	}
	return
}

func wordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsMark(r) || unicode.IsDigit(r) }

// Words gives rune ranges of words for dictionary-based services. Apostrophes
// inside words are retained, including typographic apostrophes.
func Words(s string) [][2]int {
	runes := []rune(s)
	var out [][2]int
	for i := 0; i < len(runes); {
		if !unicode.IsLetter(runes[i]) {
			i++
			continue
		}
		a := i
		i++
		for i < len(runes) && (wordRune(runes[i]) || (runes[i] == '\'' || runes[i] == '’') && i+1 < len(runes) && unicode.IsLetter(runes[i+1])) {
			i++
		}
		out = append(out, [2]int{a, i})
	}
	return out
}

// UTF16Range rejects offsets in surrogate pairs, overflow and out-of-bounds
// ranges instead of silently replacing a different character.
func UTF16Range(s string, start, length int) (a, b int, ok bool) {
	if start < 0 || length < 0 || start > int(^uint(0)>>1)-length {
		return
	}
	a, b = -1, -1
	u, n := 0, 0
	for _, r := range s {
		if u == start {
			a = n
		}
		if u == start+length {
			b = n
		}
		u += utf16.RuneLen(r)
		n++
	}
	if u == start {
		a = n
	}
	if u == start+length {
		b = n
	}
	return a, b, a >= 0 && b >= a
}

func Slice(s string, a, b int) string {
	start, end := len(s), len(s)
	n := 0
	for at := range s {
		if n == a {
			start = at
		}
		if n == b {
			end = at
			break
		}
		n++
	}
	return s[start:end]
}
