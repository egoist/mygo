//go:build darwin

package darwin

import (
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

// TestMenuKeepOpenView checks the view MyGo draws for an item that keeps
// its menu open: its label, badge, check mark and the update path.
func TestMenuKeepOpenView(t *testing.T) {
	load()
	registerMenuTarget()
	withPool(func() {
		b := &Backend{}
		item := &platform.MenuItem{
			ID: 42, Label: "Node", Badge: "45 ms", KeepOpen: true,
			Visible: true, Enabled: true, Type: platform.MenuItemCheckbox, Checked: true,
		}
		m := &platform.Menu{Items: []*platform.MenuItem{
			item,
			{Label: "Other", Visible: true, Enabled: true},
		}}
		menu := b.buildMenu(m, "", 1)
		defer dropOwner(1)

		native := send(menu, "itemAtIndex:", 0)
		view := send(native, "view")
		if view == 0 {
			t.Fatal("KeepOpen item has no view")
		}
		if got := goString(send(view, "identifier")); got != "42" {
			t.Errorf("view identifier = %q", got)
		}
		check := send(view, "viewWithTag:", menuViewCheck)
		if check == 0 || sendBool(check, "isHidden") {
			t.Fatal("checked item has no check mark")
		}
		label := send(view, "viewWithTag:", menuViewLabel)
		if label == 0 {
			t.Fatal("KeepOpen view has no label")
		}
		if got := goString(send(label, "stringValue")); got != "Node\t45 ms" {
			t.Errorf("label = %q", got)
		}
		backdrop := menuBackdrop(view)
		if backdrop == 0 || !sendBool(backdrop, "isHidden") {
			t.Fatal("KeepOpen view should start without a highlight")
		}
		setMenuViewHighlighted(view, true)
		if sendBool(backdrop, "isHidden") {
			t.Error("highlight not shown")
		}
		setMenuViewHighlighted(view, false)
		if !sendBool(backdrop, "isHidden") {
			t.Error("highlight not hidden")
		}

		item.Label, item.Badge, item.Checked, item.Enabled = "Node 2", "", false, false
		b.UpdateMenuItem(item)
		if !sendBool(check, "isHidden") {
			t.Error("unchecked item still shows the check mark")
		}
		if got := goString(send(label, "stringValue")); got != "Node 2" {
			t.Errorf("label after update = %q", got)
		}
		if got := msgFloat(view, sel("alphaValue")); got != 0.45 {
			t.Errorf("disabled view alpha = %v", got)
		}
	})
}
