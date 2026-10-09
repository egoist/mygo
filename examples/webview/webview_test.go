package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestBrowserLoadsThePickedFile drives the window's view through the headless
// tester: the sample files show in the list, and choosing one changes the
// file:// page the preview on the right asks for.
func TestBrowserLoadsThePickedFile(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a.html", "b.html"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("<html>"+n+"</html>"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	b := &browser{dir: dir, picked: 0}
	b.files = listHTML(dir)
	if len(b.files) != 2 || b.files[0] != "a.html" || b.files[1] != "b.html" {
		t.Fatalf("the files are %v", b.files)
	}
	if u := b.url(); u != "file://"+dir+"/a.html" {
		t.Fatalf("the first page is %q, want the first file", u)
	}

	tt := ui.NewTester(b.view, 900, 560)
	tt.Frame()
	if !tt.HasText("a.html") || !tt.HasText("b.html") {
		t.Fatalf("the list shows %v, want both files", tt.Texts())
	}

	if err := tt.Click("b.html"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if b.picked != 1 {
		t.Fatalf("choosing b.html left picked at %d, want 1", b.picked)
	}
	if u := b.url(); u != "file://"+dir+"/b.html" {
		t.Fatalf("the preview asks for %q, want the chosen file", u)
	}
}
