//go:build mygo_noinspector

package ui

import (
	"strings"
	"testing"
)

func TestProductionPreviewDisabled(t *testing.T) {
	defer func() {
		message, ok := recover().(string)
		if !ok || !strings.Contains(message, "production builds") || !strings.Contains(message, "mygo build -debug") {
			t.Fatalf("production preview should explain how to enable development support: %q", message)
		}
	}()
	NewPreview(PreviewOptions{})
}

func TestProductionViewAndTester(t *testing.T) {
	frames := 0
	tt := NewTester(func(c *Context) {
		frames++
		if config, ok := PreviewEnvironment(c); ok || config.Theme != "" || config.Preferences != nil {
			t.Fatal("production view has a preview environment")
		}
		Button(c, "Ordinary content")
	}, 200, 100)
	if !tt.HasText("Ordinary content") {
		t.Fatal("ordinary native UI was disabled with the playground")
	}
	tt.Close()
	tt.Close()
	before := frames
	tt.Frame()
	tt.ClickAt(10, 10)
	if frames != before {
		t.Fatal("closed production tester kept building frames")
	}
}
