package e2e

import (
	"context"
	"errors"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func TestSystemTextServices(t *testing.T) {
	info, err := mygo.TextServices.Info("en-US")
	if errors.Is(err, mygo.ErrTextServicesUnavailable) || errors.Is(err, mygo.ErrTextLanguageUnavailable) {
		t.Skipf("English service unavailable: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	if !info.Spelling || !info.Suggestions || len(info.Languages) == 0 {
		t.Fatalf("availability %+v", info)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	r, err := mygo.TextServices.Check(ctx, "😀 mispeling", mygo.TextCheckOptions{Language: "en-US", Spelling: true})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(r.Issues, func(i mygo.TextIssue) bool {
		return i.Start == 2 && i.End == 11 && i.Original == "mispeling" && len(i.Replacements) > 0
	}) {
		t.Fatalf("native ranges/suggestions: %+v", r)
	}
	_, err = mygo.TextServices.Info("zz-ZZ")
	if !errors.Is(err, mygo.ErrTextLanguageUnavailable) {
		t.Fatalf("unknown dictionary: %v", err)
	}
	if info.SmartQuotes && info.SmartDashes {
		r, err = mygo.TextServices.Check(ctx, "\"hello\" -- world", mygo.TextCheckOptions{Language: "en-US", SmartQuotes: true, SmartDashes: true})
		if err != nil || !slices.ContainsFunc(r.Issues, func(i mygo.TextIssue) bool { return i.Kind == mygo.TextQuote && len(i.Replacements) > 0 }) {
			t.Fatalf("native substitutions: %+v, %v", r, err)
		}
	}
}

func TestContentTextServices(t *testing.T) {
	if _, err := mygo.TextServices.Info("en-US"); err != nil {
		t.Skipf("English service unavailable: %v", err)
	}
	for _, multi := range []bool{false, true} {
		t.Run(map[bool]string{false: "input", true: "area"}[multi], func(t *testing.T) {
			value := "😀 mispeling"
			var status ui.TextServiceStatus
			var composing bool
			var frames atomic.Int32
			view := func(c *ui.Context) {
				frames.Add(1)
				ui.Column(c).Fill().Padding(20).Children(func() {
					var e *ui.Element
					if multi {
						e = ui.TextArea(c, &value).Height(110)
					} else {
						e = ui.TextInput(c, &value)
					}
					e.TextServices(ui.TextServicesOptions{Language: "en-US", SpellChecking: true, AutomaticCorrection: true})
					status, composing = e.TextServiceStatus(), e.Composing()
				})
			}
			get := func() (s ui.TextServiceStatus, text string, ime bool) {
				mygo.RunOnMain(func() { s, text, ime = status, value, composing })
				return
			}
			w := newWindow(t, mygo.WindowOptions{Title: "Text services", Width: 400, Height: 200, Content: ui.View(view)})
			eventually(t, "native text service suggestions", func() bool { s, _, _ := get(); return len(s.Issues) > 0 && !s.Checking })
			s, text, _ := get()
			if s.Error != nil || text != "😀 mispeling" || !slices.ContainsFunc(s.Issues, func(i ui.TextIssue) bool { return i.Start == 2 && i.End == 11 && len(i.Replacements) > 0 }) {
				t.Fatalf("native editor check %+v, %q", s, text)
			}
			if _, _, ok := inputClient(w); !ok {
				t.Skip("input method automation unavailable")
			}
			if !click(w, 300, 36) {
				t.Skip("click automation unavailable")
			}
			caret := 11 // GTK and Windows helpers return rune offsets.
			if runtime.GOOS == "darwin" {
				caret = 12
			} // NSTextInputClient exposes UTF-16 units.
			eventually(t, "caret after the checked text", func() bool { sel, _, _ := inputClient(w); return sel == [2]int{caret, 0} })
			if !composeOver(w, "に", 1, false, -1, 0) {
				t.Skip("composition automation unavailable")
			}
			eventually(t, "composition suspends text services", func() bool {
				s, text, ime := get()
				return ime && len(s.Issues) == 0 && !s.Checking && text == "😀 mispeling"
			})
			composeOver(w, "", 0, false, -1, 0)
			eventually(t, "checking resumes after composition", func() bool { s, text, ime := get(); return !ime && len(s.Issues) > 0 && text == "😀 mispeling" })
			w.Update(func() { value = "hello world" })
			eventually(t, "a new clean revision", func() bool { s, text, _ := get(); return text == "hello world" && !s.Checking && len(s.Issues) == 0 })
		})
	}
}

func TestSystemTextServicesMainThreadLongText(t *testing.T) {
	if _, err := mygo.TextServices.Info("en-US"); err != nil {
		t.Skipf("English service unavailable: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	text := strings.Repeat("hello world. ", 300) + "mispeling"
	var result mygo.TextCheckResult
	var checkErr error
	var dispatched atomic.Bool
	// A main-thread Check must keep dispatching while bounded/native async
	// requests run, including work posted by another goroutine.
	mygo.RunOnMain(func() {
		time.AfterFunc(5*time.Millisecond, func() { mygo.RunOnMain(func() { dispatched.Store(true) }) })
		result, checkErr = mygo.TextServices.Check(ctx, text, mygo.TextCheckOptions{Language: "en-US", Spelling: true})
	})
	if checkErr != nil || !dispatched.Load() || !slices.ContainsFunc(result.Issues, func(i mygo.TextIssue) bool { return i.Original == "mispeling" && i.Start == 3900 }) {
		t.Fatalf("long check dispatched=%v: %+v, %v", dispatched.Load(), result, checkErr)
	}
}
