package editor

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo/ui"
)

// onEditTexts has fn of b's OnEdit check each version it is told of,
// that it follows the one before and that its changes take the old text
// to the new, and returns the new texts told, in order.
func onEditTexts(t *testing.T, b *Buffer) (told *[]string, off func()) {
	t.Helper()
	var out []string
	off = b.OnEdit(func(old, new Snapshot, cs []Change) {
		if new.Version() != old.Version()+1 {
			t.Errorf("told version %d after %d", new.Version(), old.Version())
		}
		text := old.String()
		for i := len(cs) - 1; i >= 0; i-- {
			c := cs[i]
			text = text[:c.OldStart] + new.Slice(c.NewStart, c.NewEnd) + text[c.OldEnd:]
		}
		if text != new.String() {
			t.Errorf("changes %v make %q of %q, not %q", cs, text, old.String(), new.String())
		}
		out = append(out, new.String())
	})
	return &out, off
}

// An app hears of what is typed, undone and made again, in any editor
// of the buffer; off stops it.
func TestOnEditHearsTheEditors(t *testing.T) {
	ct := newEditorTest(t, "", "ab|")
	b := ct.st.Buffer
	other := &State{Buffer: b}
	tt2 := ui.NewTester(func(c *ui.Context) { View(c, other).Fill().AutoFocus() }, 600, 400)
	tt2.Frame()
	other.SetSelections(Selection{Anchor: 0, Head: 0})
	tt2.Frame()

	told, off := onEditTexts(t, b)
	steps := []func(){
		func() { ct.typ("c") },
		func() { tt2.Type(">") },
		func() { ct.run(Undo) },
		func() { ct.run(Redo) },
	}
	for i, step := range steps {
		step()
		if len(*told) != i+1 {
			t.Fatalf("after step %d, told %q", i, *told)
		}
	}
	if got, want := strings.Join(*told, ","), "abc,>abc,abc,>abc"; got != want {
		t.Fatalf("told %q, want %q", got, want)
	}
	off()
	ct.typ("d")
	if len(*told) != 4 {
		t.Fatalf("told after off: %q", *told)
	}
}

// Every method that changes the text tells it, as a language server kept
// in step needs, a version at a time: undoing a step of two edits tells
// two. An edit changing nothing makes no version and tells nothing.
func TestOnEditHearsTheBuffersMethods(t *testing.T) {
	b := NewBuffer("hello")
	told, _ := onEditTexts(t, b)
	for _, step := range []struct {
		name string
		do   func()
		want string // the texts told by the call
	}{
		{"Edit", func() { b.Edit(Edit{Start: 5, End: 5, Text: "!"}) }, "hello!"},
		{"EditJoined", func() { b.EditJoined(Edit{Start: 6, End: 6, Text: "!"}) }, "hello!!"},
		{"EditRemote", func() { b.EditRemote(Edit{Start: 0, End: 0, Text: ">"}) }, ">hello!!"},
		{"Undo", func() { b.Undo() }, ">hello!,>hello"},
		{"Redo", func() { b.Redo() }, ">hello!,>hello!!"},
		{"Set", func() { b.Set("set") }, "set"},
		{"Reset", func() { b.Reset("reset\n") }, "reset\n"},
		{"Edit changing nothing", func() { b.Edit(Edit{Start: 0, End: 0}) }, ""},
		{"EditRemote changing nothing", func() { b.EditRemote(Edit{Start: 1, End: 2, Text: "e"}) }, ""},
	} {
		n := len(*told)
		step.do()
		if got := strings.Join((*told)[n:], ","); got != step.want {
			t.Fatalf("%s told %q, want %q", step.name, got, step.want)
		}
	}
}

// A function that panics leaves the buffer telling again at the next
// edit, of the versions after the one it panicked at.
func TestOnEditAfterAPanic(t *testing.T) {
	b := NewBuffer("")
	var told []string
	b.OnEdit(func(_, new Snapshot, _ []Change) {
		if new.String() == "x" {
			panic("listener")
		}
		told = append(told, new.String())
	})
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("the listener's panic did not reach the edit's caller")
			}
		}()
		b.Edit(Edit{Start: 0, End: 0, Text: "x"})
	}()
	b.Edit(Edit{Start: 1, End: 1, Text: "y"})
	if got := fmt.Sprint(told); got != "[xy]" {
		t.Fatalf("told %s after the panic", got)
	}
}

// A function editing the buffer hears of its edit once it returns, in
// order, and may stop itself.
func TestOnEditFunctionEdits(t *testing.T) {
	b := NewBuffer("x")
	var told []string
	var off func()
	off = b.OnEdit(func(_, new Snapshot, _ []Change) {
		told = append(told, new.String())
		switch new.String() {
		case "xy":
			b.Edit(Edit{Start: 2, End: 2, Text: "z"})
		case "xyz":
			off()
		}
	})
	b.Edit(Edit{Start: 1, End: 1, Text: "y"})
	b.Edit(Edit{Start: 0, End: 0, Text: "_"})
	if got := fmt.Sprint(told); got != "[xy xyz]" {
		t.Fatalf("told %s", got)
	}
}

// A function too far behind the log is told one change of the whole
// text.
func TestOnEditBehindTheLog(t *testing.T) {
	b := NewBuffer("")
	var told []string
	b.OnEdit(func(old, new Snapshot, cs []Change) {
		told = append(told, fmt.Sprintf("%q>%d:%v", old.String(), new.Len(), cs))
		if new.String() == "a" {
			for range logMax + 10 {
				b.Edit(Edit{Start: 1, End: 1, Text: "b"})
			}
		}
	})
	b.Edit(Edit{Start: 0, End: 0, Text: "a"})
	n := 1 + logMax + 10
	want := fmt.Sprintf(`["">1:[{0 0 0 1}] "a">%d:[{0 1 0 %d}]]`, n, n)
	if got := fmt.Sprint(told); got != want {
		t.Fatalf("told %s, want %s", got, want)
	}
}

// Edits from several goroutines at once are each told once, in order,
// one call at a time: the last told is the last made, even when an edit
// comes as the goroutine telling the others stops.
func TestOnEditFromGoroutines(t *testing.T) {
	for round := range 2000 {
		b := NewBuffer("")
		var last atomic.Uint64
		var in atomic.Int32
		b.OnEdit(func(old, new Snapshot, _ []Change) {
			if in.Add(1) != 1 {
				t.Error("told twice at once")
			}
			if old.Version() != last.Load() || new.Version() != old.Version()+1 {
				t.Errorf("told %d to %d after %d", old.Version(), new.Version(), last.Load())
			}
			last.Store(new.Version())
			in.Add(-1)
		})
		var wg sync.WaitGroup
		for range 3 {
			wg.Go(func() { b.EditRemote(Edit{Start: 0, End: 0, Text: "x"}) })
		}
		wg.Wait()
		if got, want := last.Load(), b.Version(); got != want {
			t.Fatalf("round %d: told version %d once the edits returned, want %d", round, got, want)
		}
	}
}

// A command's edits are told once it is done: a function editing on the
// first notice of a } that outdents its line, which made the rest of the
// command stale when told in the middle of it, leaves the } typed once
// and its line outdented.
func TestOnEditAfterTheCommand(t *testing.T) {
	ct := newEditorTest(t, "go", "func main() {\n\tif x {\n\t\ty()\n\t\t|")
	b := ct.st.Buffer
	first := true
	b.OnEdit(func(_, _ Snapshot, _ []Change) {
		if first {
			first = false
			b.EditJoined(Edit{Start: 0, End: 0, Text: "//"})
		}
	})
	ct.typ("}")
	ct.want("//func main() {\n\tif x {\n\t\ty()\n\t}|")
}

// A function editing on each notice but of its own edits leaves what
// commands of several edits do as they would without it.
func TestOnEditEditingDuringCommands(t *testing.T) {
	for _, tc := range []struct {
		name, lang, text string
		do               func(ct *editorTest)
	}{
		{"outdent", "go", "func main() {\n\tif x {\n\t\ty()\n\t\t|", func(ct *editorTest) { ct.typ("}") }},
		{"auto-close", "go", "f|", func(ct *editorTest) { ct.typ("(") }},
		{"enter", "go", "func f() {|}", func(ct *editorTest) { ct.key(0, ui.KeyEnter) }},
		{"carets", "", "a|\nb|\nc|", func(ct *editorTest) { ct.typ("xy") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plain := newEditorTest(t, tc.lang, tc.text)
			tc.do(plain)
			want := plain.show()

			ct := newEditorTest(t, tc.lang, tc.text)
			b := ct.st.Buffer
			mine := map[uint64]bool{}
			notices := 0
			b.OnEdit(func(_, new Snapshot, _ []Change) {
				if !mine[new.Version()] {
					notices++
					b.EditJoined(Edit{Start: 0, End: 0, Text: "#"})
					mine[b.Version()] = true
				}
			})
			tc.do(ct)
			got := ct.show()
			if notices == 0 || got != strings.Repeat("#", notices)+want {
				t.Fatalf("with %d notices got\n%s\nwant it after as many #\n%s", notices, got, want)
			}
		})
	}
}

// A completion clicked and the find bar's Replace and Replace All, made
// outside the editor's input, are told as soon as they are.
func TestOnEditHearsCompletionsAndReplacing(t *testing.T) {
	ct := newEditorTest(t, "go", "|")
	ct.st.OnCompletion = func(r CompletionRequest) {
		ct.st.ShowCompletions(r.Offset-len(r.Prefix), []Completion{{Label: "alpha"}, {Label: "alpine"}})
	}
	ct.st.RenderCompletions = func(c *ui.Context, panel ui.Element, m *CompletionMenu) {
		panel.Children(func() {
			for i, it := range m.Items() {
				m.Item(i).Children(func() { ui.Text(c, "→ "+it.Label) })
			}
		})
	}
	ct.typ("al")
	told, _ := onEditTexts(t, ct.st.Buffer)
	if err := ct.tt.Click("→ alpine"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(*told, ","); got != "alpine" {
		t.Fatalf("told %q after the click", got)
	}

	ct = newEditorTest(t, "", "|cat cat cat")
	ct.st.RenderFindBar = func(c *ui.Context, bar ui.Element, f *FindBar) {
		*f.Replacement() = "dog"
		bar.Row().Children(func() {
			if ui.Button(c, "One").Clicked() {
				f.Replace()
			}
			if ui.Button(c, "Every").Clicked() {
				f.ReplaceAll()
			}
		})
	}
	ct.st.Find("cat")
	ct.tt.Frame()
	told, _ = onEditTexts(t, ct.st.Buffer)
	for _, button := range []string{"One", "Every"} {
		n := len(*told)
		if err := ct.tt.Click(button); err != nil {
			t.Fatal(err)
		}
		if len(*told) != n+1 {
			t.Fatalf("after %s, told %q", button, *told)
		}
	}
	if got := (*told)[len(*told)-1]; got != "dog dog dog" {
		t.Fatalf("told %q last", got)
	}
}

// A function stopped by another's call is not told again, even of the
// edit being told.
func TestOnEditOffFromAnother(t *testing.T) {
	b := NewBuffer("")
	var offB func()
	var toldB int
	b.OnEdit(func(_, _ Snapshot, _ []Change) { offB() })
	offB = b.OnEdit(func(_, _ Snapshot, _ []Change) { toldB++ })
	b.Edit(Edit{Start: 0, End: 0, Text: "x"})
	b.Edit(Edit{Start: 0, End: 0, Text: "y"})
	if toldB != 0 {
		t.Fatalf("told %d times after off", toldB)
	}
}

// Reset with the text and line endings the buffer has makes no version
// and tells nothing, as an Edit changing nothing. Other endings alone make
// a version with no changes, saved, so that a version always writes the
// same bytes.
func TestResetSameTextTellsNothing(t *testing.T) {
	b := NewBuffer("a\nb")
	v := b.Version()
	before := b.Snapshot()
	told, _ := onEditTexts(t, b)
	b.Reset("a\nb")
	if b.Version() != v || len(*told) != 0 {
		t.Fatalf("same text: version %d, was %d; told %q", b.Version(), v, *told)
	}
	var changes []Change
	b.OnEdit(func(_, _ Snapshot, cs []Change) { changes = cs })
	b.Reset("a\r\nb")
	if b.Version() != v+1 || strings.Join(*told, ",") != "a\nb" || len(changes) != 0 {
		t.Fatalf("other endings: version %d, was %d; told %q with %v", b.Version(), v, *told, changes)
	}
	var was, now strings.Builder
	before.WriteTo(&was)
	b.Snapshot().WriteTo(&now)
	if was.String() != "a\nb" || now.String() != "a\r\nb" || b.Modified() {
		t.Fatalf("writes %q then %q, modified %v", was.String(), now.String(), b.Modified())
	}
	b.Reset("a\r\nb")
	if b.Version() != v+1 {
		t.Fatalf("the same endings again: version %d, want %d", b.Version(), v+1)
	}
	b.Reset("a\nbc")
	if b.Version() != v+2 || strings.Join(*told, ",") != "a\nb,a\nbc" {
		t.Fatalf("a new text: version %d, told %q", b.Version(), *told)
	}
}

// The context menu's commands are told as soon as they are chosen, once
// each, as their keys' are.
func TestOnEditHearsTheContextMenu(t *testing.T) {
	for _, item := range [][]string{{"Cut"}, {"Transform", "Upper Case"}} {
		ct := newEditorTest(t, "", "hello")
		told, _ := onEditTexts(t, ct.st.Buffer)
		ct.tt.RightClickAt(300, 200)
		if err := ct.tt.ChooseMenuItem("Select All"); err != nil {
			t.Fatal(err)
		}
		ct.tt.RightClickAt(300, 200)
		if err := ct.tt.ChooseMenuItem(item...); err != nil {
			t.Fatal(err)
		}
		want := map[string]string{"Cut": "", "Transform": "HELLO"}[item[0]]
		if len(*told) != 1 || (*told)[0] != want {
			t.Fatalf("%s told %q, want [%q]", item, *told, want)
		}
	}
}
