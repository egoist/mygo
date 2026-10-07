package mygo

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/textcheck"
)

type testTextChecker struct {
	info   TextServiceInfo
	check  func(string, TextCheckOptions, func([]TextIssue, error))
	learn  func(string) error
	closed int
}

func (c *testTextChecker) Info() TextServiceInfo {
	if !isMainThread() {
		panic("Info off main")
	}
	return c.info
}
func (c *testTextChecker) Check(s string, o TextCheckOptions, done func([]TextIssue, error)) {
	if !isMainThread() {
		panic("Check off main")
	}
	c.check(s, o, done)
}
func (c *testTextChecker) LearnWord(word string) error {
	if !isMainThread() {
		panic("Learn off main")
	}
	return c.learn(word)
}
func (c *testTextChecker) Close() {
	if !isMainThread() {
		panic("Close off main")
	}
	c.closed++
}

func withTextFactory(t *testing.T, f func(string) (platform.TextChecker, error)) {
	t.Helper()
	onMain(func() { fb.TextCheckerFactory = f })
	t.Cleanup(func() { onMain(func() { fb.TextCheckerFactory = nil }) })
}
func englishTextInfo() TextServiceInfo {
	return TextServiceInfo{Language: "en-US", Languages: []string{"en-US", "fr-FR"}, Provider: "test", Spelling: true, Suggestions: true, LearnWord: true}
}

func TestTextServicesInfoAndUnavailable(t *testing.T) {
	if _, err := TextServices.Info("en-US"); !errors.Is(err, ErrTextServicesUnavailable) {
		t.Fatal(err)
	}
	withTextFactory(t, func(language string) (platform.TextChecker, error) {
		if language != "en-US" {
			return &testTextChecker{info: TextServiceInfo{Languages: []string{"en-US"}}}, ErrTextLanguageUnavailable
		}
		return &testTextChecker{info: englishTextInfo()}, nil
	})
	info, err := TextServices.Info("")
	if err != nil || info.Language != "en-US" || !info.Spelling || info.SmartQuotes {
		t.Fatalf("info %+v: %v", info, err)
	}
	info, err = TextServices.Info("zz-ZZ")
	if !errors.Is(err, ErrTextLanguageUnavailable) || len(info.Languages) != 1 {
		t.Fatalf("missing language %+v: %v", info, err)
	}
}

func TestTextServicesCheckRangesAndYield(t *testing.T) {
	var checker *testTextChecker
	calls := 0
	withTextFactory(t, func(language string) (platform.TextChecker, error) {
		checker = &testTextChecker{info: englishTextInfo(), check: func(s string, o TextCheckOptions, done func([]TextIssue, error)) {
			calls++
			if utf8.RuneCountInString(s) > platform.TextCheckChunkRunes {
				t.Error("unbounded native input")
			}
			var issues []TextIssue
			for _, r := range textcheck.Words(s) {
				if textcheck.Slice(s, r[0], r[1]) == "teh" {
					issues = append(issues, TextIssue{Start: r[0], End: r[1], Kind: TextSpelling, Replacements: []string{"the"}})
				}
			}
			// A malformed native range must never escape to an editor.
			issues = append(issues, TextIssue{Start: -1, End: 1})
			done(issues, nil)
		}}
		return checker, nil
	})
	s := strings.Repeat("😀 teh café.\n", 200)
	r, err := TextServices.Check(context.Background(), s, TextCheckOptions{Spelling: true, SmartQuotes: true})
	if err != nil || r.Text != s || len(r.Issues) != 200 || r.Truncated {
		t.Fatalf("check: %d issues, %v", len(r.Issues), err)
	}
	for _, i := range r.Issues {
		if textcheck.Slice(s, i.Start, i.End) != "teh" || i.Original != "teh" {
			t.Fatal(i)
		}
	}
	onMain(func() {
		if calls < 2 || checker.closed != 1 {
			t.Errorf("calls=%d, closes=%d", calls, checker.closed)
		}
	})
	// Check from main pumps events, including asynchronous completion.
	onMain(func() {
		r, err := TextServices.Check(context.Background(), "teh", TextCheckOptions{Spelling: true})
		if err != nil || len(r.Issues) != 1 {
			t.Errorf("main-thread check: %+v, %v", r, err)
		}
	})
}

func TestTextServicesCancellationAndLateReply(t *testing.T) {
	started := make(chan func(), 1)
	var checker *testTextChecker
	withTextFactory(t, func(string) (platform.TextChecker, error) {
		checker = &testTextChecker{info: englishTextInfo(), check: func(s string, o TextCheckOptions, done func([]TextIssue, error)) {
			started <- func() { done([]TextIssue{{Start: 0, End: 3}}, nil) }
		}}
		return checker, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	answer := make(chan error, 1)
	go func() { _, err := TextServices.Check(ctx, "teh", TextCheckOptions{Spelling: true}); answer <- err }()
	var reply func()
	select {
	case reply = <-started:
	case <-time.After(time.Second):
		t.Fatal("no request")
	}
	cancel()
	select {
	case err := <-answer:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation waited for service")
	}
	onMain(func() {
		reply()
		if checker.closed != 1 {
			t.Fatal("session closed more than once")
		}
	})
}

func TestTextServicesLimitsOptionsAndLearn(t *testing.T) {
	checks, learned := 0, ""
	withTextFactory(t, func(string) (platform.TextChecker, error) {
		return &testTextChecker{info: englishTextInfo(), check: func(s string, o TextCheckOptions, done func([]TextIssue, error)) {
			checks++
			var issues []TextIssue
			for _, r := range textcheck.Words(s) {
				issues = append(issues, TextIssue{Start: r[0], End: r[1], Kind: TextSpelling})
			}
			done(issues, nil)
		}, learn: func(w string) error { learned = w; return nil }}, nil
	})
	if _, err := TextServices.Check(context.Background(), "hello", TextCheckOptions{SmartQuotes: true}); !errors.Is(err, ErrTextServicesUnavailable) {
		t.Fatal(err)
	}
	if _, err := TextServices.Check(context.Background(), "hello", TextCheckOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := TextServices.Check(context.Background(), "bad\x00text", TextCheckOptions{Spelling: true}); err == nil {
		t.Fatal("accepted NUL")
	}
	if _, err := TextServices.Check(context.Background(), string([]byte{255}), TextCheckOptions{Spelling: true}); err == nil {
		t.Fatal("accepted invalid UTF-8")
	}
	r, err := TextServices.Check(context.Background(), strings.Repeat("teh ", 1100), TextCheckOptions{Spelling: true})
	if err != nil || !r.Truncated || len(r.Issues) != 1024 {
		t.Fatalf("result cap: %d, truncated %v, %v", len(r.Issues), r.Truncated, err)
	}
	r, err = TextServices.Check(context.Background(), strings.Repeat("a", 513)+" teh", TextCheckOptions{Spelling: true})
	if err != nil || !r.Truncated || len(r.Issues) != 1 || r.Issues[0].Start != 514 {
		t.Fatalf("oversized token: %+v, %v", r, err)
	}
	if err := TextServices.LearnWord("MyGo", ""); err != nil {
		t.Fatal(err)
	}
	onMain(func() {
		if learned != "MyGo" || checks < 2 {
			t.Fatal("learn/check not dispatched")
		}
	})
	if err := TextServices.LearnWord("\x00", ""); err == nil {
		t.Fatal("accepted invalid dictionary word")
	}
}

func TestTextServicesShutdownCompletesPendingChecks(t *testing.T) {
	started := make(chan func(), 1)
	withTextFactory(t, func(string) (platform.TextChecker, error) {
		return &testTextChecker{info: englishTextInfo(), check: func(s string, o TextCheckOptions, done func([]TextIssue, error)) {
			started <- func() { done(nil, nil) }
		}}, nil
	})
	answer := make(chan error, 1)
	go func() {
		_, err := TextServices.Check(context.Background(), "teh", TextCheckOptions{Spelling: true})
		answer <- err
	}()
	var reply func()
	select {
	case reply = <-started:
	case <-time.After(time.Second):
		t.Fatal("check did not start")
	}
	onMain(func() { stopTextServices(); textServicesStopped = false })
	select {
	case err := <-answer:
		if !errors.Is(err, errLoopStopped) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown left Check waiting")
	}
	onMain(reply)
}
