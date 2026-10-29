//go:build windows && (amd64 || arm64)

package windows

import "testing"

// The highlights of menu items cover their pixels inside fully, those past
// the rounded corners not at all, and those the corners cross partly.
func TestRoundedCoverage(t *testing.T) {
	r := &rect{0, 0, 40, 20}
	for _, c := range []struct {
		x, y     float64
		min, max float64
	}{
		{20.5, 10.5, 1, 1}, // the middle
		{0.5, 10.5, 1, 1},  // the left edge, between the corners
		{20.5, 0.5, 1, 1},  // the top edge
		{0.5, 0.5, 0, 0},   // past the top-left corner's curve
		{39.5, 19.5, 0, 0}, // past the bottom-right one's
		{1.5, 1.5, 0.01, 0.99},
	} {
		if got := roundedCoverage(c.x, c.y, r, 4); got < c.min || got > c.max {
			t.Errorf("coverage at (%v, %v) = %v, want %v to %v", c.x, c.y, got, c.min, c.max)
		}
	}
}

func TestMenuColors(t *testing.T) {
	if got := dibColor(0x332211); got != 0x112233 {
		t.Errorf("dibColor(0x332211) = %#x, want 0x112233", got)
	}
	if got := blend(0xFFFFFF, 0x000000, 0.5); got != 0x808080 {
		t.Errorf("blend halfway = %#x, want 0x808080", got)
	}
	if got := blend(0x102030, 0xA0B0C0, 0); got != 0xA0B0C0 {
		t.Errorf("blend with none = %#x, want the background", got)
	}
}

// An item is measured for its label and shortcut, its padding, the columns
// of marks and icons and the chevron, less what Windows adds itself.
func TestMenuItemWidth(t *testing.T) {
	for _, c := range []struct {
		l                  menuLook
		dpi                int
		text, accel, added int32
		want               int32
	}{
		{menuLook{}, 96, 100, 0, 14, 32 + 100 - 14},
		{menuLook{column: true}, 96, 100, 0, 14, 32 + 100 + 28 - 14},
		{menuLook{icons: true}, 96, 100, 0, 14, 32 + 100 + 28 - 14},
		{menuLook{accel: "Ctrl+S"}, 96, 100, 40, 14, 32 + 100 + 32 + 40 - 14},
		{menuLook{submenu: true}, 96, 100, 0, 14, 32 + 100 + 28 - 14},
		{menuLook{}, 192, 100, 0, 29, 64 + 100 - 29},
		{menuLook{}, 96, 0, 0, 100, 0}, // never negative
	} {
		if got := c.l.width(c.dpi, c.text, c.accel, c.added); got != c.want {
			t.Errorf("width(%+v, dpi %d, text %d, accel %d, added %d) = %d, want %d", c.l, c.dpi, c.text, c.accel, c.added, got, c.want)
		}
	}
}

// The label of an item has room for its text as measured, whatever Windows
// adds to the width it is asked for, as long as it adds no fewer pixels than
// the item's padding at the label's right is wide: the longest label of a
// menu sets the menu's width, with no room to spare, so a few pixels less
// than MyGo assumes would clip it.
func TestMenuLabelRoom(t *testing.T) {
	for _, dpi := range []int{96, 120, 144, 168, 192, 240, 288} {
		// What the padding at the label's right allows, with the rounding
		// of its sides.
		slack := toPx(menuInset+menuPadding, dpi) - toPx(menuInset, dpi)
		check := toPx(15, dpi) // SM_CXMENUCHECK, about
		added := check - 1
		for _, l := range []menuLook{{}, {column: true}, {icons: true}, {accel: "Ctrl+S"}, {submenu: true}, {column: true, icons: true, accel: "Ctrl+S", submenu: true}} {
			for _, text := range []int32{1, 87, 213, 640} {
				x := toPx(menuInset+menuPadding, dpi)
				if l.column {
					x += toPx(menuMarkColumn, dpi)
				}
				if l.icons {
					x += toPx(menuIconColumn, dpi)
				}
				width := l.width(dpi, text, 61, added)
				for extra := -slack; extra <= 6; extra++ {
					r := rect{0, 0, width + added + extra, toPx(menuItemHeight, dpi)}
					if room := labelEdge(r, dpi) - x; room < text {
						t.Errorf("dpi %d, %+v, text %d: Windows adds %d more than assumed, the label has room for %d", dpi, l, text, extra, room)
					}
				}
				// What Windows adds is what MyGo assumed: the room is no less than before.
				r := rect{0, 0, width + added, toPx(menuItemHeight, dpi)}
				if old := r.Right - toPx(menuInset+menuPadding, dpi) - x; labelEdge(r, dpi)-x < old || old < text {
					t.Errorf("dpi %d, %+v, text %d: room %d, before %d", dpi, l, text, labelEdge(r, dpi)-x, old)
				}
			}
		}
	}
}
