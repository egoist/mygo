// Package fuzzy matches what a user types against the items of a list, as
// a command palette, a file finder or a code editor's completions do: the
// query's letters in order, anywhere in the item, whatever their case.
//
//	p := fuzzy.NewPattern("gbi")
//	score, ok := p.Score("getElementById") // true: each letter starts a word
//	at := p.Positions("getElementById")    // [0 10 12], the letters to bold
//
// A higher score is a better match. A letter counts more at the start of
// the item, at the start of a word in it (after a space or punctuation, or
// a capital after a small letter), right after the letter matched before
// it, and in the last part of a path, after its last slash; gaps between
// the letters and a longer item count against it, and each letter in the
// query's own case a little for it. Scores compare only between the items
// one pattern matched.
//
// The match starts where the shortest stretch of the item holding the
// query does, as fzf's first algorithm finds it, or in a path's last part
// when that holds the query. From there each letter is taken right after
// the one before, or else at the next word it starts, or else at its next
// occurrence; if that leaves letters unmatched, each is taken at its next
// occurrence. So "ab" in "axb b" takes the a and the last b.
package fuzzy

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	scoreLetter      = 160
	bonusWordStart   = 100
	bonusFirstLetter = 80
	bonusAdjacent    = 60
	bonusRun         = 20
	bonusSameCase    = 10
	bonusLastPart    = 200
	penaltyGapRune   = 10
	maxGapRunes      = 20
	maxPenaltyLength = 200
)

// The slots of a query's runes in wordStarts.
const (
	maxSlots  = 32
	noWord    = -1 // the rune starts no word, not being a letter or digit
	pastSlots = -2 // more different letters than wordStarts holds
)

// Pattern is a query made ready to match many items. It is only read, so
// several goroutines may match with one.
type Pattern struct {
	exact  []rune // the query's runes as typed
	folded []rune // and without their case
	// slots holds, for each rune of the query, where wordStarts keeps the
	// last word it starts in an item, or noWord or pastSlots; starters,
	// the rune without its case of each slot.
	slots, starters []rune
	ascii           bool // every rune of folded is ASCII
}

// NewPattern makes query ready to match: once per change of the query,
// not per item.
func NewPattern(query string) Pattern {
	n := utf8.RuneCountInString(query)
	runes := make([]rune, 3*n+min(n, maxSlots))
	p := Pattern{exact: runes[:0:n], folded: runes[n : n : 2*n], slots: runes[2*n : 2*n : 3*n], starters: runes[3*n : 3*n], ascii: true}
	for _, r := range query {
		f := fold(r, false)
		p.exact = append(p.exact, r)
		p.folded = append(p.folded, f)
		p.ascii = p.ascii && f < utf8.RuneSelf
		slot := rune(noWord)
		if isLetterOrDigit(f) {
			slot = pastSlots
			for k, st := range p.starters {
				if st == f {
					slot = rune(k)
					break
				}
			}
			if slot == pastSlots && len(p.starters) < maxSlots {
				slot = rune(len(p.starters))
				p.starters = append(p.starters, f)
			}
		}
		p.slots = append(p.slots, slot)
	}
	return p
}

// Score reports whether s matches and how well; an empty query matches
// everything, with a score of 0. It allocates nothing, to rank a list of
// thousands as the user types, and takes time in proportion to the length
// of s, but for queries of more than 32 different letters, which may look
// through s once for each.
func (p Pattern) Score(s string) (int, bool) {
	score, ok, _ := p.match(s, nil)
	return score, ok
}

// Positions returns the byte offsets in s of the runes the query matched,
// one per rune of the query, in order, for the rows shown to bold after
// Score ranked them. It returns nil when s does not match, and for an
// empty query, which matches everything without a rune.
func (p Pattern) Positions(s string) []int {
	if len(p.folded) == 0 {
		return nil
	}
	at := make([]int, len(p.folded))
	if _, ok, _ := p.match(s, at); !ok {
		return nil
	}
	return at
}

// match scores s, writing the byte offsets of the runes matched to at
// when it is not nil, and counts the runes it looked at, for tests.
func (p *Pattern) match(s string, at []int) (score int, ok bool, steps int) {
	n := len(p.folded)
	if n == 0 {
		return 0, true, 0
	}
	// Forward: the earliest end of a match.
	qi, end := 0, -1
	for i := 0; i < len(s); {
		steps++
		r, size := rune(s[i]), 1
		if r >= utf8.RuneSelf {
			r, size = utf8.DecodeRuneInString(s[i:])
		}
		if fold(r, p.ascii) == p.folded[qi] {
			qi++
			if qi == n {
				end = i + size
				break
			}
		}
		i += size
	}
	if end < 0 {
		return 0, false, steps
	}
	// Backward from there: the latest start, for the shortest stretch.
	start := end
	for qi = n - 1; qi >= 0; {
		steps++
		r, size := utf8.DecodeLastRuneInString(s[:start])
		start -= size
		if fold(r, p.ascii) == p.folded[qi] {
			qi--
		}
	}

	var w wordStarts
	// A match wholly after the last slash, in a path's file name, scores
	// better.
	last := strings.LastIndexByte(s, '/') + 1
	first := 0
	if last > start {
		score, first, ok = p.from(s, last, &w, at)
	}
	if !ok {
		score, first, _ = p.from(s, start, &w, at)
	}
	if first >= last {
		score += bonusLastPart
	}
	// Shorter items win ties.
	return score - min(utf8.RuneCountInString(s), maxPenaltyLength), true, steps + w.steps
}

// from matches from byte from, preferring starts of words and runs, or,
// if that leaves runes unmatched, the first occurrences.
func (p *Pattern) from(s string, from int, w *wordStarts, at []int) (score, first int, ok bool) {
	if score, first, ok = p.pass(s, from, true, w, at); ok {
		return score, first, true
	}
	return p.pass(s, from, false, w, at)
}

// pass matches the query's runes in turn from byte from. Preferring
// starts, it takes each right after the rune before, or else at the next
// word it starts, or else at its next occurrence; without, at its next
// occurrence.
func (p *Pattern) pass(s string, from int, preferStarts bool, w *wordStarts, at []int) (score, first int, ok bool) {
	next, prev, run, steps := from, -1, 0, 0
	for qi, want := range p.folded {
		before, _ := utf8.DecodeLastRuneInString(s[:next])
		found, wordStart, sameCase := -1, false, false
		for i := next; i < len(s); {
			steps++
			r, size := rune(s[i]), 1
			if r >= utf8.RuneSelf {
				r, size = utf8.DecodeRuneInString(s[i:])
			}
			if fold(r, p.ascii) == want {
				start := isWordStart(before, r, i)
				adjacent := i == next && prev >= 0
				if found < 0 || start || adjacent {
					found, wordStart, sameCase = i, start, r == p.exact[qi]
				}
				if !preferStarts || start || adjacent || !w.after(p, s, qi, i) {
					break
				}
			}
			before = r
			i += size
		}
		if found < 0 {
			w.steps += steps
			return 0, 0, false
		}
		bonus := 0
		if wordStart {
			bonus = bonusWordStart
		}
		switch {
		case prev < 0:
			first, run = found, bonus
			if found == 0 {
				bonus += bonusFirstLetter
			}
		case found == next:
			// A run keeps the bonus its first rune earned, so a word
			// typed whole beats the same letters at starts of words.
			run = max(run, bonus)
			bonus = max(bonusAdjacent, run) + bonusRun
		default:
			score -= min(utf8.RuneCountInString(s[next:found]), maxGapRunes) * penaltyGapRune
			run = bonus
		}
		if sameCase {
			bonus += bonusSameCase
		}
		score += scoreLetter + bonus
		if at != nil {
			at[qi] = found
		}
		_, size := utf8.DecodeRuneInString(s[found:])
		prev, next = found, found+size
	}
	w.steps += steps
	return score, first, true
}

// wordStarts holds where the last word each letter of the query starts in
// an item is, found once a pass first looks past an occurrence of one for
// a word it starts, so that passes stop where there is none left; and how
// many runes the match looked at, which tests count.
type wordStarts struct {
	found bool
	last  [maxSlots]int // one more than the byte offset, or 0
	steps int
}

// after reports whether rune qi of the query starts a word in s after
// byte i.
func (w *wordStarts) after(p *Pattern, s string, qi, i int) bool {
	switch slot := p.slots[qi]; slot {
	case noWord:
		return false
	case pastSlots:
		return true
	default:
		if !w.found {
			w.find(p, s)
		}
		return w.last[slot] > i+1
	}
}

func (w *wordStarts) find(p *Pattern, s string) {
	w.found = true
	var before rune
	for j := 0; j < len(s); {
		w.steps++
		r, size := rune(s[j]), 1
		if r >= utf8.RuneSelf {
			r, size = utf8.DecodeRuneInString(s[j:])
		}
		if isWordStart(before, r, j) {
			f := fold(r, p.ascii)
			for k, st := range p.starters {
				if st == f {
					w.last[k] = j + 1
					break
				}
			}
		}
		before = r
		j += size
	}
}

// fold is r without its case: the least of the runes that fold together,
// so that Σ, σ and ς are one, and K, k and the Kelvin sign; İ, which
// folds with no other, is I. With ascii, it folds only what folds to
// ASCII, all an ASCII query compares with.
func fold(r rune, ascii bool) rune {
	if r < utf8.RuneSelf {
		if uint32(r-'a') < 26 {
			r -= 'a' - 'A'
		}
		return r
	}
	return foldNonASCII(r, ascii)
}

func foldNonASCII(r rune, ascii bool) rune {
	switch {
	case r == '\u017f': // ſ, the long s
		return 'S'
	case r == '\u212a': // K, the Kelvin sign
		return 'K'
	case r == '\u0130': // İ, whose small letter is i
		return 'I'
	case ascii:
		return r
	}
	least := r
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		least = min(least, f)
	}
	return least
}

// isWordStart reports whether r, at byte i after before, starts a word:
// a letter or digit first in the item or after something else, or a
// capital after a small letter.
func isWordStart(before, r rune, i int) bool {
	if r|before < utf8.RuneSelf {
		return isASCIIWord(r) && (i == 0 || !isASCIIWord(before) || uint32(r-'A') < 26 && uint32(before-'a') < 26)
	}
	return isWordStartNonASCII(before, r, i)
}

func isWordStartNonASCII(before, r rune, i int) bool {
	if !isLetterOrDigit(r) {
		return false
	}
	return i == 0 || !isLetterOrDigit(before) || unicode.IsUpper(r) && unicode.IsLower(before)
}

// isASCIIWord reports whether r, an ASCII rune, is a letter or a digit.
func isASCIIWord(r rune) bool {
	const digits, letters = 0x03ff_0000_0000_0000, 0x07ff_fffe_07ff_fffe
	return [2]uint64{digits, letters}[r>>6]>>(r&63)&1 != 0
}

func isLetterOrDigit(r rune) bool {
	if r < utf8.RuneSelf {
		return isASCIIWord(r)
	}
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}
