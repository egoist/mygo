//go:build darwin

package darwin

import (
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

// TestMenuBadgeTitle checks that a badge takes an attributed title with a
// right-aligned tab stop past the longest label, and that clearing the
// badge restores the plain title.
func TestMenuBadgeTitle(t *testing.T) {
	load()
	withPool(func() {
		longest := "A longer node name without a badge"
		tab := badgeTab(&platform.Menu{Items: []*platform.MenuItem{
			{Label: "Node", Badge: "120 ms", Visible: true},
			{Label: "A longer node name", Badge: "1.2s", Visible: true},
			{Label: longest, Visible: true},
		}})
		if tab <= textWidth(longest, menuFont()) {
			t.Fatalf("tab %v does not clear the longest label", tab)
		}

		item := autorelease(send(send(class("NSMenuItem"), "alloc"), "initWithTitle:action:keyEquivalent:",
			uintptr(nsString("Node")), 0, uintptr(nsString(""))))
		setItemTitle(item, &platform.MenuItem{Label: "Node", Badge: "120 ms", Visible: true}, tab)
		title := send(item, "attributedTitle")
		if title == 0 || goString(send(title, "string")) != "Node\t120 ms" {
			t.Fatalf("attributed title missing: %v", title)
		}
		style := send(title, "attribute:atIndex:effectiveRange:", uintptr(nsString("NSParagraphStyle")), 0, 0)
		if style == 0 {
			t.Fatal("paragraph style missing")
		}
		tabs := arrayItems(send(style, "tabStops"))
		if len(tabs) != 1 {
			t.Fatalf("tab stops = %d", len(tabs))
		}
		if got := msgFloat(tabs[0], sel("location")); got != tab {
			t.Errorf("tab stop at %v, want %v", got, tab)
		}

		setItemTitle(item, &platform.MenuItem{Label: "Node"}, 0)
		if got := goString(send(item, "title")); got != "Node" {
			t.Errorf("title after clearing = %q", got)
		}
		if at := send(item, "attributedTitle"); at != 0 && goString(send(at, "string")) != "Node" {
			t.Errorf("attributed title after clearing = %q", goString(send(at, "string")))
		}
	})
}
