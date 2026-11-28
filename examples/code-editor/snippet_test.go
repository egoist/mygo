package main

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestGoSnippetCompletion(t *testing.T) {
	f := newFile("main.go", "forr")
	st := f.editor
	tt := ui.NewTester(func(c *ui.Context) { editor.View(c, st).Fill().AutoFocus() }, 640, 400)
	st.Select(4, 4)
	st.ShowCompletions(0, wordsOf(st.Buffer, "forr"))
	tt.Frame()
	tt.Key(0, ui.KeyTab)
	if got := st.SelectedText(); got != "index" {
		t.Fatalf("first field = %q", got)
	}
	tt.Type("i")
	tt.Key(0, ui.KeyTab)
	if got := st.SelectedText(); got != "value" {
		t.Fatalf("second field = %q", got)
	}
	tt.Type("item")
	tt.Key(0, ui.KeyTab)
	tt.Type("items")
	tt.Key(0, ui.KeyTab)
	if got := st.Buffer.String(); got != "for i, item := range items {\n\t\n}" {
		t.Fatalf("snippet result: %q", got)
	}
	if st.Cursor() != strings.Index(st.Buffer.String(), "\t")+1 {
		t.Fatal("final stop is not in the loop body")
	}
}

func TestGoErrorfSnippet(t *testing.T) {
	f := newFile("main.go", "errorf")
	st := f.editor
	tt := ui.NewTester(func(c *ui.Context) { editor.View(c, st).Fill().AutoFocus() }, 640, 400)
	st.Select(6, 6)
	st.ShowCompletions(0, wordsOf(st.Buffer, "errorf"))
	tt.Frame()
	tt.Key(0, ui.KeyTab)
	tt.Type("open")
	for _, want := range []string{"w", ", err", "err"} {
		tt.Key(0, ui.KeyTab)
		if got := st.SelectedText(); got != want {
			t.Fatalf("field = %q, want %q", got, want)
		}
	}
	tt.Type("e")
	tt.Key(0, ui.KeyTab)
	if got := st.Buffer.String(); got != `fmt.Errorf("open: %w", e)` {
		t.Fatalf("snippet result: %q", got)
	}
}
