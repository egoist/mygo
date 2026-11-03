package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// The gallery names every symbol, and its options change them.
func TestGallery(t *testing.T) {
	g := &gallery{size: 1, weight: 1}
	tt := ui.NewTester(g.view, 840, 2000)
	for _, name := range names {
		if _, ok := tt.Find(name); !ok {
			t.Errorf("%s: not shown", name)
		}
	}
	if err := tt.Click("48"); err != nil {
		t.Fatal(err)
	}
	if g.size != 3 {
		t.Errorf("size %d after choosing 48, want 3", g.size)
	}
	if err := tt.Click("Natural size"); err != nil {
		t.Fatal(err)
	}
	if !g.natural {
		t.Error("not natural after Natural size")
	}
	if err := tt.Click("Black"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Fill"); err != nil {
		t.Fatal(err)
	}
	if g.weight != 3 || !g.fill {
		t.Errorf("weight %d, fill %v after Black and Fill, want 3 and true", g.weight, g.fill)
	}
}
