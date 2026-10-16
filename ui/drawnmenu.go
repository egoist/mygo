package ui

import (
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/internal/platform"
)

// MenuEntryKind is what a MenuEntry is.
type MenuEntryKind int

// The kinds of menu entries.
const (
	MenuEntryItem MenuEntryKind = iota
	MenuEntrySeparator
	MenuEntryCheckbox
	MenuEntryRadio
	MenuEntrySubmenu
)

// MenuEntry is an item of a menu the app draws itself (DrawnMenu).
type MenuEntry struct {
	Kind MenuEntryKind
	// ID chooses the item with ShownMenu.Choose; it means nothing else.
	ID      int
	Label   string
	ToolTip string
	// Keys is the item's shortcut, written as "Ctrl+Shift+T", "" for none.
	Keys     string
	Disabled bool
	// Checked is set for a checkbox or radio item that shows its check.
	Checked bool
	// Submenu holds the items of a MenuEntrySubmenu.
	Submenu []MenuEntry
}

// ShownMenu is a popup menu of the window, a context menu or a menu
// button's, handed to the app to draw (DrawnMenu).
type ShownMenu struct {
	Items []MenuEntry
	// Anchor is what the menu opens from, in DIPs of the window: a menu
	// button's box, below which it opens, or the point a context menu
	// opens at, a box without size.
	Anchor Rect
	// RightToLeft lays the menu out from the right, opening to the left
	// of the anchor.
	RightToLeft bool
	rt          *engine
	p           *shownMenu
}

// DrawnMenu has the window hand its popup menus to the app, rather than
// show the system's, in each frame whose view calls it: it returns the
// menu open, to draw until Choose or Close. Every popup menu comes so:
// those of ContextMenu and Element.Menu, a table's, and the editing menus
// of text inputs and editors; the menu bar's and the tray's stay the
// system's. The app's menu takes the keys and the focus as the system's
// would, and gives the focus back as it closes.
//
//	if m, ok := ui.DrawnMenu(c); ok {
//		myMenu(c, m) // calls m.Choose(id) for the item chosen, m.Close() when dismissed
//	}
func DrawnMenu(c *Context) (*ShownMenu, bool) {
	rt := c.rt
	rt.drawsMenus, handOverMenu = true, (*engine).drawMenu
	return rt.drawn, rt.drawn != nil
}

// handOverMenu is drawMenu once an app draws its menus: set by DrawnMenu
// alone, it keeps what hands menus over out of the apps that do not.
var handOverMenu func(rt *engine, p *shownMenu)

// Choose chooses the item id and closes the menu: as for the system's
// menu, the function that built the menu reports it, or the text input
// takes the editing command.
func (m *ShownMenu) Choose(id int) {
	if m.rt.drawn != m {
		return
	}
	m.rt.drawn = nil
	m.rt.menuChosen(m.p, id)
	m.rt.menu.chosenLate = m.rt.inFrame
	m.rt.soon()
}

// Close closes the menu without choosing an item.
func (m *ShownMenu) Close() {
	if m.rt.drawn == m {
		m.rt.drawn = nil
		m.rt.soon()
	}
}

// soon shows a change at once: in the frame being built, built again, or
// else in a frame asked for.
func (rt *engine) soon() {
	if rt.inFrame {
		rt.consumed = true
		return
	}
	rt.requestFrame()
}

// drawMenu hands p to the app to draw, in place of the system's menu. It
// comes as the frame ends, which a frame asked for then draws.
func (rt *engine) drawMenu(p *shownMenu) {
	anchor := Rect{X: p.x, Y: p.y}
	if s := rt.states[p.id]; p.button && s != nil {
		anchor = Rect{X: s.vx, Y: s.vy, W: s.vw, H: s.vh}
	}
	rt.drawn = &ShownMenu{Items: menuEntries(p.menu), Anchor: anchor, RightToLeft: p.menu.RightToLeft, rt: rt, p: p}
	rt.changed()
}

// menuEntries are a menu's items as an app draws them.
func menuEntries(m *platform.Menu) []MenuEntry {
	out := make([]MenuEntry, 0, len(m.Items))
	for _, it := range m.Items {
		if !it.Visible && it.Type != platform.MenuItemSeparator {
			continue
		}
		e := MenuEntry{Kind: entryKinds[it.Type], ID: it.ID, Label: it.Label, ToolTip: it.ToolTip,
			Keys: acceleratorLabel(it.Accelerator), Disabled: !it.Enabled, Checked: it.Checked}
		if it.Type == platform.MenuItemSubmenu && it.Submenu != nil {
			e.Submenu = menuEntries(it.Submenu)
		}
		out = append(out, e)
	}
	return out
}

var entryKinds = map[platform.MenuItemType]MenuEntryKind{
	platform.MenuItemNormal: MenuEntryItem, platform.MenuItemSeparator: MenuEntrySeparator,
	platform.MenuItemCheckbox: MenuEntryCheckbox, platform.MenuItemRadio: MenuEntryRadio,
	platform.MenuItemSubmenu: MenuEntrySubmenu,
}

// acceleratorLabel writes an accelerator as a menu shows it: a letter
// key in capitals, as "Ctrl+Shift+T".
func acceleratorLabel(acc string) string {
	i := strings.LastIndexByte(acc, '+') + 1
	if key := acc[i:]; utf8.RuneCountInString(key) == 1 {
		return acc[:i] + strings.ToUpper(key)
	}
	return acc
}
