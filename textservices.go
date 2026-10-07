package mygo

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/textcheck"
)

// TextServices checks text with the operating system's installed services.
// Methods are safe from any goroutine and require a running application.
var TextServices textServicesModule

type textServicesModule struct{}

// TextServiceInfo reports installed dictionaries and feature availability.
type TextServiceInfo = platform.TextServiceInfo

// TextCheckOptions selects spelling, corrections and native substitutions.
// Unsupported options are skipped; inspect TextServiceInfo for availability.
type TextCheckOptions = platform.TextCheckOptions

// TextIssue is a suggestion for [Start, End), in runes of TextCheckResult.Text.
type TextIssue = platform.TextIssue

// TextIssueKind identifies spelling, correction, quote, dash or replacement results.
type TextIssueKind = platform.TextIssueKind

const (
	TextSpelling    = platform.TextSpelling
	TextCorrection  = platform.TextCorrection
	TextQuote       = platform.TextQuote
	TextDash        = platform.TextDash
	TextReplacement = platform.TextReplacement
)

// TextCheckResult retains the checked snapshot. Check never edits it.
type TextCheckResult = platform.TextCheckResult

// ErrTextLanguageUnavailable means there is no installed dictionary for the language.
var ErrTextLanguageUnavailable = platform.ErrTextLanguageUnavailable

// ErrTextServicesUnavailable means no requested native text service is available.
var ErrTextServicesUnavailable = platform.ErrUnsupported

// Info returns the actual dictionary selected, installed language tags and
// individual capabilities. An empty language uses App.Locale. Missing services
// return ErrTextServicesUnavailable; missing dictionaries return ErrTextLanguageUnavailable
// with the installed Languages. No silent fallback to another language occurs.
func (textServicesModule) Info(language string) (info TextServiceInfo, err error) {
	validateTextLanguage(language)
	needsApp("TextServices.Info")
	err = errLoopStopped
	onMain(func() {
		if textServicesStopped {
			return
		}
		checker, e := newTextChecker(language)
		err = e
		if checker != nil {
			defer checker.Close()
			info = checker.Info()
			info.Languages = slices.Clone(info.Languages)
		}
	})
	return
}

func newTextChecker(language string) (platform.TextChecker, error) {
	if language == "" {
		language = backend().App().Locale()
	}
	return backend().NewTextChecker(textcheck.Language(language))
}

func validateTextLanguage(language string) {
	for _, r := range language {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			panic("mygo: text service language must be a language tag")
		}
	}
}

// Check checks an immutable snapshot, at most 8 MiB, yielding between bounded
// native requests. It is cancellable, including from the main thread where it
// pumps events. Results use rune offsets, never UTF-16 or byte offsets. At most
// 1024 issues are returned; Truncated also reports omitted oversized tokens.
// Zero options return no issues. A request whose selected services are all
// unavailable returns ErrTextServicesUnavailable.
func (textServicesModule) Check(ctx context.Context, text string, opts TextCheckOptions) (TextCheckResult, error) {
	if ctx == nil {
		panic("mygo: TextServices.Check requires a context")
	}
	validateTextLanguage(opts.Language)
	needsApp("TextServices.Check")
	type answer struct {
		result TextCheckResult
		err    error
	}
	ch := make(chan answer, 1)
	startTextCheck(ctx, text, opts, func(r TextCheckResult, e error) { deliver(ch, answer{r, e}) },
		func(e error) { deliver(ch, answer{TextCheckResult{Text: text}, e}) })
	a := await(ch)
	return a.result, a.err
}

// LearnWord adds a word to the native user's dictionary. This is persistent
// and affects other applications using that dictionary; UI Ignore Spelling
// instead ignores a word only in the current input and language.
func (textServicesModule) LearnWord(word, language string) (err error) {
	validateTextLanguage(language)
	if word == "" || !utf8.ValidString(word) || strings.ContainsRune(word, 0) || len(word) > 1024 {
		return errors.New("mygo: invalid dictionary word")
	}
	needsApp("TextServices.LearnWord")
	err = errLoopStopped
	onMain(func() {
		if textServicesStopped {
			return
		}
		checker, e := newTextChecker(language)
		if checker != nil {
			defer checker.Close()
		}
		if e != nil {
			err = e
			return
		}
		if !checker.Info().LearnWord {
			err = ErrTextServicesUnavailable
			return
		}
		err = checker.LearnWord(word)
	})
	return
}

func beginTextCheck(ctx context.Context, text string, opts TextCheckOptions, done func(TextCheckResult, error)) {
	startTextCheck(ctx, text, opts, done, nil)
}

func startTextCheck(ctx context.Context, text string, opts TextCheckOptions, done func(TextCheckResult, error), notPosted func(error)) {
	// Validation and splitting may scan megabytes: neither runs in a frame.
	go func() {
		var chunks []textcheck.Chunk
		var truncated bool
		err := ctx.Err()
		if err == nil && (len(text) > 8<<20 || !utf8.ValidString(text) || strings.ContainsRune(text, 0)) {
			err = errors.New("mygo: text checking requires valid UTF-8 without NUL, at most 8 MiB")
		}
		if err == nil {
			chunks, truncated = textcheck.Chunks(text, platform.TextCheckChunkRunes)
		}
		posted := postMain(func() {
			j := &textCheckJob{ctx: ctx, opts: opts, chunks: chunks, done: done, result: TextCheckResult{Text: text, Truncated: truncated}}
			if textServicesStopped {
				j.finish(errLoopStopped)
				return
			}
			if err != nil {
				j.finish(err)
				return
			}
			if e := ctx.Err(); e != nil {
				j.finish(e)
				return
			}
			requested := opts.Spelling || opts.Correction || opts.SmartQuotes || opts.SmartDashes || opts.TextReplacement
			if !requested {
				j.finish(nil)
				return
			}
			j.checker, err = newTextChecker(opts.Language)
			if j.checker != nil {
				j.result.Info = j.checker.Info()
				j.result.Info.Languages = slices.Clone(j.result.Info.Languages)
			}
			if err != nil {
				j.finish(err)
				return
			}
			i := j.result.Info
			available := opts.Spelling && i.Spelling || opts.Correction && i.Correction || opts.SmartQuotes && i.SmartQuotes || opts.SmartDashes && i.SmartDashes || opts.TextReplacement && i.TextReplacement
			if requested && !available {
				j.finish(ErrTextServicesUnavailable)
				return
			}
			j.stop = context.AfterFunc(ctx, func() { postMain(func() { j.finish(ctx.Err()) }) })
			textCheckJobs[j] = true
			j.opts.Spelling = opts.Spelling && i.Spelling
			j.opts.Correction = opts.Correction && i.Correction
			j.opts.SmartQuotes = opts.SmartQuotes && i.SmartQuotes
			j.opts.SmartDashes = opts.SmartDashes && i.SmartDashes
			j.opts.TextReplacement = opts.TextReplacement && i.TextReplacement
			j.next()
		})
		if !posted && notPosted != nil {
			notPosted(errLoopStopped)
		}
	}()
}

var textCheckJobs = map[*textCheckJob]bool{} // main-thread only
var textServicesStopped bool

func stopTextServices() {
	textServicesStopped = true
	for job := range textCheckJobs {
		job.finish(errLoopStopped)
	}
}

// textCheckJob is main-thread state. Timers only post to it. No synchronous
// service receives the whole document, and a yield separates native calls.
type textCheckJob struct {
	ctx      context.Context
	opts     TextCheckOptions
	checker  platform.TextChecker
	chunks   []textcheck.Chunk
	result   TextCheckResult
	done     func(TextCheckResult, error)
	stop     func() bool
	finished bool
}

func (j *textCheckJob) finish(err error) {
	if j.finished {
		return
	}
	j.finished = true
	delete(textCheckJobs, j)
	if j.stop != nil {
		j.stop()
	}
	if j.checker != nil {
		j.checker.Close()
	}
	// Errors never present a partial result as a completed check.
	if err != nil {
		j.result.Issues = nil
	}
	j.done(j.result, err)
	j.done = nil
	j.chunks = nil
}

func (j *textCheckJob) next() {
	if j.finished {
		return
	}
	if err := j.ctx.Err(); err != nil {
		j.finish(err)
		return
	}
	if len(j.chunks) == 0 {
		j.finish(nil)
		return
	}
	c := j.chunks[0]
	j.chunks = j.chunks[1:]
	j.checker.Check(c.Text, j.opts, func(issues []TextIssue, err error) {
		if j.finished {
			return
		}
		if err != nil {
			j.finish(fmt.Errorf("mygo: text checking: %w", err))
			return
		}
		n := utf8.RuneCountInString(c.Text)
		for _, issue := range issues {
			if issue.Start < 0 || issue.End <= issue.Start || issue.End > n || issue.Kind > TextReplacement {
				continue
			}
			if !(issue.Kind == TextSpelling && j.opts.Spelling || issue.Kind == TextCorrection && j.opts.Correction || issue.Kind == TextQuote && j.opts.SmartQuotes || issue.Kind == TextDash && j.opts.SmartDashes || issue.Kind == TextReplacement && j.opts.TextReplacement) {
				continue
			}
			if len(j.result.Issues) == 1024 {
				j.result.Truncated = true
				j.finish(nil)
				return
			}
			issue.Original = strings.Clone(textcheck.Slice(c.Text, issue.Start, issue.End))
			issue.Start += c.Start
			issue.End += c.Start
			// Native-owned suggestion arrays never escape their callback.
			issue.Replacements = append([]string(nil), issue.Replacements...)
			j.result.Issues = append(j.result.Issues, issue)
		}
		if len(j.chunks) == 0 {
			j.finish(nil)
			return
		}
		time.AfterFunc(time.Millisecond, func() { postMain(j.next) })
	})
}
