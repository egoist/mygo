package ui

import (
	"runtime"
	"testing"
	"weak"
)

func TestValueHandleExpiresBeforeStorageReuse(t *testing.T) {
	var old Element
	var oldFrame Frame
	first := true
	children := 0
	tt := NewTester(func(f Frame) {
		current := Button(f, "Current").Key("current")
		if first {
			old, oldFrame = current, f
			first = false
			return
		}
		if old.Valid() || old.Focused() || old.FocusWithin() || old.Shortcut(Cmd, KeyK) || oldFrame.Valid() {
			t.Fatal("old handles became valid in another build")
		}
		old.Background(RGB(255, 0, 0)).Focus().Children(func(Frame) { children++ })
		if oldFrame.Root().Valid() {
			t.Fatal("an old frame resolved the new root")
		}
		if !current.Valid() {
			t.Fatal("the current handle expired")
		}
	}, 200, 100)
	tt.Frame()
	if children != 0 || tt.rt.focused != 0 {
		t.Fatal("an expired handle performed an action")
	}
}

func TestScopedFrameValue(t *testing.T) {
	var outside, inside Element
	tt := NewTester(func(f Frame) {
		Column(f).Padding(20).Children(func(child Frame) {
			inside = Text(child, "Inside")
			outside = Text(f, "Outside")
		})
	}, 200, 150)
	nIn, nOut := tt.rt.nodeAt(inside.slot), tt.rt.nodeAt(outside.slot)
	if nIn.parent == nOut.parent || nOut.parent != tt.rt.c.root {
		t.Fatal("frame values did not preserve their parent scopes")
	}
}

func TestActionsRunAfterBuildOnce(t *testing.T) {
	building := false
	clicks := 0
	tt := NewTester(func(f Frame) {
		building = true
		Button(f, "Increment").Key("increment").OnClick(func() {
			if building {
				t.Fatal("handler ran while the view was being built")
			}
			clicks++
		})
		Textf(f, "Count %d", clicks)
		building = false
	}, 200, 100)
	if err := tt.Click("Increment"); err != nil {
		t.Fatal(err)
	}
	if clicks != 1 || !tt.HasText("Count 1") {
		t.Fatalf("clicks %d, text %q", clicks, tt.Texts())
	}
	tt.Frame()
	if clicks != 1 {
		t.Fatal("rebuilding repeated the action")
	}
}

func TestDeferredControlIdentityAndConfiguration(t *testing.T) {
	checked, disabled := false, true
	changes := 0
	tt := NewTester(func(f Frame) {
		Checkbox(f, &checked, "Check").Key("check").Disabled(disabled).OnChange(func() { changes++ })
	}, 200, 100)
	tt.Click("Check")
	if checked || changes != 0 {
		t.Fatal("disabled control handled input")
	}
	disabled = false
	tt.Frame()
	tt.Click("Check")
	if !checked || changes != 1 {
		t.Fatalf("checked %v, changes %d", checked, changes)
	}
	tt.Frame()
	if !checked || changes != 1 {
		t.Fatal("keyed state did not survive a build")
	}
}

func TestReferenceFocusWaitsForControl(t *testing.T) {
	var ref Ref
	show := false
	ref.RequestFocus()
	tt := NewTester(func(f Frame) {
		if show {
			Button(f, "Target").Key("target").Ref(&ref)
		}
	}, 200, 100)
	if !ref.state.requested || tt.rt.focused != 0 {
		t.Fatal("an absent control took focus")
	}
	show = true
	tt.Frame()
	if ref.state.requested || !tt.Focused("Target") {
		t.Fatal("the returning control did not receive focus")
	}
	show = false
	tt.Frame()
	if tt.Focused("Target") {
		t.Fatal("a hidden control kept focus")
	}
	ref.RequestFocus()
	tt.rt.close()
	if ref.state.requested || !ref.state.closed {
		t.Fatal("closing a window retained its focus request")
	}
}

func TestDeferredInputOptions(t *testing.T) {
	var ref Ref
	text := "original"
	readOnly := true
	ref.RequestFocus()
	tt := NewTester(func(f Frame) {
		TextInput(f, &text).Key("input").ReadOnly(readOnly).Placeholder("Type").Ref(&ref).Label("Input")
	}, 200, 100)
	tt.Type("x")
	if text != "original" {
		t.Fatal("read-only configuration was applied after editing")
	}
	readOnly = false
	tt.Frame()
	tt.Type("y")
	if text == "original" {
		t.Fatal("the keyed input did not retain its editable state")
	}
}

func frameOwnerReference() (Element, Frame, weak.Pointer[engine]) {
	var element Element
	var frame Frame
	tt := NewTester(func(f Frame) { frame = f; element = Text(f, "Temporary") }, 100, 50)
	return element, frame, weak.Make(tt.rt)
}

func TestValueHandlesDoNotRetainWindow(t *testing.T) {
	e, f, owner := frameOwnerReference()
	runtime.GC()
	if owner.Value() != nil {
		t.Fatal("a saved handle retained its window")
	}
	if e.Valid() || f.Valid() {
		t.Fatal("a released window had live handles")
	}
	runtime.KeepAlive(e)
	runtime.KeepAlive(f)
}

func TestCustomPartsUseCheckedValuesAndKeys(t *testing.T) {
	choice := "one"
	var saved SelectParts[string]
	first := true
	tt := NewTester(func(f Frame) {
		if !first && saved.Trigger.Valid() {
			t.Fatal("old custom parts became live again")
		}
		parts := SelectBase(f, &choice)
		parts.Trigger.Key("choice").Label("Choice").Padding(6).Children(func(f Frame) { Text(f, choice) })
		parts.Popup(func(f Frame, panel Element) {
			panel.Padding(4).Background(f.Theme().Background)
			for _, value := range []string{"one", "two"} {
				parts.Item(value).Padding(6).Children(func(f Frame) { Text(f, value) })
			}
		})
		saved = parts
		first = false
	}, 200, 150)
	if err := tt.Click("Choice"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("two") {
		t.Fatal("custom select did not build its popup")
	}
	if err := tt.Click("two"); err != nil {
		t.Fatal(err)
	}
	if choice != "two" {
		t.Fatalf("choice %q", choice)
	}
}

func TestPublicRowScopeAndKeyboardActions(t *testing.T) {
	var state ListState
	var selected Selection[string]
	var ref Ref
	keys := 0
	ref.RequestFocus()
	tt := NewTester(func(f Frame) {
		List(f, &state, 3).Key("items").Ref(&ref).Grow(1).
			ItemKey(func(i int) any { return []string{"a", "b", "c"}[i] }).
			Selection(&selected).
			OnShortcut(Cmd, KeyK, func() { keys++ }).
			Rows(func(row ListRow) { Textf(row.Frame, "Row %d", row.Index).Height(24) })
	}, 200, 150)
	tt.Key(Cmd, KeyK)
	if keys != 1 {
		t.Fatalf("list shortcut ran %d times", keys)
	}
	if err := tt.Click("Row 1"); err != nil {
		t.Fatal(err)
	}
	if !selected.Has("b") {
		t.Fatal("typed item selection did not use its key")
	}
}

func TestWindowServicesOutliveBuild(t *testing.T) {
	var services Services
	tt := NewTester(func(f Frame) { services = f.Services(); Text(f, "Window") }, 100, 50)
	services.WriteClipboard("value")
	if services.ReadClipboard() != "value" {
		t.Fatal("services expired with the frame")
	}
	services.Invalidate()
	if !tt.h.requested.Load() {
		t.Fatal("services did not request a frame")
	}
	tt.rt.close()
	if services.ReadClipboard() != "" {
		t.Fatal("services reached a closed window")
	}
}
