package platform

import "errors"

// ErrTextLanguageUnavailable means the service has no dictionary for the
// requested language. An unavailable service returns ErrUnsupported instead.
var ErrTextLanguageUnavailable = errors.New("text checking language unavailable")

// TextServiceInfo describes installed services for Language. Languages lists
// installed language tags. Unsupported features are always false.
type TextServiceInfo struct {
	Language, Provider                        string
	Languages                                 []string
	Spelling, Suggestions, Correction         bool
	SmartQuotes, SmartDashes, TextReplacement bool
	LearnWord                                 bool
}

// TextCheckOptions selects checks. The empty language uses App.Locale in the
// core; a backend receives the resolved language. Zero options check nothing.
type TextCheckOptions struct {
	Language                                  string
	Spelling, Correction                      bool
	SmartQuotes, SmartDashes, TextReplacement bool
}

type TextIssueKind uint8

const (
	TextSpelling TextIssueKind = iota
	TextCorrection
	TextQuote
	TextDash
	TextReplacement
)

// TextIssue addresses committed text in rune offsets, [Start, End).
// Replacements are ordered suggestions; an empty string can mean deletion.
type TextIssue struct {
	Start, End   int
	Kind         TextIssueKind
	Original     string
	Replacements []string
}

// TextCheckResult belongs to precisely Text, never to a later revision.
// Truncated means oversized words or results beyond the limit were omitted.
type TextCheckResult struct {
	Text      string
	Info      TextServiceInfo
	Issues    []TextIssue
	Truncated bool
}

// TextCheckChunkRunes bounds synchronous platform work. The core splits long
// text without splitting words, and yields between chunks.
const TextCheckChunkRunes = 512

// TextChecker is an owned checking session. All calls and done callbacks run
// on the main thread. Check checks at most TextCheckChunkRunes runes and calls
// done exactly once, possibly before returning. Close releases the session;
// an asynchronous backend defers final release until its outstanding call
// finishes. Native callbacks are shared, never allocated per session.
type TextChecker interface {
	Info() TextServiceInfo
	Check(text string, opts TextCheckOptions, done func([]TextIssue, error))
	LearnWord(word string) error
	Close()
}
