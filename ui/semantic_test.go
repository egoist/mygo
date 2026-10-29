package ui

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui/automation"
)

func TestAutomationTargetSurvivesRepaint(t *testing.T) {
	clicks := 0
	tt := NewTester(func(c *Context) {
		Button(c, "Save").OnClick(func() { clicks++ })
		Box(c).Size(10, 10).Draw(func(p *Painter, _ Rect) { p.AnimationFrame() })
	}, 300, 200)
	target := automationTarget(t, tt.Inspect(), "Save")
	tt.Advance(time.Second / 30)
	if err := tt.Perform(target, automation.Request{Action: automation.Press}); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Perform(target, automation.Request{Action: automation.Click}); err != nil {
		t.Fatal(err)
	}
	if clicks != 2 {
		t.Fatalf("clicks = %d, want 2", clicks)
	}
}

func automationTarget(t *testing.T, s automation.Snapshot, name string) automation.Target {
	t.Helper()
	target, err := s.Resolve(0, name)
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func TestAutomationSelectedStates(t *testing.T) {
	for _, test := range []struct {
		name     string
		role     Role
		selected bool
	}{
		{"list item", RoleListItem, true},
		{"table row", RoleRow, true},
		{"tab", RoleTab, true},
		{"tree item", RoleTreeItem, true},
		{"checkbox", RoleCheckBox, false},
		{"radio", RoleRadio, false},
		{"toggle button", RoleToggleButton, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, checked := range []bool{false, true} {
				tt := NewTester(func(c *Context) {
					Box(c).Label("Choice").Role(test.role).Checked(checked)
				}, 300, 200)
				s := tt.Inspect()
				target := automationTarget(t, s, "Choice")
				for _, n := range s.Nodes {
					if n.ID == target.ID && (n.Checked != checked || n.Selected != (checked && test.selected)) {
						t.Fatalf("checked=%v: %+v", checked, n)
					}
				}
			}
		})
	}
}

func TestAutomationSnapshotAndActions(t *testing.T) {
	on, value := false, "Ada"
	tt := NewTester(func(c *Context) {
		Column(c).Label("Form").Children(func() {
			Checkbox(c, &on, "Agree")
			TextInput(c, &value).Label("Name")
			Button(c, "Visible text").Label("Accessible name")
		})
	}, 400, 300)
	now := tt.Now()
	s := tt.Inspect()
	form := automationTarget(t, s, "Form")
	agree := automationTarget(t, s, "Agree")
	name := automationTarget(t, s, "Name")
	text := automationTarget(t, s, "Visible text")
	if text != automationTarget(t, s, "Accessible name") {
		t.Fatal("text and name differ")
	}
	for _, n := range s.Nodes {
		if n.ID == agree.ID && (n.Parent < 0 || s.Nodes[n.Parent].ID != form.ID || n.Bounds.W <= 0) {
			t.Fatalf("hierarchy/geometry: %+v", n)
		}
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"ID":"`) {
		t.Fatal("IDs are not JSON strings")
	}
	var decoded automation.Snapshot
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if automationTarget(t, decoded, "Agree") != agree {
		t.Fatal("JSON changed identity")
	}
	if err := tt.Perform(agree, automation.Request{Action: automation.Press}); err != nil {
		t.Fatal(err)
	}
	if !on {
		t.Fatal("press did not toggle")
	}
	if err := tt.Perform(name, automation.Request{Action: automation.SetValue, Text: "Grace"}); err != nil {
		t.Fatalf("target after another action: %v", err)
	}
	fresh := tt.Inspect()
	if automationTarget(t, fresh, "Agree").ID != agree.ID {
		t.Fatal("ID changed between frames")
	}
	if err := tt.Perform(automationTarget(t, fresh, "Name"), automation.Request{Action: automation.SetValue, Text: "Grace"}); err != nil {
		t.Fatal(err)
	}
	if value != "Grace" {
		t.Fatalf("value %q", value)
	}
	if err := tt.Perform(automationTarget(t, tt.Inspect(), "Name"), automation.Request{Action: automation.SelectText, From: 0, To: 5}); err != nil {
		t.Fatal(err)
	}
	if err := tt.Perform(automationTarget(t, tt.Inspect(), "Name"), automation.Request{Action: automation.InsertText, Text: "Lin"}); err != nil {
		t.Fatal(err)
	}
	if value != "Lin" || tt.Now() != now {
		t.Fatalf("value %q or clock advanced", value)
	}
	if err := tt.Perform(automationTarget(t, tt.Inspect(), "Name"), automation.Request{Action: automation.Click}); err != nil {
		t.Fatal(err)
	}
	if !tt.Focused("Name") {
		t.Fatal("pointer click did not focus text input")
	}
	// Returned slices cannot mutate the engine or later observations.
	s.Nodes[0].Name = "changed"
	s.Nodes[0].Actions = append(s.Nodes[0].Actions, automation.SetValue)
	if tt.Inspect().Nodes[0].Name == "changed" {
		t.Fatal("snapshot aliases engine")
	}
}

func TestAutomationResolutionAndFailures(t *testing.T) {
	count := 0
	show := true
	tt := NewTester(func(c *Context) {
		Button(c, "Duplicate").Key("a").OnClick(func() { count++ })
		Button(c, "Duplicate").Key("b").OnClick(func() { count += 10 })
		Scroll(c).Height(40).Disabled(true).Children(func() { Button(c, "Disabled") })
		Text(c, "Text")
		if show {
			Button(c, "Removed").Key("removed")
		}
	}, 400, 300)
	s := tt.Inspect()
	for _, n := range s.Nodes {
		if n.Disabled && len(n.Actions) > 0 {
			t.Fatalf("disabled actions: %+v", n)
		}
	}
	if _, err := s.Resolve(0, "Duplicate"); !errors.Is(err, automation.ErrAmbiguous) {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := s.Resolve(0, "Missing"); !errors.Is(err, automation.ErrMissing) {
		t.Fatalf("missing: %v", err)
	}
	if _, err := s.Resolve(1, "Duplicate"); !errors.Is(err, automation.ErrUnsupported) {
		t.Fatalf("invalid selector: %v", err)
	}
	var id uint64
	for _, n := range s.Nodes {
		if n.Name == "Duplicate" {
			id = n.ID
			break
		}
	}
	target, err := s.Resolve(id, "")
	if err != nil {
		t.Fatal(err)
	}
	other := NewTester(func(c *Context) { Button(c, "Duplicate") }, 400, 300)
	if err := other.Perform(target, automation.Request{Action: automation.Press}); !errors.Is(err, automation.ErrStale) {
		t.Fatalf("session: %v", err)
	}
	if err := tt.Perform(automationTarget(t, s, "Disabled"), automation.Request{Action: automation.Press}); !errors.Is(err, automation.ErrDisabled) {
		t.Fatalf("disabled: %v", err)
	}
	if err := tt.Perform(automationTarget(t, s, "Text"), automation.Request{Action: automation.Press}); !errors.Is(err, automation.ErrUnsupported) {
		t.Fatalf("unsupported: %v", err)
	}
	missing := target
	missing.ID = 0
	if err := tt.Perform(missing, automation.Request{Action: automation.Press}); !errors.Is(err, automation.ErrMissing) {
		t.Fatalf("missing ID: %v", err)
	}
	if err := tt.Perform(target, automation.Request{Action: automation.Click}); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("click reached wrong duplicate: %d", count)
	}
	removed := automationTarget(t, tt.Inspect(), "Removed")
	show = false
	tt.Frame()
	if err := tt.Perform(removed, automation.Request{Action: automation.Press}); !errors.Is(err, automation.ErrMissing) {
		t.Fatalf("removed: %v", err)
	}
	show = true
	tt.Frame()
	if err := tt.Perform(removed, automation.Request{Action: automation.Press}); !errors.Is(err, automation.ErrStale) {
		t.Fatalf("reused ID: %v", err)
	}
}

func TestAutomationTargetChecksCurrentState(t *testing.T) {
	disabled, covered := false, false
	clicks := 0
	tt := NewTester(func(c *Context) {
		Button(c, "Save").Absolute().Size(100, 40).Disabled(disabled).OnClick(func() { clicks++ })
		if covered {
			Button(c, "Cover").Absolute().Size(100, 40)
		}
	}, 300, 200)
	target := automationTarget(t, tt.Inspect(), "Save")
	disabled = true
	tt.Frame()
	if err := tt.Perform(target, automation.Request{Action: automation.Press}); !errors.Is(err, automation.ErrDisabled) {
		t.Fatalf("disabled since observation: %v", err)
	}
	disabled, covered = false, true
	tt.Frame()
	if err := tt.Perform(target, automation.Request{Action: automation.Click}); !errors.Is(err, automation.ErrCovered) {
		t.Fatalf("covered since observation: %v", err)
	}
	if clicks != 0 {
		t.Fatalf("unexpected clicks: %d", clicks)
	}
}

func TestAutomationInputScrollsVirtualList(t *testing.T) {
	list := ListState{}
	tt := NewTester(func(c *Context) {
		List(c, &list, 1000, func(i int) { Textf(c, "Row %d", i).Height(30) }).Fill()
	}, 300, 200)
	if _, err := tt.Inspect().Resolve(0, "Row 100"); !errors.Is(err, automation.ErrMissing) {
		t.Fatalf("row should not be built yet: %v", err)
	}
	if err := tt.Input(automation.Input{Kind: automation.ScrollInput, X: 150, Y: 100, DY: 3000}); err != nil {
		t.Fatal(err)
	}
	s := tt.Inspect()
	if !slices.ContainsFunc(s.Nodes, func(n automation.Node) bool { return n.Role == "listItem" && n.PosInSet >= 100 && !n.Offscreen }) {
		t.Fatal("scroll did not build distant rows")
	}
}

func TestAutomationInputBoundaries(t *testing.T) {
	value := "protected"
	disabled := false
	tt := NewTester(func(c *Context) {
		TextInput(c, &value).Label("Field").ReadOnly(true).Disabled(disabled)
	}, 300, 200)
	target := automationTarget(t, tt.Inspect(), "Field")
	if err := tt.Perform(target, automation.Request{Action: automation.Focus}); err != nil {
		t.Fatal(err)
	}
	for _, off := range []bool{false, true} {
		disabled = off
		tt.Frame()
		if err := tt.Input(automation.Input{Kind: automation.TextInput, Text: "bad"}); err != nil {
			t.Fatal(err)
		}
		if value != "protected" {
			t.Fatal("normal input bypassed a protected control")
		}
	}
	var nested error
	var handler *Tester
	handler = NewTester(func(c *Context) {
		Box(c).Label("Handler").Focusable().Fill().HandleInput(func(ev InputEvent) bool {
			if ev.Kind == InputKeyDown {
				nested = handler.Input(automation.Input{Kind: automation.TextInput, Text: "nested"})
			}
			return true
		})
	}, 300, 200)
	if err := handler.Perform(automationTarget(t, handler.Inspect(), "Handler"), automation.Request{Action: automation.Focus}); err != nil {
		t.Fatal(err)
	}
	if err := handler.Input(automation.Input{Kind: automation.KeyInput, Key: "Enter"}); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(nested, automation.ErrUnsupported) {
		t.Fatalf("reentrant input: %v", nested)
	}
}

func TestAutomationProtectedAndReadOnly(t *testing.T) {
	password := "secret-value"
	readOnly := "public-value"
	for _, disabled := range []bool{false, true} {
		tt := NewTester(func(c *Context) {
			TextInput(c, &password).Label("Password").Password().Disabled(disabled)
			TextInput(c, &readOnly).Label("Read only").ReadOnly(true)
		}, 400, 300)
		s := tt.Inspect()
		data, err := json.Marshal(s)
		if err != nil || strings.Contains(string(data), password) {
			t.Fatalf("protected JSON: %s, %v", data, err)
		}
		for _, n := range s.Nodes {
			if n.Name == "Password" && (!n.Password || n.Value != "" || n.Text != "" || n.SelStart != 0 || n.SelEnd != 0) {
				t.Fatalf("password leaked: %+v", n)
			}
			if n.Name == "Read only" && (!n.ReadOnly || slices.Contains(n.Actions, automation.SetValue)) {
				t.Fatalf("read only: %+v", n)
			}
		}
		if !disabled {
			if err := tt.Perform(automationTarget(t, s, "Password"), automation.Request{Action: automation.SetValue, Text: "changed-secret"}); err != nil {
				t.Fatal(err)
			}
			if password != "changed-secret" {
				t.Fatal("password edit failed")
			}
		}
		if err := tt.Perform(automationTarget(t, tt.Inspect(), "Read only"), automation.Request{Action: automation.SetValue, Text: "bad"}); !errors.Is(err, automation.ErrUnsupported) {
			t.Fatalf("read only write: %v", err)
		}
	}
}

func TestAutomationPointerAndModalBoundaries(t *testing.T) {
	presses := 0
	tt := NewTester(func(c *Context) {
		Button(c, "Covered").Absolute().Left(10).Top(10).Size(100, 40).OnClick(func() { presses++ })
		Button(c, "Cover").Absolute().Left(10).Top(10).Size(100, 40)
		Button(c, "Outside").Absolute().Left(1000).Size(100, 40)
	}, 300, 200)
	for _, name := range []string{"Covered", "Outside"} {
		if err := tt.Perform(automationTarget(t, tt.Inspect(), name), automation.Request{Action: automation.Click}); !errors.Is(err, automation.ErrCovered) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if err := tt.Perform(automationTarget(t, tt.Inspect(), "Covered"), automation.Request{Action: automation.Press}); err != nil {
		t.Fatal(err)
	}
	if presses != 1 {
		t.Fatal("semantic press did not activate covered control")
	}
	open := true
	modal := NewTester(func(c *Context) {
		Button(c, "Behind")
		Modal(c, &open, func() { Button(c, "OK").OnClick(func() { open = false }) })
	}, 400, 300)
	s := modal.Inspect()
	if _, err := s.Resolve(0, "Behind"); !errors.Is(err, automation.ErrMissing) {
		t.Fatalf("modal background: %v", err)
	}
	if err := modal.Perform(automationTarget(t, s, "OK"), automation.Request{Action: automation.Click}); err != nil {
		t.Fatal(err)
	}
	if open {
		t.Fatal("modal pointer click failed")
	}
	data, _ := json.Marshal(modal.Inspect())
	if !strings.Contains(string(data), "Behind") {
		t.Fatal("fresh snapshot did not restore background")
	}
}

func TestAutomationNestedPointerTarget(t *testing.T) {
	tt := NewTester(func(c *Context) {
		Box(c).Label("Outer").Focusable().OnClick(func() { t.Error("outer clicked") }).Size(100, 60).Children(func() {
			Button(c, "Inner").Fill()
		})
	}, 300, 200)
	if err := tt.Perform(automationTarget(t, tt.Inspect(), "Outer"), automation.Request{Action: automation.Click}); !errors.Is(err, automation.ErrCovered) {
		t.Fatalf("nested target: %v", err)
	}
}

func TestAutomationRangeTreeAndSelection(t *testing.T) {
	value, open := 5.0, false
	selected := -1
	list := ListState{Selected: &selected}
	tt := NewTester(func(c *Context) {
		Stepper(c, &value, 0, 10, 1).Label("Amount")
		Tree(c, func() { TreeItem(c, "Folder", &open, func() { TreeItem(c, "Child", nil, nil) }) })
		List(c, &list, 1000, func(i int) { Textf(c, "Row %d", i).Height(30) }).Grow(1)
	}, 300, 400)
	act := func(name string, action automation.Action) {
		t.Helper()
		if err := tt.Perform(automationTarget(t, tt.Inspect(), name), automation.Request{Action: action}); err != nil {
			t.Fatal(err)
		}
	}
	act("Amount", automation.Increment)
	if value != 6 {
		t.Fatalf("increment: %v", value)
	}
	act("Amount", automation.Decrement)
	if value != 5 {
		t.Fatalf("decrement: %v", value)
	}
	act("Folder", automation.Expand)
	if !open {
		t.Fatal("expand failed")
	}
	act("Folder", automation.Collapse)
	if open {
		t.Fatal("collapse failed")
	}
	// Rows and their text share a name, so use the semantic row's ID.
	s := tt.Inspect()
	var row automation.Node
	for _, n := range s.Nodes {
		if n.Role == "listItem" && n.PosInSet == 4 {
			row = n
		}
	}
	target, err := s.Resolve(row.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := tt.Perform(target, automation.Request{Action: automation.Focus}); err != nil {
		t.Fatal(err)
	}
	if selected != 3 {
		t.Fatalf("selection: %d", selected)
	}
	s = tt.Inspect()
	for _, n := range s.Nodes {
		if n.Role == "listItem" && n.PosInSet > row.PosInSet {
			row = n
		}
	}
	target, err = s.Resolve(row.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := tt.Perform(target, automation.Request{Action: automation.ScrollIntoView}); err != nil {
		t.Fatal(err)
	}
	for _, n := range tt.Inspect().Nodes {
		if n.ID == row.ID && n.Offscreen {
			t.Fatal("row remained offscreen")
		}
	}
}
