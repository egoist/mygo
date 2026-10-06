//go:build windows && (amd64 || arm64)

package windows

import (
	"fmt"
	"unsafe"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/textcheck"
)

var (
	clsidSpellCheckerFactory = guid("7ab36653-1796-484b-bdfa-e74f1db7c1dc")
	iidSpellCheckerFactory   = guid("8e018a9d-2415-4677-bf08-794ea61f94bb")
)

type windowsTextChecker struct {
	info    platform.TextServiceInfo
	checker uintptr
}

func (*Backend) NewTextChecker(language string) (platform.TextChecker, error) {
	var factory uintptr
	hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidSpellCheckerFactory)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidSpellCheckerFactory)), uintptr(unsafe.Pointer(&factory)))
	if failed(hr) || factory == 0 {
		return nil, platform.ErrUnsupported
	}
	defer release(factory)
	c := &windowsTextChecker{info: platform.TextServiceInfo{Provider: "Windows Spell Checking API"}}
	var langs uintptr
	if hr := comCall(factory, 3, uintptr(unsafe.Pointer(&langs))); failed(hr) {
		return c, hresultError("spell checking languages", hr)
	}
	var err error
	c.info.Languages, err = spellStrings(langs, 256)
	if err != nil {
		return c, err
	}
	c.info.Language = textcheck.MatchLanguage(language, c.info.Languages)
	if c.info.Language == "" {
		return c, fmt.Errorf("%w: %s", platform.ErrTextLanguageUnavailable, language)
	}
	hr = comCall(factory, 5, uintptr(unsafe.Pointer(u16(c.info.Language))), uintptr(unsafe.Pointer(&c.checker)))
	if failed(hr) || c.checker == 0 {
		return c, fmt.Errorf("%w: %s", platform.ErrTextLanguageUnavailable, language)
	}
	c.info.Spelling, c.info.Suggestions, c.info.Correction, c.info.LearnWord = true, true, true, true
	return c, nil
}

func (c *windowsTextChecker) Info() platform.TextServiceInfo { return c.info }
func (c *windowsTextChecker) Close()                         { release(c.checker); c.checker = 0 }

// spellStrings consumes and releases an IEnumString and its CoTaskMem strings.
func spellStrings(enumerator uintptr, limit int) ([]string, error) {
	if enumerator == 0 {
		return nil, nil
	}
	defer release(enumerator)
	var out []string
	for len(out) < limit {
		var p uintptr
		var fetched uint32
		hr := comCall(enumerator, 3, 1, uintptr(unsafe.Pointer(&p)), uintptr(unsafe.Pointer(&fetched)))
		s := takeWstr(p)
		if failed(hr) {
			return nil, hresultError("spell checking enumeration", hr)
		}
		if fetched == 0 {
			break
		}
		out = append(out, s)
	}
	return out, nil
}

func (c *windowsTextChecker) Check(text string, opts platform.TextCheckOptions, done func([]platform.TextIssue, error)) {
	if !opts.Spelling && !opts.Correction {
		done(nil, nil)
		return
	}
	var enumerator uintptr
	// ISpellChecker::ComprehensiveCheck, including the provider's automatic
	// replacements/deletions. Their indices are UTF-16, not rune offsets.
	hr := comCall(c.checker, 16, uintptr(unsafe.Pointer(u16(text))), uintptr(unsafe.Pointer(&enumerator)))
	if failed(hr) {
		done(nil, hresultError("spell checking", hr))
		return
	}
	defer release(enumerator)
	var issues []platform.TextIssue
	if enumerator != 0 {
		for {
			var entry uintptr
			hr := comCall(enumerator, 3, uintptr(unsafe.Pointer(&entry)))
			if failed(hr) {
				release(entry)
				done(nil, hresultError("spelling errors", hr))
				return
			}
			if entry == 0 {
				break
			}
			issue, include, err := c.issue(text, entry, opts)
			release(entry)
			if err != nil {
				done(nil, err)
				return
			}
			if include {
				issues = append(issues, issue)
			}
		}
	}
	done(issues, nil)
}

func (c *windowsTextChecker) issue(text string, entry uintptr, opts platform.TextCheckOptions) (issue platform.TextIssue, include bool, err error) {
	var start, length, action uint32
	for _, field := range []struct {
		method int
		value  *uint32
	}{{3, &start}, {4, &length}, {5, &action}} {
		if hr := comCall(entry, field.method, uintptr(unsafe.Pointer(field.value))); failed(hr) {
			return issue, false, hresultError("spelling error", hr)
		}
	}
	a, b, ok := textcheck.UTF16Range(text, int(start), int(length))
	if !ok || a == b || action == 0 {
		return
	}
	issue = platform.TextIssue{Start: a, End: b, Kind: platform.TextSpelling}
	switch action {
	case 1: // CORRECTIVE_ACTION_GET_SUGGESTIONS
		if !opts.Spelling {
			return issue, false, nil
		}
		var suggestions uintptr
		hr := comCall(c.checker, 5, uintptr(unsafe.Pointer(u16(textcheck.Slice(text, a, b)))), uintptr(unsafe.Pointer(&suggestions)))
		if failed(hr) {
			release(suggestions)
			return issue, false, hresultError("spelling suggestions", hr)
		}
		issue.Replacements, err = spellStrings(suggestions, 8)
	case 2, 3: // REPLACE / DELETE are confident native corrections.
		if opts.Correction {
			issue.Kind = platform.TextCorrection
		} else if !opts.Spelling {
			return issue, false, nil
		}
		replacement := ""
		if action == 2 {
			var p uintptr
			hr := comCall(entry, 6, uintptr(unsafe.Pointer(&p)))
			replacement = takeWstr(p)
			if failed(hr) {
				return issue, false, hresultError("spelling replacement", hr)
			}
		}
		issue.Replacements = []string{replacement}
	default:
		return issue, false, nil
	}
	return issue, err == nil, err
}

func (c *windowsTextChecker) LearnWord(word string) error {
	if hr := comCall(c.checker, 6, uintptr(unsafe.Pointer(u16(word)))); failed(hr) {
		return hresultError("learn spelling", hr)
	}
	return nil
}
