//go:build !js

package mygo

import (
	"errors"
	"math"
	"testing"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/automation"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/richtext"
)

func TestWindowAutomation(t *testing.T) {
	count := 0
	w := NewWindow(WindowOptions{Width: 300, Height: 200, Content: ui.View(func(c *ui.Context) {
		if !backend().IsMainThread() {
			t.Error("view ran off the main thread")
		}
		ui.Button(c, "Increment").OnClick(func() { count++ })
		ui.Textf(c, "Count %d", count)
	}).Automatable()})
	t.Cleanup(w.Destroy)
	driver := Automate(w)
	s, err := driver.Inspect()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Nodes) != 0 {
		t.Fatal("inspect ran an unrendered view")
	}
	wins := fb.Windows()
	surface := wins[len(wins)-1].FakeSurface()
	onMain(func() { surface.Frame() })
	s, err = driver.Inspect()
	if err != nil {
		t.Fatal(err)
	}
	target, err := s.Resolve(0, "Increment")
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []automation.Action{automation.Press, automation.Click} {
		if err := driver.Perform(target, automation.Request{Action: action}); err != nil {
			t.Fatal(err)
		}
		s, err = driver.Inspect()
		if err != nil {
			t.Fatal(err)
		}
		target, err = s.Resolve(0, "Increment")
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Resolve(0, "Count 2"); err != nil {
		t.Fatal(err)
	}
	w.Destroy()
	if _, err := driver.Inspect(); !errors.Is(err, automation.ErrClosed) {
		t.Fatalf("inspect closed: %v", err)
	}
	if err := driver.Perform(target, automation.Request{Action: automation.Press}); !errors.Is(err, automation.ErrClosed) {
		t.Fatalf("perform closed: %v", err)
	}
}

func TestWindowAutomationOptIn(t *testing.T) {
	w, _, _ := contentWindow(t, func(c *ui.Context) { ui.Button(c, "Private") })
	if _, err := Automate(w).Inspect(); !errors.Is(err, automation.ErrUnsupported) {
		t.Fatalf("not opted in: %v", err)
	}
	if err := Automate(w).Perform(automation.Target{}, automation.Request{}); !errors.Is(err, automation.ErrUnsupported) {
		t.Fatalf("not opted in: %v", err)
	}
	if err := Automate(w).Input(automation.Input{Kind: automation.TextInput, Text: "private"}); !errors.Is(err, automation.ErrUnsupported) {
		t.Fatalf("input not opted in: %v", err)
	}
	page := NewWindow(WindowOptions{})
	t.Cleanup(page.Destroy)
	if _, err := Automate(page).Inspect(); !errors.Is(err, automation.ErrUnsupported) {
		t.Fatalf("web page: %v", err)
	}
	// Existing platform accessibility still works independently of automation.
	onMain(func() { w.conn.Event(platform.SurfaceEvent{Kind: platform.AccessibilityOn}) })
}

func TestWindowAutomationInput(t *testing.T) {
	code := &editor.State{Buffer: editor.NewBuffer("before")}
	doc := richtext.NewDocument(richtext.NewText("before"))
	for _, test := range []struct {
		name string
		view func(*ui.Context)
	}{
		{"code", func(c *ui.Context) { editor.View(c, code).Fill().Label("Editor") }},
		{"rich text", func(c *ui.Context) { richtext.Editor(c, doc, richtext.EditorOptions{Label: "Editor"}).Fill() }},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := NewWindow(WindowOptions{Width: 300, Height: 200, Content: ui.View(test.view).Automatable()})
			t.Cleanup(w.Destroy)
			wins := fb.Windows()
			surface := wins[len(wins)-1].FakeSurface()
			onMain(func() {
				surface.Frame()
				surface.Send(platform.SurfaceEvent{Kind: platform.SurfaceFocus})
			})
			driver := Automate(w)
			snapshot, err := driver.Inspect()
			if err != nil {
				t.Fatal(err)
			}
			target, err := snapshot.Resolve(0, "Editor")
			if err != nil {
				t.Fatal(err)
			}
			if err := driver.Perform(target, automation.Request{Action: automation.Focus}); err != nil {
				t.Fatal(err)
			}
			for _, input := range []automation.Input{
				{Kind: automation.KeyInput, Key: "CmdOrCtrl+A"},
				{Kind: automation.TextInput, Text: "after 🌍"},
				{Kind: automation.KeyInput, Key: "Enter"},
				{Kind: automation.TextInput, Text: "next"},
			} {
				if err := driver.Input(input); err != nil {
					t.Fatal(err)
				}
			}
			snapshot, err = driver.Inspect()
			if err != nil {
				t.Fatal(err)
			}
			for _, n := range snapshot.Nodes {
				if n.ID == target.ID && n.Value != "after 🌍\nnext" {
					t.Fatalf("value = %q", n.Value)
				}
			}
			for _, input := range []automation.Input{
				{}, {Kind: automation.KeyInput, Key: "invalid"},
				{Kind: automation.KeyInput, Key: "F24"},
				{Kind: automation.ScrollInput, X: math.NaN()},
				{Kind: automation.ScrollInput, DY: math.Inf(1)},
				{Kind: automation.ScrollInput, DX: math.MaxFloat64},
				{Kind: automation.ScrollInput, X: -1},
			} {
				if err := driver.Input(input); err == nil {
					t.Fatalf("accepted invalid input: %+v", input)
				}
			}
			w.Destroy()
			if err := driver.Input(automation.Input{Kind: automation.TextInput}); !errors.Is(err, automation.ErrClosed) {
				t.Fatalf("input after close: %v", err)
			}
		})
	}
}
