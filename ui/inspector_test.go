package ui

import (
	"fmt"
	"io"
	"log"
	"os"
	"testing"
)

// inspected returns a tester of view whose window lets the inspector open,
// and the width the view had in its last frame.
func inspected(view func(c *Context)) (*Tester, *float32) {
	var width float32
	tt := NewTester(func(c *Context) {
		width, _ = c.Size()
		view(c)
	}, 1000, 600)
	tt.rt.insp.enabled = true
	return tt, &width
}

func TestInspectorOpensAndCloses(t *testing.T) {
	tt, width := inspected(func(c *Context) {
		Column(c).Padding(20).Children(func() { Text(c, "Hello") })
	})
	tt.Key(0, KeyF12)
	if !tt.rt.insp.open {
		t.Fatal("F12 did not open the inspector")
	}
	if *width != 1000-inspectorWidth {
		t.Errorf("the content is %v wide beside the inspector", *width)
	}
	if !tt.HasText("Elements") || !tt.HasText(`"Hello"`) {
		t.Errorf("the inspector shows %q", tt.Texts())
	}
	// The panel takes the pointer beside the content.
	if r, ok := tt.Find("Pick"); !ok || r.X < 1000-inspectorWidth {
		t.Errorf("the Pick button is at %v", r)
	}
	tt.Key(0, KeyF12)
	if tt.rt.insp.open || *width != 1000 || tt.HasText("Elements") {
		t.Error("F12 did not close the inspector")
	}
	// Alt+Cmd+I (Ctrl+Shift+I outside macOS) toggles it too.
	mods := Ctrl | Shift
	if Cmd == Super {
		mods = Super | Alt
	}
	tt.Key(mods, KeyI)
	if !tt.rt.insp.open {
		t.Error("the shortcut did not open the inspector")
	}
	tt.rt.toggleInspector()
	tt.Frame()

	// Without DevTools, the keys go to the view.
	tt.rt.insp.enabled = false
	tt.Key(0, KeyF12)
	if tt.rt.insp.open {
		t.Error("the inspector opened without DevTools")
	}
}

func TestInspectorFollowsTheContent(t *testing.T) {
	n := 0
	tt, _ := inspected(func(c *Context) {
		Column(c).Padding(20).Gap(8).Children(func() {
			Text(c, fmt.Sprintf("Count %d", n))
			if Button(c, "Add").Clicked() {
				n++
			}
		})
	})
	tt.rt.toggleInspector()
	tt.Frame()
	if !tt.HasText(`"Count 0"`) {
		t.Fatalf("the tree shows %q", tt.Texts())
	}
	if err := tt.Click("Add"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText(`"Count 1"`) || tt.HasText(`"Count 0"`) {
		t.Errorf("the tree did not follow the content: %q", tt.Texts())
	}
}

func TestInspectorPicksAndDescribes(t *testing.T) {
	clicks := 0
	tt, _ := inspected(func(c *Context) {
		Column(c).Padding(20).Children(func() {
			if Button(c, "Press").Width(120).Clicked() {
				clicks++
			}
		})
	})
	tt.rt.toggleInspector()
	tt.Frame()
	if !tt.HasText("Choose an element in the tree, or pick one in the window.") {
		t.Errorf("no hint before an element is chosen: %q", tt.Texts())
	}
	if err := tt.Click("Pick"); err != nil {
		t.Fatal(err)
	}
	if !tt.rt.insp.picking {
		t.Fatal("Pick does not pick")
	}
	r, _ := tt.Find("Press")
	x, y := r.X+r.W/2, r.Y+r.H/2
	before := tt.Image().RGBAAt(int(r.X)+1, int(y))
	tt.Move(x, y)
	if tt.rt.insp.hovered == 0 || tt.rt.insp.hoverElem == nil {
		t.Fatal("picking does not follow the pointer")
	}
	// The element under the pointer is tinted blue over the content.
	if after := tt.Image().RGBAAt(int(r.X)+1, int(y)); after == before || after.B <= after.R {
		t.Errorf("the element picked is not outlined: %v, then %v", before, after)
	}
	tt.ClickAt(x, y)
	if clicks != 0 {
		t.Error("the click that picked reached the button")
	}
	if tt.rt.insp.picking || tt.rt.insp.selected == 0 {
		t.Fatal("the click did not pick")
	}
	for _, s := range []string{"Element", "Box", "Size", "Font"} {
		if !tt.HasText(s) {
			t.Errorf("the details lack %s: %q", s, tt.Texts())
		}
	}
	tt.Click("Press")
	if clicks != 1 {
		t.Errorf("after picking, the button took %d clicks", clicks)
	}
}

func TestInspectorTreeChoosesAndCollapses(t *testing.T) {
	tt, _ := inspected(func(c *Context) {
		Column(c).Padding(20).Children(func() {
			Row(c).Children(func() { Text(c, "Inside") })
		})
	})
	tt.rt.toggleInspector()
	tt.Frame()
	if err := tt.Click(`"Inside"`); err != nil {
		t.Fatal(err)
	}
	sel := tt.rt.insp.selected
	if s := tt.rt.states[sel]; s == nil || !tt.HasText("Font") {
		t.Fatalf("choosing the text in the tree shows %q", tt.Texts())
	}
	// Hovering a row of the tree outlines its element over the content.
	r, _ := tt.Find(`"Inside"`)
	tt.Move(r.X+2, r.Y+2)
	if tt.rt.insp.hoverElem == nil || tt.rt.insp.hoverElem.id != sel {
		t.Error("hovering the tree does not outline the element")
	}
	// Collapsing the row hides the text.
	var row inspNode
	for _, n := range tt.rt.insp.nodes {
		if n.name == "Row" {
			row = n
		}
	}
	tt.rt.insp.collapsed[row.id] = true
	tt.rt.insp.changed = true
	tt.Frame()
	if tt.HasText(`"Inside"`) {
		t.Error("the collapsed row still shows the text")
	}
}

func TestInspectorListsWarnings(t *testing.T) {
	strictKeys = false
	defer func() { strictKeys = true }()
	log.SetOutput(io.Discard)
	defer log.SetOutput(os.Stderr)
	tt, _ := inspected(func(c *Context) {
		Row(c).Key("twice")
		Row(c).Key("twice")
	})
	tt.rt.toggleInspector()
	tt.Frame()
	if !tt.HasText("Warnings (1)") {
		t.Errorf("the inspector does not list the warning: %q", tt.Texts())
	}
}
