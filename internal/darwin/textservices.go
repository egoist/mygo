//go:build darwin

package darwin

import (
	"context"
	"fmt"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/textcheck"
)

var (
	msgTextCheck        func(id, objc.SEL, id, nsRange, uint64, id, int, objc.Block) int
	msgTextGuesses      func(id, objc.SEL, nsRange, id, id, int) id
	msgTextRange        func(id, objc.SEL) nsRange
	textCheckCompletion objc.Block
	textRequests        = map[int]macTextRequest{}
)

type macTextRequest struct {
	checker *macTextChecker
	text    string
	done    func([]platform.TextIssue, error)
}

// The one block and callback signature are allocated at application startup;
// NSSpellChecker's sequence number routes every request to its Go state.
func initTextServices() {
	purego.RegisterFunc(&msgTextCheck, msgSendAddr)
	purego.RegisterFunc(&msgTextGuesses, msgSendAddr)
	purego.RegisterFunc(&msgTextRange, msgSendAddr)
	textCheckCompletion = newBlock(func(_ objc.Block, sequence int, results, orthography id, words int) {
		// AppKit delivers this in an arbitrary context. Retaining the result
		// is thread-safe; all service calls, conversion and done run on main.
		send(results, "retain")
		theBackend.post(func() {
			defer release(results)
			r, ok := textRequests[sequence]
			if !ok {
				return
			}
			delete(textRequests, sequence)
			r.checker.pending--
			if r.checker.closing {
				// A cancelled snapshot needs no native suggestion generation.
				r.done(nil, context.Canceled)
				r.checker.Close()
				return
			}
			issues := r.checker.results(r.text, results)
			r.done(issues, nil)
			if r.checker.closing && r.checker.pending == 0 {
				r.checker.Close()
			}
		})
	})
}

type macTextChecker struct {
	info    platform.TextServiceInfo
	checker id // sharedSpellChecker, not owned
	tag     int
	pending int
	closing bool
}

func (b *Backend) NewTextChecker(language string) (platform.TextChecker, error) {
	var out *macTextChecker
	var err error
	withPool(func() {
		checker := send(class("NSSpellChecker"), "sharedSpellChecker")
		if checker == 0 {
			err = platform.ErrUnsupported
			return
		}
		info := platform.TextServiceInfo{Provider: "NSSpellChecker"}
		for _, item := range arrayItems(send(checker, "availableLanguages")) {
			info.Languages = append(info.Languages, textcheck.Language(goString(item)))
		}
		info.Language = textcheck.MatchLanguage(language, info.Languages)
		out = &macTextChecker{checker: checker, info: info}
		if info.Language == "" {
			err = fmt.Errorf("%w: %s", platform.ErrTextLanguageUnavailable, language)
			return
		}
		out.info.Spelling, out.info.Suggestions, out.info.Correction = true, true, true
		out.info.SmartQuotes, out.info.SmartDashes, out.info.TextReplacement = true, true, true
		out.info.LearnWord = true
		out.tag = sendInt(class("NSSpellChecker"), "uniqueSpellDocumentTag")
	})
	return out, err
}

func (c *macTextChecker) Info() platform.TextServiceInfo { return c.info }
func (c *macTextChecker) Close() {
	c.closing = true
	if c.tag != 0 && c.pending == 0 {
		send(c.checker, "closeSpellDocumentWithTag:", uintptr(c.tag))
		c.tag = 0
	}
}

func (c *macTextChecker) Check(text string, opts platform.TextCheckOptions, done func([]platform.TextIssue, error)) {
	withPool(func() {
		var types uint64
		if opts.Spelling {
			types |= 1 << 1
		}
		if opts.Correction {
			types |= 1 << 9
		}
		if opts.SmartQuotes {
			types |= 1 << 6
		}
		if opts.SmartDashes {
			types |= 1 << 7
		}
		if opts.TextReplacement {
			types |= 1 << 8
		}
		options := send(class("NSMutableDictionary"), "dictionary")
		ortho := send(class("NSOrthography"), "defaultOrthographyForLanguage:", uintptr(nsString(c.info.Language)))
		send(options, "setObject:forKey:", uintptr(ortho), uintptr(appKitString("NSTextCheckingOrthographyKey")))
		if opts.SmartQuotes {
			quotes := send(c.checker, "userQuotesArrayForLanguage:", uintptr(nsString(c.info.Language)))
			if quotes != 0 {
				send(options, "setObject:forKey:", uintptr(quotes), uintptr(appKitString("NSTextCheckingQuotesKey")))
			}
		}
		s := nsString(text)
		c.pending++
		sequence := msgTextCheck(c.checker, sel("requestCheckingOfString:range:types:options:inSpellDocumentWithTag:completionHandler:"), s,
			nsRange{Length: uint(sendInt(s, "length"))}, types, options, c.tag, textCheckCompletion)
		textRequests[sequence] = macTextRequest{c, text, done}
	})
}

func (c *macTextChecker) results(text string, results id) []platform.TextIssue {
	var issues []platform.TextIssue
	for _, result := range arrayItems(results) {
		r := msgTextRange(result, sel("range"))
		a, b, ok := textcheck.UTF16Range(text, int(r.Location), int(r.Length))
		if !ok || a == b {
			continue
		}
		issue := platform.TextIssue{Start: a, End: b}
		switch uint64(send(result, "resultType")) {
		case 1 << 1:
			issue.Kind = platform.TextSpelling
			guesses := msgTextGuesses(c.checker, sel("guessesForWordRange:inString:language:inSpellDocumentWithTag:"), r, nsString(text), nsString(c.info.Language), c.tag)
			for _, guess := range arrayItems(guesses) {
				issue.Replacements = append(issue.Replacements, goString(guess))
				if len(issue.Replacements) == 8 {
					break
				}
			}
		case 1 << 9:
			issue.Kind = platform.TextCorrection
		case 1 << 6:
			issue.Kind = platform.TextQuote
		case 1 << 7:
			issue.Kind = platform.TextDash
		case 1 << 8:
			issue.Kind = platform.TextReplacement
		default:
			continue
		}
		if issue.Kind != platform.TextSpelling {
			if replacement := send(result, "replacementString"); replacement != 0 {
				issue.Replacements = []string{goString(replacement)}
			}
		}
		issues = append(issues, issue)
	}
	return issues
}

func (c *macTextChecker) LearnWord(word string) error {
	withPool(func() { send(c.checker, "learnWord:", uintptr(nsString(word))) })
	return nil
}
