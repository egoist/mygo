package main

import "testing"

func TestContactPreviews(t *testing.T) {
	p := playground()
	tt := p.NewTester()
	t.Cleanup(tt.Close)
	if !tt.HasText("Contact") {
		t.Fatal("empty contact not built")
	}
	if err := tt.Click("Name"); err != nil {
		t.Fatal(err)
	}
	tt.Type("Grace Hopper")
	if err := tt.Click("Save contact"); err != nil || !tt.HasText("Saved") {
		t.Fatalf("empty sample cannot be filled and saved: %v", err)
	}
	if err := p.SelectPreset("Populated"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Save contact"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Saved") {
		t.Fatal("sample is not interactive")
	}
	_ = p.SelectPreset("Japanese / large text")
	tt.Frame()
	if !tt.HasText("連絡先") || !tt.HasText("保存") || tt.Image().Bounds().Dx() != 1040 {
		t.Fatalf("Japanese preset: %q, image %v", tt.Texts(), tt.Image().Bounds())
	}
}
