//go:build !mygo_noinspector

package ui

import "testing"

func TestPreviewUsesExistingInspector(t *testing.T) {
	var made, disposed int
	p := NewPreview(PreviewOptions{Config: PreviewConfig{Width: 1000, Height: 600}, Presets: []PreviewPreset{
		{Name: "Counter", New: previewCounter(0, &made, &disposed)},
	}})
	tt := p.NewTester()
	t.Cleanup(tt.Close)
	tt.Key(0, KeyF12)
	if !tt.rt.insp.open || !tt.HasText("<Text>Count 0</Text>") {
		t.Fatalf("preview has no inspector tree: %q", tt.Texts())
	}
	_ = tt.Click("Add")
	if !tt.HasText("<Text>Count 1</Text>") {
		t.Fatal("inspector did not follow sample interaction")
	}
	c := p.Config()
	c.Theme, c.Scale = PreviewDark, 1.5
	_ = p.SetConfig(c)
	tt.Frame()
	if !tt.rt.insp.open || !tt.rt.dark || !tt.HasText("<Text>Count 1</Text>") || made != 1 {
		t.Fatal("configuration change lost inspector or sample state")
	}
	p.Reset()
	tt.Frame()
	if !tt.rt.insp.open || !tt.HasText("<Text>Count 0</Text>") || disposed != 1 {
		t.Fatal("sample reset lost inspector or left an old tree")
	}
}
