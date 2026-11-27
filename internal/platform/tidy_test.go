package platform

import "testing"

// TestTidySeparators checks the separators a menu shown drops: the ones
// that would start or end it, or follow another, as the items removed
// between them leave.
func TestTidySeparators(t *testing.T) {
	build := func(items ...*MenuItem) *Menu { return &Menu{Items: items} }
	item := func(label string) *MenuItem { return &MenuItem{Type: MenuItemNormal, Label: label, Visible: true} }
	sep := func() *MenuItem { return &MenuItem{Type: MenuItemSeparator, Visible: true} }
	hidden := func(label string) *MenuItem { return &MenuItem{Type: MenuItemNormal, Label: label} }
	labels := func(m *Menu) []string {
		var out []string
		for _, it := range m.Items {
			out = append(out, it.Label)
		}
		return out
	}
	want := func(m *Menu, want ...string) {
		t.Helper()
		got := labels(m)
		if len(got) != len(want) {
			t.Fatalf("menu = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("menu = %v, want %v", got, want)
			}
		}
	}

	// One that would start the menu, one that would end it, and one that
	// follows another go.
	m := build(sep(), item("a"), sep(), sep(), item("b"), sep())
	m.TidySeparators()
	want(m, "a", "", "b")

	// A separator the items removed before leave still starts the menu:
	// Remove(back, forward, reload) leaves the one after them first.
	m = build(sep(), hidden("back"), hidden("forward"), hidden("reload"), item("a"))
	m.TidySeparators()
	want(m, "back", "forward", "reload", "a")
	// One the items removed after leave ends it.
	m = build(item("a"), hidden("quit"), sep())
	m.TidySeparators()
	want(m, "a", "quit")
	// Items nobody can see are not what a separator follows.
	m = build(item("a"), hidden("gone"), sep(), item("b"))
	m.TidySeparators()
	want(m, "a", "gone", "", "b")

	// A submenu is tidied as its own menu.
	sub := build(sep(), item("in"), sep())
	m = build(item("a"), &MenuItem{Type: MenuItemSubmenu, Visible: true, Submenu: sub})
	m.TidySeparators()
	want(sub, "in")
	want(m, "a", "")

	// An empty menu stays empty.
	m = build()
	m.TidySeparators()
	want(m)
}
