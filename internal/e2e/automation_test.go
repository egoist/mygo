//go:build !js

package e2e

import (
	"errors"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/automation"
	"github.com/egoist/mygo/ui/editor"
)

func inspectAutomation(t *testing.T, driver *mygo.Automation) automation.Snapshot {
	t.Helper()
	s, err := driver.Inspect()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func waitForAutomation(t *testing.T, driver *mygo.Automation, name string) automation.Snapshot {
	t.Helper()
	var s automation.Snapshot
	eventually(t, name, func() bool {
		s = inspectAutomation(t, driver)
		_, err := s.Resolve(0, name)
		if err != nil && !errors.Is(err, automation.ErrMissing) {
			t.Fatal(err)
		}
		return err == nil
	})
	return s
}

func automationTarget(t *testing.T, s automation.Snapshot, name string) automation.Target {
	t.Helper()
	target, err := s.Resolve(0, name)
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func automationNode(t *testing.T, s automation.Snapshot, id uint64) automation.Node {
	t.Helper()
	for _, n := range s.Nodes {
		if n.ID == id {
			return n
		}
	}
	t.Fatalf("automation node %d missing", id)
	return automation.Node{}
}

func TestContentWindowAutomation(t *testing.T) {
	on := false
	w := newWindow(t, mygo.WindowOptions{Title: "Automation", Width: 320, Height: 240,
		Content: ui.View(func(c *ui.Context) { ui.Checkbox(c, &on, "Agree") }).Automatable()})
	driver := mygo.Automate(w)
	var snapshot automation.Snapshot
	eventually(t, "the semantic tree", func() bool {
		var err error
		snapshot, err = driver.Inspect()
		if err != nil {
			t.Fatal(err)
		}
		_, err = snapshot.Resolve(0, "Agree")
		return err == nil
	})
	for _, action := range []automation.Action{automation.Press, automation.Click} {
		target, err := snapshot.Resolve(0, "Agree")
		if err != nil {
			t.Fatal(err)
		}
		if err := driver.Perform(target, automation.Request{Action: action}); err != nil {
			t.Fatal(err)
		}
		snapshot, err = driver.Inspect()
		if err != nil {
			t.Fatal(err)
		}
		for _, node := range snapshot.Nodes {
			if node.Name == "Agree" && node.Checked != (action == automation.Press) {
				t.Fatalf("state after %s: %+v", action, node)
			}
		}
	}
	w.Destroy()
	if _, err := driver.Inspect(); !errors.Is(err, automation.ErrClosed) {
		t.Fatalf("closed: %v", err)
	}
}

func TestContentWindowAutomationInput(t *testing.T) {
	value := "before"
	w := newWindow(t, mygo.WindowOptions{Width: 320, Height: 240,
		Content: ui.View(func(c *ui.Context) { ui.TextInput(c, &value).Label("Name") }).Automatable()})
	driver := mygo.Automate(w)
	var target automation.Target
	eventually(t, "the text field", func() bool {
		s, err := driver.Inspect()
		if err != nil {
			t.Fatal(err)
		}
		target, err = s.Resolve(0, "Name")
		return err == nil
	})
	if err := driver.Perform(target, automation.Request{Action: automation.Focus}); err != nil {
		t.Fatal(err)
	}
	for _, input := range []automation.Input{
		{Kind: automation.KeyInput, Key: "CmdOrCtrl+A"},
		{Kind: automation.TextInput, Text: "after 🌍"},
	} {
		if err := driver.Input(input); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := driver.Inspect()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := snapshot.Resolve(target.ID, ""); err != nil {
		t.Fatal(err)
	}
	for _, node := range snapshot.Nodes {
		if node.ID == target.ID && node.Value != "after 🌍" {
			t.Fatalf("text input: %q", node.Value)
		}
	}
}

func TestContentWindowAutomationCoveredClick(t *testing.T) {
	clicks, coveredClicks := 0, 0
	w := newWindow(t, mygo.WindowOptions{Width: 360, Height: 240,
		Content: ui.View(func(c *ui.Context) {
			ui.Button(c, "Visible").OnClick(func() { clicks++ })
			ui.Box(c).Size(120, 40).Children(func() {
				ui.Button(c, "Under").Absolute().Size(120, 40).OnClick(func() { coveredClicks++ })
				ui.Button(c, "Cover").Absolute().Size(120, 40)
			})
		}).Automatable()})
	driver := mygo.Automate(w)
	s := waitForAutomation(t, driver, "Under")
	if err := driver.Perform(automationTarget(t, s, "Visible"), automation.Request{Action: automation.Click}); err != nil {
		t.Fatal(err)
	}
	if err := driver.Perform(automationTarget(t, s, "Under"), automation.Request{Action: automation.Click}); !errors.Is(err, automation.ErrCovered) {
		t.Fatalf("covered click: %v", err)
	}
	if clicks != 1 || coveredClicks != 0 {
		t.Fatalf("clicks=%d, coveredClicks=%d", clicks, coveredClicks)
	}
}

func TestContentWindowAutomationEditorAndList(t *testing.T) {
	code := &editor.State{Buffer: editor.NewBuffer("before")}
	selected := -1
	list := ui.ListState{Selected: &selected}
	w := newWindow(t, mygo.WindowOptions{Width: 420, Height: 360,
		Content: ui.View(func(c *ui.Context) {
			editor.View(c, code).Height(80).Label("Code")
			ui.List(c, &list, 5000, func(i int) { ui.Textf(c, "Row %d", i).Height(24) }).Grow(1).Label("Rows")
		}).Automatable()})
	driver := mygo.Automate(w)
	s := waitForAutomation(t, driver, "Code")
	codeTarget := automationTarget(t, s, "Code")
	if err := driver.Perform(codeTarget, automation.Request{Action: automation.Focus}); err != nil {
		t.Fatal(err)
	}
	for _, input := range []automation.Input{
		{Kind: automation.KeyInput, Key: "CmdOrCtrl+A"},
		{Kind: automation.TextInput, Text: "first 🌍"},
		{Kind: automation.KeyInput, Key: "Enter"},
		{Kind: automation.TextInput, Text: "second"},
	} {
		if err := driver.Input(input); err != nil {
			t.Fatalf("%+v: %v", input, err)
		}
	}
	s = inspectAutomation(t, driver)
	if got := automationNode(t, s, codeTarget.ID).Value; got != "first 🌍\nsecond" {
		t.Fatalf("code editor value: %q", got)
	}
	if _, err := s.Resolve(0, "Row 3000"); !errors.Is(err, automation.ErrMissing) {
		t.Fatalf("distant row already built: %v", err)
	}
	rows := automationNode(t, s, automationTarget(t, s, "Rows").ID)
	if err := driver.Input(automation.Input{Kind: automation.ScrollInput,
		X: rows.Bounds.X + rows.Bounds.W/2, Y: rows.Bounds.Y + rows.Bounds.H/2, DY: 3000 * 24}); err != nil {
		t.Fatal(err)
	}
	s = inspectAutomation(t, driver)
	var row automation.Node
	for _, n := range s.Nodes {
		if n.Role == "listItem" && n.PosInSet == 3001 {
			row = n
		}
	}
	if row.ID == 0 || row.Offscreen || row.Selected {
		t.Fatalf("distant row before click: %+v", row)
	}
	target, err := s.Resolve(row.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := driver.Perform(target, automation.Request{Action: automation.Click}); err != nil {
		t.Fatal(err)
	}
	row = automationNode(t, inspectAutomation(t, driver), row.ID)
	if !row.Selected || !row.Checked || selected != 3000 {
		t.Fatalf("selected row=%d, node=%+v", selected, row)
	}
}

func TestContentWindowAutomationTargets(t *testing.T) {
	show, presses := true, 0
	content := func() *ui.Content {
		return ui.View(func(c *ui.Context) {
			ui.Button(c, "Toggle").OnClick(func() { show = !show })
			if show {
				ui.Button(c, "Ephemeral").Key("ephemeral").OnClick(func() { presses++ })
			}
			ui.Box(c).Size(10, 10).Draw(func(p *ui.Painter, _ ui.Rect) { p.AnimationFrame() })
		}).Automatable()
	}
	w := newWindow(t, mygo.WindowOptions{Title: "Automation targets", Width: 320, Height: 240, Content: content()})
	driver := mygo.Automate(w)
	s := waitForAutomation(t, driver, "Ephemeral")
	target := automationTarget(t, s, "Ephemeral")
	eventually(t, "an animation repaint", func() bool { return inspectAutomation(t, driver).Revision > s.Revision })
	for _, action := range []automation.Action{automation.Press, automation.Click} {
		if err := driver.Perform(target, automation.Request{Action: action}); err != nil {
			t.Fatalf("%s after repaint: %v", action, err)
		}
	}
	if presses != 2 {
		t.Fatalf("presses after repaint: %d", presses)
	}
	toggle := automationTarget(t, s, "Toggle")
	if err := driver.Perform(toggle, automation.Request{Action: automation.Press}); err != nil {
		t.Fatal(err)
	}
	if err := driver.Perform(target, automation.Request{Action: automation.Press}); !errors.Is(err, automation.ErrMissing) {
		t.Fatalf("removed target: %v", err)
	}
	if err := driver.Perform(toggle, automation.Request{Action: automation.Press}); err != nil {
		t.Fatal(err)
	}
	again := automationTarget(t, inspectAutomation(t, driver), "Ephemeral")
	if again.ID != target.ID || again.Generation == target.Generation {
		t.Fatalf("recreated target: old=%+v, new=%+v", target, again)
	}
	if err := driver.Perform(target, automation.Request{Action: automation.Press}); !errors.Is(err, automation.ErrStale) {
		t.Fatalf("recreated target accepted old generation: %v", err)
	}
	if err := driver.Perform(again, automation.Request{Action: automation.Press}); err != nil {
		t.Fatal(err)
	}
	other := newWindow(t, mygo.WindowOptions{Width: 320, Height: 240, Content: content()})
	otherDriver := mygo.Automate(other)
	otherTarget := automationTarget(t, waitForAutomation(t, otherDriver, "Ephemeral"), "Ephemeral")
	if err := otherDriver.Perform(again, automation.Request{Action: automation.Press}); !errors.Is(err, automation.ErrStale) {
		t.Fatalf("another window's target: %v", err)
	}
	if err := driver.Perform(otherTarget, automation.Request{Action: automation.Press}); !errors.Is(err, automation.ErrStale) {
		t.Fatalf("other window's target on original: %v", err)
	}
	w.Destroy()
	if err := driver.Perform(again, automation.Request{Action: automation.Press}); !errors.Is(err, automation.ErrClosed) {
		t.Fatalf("closed window perform: %v", err)
	}
	if err := driver.Input(automation.Input{Kind: automation.TextInput, Text: "x"}); !errors.Is(err, automation.ErrClosed) {
		t.Fatalf("closed window input: %v", err)
	}
	rebuilt := newWindow(t, mygo.WindowOptions{Title: "Automation targets", Width: 320, Height: 240, Content: content()})
	rebuiltDriver := mygo.Automate(rebuilt)
	waitForAutomation(t, rebuiltDriver, "Ephemeral")
	if err := rebuiltDriver.Perform(again, automation.Request{Action: automation.Press}); !errors.Is(err, automation.ErrStale) {
		t.Fatalf("old window's target on rebuilt window: %v", err)
	}
	if presses != 3 {
		t.Fatalf("a rejected target invoked its handler: presses=%d", presses)
	}
}
